package esx_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gtkit/esx"
)

func TestIndexExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   bool
		hasErr bool
	}{
		{"存在", http.StatusOK, true, false},
		{"不存在返回 false 而非错误", http.StatusNotFound, false, false},
		{"服务端错误", http.StatusInternalServerError, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := stubES(t, map[string]route{"/orders": {status: tt.status, body: `{}`}})
			got, err := c.IndexExists(t.Context(), "orders")
			if tt.hasErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("want %v, got %v", tt.want, got)
			}
		})
	}
}

func TestCreateIndex(t *testing.T) {
	t.Parallel()

	t.Run("无 mapping 无别名时不带请求体", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		if err := c.CreateIndex(t.Context(), "orders"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body := s.bodyWithSuffix("/orders"); body != "" {
			t.Errorf("不应带请求体，实得 %q", body)
		}
	})

	t.Run("带 mapping", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		mapping := []byte(`{"mappings":{"properties":{"status":{"type":"keyword"}}}}`)
		if err := c.CreateIndex(t.Context(), "orders", esx.WithIndexMapping(mapping)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/orders"))
		if got := dig(body, "mappings", "properties", "status", "type"); got != "keyword" {
			t.Errorf("mapping 未透传: %#v", body)
		}
	})

	t.Run("带别名并标记为写索引", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		if err := c.CreateIndex(t.Context(), "orders-v1", esx.WithIndexAliases("orders")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/orders-v1"))
		if got := dig(body, "aliases", "orders", "is_write_index"); got != true {
			t.Errorf("别名应标记为写索引: %#v", body)
		}
	})

	t.Run("mapping 与别名并存", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		mapping := []byte(`{"mappings":{"properties":{"status":{"type":"keyword"}}}}`)
		err := c.CreateIndex(t.Context(), "orders-v1",
			esx.WithIndexMapping(mapping), esx.WithIndexAliases("orders"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/orders-v1"))
		if got := dig(body, "mappings", "properties", "status", "type"); got != "keyword" {
			t.Errorf("mapping 丢失: %#v", body)
		}
		if got := dig(body, "aliases", "orders", "is_write_index"); got != true {
			t.Errorf("别名丢失: %#v", body)
		}
	})

	t.Run("索引已存在返回带状态码的错误", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/orders": {
				status: http.StatusBadRequest,
				body:   `{"error":{"type":"resource_already_exists_exception","reason":"exists"},"status":400}`,
			},
		})
		err := c.CreateIndex(t.Context(), "orders")
		e, ok := errors.AsType[*esx.Error](err)
		if !ok {
			t.Fatalf("want *esx.Error, got %T: %v", err, err)
		}
		if e.StatusCode != http.StatusBadRequest {
			t.Errorf("want 400, got %d", e.StatusCode)
		}
	})
}

func TestDeleteIndices(t *testing.T) {
	t.Parallel()

	t.Run("空列表是无操作且不发请求", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		before := s.count()
		if err := c.DeleteIndices(t.Context()); err != nil {
			t.Fatalf("空列表不应报错: %v", err)
		}
		if after := s.count(); after != before {
			t.Errorf("不应发出请求，请求数由 %d 变为 %d", before, after)
		}
	})

	t.Run("多个索引合并为一次请求", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		if err := c.DeleteIndices(t.Context(), "a", "b"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		reqs := s.count()
		if reqs != 1 {
			t.Errorf("应只发一次请求，实得 %d", reqs)
		}
	})
}

