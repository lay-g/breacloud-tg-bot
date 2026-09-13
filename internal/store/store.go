// Package store 是持久层：SQLite 的 schema、查询与业务读写方法。
//
// 数据库只存可重建的状态与设置，不存凭据。删掉 bot.db 的后果是白名单退回配置
// 文件里的 owner_chat_id、设置回到默认值、日用量缓存重新抓取；没有不可恢复的损失。
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/store/sqlcgen"
	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，驱动名 sqlite
)

//go:embed schema.sql
var schemaSQL string

// Defaults 与 schema.sql 里的 DEFAULT 保持一致，用于修复脏数据。
const (
	defaultReportTime = "09:00"
	defaultThresholds = "80,100"
	defaultExpiryDays = 3
)

// DriverName 是 modernc.org/sqlite 注册的驱动名。
const DriverName = "sqlite"

// Settings 是全局单行设置。TrafficThresholds 在存储层是逗号分隔字符串，对外是切片。
type Settings struct {
	ReportEnabled       bool
	ReportTime          string // 主机本地时区的 HH:MM
	TrafficAlertEnabled bool
	TrafficThresholds   []int // 百分比，升序去重
	ExpiryAlertEnabled  bool
	ExpiryDays          int
	UpdatedAt           time.Time
}

// DailyUsage 是某台服务在某一天（UTC+8 自然日）的用量。
type DailyUsage struct {
	ServiceID int64
	Day       string
	InBytes   int64
	OutBytes  int64
	FetchedAt time.Time
}

// Store 封装数据库连接与查询。
type Store struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// Open 打开数据库、应用 schema 并保证设置行存在。可重复调用。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("数据库路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("创建数据库目录: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s: %w", path, err)
	}
	// SQLite 只有单写者，连接池开大反而会拿到 database is locked。
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("应用 schema: %w", err)
	}
	s := &Store{db: db, q: sqlcgen.New(db)}
	if err := s.q.InsertDefaultSettings(context.Background(), formatTime(time.Now())); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化设置行: %w", err)
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping 检查数据库可用。
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Settings 读取全局设置。设置行在 Open 时已保证存在。
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	row, err := s.q.GetSettings(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("读取设置: %w", err)
	}
	return Settings{
		ReportEnabled:       row.ReportEnabled != 0,
		ReportTime:          row.ReportTime,
		TrafficAlertEnabled: row.TrafficAlertEnabled != 0,
		TrafficThresholds:   parseThresholds(row.TrafficThresholds),
		ExpiryAlertEnabled:  row.ExpiryAlertEnabled != 0,
		ExpiryDays:          int(row.ExpiryDays),
		UpdatedAt:           parseTime(row.UpdatedAt),
	}, nil
}

// UpdateSettings 读—改—写整行设置。修改函数拿到的是当前值的副本，可以只改需要的字段。
// 写回前会做一次规范化，脏数据被修回默认值而不是让预警静默失效。
func (s *Store) UpdateSettings(ctx context.Context, fn func(*Settings)) (Settings, error) {
	current, err := s.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	next := current
	if fn != nil {
		fn(&next)
	}
	normalize(&next)
	next.UpdatedAt = time.Now()

	err = s.q.UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{
		ReportEnabled:       boolToInt(next.ReportEnabled),
		ReportTime:          next.ReportTime,
		TrafficAlertEnabled: boolToInt(next.TrafficAlertEnabled),
		TrafficThresholds:   formatThresholds(next.TrafficThresholds),
		ExpiryAlertEnabled:  boolToInt(next.ExpiryAlertEnabled),
		ExpiryDays:          int64(next.ExpiryDays),
		UpdatedAt:           formatTime(next.UpdatedAt),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("写入设置: %w", err)
	}
	return next, nil
}

// Chats 返回白名单内全部 chat id。
func (s *Store) Chats(ctx context.Context) ([]int64, error) {
	ids, err := s.q.ListChatIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取白名单: %w", err)
	}
	return ids, nil
}

// IsAllowed 报告 chat 是否在白名单内。
func (s *Store) IsAllowed(ctx context.Context, chatID int64) (bool, error) {
	_, err := s.q.GetChat(ctx, chatID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("查询白名单 %d: %w", chatID, err)
	}
}

