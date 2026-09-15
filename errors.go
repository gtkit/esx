package esx

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// ErrNotFound 表示目标资源在 Elasticsearch 中不存在。
//
// 文档与索引操作把「不存在」统一映射为它，调用方用 errors.Is 判断：
//
//	if _, err := esx.GetDoc[Order](ctx, c, "orders", id); errors.Is(err, esx.ErrNotFound) {
//		// 文档不存在
//	}
var ErrNotFound = errors.New("esx: not found")

// Error 是来自 Elasticsearch 的失败，携带定位问题所需的上下文。
//
// 用 errors.AsType 取出：
//
//	if e, ok := errors.AsType[*esx.Error](err); ok && e.StatusCode == http.StatusTooManyRequests {
//		// 限流，退避重试
//	}
type Error struct {
	// Op 是发起该请求的操作名，如 "create index"、"get document"。
	Op string
	// Index 是操作针对的索引或别名；无索引概念的操作为空。
	Index string
	// StatusCode 是 Elasticsearch 返回的 HTTP 状态码。
	// 传输层失败，或客户端在发请求前就判定失败时，为 0。
	StatusCode int
	// Err 是底层错误：不存在时为 ErrNotFound，服务端拒绝时为 ES 返回的错误，
	// 传输失败时为网络错误。
	Err error
}

func (e *Error) Error() string {
	switch {
	case e.Index != "" && e.StatusCode != 0:
		return fmt.Sprintf("esx: %s %q: status %d: %v", e.Op, e.Index, e.StatusCode, e.Err)
	case e.Index != "":
		return fmt.Sprintf("esx: %s %q: %v", e.Op, e.Index, e.Err)
	case e.StatusCode != 0:
		return fmt.Sprintf("esx: %s: status %d: %v", e.Op, e.StatusCode, e.Err)
	default:
		return fmt.Sprintf("esx: %s: %v", e.Op, e.Err)
	}
}

func (e *Error) Unwrap() error { return e.Err }

// notFound 构造一个「资源不存在」错误。
//
// typedapi 对 404 的处理并不统一——core/get 与 core/delete 把 404 当正常响应返回
// （靠 Response.Found / Result 区分），core/update 则返回错误。归一到这一个构造点，
// 调用方才能只用 errors.Is(err, ErrNotFound) 一种判断。
func notFound(op, index string) error {
	return &Error{Op: op, Index: index, StatusCode: http.StatusNotFound, Err: ErrNotFound}
}

// invalid 构造一个在发出请求之前就判定的参数错误。
func invalid(op string, err error) error {
	return &Error{Op: op, Err: err}
}

// wrapErr 把一次 typedapi 调用返回的错误归一为本包的错误模型。
func wrapErr(op, index string, err error) error {
	if err == nil {
		return nil
	}
	status := statusOf(err)
	if status == http.StatusNotFound {
		return &Error{Op: op, Index: index, StatusCode: status, Err: ErrNotFound}
	}
	return &Error{Op: op, Index: index, StatusCode: status, Err: err}
}

// statusOf 取 Elasticsearch 返回的 HTTP 状态码；错误不是 ES 结构化错误时返回 0。
func statusOf(err error) int {
	if e, ok := errors.AsType[*types.ElasticsearchError](err); ok {
		return e.Status
	}
	return 0
}
