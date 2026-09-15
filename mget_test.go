package esx_test

import (
	"errors"
	"testing"

	"github.com/gtkit/esx"
)

type mgetDoc struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestMGetSingleRequest(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{
		"_mget": {body: `{"docs":[
			{"_index":"orders","_id":"1","found":true,"_source":{"id":"1","name":"甲"}},
			{"_index":"orders","_id":"2","found":true,"_source":{"id":"2","name":"乙"}},
			{"_index":"orders","_id":"3","found":true,"_source":{"id":"3","name":"丙"}}
		]}`},
	})

	docs, err := esx.MGet[mgetDoc](t.Context(), c, "orders", "1", "2", "3")
	if err != nil {
		t.Fatalf("MGet 失败: %v", err)
	}
	if n := s.count(); n != 1 {
		t.Errorf("应只发出 1 次请求，实得 %d", n)
	}
	if len(docs) != 3 {
		t.Fatalf("应取回 3 个文档，实得 %d", len(docs))
	}
	if docs["2"].Name != "乙" {
		t.Errorf("id=2 的文档不符: %+v", docs["2"])
	}

	// 请求体应带全部 id
	body := decodeJSON(t, s.bodyWithSuffix("_mget"))
	ids, ok := body["ids"].([]any)
	if !ok || len(ids) != 3 {
		t.Fatalf("请求体 ids 应为 3 元素数组，实得 %#v", body["ids"])
	}
}

func TestMGetPartiallyMissing(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"_mget": {body: `{"docs":[
			{"_index":"orders","_id":"1","found":true,"_source":{"id":"1","name":"甲"}},
			{"_index":"orders","_id":"2","found":false},
			{"_index":"orders","_id":"3","found":false}
		]}`},
	})

	docs, err := esx.MGet[mgetDoc](t.Context(), c, "orders", "1", "2", "3")
	if err != nil {
		t.Fatalf("部分缺失不应返回错误: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("应只含 1 个文档，实得 %d: %+v", len(docs), docs)
	}
	if _, ok := docs["2"]; ok {
		t.Error("不存在的 id 不应出现在结果中")
	}
}

func TestMGetEmptyIDsSkipsRequest(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, nil)

	docs, err := esx.MGet[mgetDoc](t.Context(), c, "orders")
	if err != nil {
		t.Fatalf("空 id 列表不应报错: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("应返回空结果，实得 %+v", docs)
	}
	if n := s.count(); n != 0 {
		t.Errorf("空 id 列表不应发出请求，实得 %d 次", n)
	}
}

func TestMGetRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		index string
		ids   []string
	}{
		{"空索引名", "", []string{"1"}},
		{"id 含空字符串", "orders", []string{"1", "", "3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, s := stubES(t, nil)
			if _, err := esx.MGet[mgetDoc](t.Context(), c, tt.index, tt.ids...); err == nil {
				t.Fatal("应返回错误")
			}
			if n := s.count(); n != 0 {
				t.Errorf("不应发出请求，实得 %d 次", n)
			}
		})
	}
}

func TestMGetItemErrorNotSwallowed(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"_mget": {body: `{"docs":[
			{"_index":"orders","_id":"1","found":true,"_source":{"id":"1","name":"甲"}},
			{"_index":"missing","_id":"2","error":{"type":"index_not_found_exception","reason":"no such index [missing]"}}
		]}`},
	})

	_, err := esx.MGet[mgetDoc](t.Context(), c, "orders", "1", "2")
	if err == nil {
		t.Fatal("条目级错误应返回错误，不应静默丢弃")
	}
	var e *esx.Error
	if !errors.As(err, &e) {
		t.Fatalf("应为 *esx.Error，实得 %T", err)
	}
	if e.Op != "mget" {
		t.Errorf("Op 应为 mget，实得 %q", e.Op)
	}
}

func TestMGetDecodeFailure(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"_mget": {body: `{"docs":[
			{"_index":"orders","_id":"1","found":true,"_source":{"id":12345}}
		]}`},
	})

	if _, err := esx.MGet[mgetDoc](t.Context(), c, "orders", "1"); err == nil {
		t.Fatal("_source 无法解码为 T 时应返回错误")
	}
}
