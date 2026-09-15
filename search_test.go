package esx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/gtkit/esx"
)

const twoHits = `{"hits":{"total":{"value":2,"relation":"eq"},"hits":[
	{"_id":"1","_score":1.5,"_source":{"id":"1","status":"paid"},"highlight":{"status":["<em>paid</em>"]}},
	{"_id":"2","_score":1.0,"_source":{"id":"2","status":"new"}}]}}`

func TestSearchRequestShape(t *testing.T) {
	t.Parallel()

	c, s := stubES(t, map[string]route{"/_search": {body: emptyHits}})

	_, err := esx.NewSearch[order](c, "orders").
		Must(esx.Match("title", "手机")).
		Must(esx.Term("brand", "x")).
		Filter(esx.Term("tenant", 7)).
		MustNot(esx.Exists("deleted_at")).
		Should(esx.Prefix("sku", "A"), esx.Prefix("sku", "B")).
		MinimumShouldMatch(1).
		Sort("created_at", false).
		Sort("id", true).
		Page(2, 20).
		Highlight("status").
		Select("id", "status").
		Exclude("secret").
		Do(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("/_search"))

	t.Run("同类子句累积", func(t *testing.T) {
		must, ok := dig(body, "query", "bool", "must").([]any)
		if !ok || len(must) != 2 {
			t.Errorf("must 应累积为 2 条，实得 %#v", dig(body, "query", "bool", "must"))
		}
	})

	t.Run("minimum_should_match", func(t *testing.T) {
		if got := dig(body, "query", "bool", "minimum_should_match"); got != "1" {
			t.Errorf("want \"1\", got %#v", got)
		}
	})

	t.Run("多级排序按调用顺序", func(t *testing.T) {
		sorts, ok := body["sort"].([]any)
		if !ok || len(sorts) != 2 {
			t.Fatalf("sort 应为 2 元素，实得 %#v", body["sort"])
		}
		first, _ := sorts[0].(map[string]any)
		second, _ := sorts[1].(map[string]any)
		if dig(first, "created_at", "order") != "desc" {
			t.Errorf("第一级应为 created_at desc，实得 %#v", first)
		}
		if dig(second, "id", "order") != "asc" {
			t.Errorf("第二级应为 id asc，实得 %#v", second)
		}
	})

	t.Run("分页", func(t *testing.T) {
		if body["from"] != float64(20) || body["size"] != float64(20) {
			t.Errorf("want from=20 size=20, got from=%#v size=%#v", body["from"], body["size"])
		}
	})

	t.Run("高亮为 v9 的数组形状", func(t *testing.T) {
		fields, ok := dig(body, "highlight", "fields").([]any)
		if !ok || len(fields) != 1 {
			t.Fatalf("highlight.fields 应为单元素数组，实得 %#v", dig(body, "highlight", "fields"))
		}
		el, _ := fields[0].(map[string]any)
		if _, ok := el["status"]; !ok {
			t.Errorf("应含 status 键，实得 %#v", el)
		}
	})

	t.Run("_source 过滤", func(t *testing.T) {
		inc, _ := dig(body, "_source", "includes").([]any)
		exc, _ := dig(body, "_source", "excludes").([]any)
		if len(inc) != 2 || inc[0] != "id" || inc[1] != "status" {
			t.Errorf("includes 不符: %#v", inc)
		}
		if len(exc) != 1 || exc[0] != "secret" {
			t.Errorf("excludes 不符: %#v", exc)
		}
	})
}

func TestSearchOmitsUnsetStructures(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"/_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := decodeJSON(t, s.bodyWithSuffix("/_search"))

	for _, key := range []string{"highlight", "_source", "sort", "from", "size", "aggregations", "search_after"} {
		if _, ok := body[key]; ok {
			t.Errorf("未设置时不应生成 %q，实得 %#v", key, body[key])
		}
	}
}

func TestSearchPageNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		page, size       int
		wantFrom, wantSz float64
	}{
		{"页码 0 归一到第 1 页", 0, 20, 0, 20},
		{"页码负数归一到第 1 页", -5, 20, 0, 20},
		{"每页条数 0 用默认值", 1, 0, 0, 20},
		{"每页条数负数用默认值", 1, -3, 0, 20},
		{"正常分页", 3, 10, 20, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, s := stubES(t, map[string]route{"/_search": {body: emptyHits}})
			if _, err := esx.NewSearch[order](c, "orders").Page(tt.page, tt.size).Do(t.Context()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			body := decodeJSON(t, s.bodyWithSuffix("/_search"))
			if body["from"] != tt.wantFrom || body["size"] != tt.wantSz {
				t.Errorf("want from=%v size=%v, got from=%#v size=%#v",
					tt.wantFrom, tt.wantSz, body["from"], body["size"])
			}
		})
	}
}

