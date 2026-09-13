package breacloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/lay-g/breacloud-tg-bot/internal/config"
)

const (
	// maxBodyBytes 限制单次响应读取量，避免异常响应把内存吃满。
	maxBodyBytes = 8 << 20
	// bodySnippet 是解码失败时保留的响应片段长度。
	bodySnippet = 200
	// pageSize 是列表接口的每页条数；上限 50 页，超出直接报错而不是静默截断。
	pageSize   = 100
	maxPages   = 50
	maxRetries = 3
)

// Client 是 BreaCloud API 客户端，零额外依赖，可直接并发使用。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	sem     chan struct{}

	// backoffBase 是首次重试的等待时间，测试里会调小。
	backoffBase time.Duration
	// sleep 可在测试中替换，避免真的等待。
	sleep func(context.Context, time.Duration) error
	// rand 用于给退避加抖动，避免大量请求同时重试。
	rand func() float64
}

// New 按配置构造客户端。baseURL 末尾斜杠会被去掉，concurrency 小于 1 时按 1 处理。
func New(cfg config.BreaCloudConfig) *Client {
	concurrency := cfg.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		baseURL:     trimTrailingSlash(cfg.BaseURL),
		token:       cfg.APIToken,
		http:        &http.Client{Timeout: timeout},
		sem:         make(chan struct{}, concurrency),
		backoffBase: time.Second,
		sleep:       sleepCtx,
		rand:        rand.Float64,
	}
}

// ListServices 返回全部服务，自动翻页。
//
// 响应没有 total 字段，只能翻到空页为止；因此最后一页总会多一次请求。
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	var all []Service
	seen := make(map[int64]bool)
	for page := 1; page <= maxPages; page++ {
		var resp struct {
			Services []Service `json:"services"`
		}
		path := fmt.Sprintf("/services?page=%d&page_size=%d", page, pageSize)
		if err := c.do(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
			return nil, err
		}
		if len(resp.Services) == 0 {
			return all, nil
		}
		// 去重是防呆：如果服务端忽略了 page 参数，不去重就会把同一页当成多页累加。
		for _, s := range resp.Services {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			all = append(all, s)
		}
	}
	return nil, fmt.Errorf("服务列表超过 %d 页仍未结束，已中止翻页", maxPages)
}

// GetService 返回单台服务的详情。
func (c *Client) GetService(ctx context.Context, id int64) (ServiceDetail, error) {
	var resp ServiceDetail
	if err := c.do(ctx, http.MethodGet, "/services/"+strconv.FormatInt(id, 10), nil, &resp, true); err != nil {
		return ServiceDetail{}, err
	}
	return resp, nil
}

// GetTraffic 返回当前计费周期的用量与配额。
func (c *Client) GetTraffic(ctx context.Context, id int64) (Traffic, error) {
	var resp struct {
		Traffic Traffic `json:"traffic"`
	}
	path := "/services/" + strconv.FormatInt(id, 10) + "/traffic"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return Traffic{}, err
	}
	return resp.Traffic, nil
}

// GetTrafficWeek 返回最近 8 天的日用量。
//
// 固定用 range=week：range=day 的「昨天」桶被服务端 24 小时窗口截断，会系统性少算，
// 而 week 与 5 分钟样本、计费口径都逐字节一致。详见 docs/rules/breacloud-api.md。
func (c *Client) GetTrafficWeek(ctx context.Context, id int64) (History, error) {
	var resp History
	path := "/services/" + strconv.FormatInt(id, 10) + "/traffic-history?range=week"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return History{}, err
	}
	return resp, nil
}

// DoAction 下发电源操作。
//
// action 取值：start / shutdown / stop / reboot / cold_reboot。响应只表示入队。
//
// 这个请求**不重试**：非幂等，重试可能对一台已经关机的机器再下一次指令。
func (c *Client) DoAction(ctx context.Context, id int64, action string) error {
	path := "/services/" + strconv.FormatInt(id, 10) + "/actions"
	return c.do(ctx, http.MethodPost, path, map[string]string{"action": action}, nil, false)
}

// ListTasks 返回某台服务的任务历史。
func (c *Client) ListTasks(ctx context.Context, id int64) ([]Task, error) {
	var resp struct {
		Tasks []Task `json:"tasks"`
	}
	path := "/services/" + strconv.FormatInt(id, 10) + "/tasks"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp, true); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// do 是唯一的 HTTP 出口：并发信号量、超时、信封解码、错误分类、退避重试都在这里。
func (c *Client) do(ctx context.Context, method, path string, body, out any, retry bool) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体: %w", err)
		}
	}

	attempts := 1
	if retry {
		attempts = maxRetries + 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := c.acquire(ctx); err != nil {
			return err
		}
		err := c.once(ctx, method, path, payload, out)
		c.release()

		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == attempts-1 || !isRetryable(err) {
			return err
		}
		// 1s / 2s / 4s，再加 0~500ms 抖动
		delay := c.backoffBase << attempt
		delay += time.Duration(c.rand() * float64(500*time.Millisecond))
		if err := c.sleep(ctx, delay); err != nil {
			return lastErr
		}
	}
	return lastErr
}

// once 执行一次请求并解码响应。
func (c *Client) once(ctx context.Context, method, path string, payload []byte, out any) error {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return &APIError{Message: "构造请求失败: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return &APIError{Message: "请求失败: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return &APIError{Status: resp.StatusCode, Message: "读取响应失败: " + err.Error()}
	}
	return decodeEnvelope(resp.StatusCode, raw, out)
}

// decodeEnvelope 解析统一信封 {code, message, data}。
func decodeEnvelope(status int, raw []byte, out any) error {
	var envelope struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return &APIError{
			Status:  status,
			Message: "响应不是合法 JSON",
			Body:    snippet(raw),
		}
	}
	if envelope.Code != "OK" {
		message := envelope.Message
		if message == "" {
			message = "接口返回错误"
		}
		return &APIError{Status: status, Code: envelope.Code, Message: message}
	}
	if out == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return &APIError{
			Status:  status,
			Code:    envelope.Code,
			Message: "解析 data 失败: " + err.Error(),
			Body:    snippet(envelope.Data),
		}
	}
	return nil
}

// isRetryable 报告错误是否值得重试：传输层错误与 5xx 可重试，4xx 不可。
func isRetryable(err error) bool {
	if apiErr, ok := asAPIError(err); ok {
		return apiErr.retryable()
	}
	return true
}

func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() {
	<-c.sem
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func snippet(raw []byte) string {
	if len(raw) <= bodySnippet {
		return string(raw)
	}
	return string(raw[:bodySnippet]) + "..."
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
