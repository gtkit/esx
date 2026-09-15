package esx_test

import (
	"cmp"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gtkit/esx"
)

// route 是一条应答。status 为 0 时按 200 处理。
type route struct {
	status int
	body   string
}

// stubES 起一个假 Elasticsearch：按请求路径后缀选应答，其余走默认应答，
// 并记录收到的每个请求供断言。
//
// 用 httptest.NewServer 而非 NewTestServer：后者默认走内存网络，而 esx 装的是自建的
// http.Transport（带真实 dialer），够不到内存 server。
func stubES(t *testing.T, routes map[string]route, opts ...esx.Option) (*esx.Client, *seen) {
	t.Helper()
	s := &seen{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.record(r.Method, r.URL.Path, string(body))

		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")

		// 按后缀长度降序匹配，最长者优先：/_alias/orders 同时也以 /orders 结尾，
		// 直接遍历 map 会因为 Go 的随机遍历顺序产生 flaky 结果。
		for _, suffix := range longestFirst(routes) {
			if strings.HasSuffix(r.URL.Path, suffix) {
				rt := routes[suffix]
				if rt.status != 0 {
					w.WriteHeader(rt.status)
				}
				_, _ = w.Write([]byte(rt.body))
				return
			}
		}
		_, _ = w.Write([]byte(`{"acknowledged":true}`))
	}))
	t.Cleanup(srv.Close)

	c, err := esx.New(append([]esx.Option{esx.WithAddresses(srv.URL)}, opts...)...)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(t.Context()) })
	return c, s
}

// longestFirst 返回按长度降序排列的路由后缀，同长度按字典序，保证匹配结果确定。
func longestFirst(routes map[string]route) []string {
	keys := slices.Collect(maps.Keys(routes))
	slices.SortFunc(keys, func(a, b string) int {
		if n := cmp.Compare(len(b), len(a)); n != 0 {
			return n
		}
		return cmp.Compare(a, b)
	})
	return keys
}

type request struct{ Method, Path, Body string }

type seen struct {
	mu   sync.Mutex
	reqs []request
}

func (s *seen) record(method, path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, request{method, path, body})
}

func (s *seen) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// bodyWithSuffix 返回最后一个路径以 suffix 结尾的请求体。
func (s *seen) bodyWithSuffix(suffix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range slices.Backward(s.reqs) {
		if strings.HasSuffix(v.Path, suffix) {
			return v.Body
		}
	}
	return ""
}

// decodeJSON 把请求体解成映射，便于断言结构而非比对字符串——
// Go 的映射序列化顺序不保证，字面比对会随机失败。
func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("解析 JSON 失败: %v\n原文: %s", err, raw)
	}
	return m
}

// dig 按路径取嵌套值，取不到返回 nil。
func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, k := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		if cur, ok = obj[k]; !ok {
			return nil
		}
	}
	return cur
}

// queryJSON 把一个查询构造器的产物解成映射，用于结构断言。
func queryJSON(t *testing.T, q any) map[string]any {
	t.Helper()
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("编码查询失败: %v", err)
	}
	return decodeJSON(t, string(data))
}

const emptyHits = `{"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`

type order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
