package esx

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/result"
)

var errNoDocID = errors.New("document id is required")

// GetDoc 按 id 读取文档，把 _source 解码为 T。
//
// 文档不存在时返回的错误满足 errors.Is(err, ErrNotFound)。解码失败返回的错误不匹配
// ErrNotFound，两者可以区分。
func GetDoc[T any](ctx context.Context, c *Client, index, id string) (T, error) {
	var doc T
	if index == "" {
		return doc, invalid("get document", errNoIndexName)
	}
	if id == "" {
		return doc, invalid("get document", errNoDocID)
	}

	res, err := c.typed.Get(index, id).Do(ctx)
	if err != nil {
		return doc, wrapErr("get document", index, err)
	}
	// typedapi 把 404 当正常响应解码，不返回错误，靠 Found 区分。
	if !res.Found {
		return doc, notFound("get document", index)
	}
	if err := json.Unmarshal(res.Source_, &doc); err != nil {
		return doc, &Error{Op: "get document", Index: index, Err: err}
	}
	return doc, nil
}

// IndexDoc 按 id 写入文档，已存在时整体替换。
func (c *Client) IndexDoc(ctx context.Context, index, id string, doc any) error {
	if index == "" {
		return invalid("index document", errNoIndexName)
	}
	if id == "" {
		return invalid("index document", errNoDocID)
	}
	if _, err := c.typed.Index(index).Id(id).Document(doc).Do(ctx); err != nil {
		return wrapErr("index document", index, err)
	}
	return nil
}

// UpdateDoc 用 partial 中的字段做部分更新，未提及的字段保持原值。
//
// 目标文档不存在时返回的错误满足 errors.Is(err, ErrNotFound)。
func (c *Client) UpdateDoc(ctx context.Context, index, id string, partial any) error {
	if index == "" {
		return invalid("update document", errNoIndexName)
	}
	if id == "" {
		return invalid("update document", errNoDocID)
	}
	if _, err := c.typed.Update(index, id).Doc(partial).Do(ctx); err != nil {
		return wrapErr("update document", index, err)
	}
	return nil
}

// DeleteDoc 按 id 删除文档。
//
// 文档不存在时返回的错误满足 errors.Is(err, ErrNotFound)，由调用方决定是否当作
// 幂等成功处理。
func (c *Client) DeleteDoc(ctx context.Context, index, id string) error {
	if index == "" {
		return invalid("delete document", errNoIndexName)
	}
	if id == "" {
		return invalid("delete document", errNoDocID)
	}
	res, err := c.typed.Delete(index, id).Do(ctx)
	if err != nil {
		return wrapErr("delete document", index, err)
	}
	// 与 Get 一样，typedapi 把删除时的 404 当正常响应，靠 Result 区分。
	if res.Result == result.Notfound {
		return notFound("delete document", index)
	}
	return nil
}

// DocExists 判断文档是否存在。
//
// 文档不存在、索引不存在，都返回 (false, nil)。
func (c *Client) DocExists(ctx context.Context, index, id string) (bool, error) {
	if index == "" {
		return false, invalid("document exists", errNoIndexName)
	}
	if id == "" {
		return false, invalid("document exists", errNoDocID)
	}
	ok, err := c.typed.Exists(index, id).Do(ctx)
	if err != nil {
		return false, wrapErr("document exists", index, err)
	}
	return ok, nil
}