// AllowChat 把 chat 加入白名单。已存在时不做任何事，也不会重置招呼状态。
func (s *Store) AllowChat(ctx context.Context, chatID int64) error {
	if err := s.q.InsertChat(ctx, sqlcgen.InsertChatParams{
		ChatID:    chatID,
		CreatedAt: formatTime(time.Now()),
	}); err != nil {
		return fmt.Errorf("加入白名单 %d: %w", chatID, err)
	}
	return nil
}

// DenyChat 把 chat 移出白名单。
func (s *Store) DenyChat(ctx context.Context, chatID int64) error {
	if err := s.q.DeleteChat(ctx, chatID); err != nil {
		return fmt.Errorf("移出白名单 %d: %w", chatID, err)
	}
	return nil
}

// SeedOwnerIfEmpty 在白名单为空时用配置文件里的 owner_chat_id 兜底，避免把自己锁在外面。
//
// 只在为空时播种：用户主动 /deny 掉 owner 之后（白名单里还有别人）不会被重启加回来。
func (s *Store) SeedOwnerIfEmpty(ctx context.Context, ownerChatID int64) error {
	if ownerChatID == 0 {
		return nil
	}
	ids, err := s.Chats(ctx)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return nil
	}
	return s.AllowChat(ctx, ownerChatID)
}

// NeedGreeting 报告该 chat 是否还没收到首次招呼。
// 不在白名单内的 chat 一律返回 false，避免对陌生 chat 发消息。
func (s *Store) NeedGreeting(ctx context.Context, chatID int64) (bool, error) {
	greetedAt, err := s.q.GetChatGreetedAt(ctx, chatID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("查询招呼状态 %d: %w", chatID, err)
	default:
		return !greetedAt.Valid || greetedAt.String == "", nil
	}
}

// MarkGreeted 记录首次招呼已发送。
func (s *Store) MarkGreeted(ctx context.Context, chatID int64) error {
	if err := s.q.MarkChatGreeted(ctx, sqlcgen.MarkChatGreetedParams{
		GreetedAt: sql.NullString{String: formatTime(time.Now()), Valid: true},
		ChatID:    chatID,
	}); err != nil {
		return fmt.Errorf("记录招呼状态 %d: %w", chatID, err)
	}
	return nil
}

