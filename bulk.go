package esx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v9/esutil"
)

// 批量写入器的默认值。
const (
	defaultFlushBytes    = 5 << 20
	defaultFlushInterval = 2 * time.Second
)

// ErrBulkClosed 表示对已关闭的批量写入器做了提交或刷写。
var ErrBulkClosed = errors.New("esx: bulk indexer is closed")

// BulkOption 配置批量写入器。
type BulkOption func(*bulkConfig)

type bulkConfig struct {
	index         string
	workers       int
	flushBytes    int
	flushInterval time.Duration
	onError       func(context.Context, error)
	onSuccess     func(context.Context, BulkItem)
	onFailure     func(context.Context, BulkItem, error)
}

// WithBulkIndex 设置默认索引，用于未单独指定索引的条目。
func WithBulkIndex(index string) BulkOption {
	return func(c *bulkConfig) { c.index = index }
}

// WithBulkWorkers 设置并发 worker 数。非正值被忽略。
func WithBulkWorkers(n int) BulkOption {
	return func(c *bulkConfig) {
		if n > 0 {
			c.workers = n
		}
	}
}

// WithBulkFlushBytes 设置触发刷写的缓冲字节阈值。非正值被忽略。
func WithBulkFlushBytes(n int) BulkOption {
	return func(c *bulkConfig) {
		if n > 0 {
			c.flushBytes = n
		}
	}
}

// WithBulkFlushInterval 设置定时刷写间隔。非正值被忽略。
func WithBulkFlushInterval(d time.Duration) BulkOption {
	return func(c *bulkConfig) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithBulkOnError 设置写入器级错误回调，用于整批请求失败这类不归属单条目的错误。
func WithBulkOnError(fn func(context.Context, error)) BulkOption {
	return func(c *bulkConfig) { c.onError = fn }
}

// WithBulkOnSuccess 设置条目成功回调。回调在 worker goroutine 上执行，必须自行保证并发安全。
func WithBulkOnSuccess(fn func(context.Context, BulkItem)) BulkOption {
	return func(c *bulkConfig) { c.onSuccess = fn }
}

// WithBulkOnFailure 设置条目失败回调。回调在 worker goroutine 上执行，必须自行保证并发安全。
func WithBulkOnFailure(fn func(context.Context, BulkItem, error)) BulkOption {
	return func(c *bulkConfig) { c.onFailure = fn }
}

// BulkAction 是批量条目的操作类型。
type BulkAction string

const (
	// BulkIndex 写入文档，已存在时整体替换。
	BulkIndex BulkAction = "index"
	// BulkUpdate 部分更新文档。
	BulkUpdate BulkAction = "update"
	// BulkDelete 删除文档。
	BulkDelete BulkAction = "delete"
)

// BulkItem 是一个待批量处理的条目。
type BulkItem struct {
	// Action 是操作类型，为空时按 BulkIndex 处理。
	Action BulkAction
	// Index 是目标索引，为空时用 WithBulkIndex 设置的默认索引。
	Index string
	// DocumentID 是文档 id。BulkUpdate 与 BulkDelete 必填。
	DocumentID string
	// Doc 是文档内容，会被 JSON 编码。BulkDelete 忽略该字段。
	// BulkUpdate 时它是部分字段，会被包进 {"doc": ...}。
	Doc any
}

// Bulk 是批量写入器。
//
// 并发安全：Add 与 Flush 可被多个 goroutine 同时调用。
type Bulk struct {
	indexer esutil.BulkIndexer
	cfg     *bulkConfig

	closeOnce sync.Once
	closeErr  error
	closed    sync.RWMutex
	isClosed  bool
}

// NewBulk 构造批量写入器。
//
// 构造成功后 worker goroutine 即已启动，必须调用 Close 回收。
func (c *Client) NewBulk(opts ...BulkOption) (*Bulk, error) {
	cfg := &bulkConfig{
		workers:       max(runtime.NumCPU(), 1),
		flushBytes:    defaultFlushBytes,
		flushInterval: defaultFlushInterval,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	b := &Bulk{cfg: cfg}

	// esutil.NewBulkIndexer 一旦成功就已启动 worker goroutine，所以它必须排在本函数
	// 所有可能失败的步骤之后：反过来的话，后续校验失败提前返回就会漏掉这些 goroutine。
	indexer, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        c.es,
		Index:         cfg.index,
		NumWorkers:    cfg.workers,
		FlushBytes:    cfg.flushBytes,
		FlushInterval: cfg.flushInterval,
		OnError:       cfg.onError,
	})
	if err != nil {
		return nil, invalid("create bulk indexer", err)
	}
	b.indexer = indexer
	return b, nil
}