func TestResolveAlias(t *testing.T) {
	t.Parallel()

	t.Run("别名指向多个索引且顺序确定", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_alias/orders": {body: `{"orders-v2":{"aliases":{"orders":{}}},"orders-v1":{"aliases":{"orders":{}}}}`},
		})
		// 连续解析两次，顺序必须一致，否则「顺序确定」这条契约不成立。
		first, err := c.ResolveAlias(t.Context(), "orders")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		second, err := c.ResolveAlias(t.Context(), "orders")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(first.Targets) != 2 {
			t.Fatalf("want 2 targets, got %#v", first.Targets)
		}
		if first.Targets[0] != "orders-v1" || first.Targets[1] != "orders-v2" {
			t.Errorf("应按名字升序，实得 %#v", first.Targets)
		}
		if first.Targets[0] != second.Targets[0] || first.Targets[1] != second.Targets[1] {
			t.Errorf("两次解析顺序不一致: %#v vs %#v", first.Targets, second.Targets)
		}
		if first.ConcreteIndex {
			t.Error("不应标记为同名具体索引")
		}
	})

	t.Run("别名不存在但同名具体索引存在", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_alias/orders": {
				status: http.StatusNotFound,
				body:   `{"error":{"type":"alias_not_found_exception","reason":"missing"},"status":404}`,
			},
			"/orders": {status: http.StatusOK, body: `{}`},
		})
		st, err := c.ResolveAlias(t.Context(), "orders")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !st.ConcreteIndex {
			t.Error("应标记为同名具体索引")
		}
		if len(st.Targets) != 1 || st.Targets[0] != "orders" {
			t.Errorf("want [orders], got %#v", st.Targets)
		}
	})

	t.Run("别名与同名索引都不存在", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_alias/nope": {
				status: http.StatusNotFound,
				body:   `{"error":{"type":"alias_not_found_exception","reason":"missing"},"status":404}`,
			},
			"/nope": {status: http.StatusNotFound, body: `{}`},
		})
		st, err := c.ResolveAlias(t.Context(), "nope")
		if err != nil {
			t.Fatalf("不存在不应报错: %v", err)
		}
		if len(st.Targets) != 0 || st.ConcreteIndex {
			t.Errorf("应为空状态，实得 %#v", st)
		}
	})

	t.Run("服务端错误原样返回", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_alias/orders": {
				status: http.StatusInternalServerError,
				body:   `{"error":{"type":"internal","reason":"boom"},"status":500}`,
			},
		})
		if _, err := c.ResolveAlias(t.Context(), "orders"); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func TestSwitchAlias(t *testing.T) {
	t.Parallel()

	t.Run("别名此前不存在时只有 add", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		err := c.SwitchAlias(t.Context(), esx.AliasState{Name: "orders"}, "orders-v1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		actions := aliasActions(t, s.bodyWithSuffix("/_aliases"))
		if len(actions) != 1 {
			t.Fatalf("want 1 action, got %d: %#v", len(actions), actions)
		}
		if _, ok := actions[0]["add"]; !ok {
			t.Errorf("唯一动作应为 add，实得 %#v", actions[0])
		}
	})

	t.Run("目标已是新索引时不摘掉自己", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, nil)
		state := esx.AliasState{Name: "orders", Targets: []string{"orders-v2"}}
		if err := c.SwitchAlias(t.Context(), state, "orders-v2"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		actions := aliasActions(t, s.bodyWithSuffix("/_aliases"))
		for _, a := range actions {
			if _, ok := a["remove"]; ok {
				t.Errorf("不应移除即将添加的同一索引: %#v", actions)
			}
		}
	})

	t.Run("服务端拒绝时返回错误", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_aliases": {
				status: http.StatusBadRequest,
				body:   `{"error":{"type":"illegal_argument_exception","reason":"bad"},"status":400}`,
			},
		})
		state := esx.AliasState{Name: "orders", Targets: []string{"orders-v1"}}
		if err := c.SwitchAlias(t.Context(), state, "orders-v2"); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}

func aliasActions(t *testing.T, body string) []map[string]any {
	t.Helper()
	m := decodeJSON(t, body)
	raw, ok := m["actions"].([]any)
	if !ok {
		t.Fatalf("actions 应为数组，实得 %#v", m)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, a := range raw {
		obj, ok := a.(map[string]any)
		if !ok {
			t.Fatalf("动作应为对象，实得 %#v", a)
		}
		out = append(out, obj)
	}
	return out
}
