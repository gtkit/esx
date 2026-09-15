package esx_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gtkit/esx"
)

func TestNewDefaults(t *testing.T) {
	t.Parallel()

	// 不传地址也应构造成功：走 go-elasticsearch 的地址回退。
	c, err := esx.New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(t.Context()) })

	if c.Typed() == nil {
		t.Error("Typed() 不应为 nil")
	}
	if c.ES() == nil {
		t.Error("ES() 不应为 nil")
	}
}

func TestNewDoesNotRequestWithoutPing(t *testing.T) {
	t.Parallel()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := esx.New(esx.WithAddresses(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(t.Context()) })

	if hits != 0 {
		t.Errorf("未启用 Ping 时不应发出请求，实得 %d 次", hits)
	}
}

func TestWithPing(t *testing.T) {
	t.Parallel()

	t.Run("集群可达时构造成功", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Elastic-Product", "Elasticsearch")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"n","cluster_name":"c","version":{"number":"9.0.0"},"tagline":"You Know, for Search"}`))
		}))
		t.Cleanup(srv.Close)

		c, err := esx.New(esx.WithAddresses(srv.URL), esx.WithPing())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_ = c.Close(t.Context())
	})

	t.Run("集群不可达时构造失败", func(t *testing.T) {
		t.Parallel()
		// 立即关闭，制造连不上的地址。
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		addr := srv.URL
		srv.Close()

		if _, err := esx.New(esx.WithAddresses(addr), esx.WithPing()); err == nil {
			t.Fatal("want error, got nil")
		}
	})

	t.Run("超时受请求超时约束", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
			}
		}))
		t.Cleanup(srv.Close)

		_, err := esx.New(
			esx.WithAddresses(srv.URL),
			esx.WithRequestTimeout(150*time.Millisecond),
			esx.WithPing(),
		)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want DeadlineExceeded, got %v", err)
		}
	})

	t.Run("服务端报错时构造失败", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Elastic-Product", "Elasticsearch")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"security_exception","reason":"denied"},"status":401}`))
		}))
		t.Cleanup(srv.Close)

		if _, err := esx.New(esx.WithAddresses(srv.URL), esx.WithPing()); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func TestAuthOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opt    esx.Option
		header string
		check  func(t *testing.T, value string)
	}{
		{
			name:   "WithBasicAuth",
			opt:    esx.WithBasicAuth("user", "pass"),
			header: "Authorization",
			check: func(t *testing.T, v string) {
				if !strings.HasPrefix(v, "Basic ") {
					t.Errorf("want Basic 前缀，got %q", v)
				}
			},
		},
		{
			name:   "WithAPIKey",
			opt:    esx.WithAPIKey("abc123"),
			header: "Authorization",
			check: func(t *testing.T, v string) {
				if !strings.HasPrefix(v, "APIKey ") {
					t.Errorf("want APIKey 前缀，got %q", v)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case got <- r.Header.Get(tt.header):
				default:
				}
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(emptyHits))
			}))
			t.Cleanup(srv.Close)

			c, err := esx.New(esx.WithAddresses(srv.URL), tt.opt)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			t.Cleanup(func() { _ = c.Close(t.Context()) })

			if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err != nil {
				t.Fatalf("search: %v", err)
			}
			tt.check(t, <-got)
		})
	}
}

func TestWithRetry(t *testing.T) {
	t.Parallel()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"type":"unavailable","reason":"retry"},"status":503}`))
			return
		}
		_, _ = w.Write([]byte(emptyHits))
	}))
	t.Cleanup(srv.Close)

	c, err := esx.New(esx.WithAddresses(srv.URL), esx.WithRetry(3, http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(t.Context()) })

	if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if attempts < 3 {
		t.Errorf("应重试到第 3 次，实得 %d 次", attempts)
	}
}

func TestOptionOverride(t *testing.T) {
	t.Parallel()

	// 同一选项重复传入，后者生效：先设极短超时再设足够长的，请求应当成功。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(emptyHits))
	}))
	t.Cleanup(srv.Close)

	c, err := esx.New(
		esx.WithAddresses(srv.URL),
		esx.WithRequestTimeout(10*time.Millisecond),
		esx.WithRequestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(t.Context()) })

	if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err != nil {
		t.Fatalf("后传的 5s 超时应生效: %v", err)
	}
}

func TestWithRequestTimeoutIgnoresNonPositive(t *testing.T) {
	t.Parallel()

	// 非正值被忽略，保持默认值，构造不应失败。
	for _, d := range []time.Duration{0, -time.Second} {
		c, err := esx.New(esx.WithRequestTimeout(d))
		if err != nil {
			t.Fatalf("d=%v: unexpected error: %v", d, err)
		}
		_ = c.Close(t.Context())
	}
}

func TestWithCACertInvalid(t *testing.T) {
	t.Parallel()

	// 非法 CA 证书应让构造失败，而不是产出一个连不上的客户端。
	if _, err := esx.New(esx.WithAddresses("https://example.test"), esx.WithCACert([]byte("not a cert"))); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestWithInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(emptyHits))
	}))
	t.Cleanup(srv.Close)

	t.Run("未启用时自签证书被拒", func(t *testing.T) {
		c, err := esx.New(esx.WithAddresses(srv.URL))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Cleanup(func() { _ = c.Close(t.Context()) })
		if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err == nil {
			t.Fatal("自签证书应被拒绝")
		}
	})

	t.Run("启用后可连通", func(t *testing.T) {
		c, err := esx.New(esx.WithAddresses(srv.URL), esx.WithInsecureSkipVerify())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Cleanup(func() { _ = c.Close(t.Context()) })
		if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err != nil {
			t.Fatalf("启用后应连通: %v", err)
		}
	})
}

func TestErrorFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *esx.Error
		want []string
	}{
		{
			name: "含索引与状态码",
			err:  &esx.Error{Op: "get document", Index: "orders", StatusCode: 404, Err: esx.ErrNotFound},
			want: []string{"get document", "orders", "404"},
		},
		{
			name: "只有索引",
			err:  &esx.Error{Op: "create index", Index: "orders", Err: errors.New("boom")},
			want: []string{"create index", "orders", "boom"},
		},
		{
			name: "只有状态码",
			err:  &esx.Error{Op: "ping", StatusCode: 500, Err: errors.New("boom")},
			want: []string{"ping", "500"},
		},
		{
			name: "两者都无",
			err:  &esx.Error{Op: "close", Err: errors.New("boom")},
			want: []string{"close", "boom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.err.Error()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("错误信息应含 %q，实得 %q", w, got)
				}
			}
			if !strings.HasPrefix(got, "esx: ") {
				t.Errorf("应以 esx: 开头，实得 %q", got)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	t.Parallel()

	inner := errors.New("inner")
	err := &esx.Error{Op: "op", Err: inner}

	if !errors.Is(err, inner) {
		t.Error("errors.Is 应能匹配到底层错误")
	}
	if got := err.Unwrap(); got != inner {
		t.Errorf("Unwrap 应返回底层错误，实得 %v", got)
	}

	wrapped := fmt.Errorf("outer: %w", err)
	e, ok := errors.AsType[*esx.Error](wrapped)
	if !ok || e.Op != "op" {
		t.Errorf("应能从包装链中取出 *esx.Error，实得 %#v", e)
	}
}