// Add 提交一个条目。
//
// 条目的成功与失败通过 WithBulkOnSuccess 与 WithBulkOnFailure 回调投递，
// 不由本方法的返回值表达；返回错误只表示条目未能入队。
func (b *Bulk) Add(ctx context.Context, item BulkItem) error {
	b.closed.RLock()
	defer b.closed.RUnlock()
	if b.isClosed {
		return &Error{Op: "bulk add", Index: item.Index, Err: ErrBulkClosed}
	}

	body, err := encodeBulkBody(item)
	if err != nil {
		return invalid("bulk add", err)
	}

	action := item.Action
	if action == "" {
		action = BulkIndex
	}

	esItem := esutil.BulkIndexerItem{
		Index:      item.Index,
		Action:     string(action),
		DocumentID: item.DocumentID,
		Body:       body,
	}
	if fn := b.cfg.onSuccess; fn != nil {
		esItem.OnSuccess = func(ctx context.Context, ei esutil.BulkIndexerItem, _ esutil.BulkIndexerResponseItem) {
			fn(ctx, itemFrom(ei, item))
		}
	}
	if fn := b.cfg.onFailure; fn != nil {
		esItem.OnFailure = func(ctx context.Context, ei esutil.BulkIndexerItem, res esutil.BulkIndexerResponseItem, err error) {
			fn(ctx, itemFrom(ei, item), bulkItemErr(res, err))
		}
	}

	if err := b.indexer.Add(ctx, esItem); err != nil {
		return wrapErr("bulk add", item.Index, err)
	}
	return nil
}

// Flush 把当前已入队的条目发送到 Elasticsearch 并等待发送完成。
//
// 返回后写入器仍可继续 Add。与 Add 并发调用是安全的；与 Flush 并发调用会被串行化。
// 条目级结果仍走 Add 时注册的回调，本方法只在传输层失败或 ctx 取消时返回错误。
func (b *Bulk) Flush(ctx context.Context) error {
	b.closed.RLock()
	defer b.closed.RUnlock()
	if b.isClosed {
		return &Error{Op: "bulk flush", Err: ErrBulkClosed}
	}
	if err := b.indexer.Flush(ctx); err != nil {
		return wrapErr("bulk flush", "", err)
	}
	return nil
}

// Close 等待全部已提交条目刷写完成后回收 worker。
//
// Close 是幂等的：重复调用返回首次调用的结果，不 panic。
// 返回后本写入器启动的 worker goroutine 均已退出。
func (b *Bulk) Close(ctx context.Context) error {
	b.closeOnce.Do(func() {
		b.closed.Lock()
		b.isClosed = true
		b.closed.Unlock()

		if err := b.indexer.Close(ctx); err != nil {
			b.closeErr = wrapErr("bulk close", "", err)
		}
	})
	return b.closeErr
}

// Stats 返回累计统计。可在提交进行中安全读取。
func (b *Bulk) Stats() esutil.BulkIndexerStats { return b.indexer.Stats() }

// encodeBulkBody 把条目内容编码成 bulk 请求体。删除操作没有请求体。
func encodeBulkBody(item BulkItem) (io.ReadSeeker, error) {
	if item.Action == BulkDelete {
		return nil, nil
	}
	if item.Doc == nil {
		return nil, errors.New("document is required")
	}

	payload := item.Doc
	if item.Action == BulkUpdate {
		// 部分更新的 bulk 请求体要求包一层 doc，否则整份文档会被当成替换内容。
		payload = map[string]any{"doc": item.Doc}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func itemFrom(ei esutil.BulkIndexerItem, orig BulkItem) BulkItem {
	orig.Index = ei.Index
	orig.DocumentID = ei.DocumentID
	return orig
}

// bulkItemErr 取条目失败的原因：传输层错误优先，否则用 ES 返回的错误描述。
func bulkItemErr(res esutil.BulkIndexerResponseItem, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s: %s", res.Error.Type, res.Error.Reason)
}
