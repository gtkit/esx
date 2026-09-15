package esx_test

import (
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/calendarinterval"
	"github.com/gtkit/esx"
)

func TestTermsAgg(t *testing.T) {
	t.Parallel()
	m := queryJSON(t, esx.TermsAgg("status", 5))
	if got := dig(m, "terms", "field"); got != "status" {
		t.Errorf("field 应为 status，实得 %#v", got)
	}
	if got := dig(m, "terms", "size"); got != float64(5) {
		t.Errorf("size 应为 5，实得 %#v", got)
	}
}

func TestTermsAggNonPositiveSizeOmitted(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, -1} {
		m := queryJSON(t, esx.TermsAgg("status", size))
		if got := dig(m, "terms", "size"); got != nil {
			t.Errorf("size=%d 时不应写入 size，实得 %#v", size, got)
		}
		if got := dig(m, "terms", "field"); got != "status" {
			t.Errorf("size=%d 时 field 仍应存在，实得 %#v", size, got)
		}
	}
}

func TestDateHistogramAgg(t *testing.T) {
	t.Parallel()
	m := queryJSON(t, esx.DateHistogramAgg("created_at", calendarinterval.Day))
	if got := dig(m, "date_histogram", "field"); got != "created_at" {
		t.Errorf("field 应为 created_at，实得 %#v", got)
	}
	if got := dig(m, "date_histogram", "calendar_interval"); got != "day" {
		t.Errorf("calendar_interval 应为 day，实得 %#v", got)
	}
}

func TestSingleValueAggs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		agg  types.Aggregations
		key  string
	}{
		{"平均值", esx.AvgAgg("amount"), "avg"},
		{"求和", esx.SumAgg("amount"), "sum"},
		{"最小值", esx.MinAgg("amount"), "min"},
		{"最大值", esx.MaxAgg("amount"), "max"},
		{"去重计数", esx.CardinalityAgg("user_id"), "cardinality"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := queryJSON(t, tt.agg)
			if len(m) != 1 {
				t.Fatalf("应只含一个聚合类型，实得 %#v", m)
			}
			inner, ok := m[tt.key].(map[string]any)
			if !ok {
				t.Fatalf("应含 %s，实得 %#v", tt.key, m)
			}
			if len(inner) != 1 {
				t.Errorf("%s 应只含 field 一个参数，实得 %#v", tt.key, inner)
			}
		})
	}
}

// TestAggIntegratesWithSearch 验证构造器产物能直接交给 Agg 并进入请求体。
func TestAggIntegratesWithSearch(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{
		"_search": {body: `{"hits":{"total":{"value":0,"relation":"eq"},"hits":[]},"aggregations":{}}`},
	})

	if _, err := esx.NewSearch[mgetDoc](c, "orders").
		Agg("by_status", esx.TermsAgg("status", 5)).
		DoAgg(t.Context()); err != nil {
		t.Fatalf("DoAgg 失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_search"))
	if got := dig(body, "aggregations", "by_status", "terms", "field"); got != "status" {
		t.Errorf("请求体中聚合字段不符，实得 %#v", got)
	}
}
