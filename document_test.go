package esx_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gtkit/esx"
)

func TestGetDoc(t *testing.T) {
	t.Parallel()

	t.Run("命中并解码", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/orders/_doc/1": {body: `{"_index":"orders","_id":"1","found":true,"_source":{"id":"1","status":"paid"}}`},
		})
		got, err := esx.GetDoc[order](t.Context(), c, "orders", "1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "1" || got.Status != "paid" {
			t.Errorf("解码结果不符: %+v", got)
		}
	})

	t.Run("不存在映射为 ErrNotFound", func(t *testing.T) {
		t.Parallel()
		// typedapi 把 404 当正常响应解码，靠 found 区分，不返回错误。
		c, _ := stubES(t, map[string]route{
			"/orders/_doc/missing": {status: http.StatusNotFound, body: `{"_index":"orders","_id":"missing","found":false}`},
		})
		_, err := esx.GetDoc[order](t.Context(), c, "orders", "missing")
		if !errors.Is(err, esx.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("解码失败不匹配 ErrNotFound", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/orders/_doc/1": {body: `{"_index":"orders","_id":"1","found":true,"_source":{"id":12345}}`},
		})
		_, err := esx.GetDoc[order](t.Context(), c, "orders", "1")
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if errors.Is(err, esx.ErrNotFound) {
			t.Errorf("解码失败不应匹配 ErrNotFound: %v", err)
		}
	})

	t.Run("服务端错误携带状态码", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/orders/_doc/1": {
				status: http.StatusInternalServerError,
				body:   `{"error":{"type":"internal","reason":"boom"},"status":500}`,
			},
		})
		_, err := esx.GetDoc[order](t.Context(), c, "orders", "1")
		e, ok := errors.AsType[*esx.Error](err)
		if !ok {
			t.Fatalf("want *esx.Error, got %T: %v", err, err)
		}
		if e.StatusCode != http.StatusInternalServerError {
			t.Errorf("want status 500, got %d", e.StatusCode)
		}
		if e.Index != "orders" {
			t.Errorf("want index orders, got %q", e.Index)
		}
	})
}

// 前置校验契约：参数非法时不发出请求。只断言返回了错误不足以证伪该契约，
// 必须一并断言请求数没有增加。
func TestDocumentPreflightRejectsWithoutRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(c *esx.Client) error
	}{
		{"GetDoc 空索引", func(c *esx.Client) error {
			_, err := esx.GetDoc[order](t.Context(), c, "", "1")
			return err
		}},
		{"GetDoc 空 id", func(c *esx.Client) error {
			_, err := esx.GetDoc[order](t.Context(), c, "orders", "")
			return err
		}},
		{"IndexDoc 空索引", func(c *esx.Client) error {
			return c.IndexDoc(t.Context(), "", "1", order{})
		}},
		{"IndexDoc 空 id", func(c *esx.Client) error {
			return c.IndexDoc(t.Context(), "orders", "", order{})
		}},
		{"UpdateDoc 空索引", func(c *esx.Client) error {
			return c.UpdateDoc(t.Context(), "", "1", order{})
		}},
		{"UpdateDoc 空 id", func(c *esx.Client) error {
			return c.UpdateDoc(t.Context(), "orders", "", order{})
		}},
		{"DeleteDoc 空索引", func(c *esx.Client) error {
			return c.DeleteDoc(t.Context(), "", "1")
		}},
		{"DeleteDoc 空 id", func(c *esx.Client) error {
			return c.DeleteDoc(t.Context(), "orders", "")
		}},
		{"DocExists 空索引", func(c *esx.Client) error {
			_, err := c.DocExists(t.Context(), "", "1")
			return err
		}},
		{"DocExists 空 id", func(c *esx.Client) error {
			_, err := c.DocExists(t.Context(), "orders", "")
			return err
		}},
		{"IndexExists 空索引", func(c *esx.Client) error {
			_, err := c.IndexExists(t.Context(), "")
			return err
		}},
		{"CreateIndex 空索引", func(c *esx.Client) error {
			return c.CreateIndex(t.Context(), "")
		}},
		{"DeleteIndices 含空串", func(c *esx.Client) error {
			return c.DeleteIndices(t.Context(), "orders", "")
		}},
		{"ResolveAlias 空别名", func(c *esx.Client) error {
			_, err := c.ResolveAlias(t.Context(), "")
			return err
		}},
		{"SwitchAlias 空别名", func(c *esx.Client) error {
			return c.SwitchAlias(t.Context(), esx.AliasState{}, "orders-v2")
		}},
		{"SwitchAlias 空新索引", func(c *esx.Client) error {
			return c.SwitchAlias(t.Context(), esx.AliasState{Name: "orders"}, "")
		}},
		{"CreateIndex mapping 非法 JSON", func(c *esx.Client) error {
			return c.CreateIndex(t.Context(), "orders", esx.WithIndexMapping([]byte("{not json")))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, s := stubES(t, nil)
			before := s.count()
			if err := tt.call(c); err == nil {
				t.Fatal("want error, got nil")
			}
			if after := s.count(); after != before {
				t.Errorf("不应发出请求，请求数由 %d 变为 %d", before, after)
			}
		})
	}
}

func TestIndexUpdateDeleteDoc(t *testing.T) {
	t.Parallel()

	t.Run("IndexDoc 写入文档体", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, map[string]route{"/orders/_doc/1": {body: `{"result":"created"}`}})
		if err := c.IndexDoc(t.Context(), "orders", "1", order{ID: "1", Status: "paid"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/orders/_doc/1"))
		if body["id"] != "1" || body["status"] != "paid" {
			t.Errorf("请求体不符: %#v", body)
		}
	})

	t.Run("UpdateDoc 包一层 doc", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, map[string]route{"/_update/1": {body: `{"result":"updated"}`}})
		if err := c.UpdateDoc(t.Context(), "orders", "1", map[string]any{"status": "shipped"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/_update/1"))
		if got := dig(body, "doc", "status"); got != "shipped" {
			t.Errorf("应包一层 doc，实得 %#v", body)
		}
	})

	t.Run("UpdateDoc 不存在映射为 ErrNotFound", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{
			"/_update/missing": {
				status: http.StatusNotFound,
				body:   `{"error":{"type":"document_missing_exception","reason":"missing"},"status":404}`,
			},
		})
		err := c.UpdateDoc(t.Context(), "orders", "missing", map[string]any{"a": 1})
		if !errors.Is(err, esx.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("DeleteDoc 成功", func(t *testing.T) {
		t.Parallel()
		c, _ := stubES(t, map[string]route{"/orders/_doc/1": {body: `{"result":"deleted"}`}})
		if err := c.DeleteDoc(t.Context(), "orders", "1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DeleteDoc 不存在映射为 ErrNotFound", func(t *testing.T) {
		t.Parallel()
		// typedapi 对删除的 404 同样当正常响应，靠 result 区分。
		c, _ := stubES(t, map[string]route{
			"/orders/_doc/missing": {status: http.StatusNotFound, body: `{"result":"not_found"}`},
		})
		err := c.DeleteDoc(t.Context(), "orders", "missing")
		if !errors.Is(err, esx.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestDocExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   bool
		hasErr bool
	}{
		{"文档存在", http.StatusOK, true, false},
		{"文档不存在", http.StatusNotFound, false, false},
		{"索引不存在", http.StatusNotFound, false, false},
		{"服务端错误", http.StatusInternalServerError, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := stubES(t, map[string]route{"/orders/_doc/1": {status: tt.status, body: `{}`}})
			got, err := c.DocExists(t.Context(), "orders", "1")
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
