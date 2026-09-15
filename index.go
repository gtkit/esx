package esx

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/create"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// IndexOption 配置 CreateIndex。
type IndexOption func(*indexConfig)

type indexConfig struct {
	mapping json.RawMessage
	aliases []string
}

// WithIndexMapping 指定索引的 mapping 与 settings，内容为 ES 索引创建请求体的 JSON，
// 即包含 mappings、settings、aliases 的对象。非法 JSON 在发出请求之前被拒绝。
func WithIndexMapping(m json.RawMessage) IndexOption {
	return func(c *indexConfig) { c.mapping = m }
}

// WithIndexAliases 在创建索引的同时挂上别名，并把该索引标记为这些别名的写索引。
//
// 与 WithIndexMapping 中已有的 aliases 合并；同名别名以本选项为准。
func WithIndexAliases(aliases ...string) IndexOption {
	return func(c *indexConfig) { c.aliases = append(c.aliases, aliases...) }
}

// IndexExists 判断索引是否存在。
//
// 索引不存在返回 (false, nil)，只有传输失败或服务端异常才返回错误。
func (c *Client) IndexExists(ctx context.Context, index string) (bool, error) {
	if index == "" {
		return false, invalid("index exists", errNoIndexName)
	}
	ok, err := c.typed.Indices.Exists(index).Do(ctx)
	if err != nil {
		return false, wrapErr("index exists", index, err)
	}
	return ok, nil
}

// CreateIndex 创建索引，可选地附带 mapping 与别名。
//
// 索引已存在时返回错误，可用 errors.AsType[*Error] 取状态码区分。
func (c *Client) CreateIndex(ctx context.Context, index string, opts ...IndexOption) error {
	if index == "" {
		return invalid("create index", errNoIndexName)
	}

	cfg := &indexConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	req, err := buildCreateRequest(cfg)
	if err != nil {
		return invalid("create index", err)
	}

	builder := c.typed.Indices.Create(index)
	if req != nil {
		builder = builder.Request(req)
	}
	if _, err := builder.Do(ctx); err != nil {
		return wrapErr("create index", index, err)
	}
	return nil
}

// buildCreateRequest 把 mapping 与别名合成索引创建请求。
//
// 两者都没有时返回 nil，表示用集群默认设置建一个空索引。
func buildCreateRequest(cfg *indexConfig) (*create.Request, error) {
	if len(cfg.mapping) == 0 && len(cfg.aliases) == 0 {
		return nil, nil
	}

	req := create.NewRequest()
	if len(cfg.mapping) > 0 {
		// 解到 create.Request 上，非法 JSON 与不认识的结构在这里就被挡下，不发出请求。
		if err := json.Unmarshal(cfg.mapping, req); err != nil {
			return nil, err
		}
	}
	if len(cfg.aliases) > 0 {
		if req.Aliases == nil {
			req.Aliases = make(map[string]types.Alias, len(cfg.aliases))
		}
		for _, alias := range cfg.aliases {
			req.Aliases[alias] = types.Alias{IsWriteIndex: new(true)}
		}
	}
	return req, nil
}

// DeleteIndices 删除一个或多个索引。传入空列表是无操作，不发出请求。
func (c *Client) DeleteIndices(ctx context.Context, indices ...string) error {
	if len(indices) == 0 {
		return nil
	}
	if slices.Contains(indices, "") {
		return invalid("delete indices", errNoIndexName)
	}
	target := strings.Join(indices, ",")
	if _, err := c.typed.Indices.Delete(target).Do(ctx); err != nil {
		return wrapErr("delete indices", target, err)
	}
	return nil
}

var errNoIndexName = errors.New("index name is required")