// SaveDailyUsage 按 (service_id, day) 覆盖写入日用量。
//
// 用覆盖而不是累加：BreaCloud 的日桶会滚动修正，后到的值才是权威值。
func (s *Store) SaveDailyUsage(ctx context.Context, serviceID int64, buckets []DailyUsage) error {
	if len(buckets) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后回滚是空操作
	q := s.q.WithTx(tx)
	for _, b := range buckets {
		if b.Day == "" {
			continue
		}
		fetchedAt := b.FetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = time.Now()
		}
		if err := q.UpsertDailyUsage(ctx, sqlcgen.UpsertDailyUsageParams{
			ServiceID: serviceID,
			Day:       b.Day,
			InBytes:   b.InBytes,
			OutBytes:  b.OutBytes,
			FetchedAt: formatTime(fetchedAt),
		}); err != nil {
			return fmt.Errorf("写入日用量 %d/%s: %w", serviceID, b.Day, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交日用量: %w", err)
	}
	return nil
}

// DailyUsage 返回某台服务在 [from, to] 闭区间内的日用量，按日期倒序。
// from / to 是 YYYY-MM-DD 字符串，直接与 day 列做字典序比较。
func (s *Store) DailyUsage(ctx context.Context, serviceID int64, from, to string) ([]DailyUsage, error) {
	rows, err := s.q.ListDailyUsage(ctx, sqlcgen.ListDailyUsageParams{
		ServiceID: serviceID,
		From:      from,
		To:        to,
	})
	if err != nil {
		return nil, fmt.Errorf("读取日用量 %d: %w", serviceID, err)
	}
	out := make([]DailyUsage, 0, len(rows))
	for _, r := range rows {
		out = append(out, DailyUsage{
			ServiceID: r.ServiceID,
			Day:       r.Day,
			InBytes:   r.InBytes,
			OutBytes:  r.OutBytes,
			FetchedAt: parseTime(r.FetchedAt),
		})
	}
	return out, nil
}

// PruneDailyUsage 删除 before 之前的日用量，用于控制长期增长。
func (s *Store) PruneDailyUsage(ctx context.Context, before string) error {
	if err := s.q.DeleteDailyUsageBefore(ctx, before); err != nil {
		return fmt.Errorf("清理日用量: %w", err)
	}
	return nil
}

// AlertState 返回某台服务某个阈值的预警状态；nil 表示尚未推送、处于「已武装」状态。
func (s *Store) AlertState(ctx context.Context, serviceID int64, threshold int) (*time.Time, error) {
	alertedAt, err := s.q.GetTrafficAlertState(ctx, sqlcgen.GetTrafficAlertStateParams{
		ServiceID: serviceID,
		Threshold: int64(threshold),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取预警状态 %d/%d: %w", serviceID, threshold, err)
	}
	if !alertedAt.Valid || alertedAt.String == "" {
		return nil, nil
	}
	t := parseTime(alertedAt.String)
	return &t, nil
}

// SetAlerted 写入预警状态：at 为 nil 表示重新武装（比值已回落到阈值以下），
// 非 nil 表示该阈值已推送、当前处于解除武装状态。
func (s *Store) SetAlerted(ctx context.Context, serviceID int64, threshold int, at *time.Time) error {
	value := sql.NullString{}
	if at != nil {
		value = sql.NullString{String: formatTime(*at), Valid: true}
	}
	if err := s.q.UpsertTrafficAlertState(ctx, sqlcgen.UpsertTrafficAlertStateParams{
		ServiceID: serviceID,
		Threshold: int64(threshold),
		AlertedAt: value,
	}); err != nil {
		return fmt.Errorf("写入预警状态 %d/%d: %w", serviceID, threshold, err)
	}
	return nil
}

// JobRan 报告某天的某个任务是否已经执行过。
func (s *Store) JobRan(ctx context.Context, day, job string) (bool, error) {
	_, err := s.q.GetJobRun(ctx, sqlcgen.GetJobRunParams{Day: day, Job: job})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("查询任务标记 %s/%s: %w", day, job, err)
	}
}

// MarkJobRan 写入任务幂等标记。同一天同一任务重复写入是空操作。
func (s *Store) MarkJobRan(ctx context.Context, day, job string) error {
	if err := s.q.InsertJobRun(ctx, sqlcgen.InsertJobRunParams{
		Day:   day,
		Job:   job,
		RanAt: formatTime(time.Now()),
	}); err != nil {
		return fmt.Errorf("写入任务标记 %s/%s: %w", day, job, err)
	}
	return nil
}

// PruneJobRuns 删除 before 之前的任务标记。
func (s *Store) PruneJobRuns(ctx context.Context, before string) error {
	if err := s.q.DeleteJobRunsBefore(ctx, before); err != nil {
		return fmt.Errorf("清理任务标记: %w", err)
	}
	return nil
}

// normalize 把可能被写坏的设置修回合法范围。宁可回到默认值，也不要让预警静默失效。
func normalize(s *Settings) {
	if _, err := time.Parse("15:04", s.ReportTime); err != nil {
		s.ReportTime = defaultReportTime
	}
	valid := s.TrafficThresholds[:0]
	for _, t := range s.TrafficThresholds {
		if t > 0 && t <= 100 {
			valid = append(valid, t)
		}
	}
	sort.Ints(valid)
	s.TrafficThresholds = dedupe(valid)
	if len(s.TrafficThresholds) == 0 {
		s.TrafficThresholds = parseThresholds(defaultThresholds)
	}
	if s.ExpiryDays < 0 {
		s.ExpiryDays = 0
	}
	if s.ExpiryDays > 365 {
		s.ExpiryDays = defaultExpiryDays
	}
}

func dedupe(sorted []int) []int {
	out := sorted[:0]
	for i, v := range sorted {
		if i == 0 || v != sorted[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func parseThresholds(raw string) []int {
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return parseThresholds(defaultThresholds)
	}
	sort.Ints(out)
	return dedupe(out)
}

func formatThresholds(ts []int) string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, strconv.Itoa(t))
	}
	return strings.Join(parts, ",")
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// formatTime / parseTime 统一用 RFC3339 UTC 存时间字符串，保证字典序与时间序一致。
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
