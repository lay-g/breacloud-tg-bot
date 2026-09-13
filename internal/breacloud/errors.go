package breacloud

import (
	"fmt"
	"strings"
)

// APIError 是 BreaCloud 返回的错误。
//
// 三种情况都归一成它：HTTP 非 2xx、信封 code != "OK"、响应不是合法 JSON。
type APIError struct {
	Status  int    // HTTP 状态码
	Code    string // 信封里的业务错误码，如 ERR_FORBIDDEN
	Message string // 可直接展示给用户的中文消息
	Body    string // 解码失败时保留的原始响应片段（截断）
}

func (e *APIError) Error() string {
	var b strings.Builder
	if e.Status != 0 {
		fmt.Fprintf(&b, "HTTP %d", e.Status)
	}
	if e.Code != "" {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(e.Code)
	}
	if b.Len() > 0 {
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Body != "" {
		fmt.Fprintf(&b, "（响应片段: %s）", e.Body)
	}
	return b.String()
}

// Unauthorized 报告是否为认证失败。
func (e *APIError) Unauthorized() bool {
	return e.Status == 401 || e.Code == "ERR_UNAUTHORIZED"
}

// Forbidden 报告是否为权限不足。消息里通常带上缺失的 scope，可直接展示给用户。
func (e *APIError) Forbidden() bool {
	return e.Status == 403 || e.Code == "ERR_FORBIDDEN"
}

// NotFound 报告资源不存在。
func (e *APIError) NotFound() bool {
	return e.Status == 404 || e.Code == "ERR_NOT_FOUND"
}

// retryable 报告该错误是否值得重试。5xx 与传输层错误可重试，4xx 不可。
func (e *APIError) retryable() bool {
	return e.Status >= 500
}

// asAPIError 把 error 归一成 *APIError。
func asAPIError(err error) (*APIError, bool) {
	apiErr, ok := err.(*APIError)
	return apiErr, ok
}
