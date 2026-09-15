package esx_test

import (
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/textquerytype"
	"github.com/gtkit/esx"
)

func TestAnySetsMinimumShouldMatch(t *testing.T) {
	t.Parallel()
	m := queryJSON(t, esx.Any(esx.Match("title", "手机"), esx.Match("body", "手机")))

	should, ok := dig(m, "bool", "should").([]any)
	if !ok || len(should) != 2 {
		t.Fatalf("should 应为 2 元素数组，实得 %#v", dig(m, "bool", "should"))
	}
	if got := dig(m, "bool", "minimum_should_match"); got != "1" {
		t.Errorf("minimum_should_match 应为 1，实得 %#v", got)
	}
}

// TestAnyKeepsSemanticsBesideFilter 是「Any 不依赖 ES 默认值」这条契约的反证测试：
// 若 Any 不自带 minimum_should_match，同级存在 filter 时该默认值为 0，
// should 就只参与打分而不构成过滤，本用例会失败。
func TestAnyKeepsSemanticsBesideFilter(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "orders").
		Filter(esx.Term("tenant_id", 7)).
		Must(esx.Any(esx.Match("title", "手机"), esx.Match("body", "手机"))).
		Do(t.Context()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_search"))
	must, ok := dig(body, "query", "bool", "must").([]any)
	if !ok || len(must) != 1 {
		t.Fatalf("must 应为 1 元素数组，实得 %#v", dig(body, "query", "bool", "must"))
	}
	inner, ok := must[0].(map[string]any)
	if !ok {
		t.Fatalf("must[0] 应为对象，实得 %#v", must[0])
	}
	if got := dig(inner, "bool", "minimum_should_match"); got != "1" {
		t.Errorf("嵌套 bool 应自带 minimum_should_match=1，实得 %#v", got)
	}
}

func TestAllAndNot(t *testing.T) {
	t.Parallel()
	a, b := esx.Match("title", "手机"), esx.Match("body", "手机")

	tests := []struct {
		name   string
		query  types.Query
		clause string
	}{
		{"All 归入 must", esx.All(a, b), "must"},
		{"Not 归入 must_not", esx.Not(a, b), "must_not"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := queryJSON(t, tt.query)
			clauses, ok := dig(m, "bool", tt.clause).([]any)
			if !ok || len(clauses) != 2 {
				t.Fatalf("%s 应为 2 元素数组，实得 %#v", tt.clause, dig(m, "bool", tt.clause))
			}
			if got := dig(m, "bool", "minimum_should_match"); got != nil {
				t.Errorf("%s 不应设置 minimum_should_match，实得 %#v", tt.clause, got)
			}
		})
	}
}

func TestCombinatorsWithNoClauses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query types.Query
	}{
		{"Any", esx.Any()},
		{"All", esx.All()},
		{"Not", esx.Not()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := queryJSON(t, tt.query)
			b, ok := m["bool"].(map[string]any)
			if !ok {
				t.Fatalf("应生成 bool 查询，实得 %#v", m)
			}
			if len(b) != 0 {
				t.Errorf("空子句时不应施加任何约束，实得 %#v", b)
			}
		})
	}
}

func TestMultiMatchOptions(t *testing.T) {
	t.Parallel()

	t.Run("指定类型与操作符", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.MultiMatch("张三 北京", []string{"name", "city"},
			esx.WithMultiMatchType(textquerytype.Crossfields),
			esx.WithMultiMatchOperator(operator.And)))
		if got := dig(m, "multi_match", "type"); got != "cross_fields" {
			t.Errorf("type 应为 cross_fields，实得 %#v", got)
		}
		if got := dig(m, "multi_match", "operator"); got != "and" {
			t.Errorf("operator 应为 and，实得 %#v", got)
		}
	})

	t.Run("未指定时不生成对应字段", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.MultiMatch("手机", []string{"title"}))
		for _, k := range []string{"type", "operator"} {
			if got := dig(m, "multi_match", k); got != nil {
				t.Errorf("未指定时不应生成 %s，实得 %#v", k, got)
			}
		}
	})

	t.Run("空字段列表", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.MultiMatch("手机", nil))
		if got := dig(m, "multi_match", "fields"); got != nil {
			t.Errorf("空字段列表不应生成 fields，实得 %#v", got)
		}
		if got := dig(m, "multi_match", "query"); got != "手机" {
			t.Errorf("query 应保留，实得 %#v", got)
		}
	})
}
