package esx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	"github.com/elastic/go-elasticsearch/v9"
)

// Client 是 Elasticsearch 客户端。
//
// 它同时持有低级客户端与类型化客户端，两者共享同一传输层与连接池。
// 并发安全：构造后可被多个 goroutine 同时使用。
type Client struct {
	es    *elasticsearch.Client
	typed *elasticsearch.TypedClient

	closeOnce sync.Once
	closeErr  error
}

// New 构造客户端。
//
// 本包面向 Elasticsearch 9.x 服务端，详见包文档。传 WithPing 可让连通性与认证问题
// 在此处暴露；不传则 New 不发起任何请求。
func New(opts ...Option) (*Client, error) {
	cfg := newConfig(opts)

	esOpts, err := cfg.clientOptions()
	if err != nil {
		return nil, err
	}

	es, err := elasticsearch.New(esOpts...)
	if err != nil {
		return nil, invalid("create client", err)
	}

	c := &Client{
		es: es,
		// 共享 es 的传输层与连接池：分别构造两个客户端会建出两套连接池，
		// 连接数翻倍，且各自重跑一遍 product check。
		typed: elasticsearch.NewTypedFrom(es),
	}

	if cfg.ping {
		if err := c.ping(cfg.requestTimeout); err != nil {
			// 校验失败就不把客户端交出去，顺手关掉已经建立的连接。
			_ = c.Close(context.Background())
			return nil, err
		}
	}
	return c, nil
}

// clientOptions 把本包的配置翻译成 go-elasticsearch 的选项。
func (c *config) clientOptions() ([]elasticsearch.Option, error) {
	transport, err := c.httpTransport()
	if err != nil {
		return nil, err
	}

	opts := []elasticsearch.Option{
		elasticsearch.WithTransportOptions(elastictransport.WithTransport(transport)),
	}
	if len(c.addresses) > 0 {
		opts = append(opts, elasticsearch.WithAddresses(c.addresses...))
	}
	if c.username != "" {
		opts = append(opts, elasticsearch.WithBasicAuth(c.username, c.password))
	}
	if c.apiKey != "" {
		opts = append(opts, elasticsearch.WithAPIKey(c.apiKey))
	}
	if len(c.caCert) > 0 {
		opts = append(opts, elasticsearch.WithCACert(c.caCert))
	}
	if c.maxRetries > 0 {
		opts = append(opts, elasticsearch.WithRetry(c.maxRetries, c.retryOnStatus...))
	}
	return opts, nil
}

// httpTransport 构造带超时与 TLS 硬化的传输层。
func (c *config) httpTransport() (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, invalid("create transport", fmt.Errorf("unexpected default transport type %T", http.DefaultTransport))
	}
	t := base.Clone()
	t.DialContext = (&net.Dialer{
		Timeout:   c.requestTimeout,
		KeepAlive: defaultKeepAlive,
	}).DialContext
	t.TLSHandshakeTimeout = c.requestTimeout
	t.ResponseHeaderTimeout = c.requestTimeout
	t.TLSClientConfig = c.tlsConfig()
	return t, nil
}

// ping 做一次集群信息查询，确认连通性与认证可用。
func (c *Client) ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if _, err := c.typed.Info().Do(ctx); err != nil {
		// 超时要能被 errors.Is(err, context.DeadlineExceeded) 命中：传输层返回的
		// 错误未必包裹了 ctx 的原因，这里显式并上。
		if ctx.Err() != nil {
			return &Error{Op: "ping", Err: errors.Join(err, ctx.Err())}
		}
		return wrapErr("ping", "", err)
	}
	return nil
}

// Close 释放客户端持有的连接，阻塞至传输层关闭完成或 ctx 到期。
//
// 低级客户端与类型化客户端共享同一传输层，因此 Close 同时作用于两者；
// 调用后不要再使用 Typed 或 ES 返回的客户端。
//
// Close 是幂等的：重复调用返回首次调用的结果，不 panic。
// 这一点与底层 go-elasticsearch 不同——它的 Close 第二次调用会返回 ErrAlreadyClosed。
func (c *Client) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		if err := c.es.Close(ctx); err != nil {
			c.closeErr = &Error{Op: "close", Err: err}
		}
	})
	return c.closeErr
}

// Typed 返回底层的类型化客户端，供本包未覆盖的操作使用。
//
// 返回的客户端与本 Client 共享传输层，不要单独关闭它。
func (c *Client) Typed() *elasticsearch.TypedClient { return c.typed }

// ES 返回底层的低级客户端，供本包未覆盖的操作使用。
//
// 返回的客户端与本 Client 共享传输层，不要单独关闭它。
func (c *Client) ES() *elasticsearch.Client { return c.es }
