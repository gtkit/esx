package esx

import (
	"crypto/tls"
	"time"
)

// 传输层默认值。调用方不配置也不应该裸奔，所以这些值都非零。
const (
	defaultRequestTimeout = 5 * time.Second
	defaultKeepAlive      = 30 * time.Second
)

// Option 配置客户端。用 With* 函数获得。
type Option func(*config)

// config 汇总解析后的客户端设置。
//
// 它不直接复用 elasticsearch.Config：本包需要配置请求超时、拨号超时与 TLS 最低版本，
// 这些只能落在自建的 http.Transport 上，v9 的 Option 体系没有对应入口。
type config struct {
	addresses      []string
	username       string
	password       string
	apiKey         string
	caCert         []byte
	maxRetries     int
	retryOnStatus  []int
	requestTimeout time.Duration
	insecureTLS    bool
	ping           bool
}

// WithAddresses 设置 Elasticsearch 节点地址。多个地址启用轮询与故障转移。
//
// 未设置时沿用 go-elasticsearch 的回退顺序：环境变量 ELASTICSEARCH_URL，其后
// http://localhost:9200。
func WithAddresses(addrs ...string) Option {
	return func(c *config) { c.addresses = append(c.addresses, addrs...) }
}

// WithBasicAuth 配置用户名密码认证。
func WithBasicAuth(username, password string) Option {
	return func(c *config) {
		c.username = username
		c.password = password
	}
}

// WithAPIKey 配置 base64 编码的 API Key 认证。
func WithAPIKey(key string) Option {
	return func(c *config) { c.apiKey = key }
}

// WithCACert 配置校验服务端证书用的 CA 证书（PEM 内容）。
func WithCACert(cert []byte) Option {
	return func(c *config) { c.caCert = cert }
}

// WithRetry 配置最大重试次数，以及触发重试的 HTTP 状态码。
// 不给状态码时沿用 go-elasticsearch 的默认值（502、503、504）。
func WithRetry(maxRetries int, onStatus ...int) Option {
	return func(c *config) {
		c.maxRetries = maxRetries
		c.retryOnStatus = onStatus
	}
}

// WithRequestTimeout 设置单次请求的超时，同时用作拨号与 TLS 握手的超时。
// 它也约束 WithPing 的连通性校验。非正值被忽略。
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithInsecureSkipVerify 跳过服务端 TLS 证书校验。
//
// 仅限本地开发调试：启用后连接可被中间人截获。TLS 最低版本仍固定为 1.2，
// 本包不提供下调它的入口。
func WithInsecureSkipVerify() Option {
	return func(c *config) { c.insecureTLS = true }
}

// WithPing 让 New 在返回前做一次连通性校验。
//
// 校验失败时 New 返回错误而不是一个不可用的客户端，使集群不可达、认证失败、
// 服务端大版本不匹配这类问题在启动时暴露，而不是拖到第一次查询。
func WithPing() Option {
	return func(c *config) { c.ping = true }
}

func newConfig(opts []Option) *config {
	c := &config{requestTimeout: defaultRequestTimeout}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// tlsConfig 构造 TLS 设置。最低版本固定 TLS 1.2，没有下调入口。
func (c *config) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.insecureTLS, //nolint:gosec // 由 WithInsecureSkipVerify 显式开启，其 GoDoc 已标注仅限开发调试
	}
}
