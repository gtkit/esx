package esx

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/mget"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// MGet 按 id 批量读取文档，把全部 id 收敛到一次请求。
//
// 返回的映射以文档 id 为键；不存在的文档不在其中，不构成错误——调用方按自己手里的
// id 列表遍历即可保持顺序，并据缺失与否判断哪些没取到：
//
//	docs, err := esx.MGet[Order](ctx, c, "orders", ids...)
//	for _, id := range ids {
//		if doc, ok := docs[id]; ok {
//			// 命中
//		}
//	}
//
// ids 为空时不发出请求。某个条目被 Elasticsearch 判定为错误时整次调用返回错误，
// 不静默丢弃该条目。
func MGet[T any](ctx context.Context, c *Client, index string, ids ...string) (map[string]T, error) {
	if index == "" {
		return nil, invalid("mget", errNoIndexName)
	}
	if slices.Contains(ids, "") {
		return nil, invalid("mget", errNoDocID)
	}
	if len(ids) == 0 {
		return map[string]T{}, nil
	}

	req := mget.NewRequest()
	req.Ids = ids

	resp, err := c.typed.Mget().Index(index).Request(req).Do(ctx)
	if err != nil {
		return nil, wrapErr("mget", index, err)
	}

	docs := make(map[string]T, len(resp.Docs))
	for _, item := range resp.Docs {
		// typedapi 把 docs 数组解成 union：带 found 的解成 *GetResult，
		// 带 error 的解成 *MultiGetError，两者都不匹配时留作原始值。
		switch doc := item.(type) {
		case *types.GetResult:
			if !doc.Found {
				continue
			}
			var out T
			if err := json.Unmarshal(doc.Source_, &out); err != nil {
				return nil, &Error{Op: "mget", Index: index, Err: err}
			}
			docs[doc.Id_] = out
		case *types.MultiGetError:
			// Reason 是可选字段，缺失时退回必填的 Type，避免错误信息只剩一个 id。
			detail := doc.Error.Type
			if doc.Error.Reason != nil {
				detail = *doc.Error.Reason
			}
			return nil, &Error{
				Op:    "mget",
				Index: index,
				Err:   fmt.Errorf("document %q: %s", doc.Id_, detail),
			}
		}
	}
	return docs, nil
}
