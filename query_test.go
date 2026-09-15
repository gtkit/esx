package esx_test

import (
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/gtkit/esx"
)

func TestQueryConstructors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query types.Query
		// path 是断言取值的路径，want 是该处应有的值。
		path []string
		want any
	}{
		{
			name:  "Term 精确匹配",
			query: esx.Term("status", "paid"),
			path:  []string{"term", "status", "value"},
			want:  "paid",
		},
		{
			name:  "Term 数值取值",
			query: esx.Term("tenant", 7),
			path:  []string{"term", "tenant", "value"},
			want:  float64(7),
		},
		{
			name:  "Term 元字符按字面量处理",
			query: esx.Term("sku", `a*b?c\d"e`),
			path:  []string{"term", "sku", "value"},
			want:  `a*b?c\d"e`,
		},
		{
			name:  "Match 全文匹配",
			query: esx.Match("title", "手机"),
			path:  []string{"match", "title", "query"},
			want:  "手机",
		},
		{
			name:  "MatchPhrase 短语匹配",
			query: esx.MatchPhrase("title", "红色 手机"),
			path:  []string{"match_phrase", "title", "query"},
			want:  "红色 手机",
		},
		{
			name:  "MultiMatch 跨字段",
			query: esx.MultiMatch("手机"),
			path:  []string{"multi_match", "query"},
			want:  "手机",
		},
		{
			name:  "Prefix 前缀",
			query: esx.Prefix("sku", "A-"),
			path:  []string{"prefix", "sku", "value"},
			want:  "A-",
		},
		{
			name:  "Wildcard 通配符",
			query: esx.Wildcard("sku", "A*"),
			path:  []string{"wildcard", "sku", "value"},
			want:  "A*",
		},
		{
			name:  "Exists 字段存在",
			query: esx.Exists("deleted_at"),
			path:  []string{"exists", "field"},
			want:  "deleted_at",
		},
		{
			name:  "DateRange 下界",
			query: esx.DateRange("created_at", "now-7d", ""),
			path:  []string{"range", "created_at", "gte"},
			want:  "now-7d",
		},
		{
			name:  "DateRange 上界",
			query: esx.DateRange("created_at", "", "now"),
			path:  []string{"range", "created_at", "lte"},
			want:  "now",
		},
		{
			name:  "Range 下界",
			query: esx.Range("amount", new(100.0), nil),
			path:  []string{"range", "amount", "gte"},
			want:  float64(100),
		},
		{
			name:  "Range 上界",
			query: esx.Range("amount", nil, new(500.0)),
			path:  []string{"range", "amount", "lte"},
			want:  float64(500),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := dig(queryJSON(t, tt.query), tt.path...)
			if got != tt.want {
				t.Errorf("路径 %v: want %#v, got %#v", tt.path, tt.want, got)
			}
		})
	}
}

func TestRangeSingleSidedOpen(t *testing.T) {
	t.Parallel()

	t.Run("只给下界时不产生上界", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.Range("amount", new(100.0), nil))
		if v := dig(m, "range", "amount", "lte"); v != nil {
			t.Errorf("不应有上界，实得 %#v", v)
		}
	})

	t.Run("两侧都不给时区间为空", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.Range("amount", nil, nil))
		if v := dig(m, "range", "amount", "gte"); v != nil {
			t.Errorf("不应有下界，实得 %#v", v)
		}
		if v := dig(m, "range", "amount", "lte"); v != nil {
			t.Errorf("不应有上界，实得 %#v", v)
		}
	})

	t.Run("DateRange 两侧都为空", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.DateRange("created_at", "", ""))
		if v := dig(m, "range", "created_at", "gte"); v != nil {
			t.Errorf("不应有下界，实得 %#v", v)
		}
		if v := dig(m, "range", "created_at", "lte"); v != nil {
			t.Errorf("不应有上界，实得 %#v", v)
		}
	})
}

func TestTermsAndMatchAll(t *testing.T) {
	t.Parallel()

	t.Run("Terms 多取值", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.Terms("status", "paid", "shipped"))
		vals, ok := dig(m, "terms", "status").([]any)
		if !ok {
			t.Fatalf("terms.status 应为数组，实得 %#v", dig(m, "terms", "status"))
		}
		if len(vals) != 2 || vals[0] != "paid" || vals[1] != "shipped" {
			t.Errorf("取值不符: %#v", vals)
		}
	})

	t.Run("Terms 无取值", func(t *testing.T) {
		t.Parallel()
		// 不应 panic，且结构仍合法。
		_ = queryJSON(t, esx.Terms("status"))
	})

	t.Run("MatchAll", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.MatchAll())
		if _, ok := m["match_all"]; !ok {
			t.Errorf("应含 match_all，实得 %#v", m)
		}
	})

	t.Run("MultiMatch 指定字段", func(t *testing.T) {
		t.Parallel()
		m := queryJSON(t, esx.MultiMatch("手机", "title", "body"))
		fields, ok := dig(m, "multi_match", "fields").([]any)
		if !ok || len(fields) != 2 {
			t.Fatalf("fields 应为 2 元素数组，实得 %#v", dig(m, "multi_match", "fields"))
		}
		if fields[0] != "title" || fields[1] != "body" {
			t.Errorf("字段不符: %#v", fields)
		}
	})
}