func TestSearchDecodeResult(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{"/_search": {body: twoHits}})

	res, err := esx.NewSearch[order](c, "orders").Do(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Total != 2 || res.TotalRelation != "eq" {
		t.Errorf("want total=2 relation=eq, got %d %q", res.Total, res.TotalRelation)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(res.Hits))
	}
	if res.Hits[0].ID != "1" || res.Hits[0].Doc.Status != "paid" {
		t.Errorf("首条命中不符: %+v", res.Hits[0])
	}
	if res.Hits[0].Score == nil || *res.Hits[0].Score != 1.5 {
		t.Errorf("score 不符: %#v", res.Hits[0].Score)
	}
	if got := res.Hits[0].Highlight["status"]; len(got) != 1 || got[0] != "<em>paid</em>" {
		t.Errorf("高亮片段不符: %#v", res.Hits[0].Highlight)
	}
	if res.Hits[1].Highlight != nil {
		t.Errorf("无高亮的命中应为 nil，实得 %#v", res.Hits[1].Highlight)
	}

	docs := res.Docs()
	if len(docs) != 2 || docs[0].ID != "1" || docs[1].Status != "new" {
		t.Errorf("Docs() 不符: %#v", docs)
	}
}

func TestSearchEmptyResult(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{"/_search": {body: emptyHits}})

	res, err := esx.NewSearch[order](c, "orders").Do(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Total != 0 || len(res.Hits) != 0 || len(res.Docs()) != 0 {
		t.Errorf("应为空结果，实得 total=%d hits=%d", res.Total, len(res.Hits))
	}
}

func TestSearchDecodeFailure(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"/_search": {body: `{"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_id":"1","_source":{"id":123}}]}}`},
	})
	if _, err := esx.NewSearch[order](c, "orders").Do(t.Context()); err == nil {
		t.Fatal("want decode error, got nil")
	}
}

func TestSearchContextCancelled(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{"/_search": {body: emptyHits}, "/_count": {body: `{"count":0}`}})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	t.Run("Do", func(t *testing.T) {
		if _, err := esx.NewSearch[order](c, "orders").Do(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})
	t.Run("DoAgg", func(t *testing.T) {
		if _, err := esx.NewSearch[order](c, "orders").DoAgg(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})
	t.Run("Count", func(t *testing.T) {
		if _, err := esx.NewSearch[order](c, "orders").Count(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})
}

func TestSearchAggAndCount(t *testing.T) {
	t.Parallel()

	t.Run("DoAgg 把 size 置 0", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, map[string]route{
			"/_search": {body: `{"hits":{"total":{"value":0,"relation":"eq"},"hits":[]},"aggregations":{"by_status":{"buckets":[]}}}`},
		})
		agg := types.Aggregations{Terms: &types.TermsAggregation{Field: new("status")}}
		got, err := esx.NewSearch[order](c, "orders").Page(2, 50).Agg("by_status", agg).DoAgg(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/_search"))
		if body["size"] != float64(0) {
			t.Errorf("聚合请求的 size 应为 0，实得 %#v", body["size"])
		}
		if dig(body, "aggregations", "by_status") == nil {
			t.Errorf("聚合未进入请求: %#v", body)
		}
		if _, ok := got["by_status"]; !ok {
			t.Errorf("聚合结果未返回: %#v", got)
		}
	})

	t.Run("同名聚合后者覆盖前者", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, map[string]route{"/_search": {body: emptyHits}})
		first := types.Aggregations{Terms: &types.TermsAggregation{Field: new("status")}}
		second := types.Aggregations{Terms: &types.TermsAggregation{Field: new("brand")}}
		_, err := esx.NewSearch[order](c, "orders").Agg("a", first).Agg("a", second).DoAgg(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/_search"))
		if got := dig(body, "aggregations", "a", "terms", "field"); got != "brand" {
			t.Errorf("后者应覆盖前者，实得 %#v", got)
		}
	})

	t.Run("Count 复用查询条件且不受分页影响", func(t *testing.T) {
		t.Parallel()
		c, s := stubES(t, map[string]route{"/_count": {body: `{"count":42}`}})
		n, err := esx.NewSearch[order](c, "orders").
			Filter(esx.Term("tenant", 7)).
			Sort("id", true).
			Page(3, 10).
			Count(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 42 {
			t.Errorf("want 42, got %d", n)
		}
		body := decodeJSON(t, s.bodyWithSuffix("/_count"))
		if dig(body, "query", "bool", "filter") == nil {
			t.Errorf("Count 应复用查询条件: %#v", body)
		}
		for _, key := range []string{"from", "size", "sort"} {
			if _, ok := body[key]; ok {
				t.Errorf("Count 不应携带 %q: %#v", key, body)
			}
		}
	})
}

func TestSearchOffsetAndSearchAfter(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"/_search": {body: emptyHits}})

	_, err := esx.NewSearch[order](c, "orders").
		Offset(100, 25).
		SearchAfter(types.FieldValue("2026-09-15"), types.FieldValue("id-1")).
		Do(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("/_search"))
	if body["from"] != float64(100) || body["size"] != float64(25) {
		t.Errorf("Offset 未生效: from=%#v size=%#v", body["from"], body["size"])
	}
	after, ok := body["search_after"].([]any)
	if !ok || len(after) != 2 {
		t.Fatalf("search_after 应为 2 元素，实得 %#v", body["search_after"])
	}
}
