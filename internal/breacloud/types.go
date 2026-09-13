// Package breacloud 是 BreaCloud API 的客户端。
//
// 它把 HTTP 接口包装成类型安全的方法，并把统一信封解码、并发上限、退避重试
// 收敛在一处。上层不应该自己拼 URL、判 code、写重试循环。
//
// 接口字段与实测行为见 docs/references/breacloud-api.md。
package breacloud

// Service 是服务列表里的一台机器。只建模用得到的字段。
//
// 时间字段用 string 接收：服务端可能返回 null，也可能返回 RFC3339，
// 空串表示「没有这个值」，业务侧再决定怎么处理，客户端不做业务判断。
type Service struct {
	ID              int64  `json:"id"`
	DomainName      string `json:"domain_name"`
	Status          string `json:"status"`
	BillingCycle    string `json:"billing_cycle"`
	BillingMode     string `json:"billing_mode"`
	ProductName     string `json:"product_name"`
	RegionName      string `json:"region_name"`
	OSName          string `json:"os_name"`
	PrimaryIP       string `json:"primary_ip"`
	BandwidthMbps   int64  `json:"bandwidth_mbps"`
	NextDueDate     string `json:"next_due_date"`
	ExpireAt        string `json:"expire_at"`
	AutoRenew       bool   `json:"auto_renew_enabled"`
	RenewCanceledAt string `json:"renew_canceled_at"`
}

// IsPeriodic 判断是否为周期计费服务（到期提醒只针对这类机器）。
//
// 列表接口并不总是返回 billing_mode（实测某些服务该字段缺失），因此缺失时
// 退回「有 next_due_date 且没有 expire_at」的启发式。
func (s Service) IsPeriodic() bool {
	if s.BillingMode != "" {
		return s.BillingMode == "periodic"
	}
	return s.NextDueDate != "" && s.ExpireAt == ""
}

// DisplayName 返回用于展示的名称，域名缺失时退回服务 id。
func (s Service) DisplayName() string {
	if s.DomainName != "" {
		return s.DomainName
	}
	return "服务 #" + itoa(s.ID)
}

// Resource 是服务详情里的资源信息。
type Resource struct {
	VMID         int64  `json:"vmid"`
	NodeID       int64  `json:"node_id"`
	NodeName     string `json:"node_name"`
	NodeCluster  string `json:"node_cluster"`
	NodeLocation string `json:"node_location"`
	NodeCountry  string `json:"node_country"`
	PrimaryIP    string `json:"primary_ip"`
	IPv6         string `json:"ipv6"`
	OSTemplate   string `json:"os_template"`
	OSName       string `json:"os_name"`
	CPUCores     int64  `json:"cpu_cores"`
	MemoryMB     int64  `json:"memory_mb"`
	DiskGB       int64  `json:"disk_gb"`
}

// ServiceDetail 是 GET /services/:id 的 data。
type ServiceDetail struct {
	Service     Service  `json:"service"`
	Resource    Resource `json:"resource"`
	Onboot      bool     `json:"onboot"`
	BillingMode string   `json:"billing_mode"`
}

// Traffic 是 GET /services/:id/traffic 的周期用量。
//
// QuotaGB 与 UsedGB 的单位是 GiB（2^30），不是 GB。判断阈值请用 InBytes + OutBytes。
type Traffic struct {
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Mode        string `json:"mode"`
	QuotaGB     int64  `json:"quota_gb"`
	UsedGB      int64  `json:"used_gb"`
	OverGB      int64  `json:"over_gb"`
	InBytes     int64  `json:"in_bytes"`
	OutBytes    int64  `json:"out_bytes"`
	Unlimited   bool   `json:"unlimited"`
}

// Total 返回周期内双向流量之和。
func (t Traffic) Total() int64 {
	return t.InBytes + t.OutBytes
}

// QuotaBytes 返回配额的字节数。QuotaGB 的单位是 GiB。
func (t Traffic) QuotaBytes() int64 {
	return t.QuotaGB << 30
}

// DailyBucket 是 traffic-history 的一个日桶。
//
// Bucket 是 BreaCloud 后端本地日（UTC+8）的 YYYY-MM-DD，原样使用，不要做时区换算。
type DailyBucket struct {
	Bucket   string `json:"bucket"`
	InBytes  int64  `json:"in_bytes"`
	OutBytes int64  `json:"out_bytes"`
}

// Total 返回该日双向流量之和。
func (b DailyBucket) Total() int64 {
	return b.InBytes + b.OutBytes
}

// History 是 GET /services/:id/traffic-history 的 data。
type History struct {
	Range string        `json:"range"`
	Daily []DailyBucket `json:"daily"`
}

// FindDay 返回指定日期的日桶，找不到时返回 false。
func (h History) FindDay(day string) (DailyBucket, bool) {
	for _, b := range h.Daily {
		if b.Bucket == day {
			return b, true
		}
	}
	return DailyBucket{}, false
}

// Task 是 GET /services/:id/tasks 里的一条任务记录。
type Task struct {
	ID         int64  `json:"id"`
	Op         string `json:"op"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at"`
}

// itoa 避免为了一个整数格式化引入 strconv 到调用点。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
