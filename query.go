package esx

import "github.com/elastic/go-elasticsearch/v9/typedapi/types"

// 本文件提供最常用查询的构造器，省掉调用方手拼 types.Query 的样板。
// 这里没覆盖的查询直接构造 types.Query 传给 Must / Filter 即可，
// 本包不复制一套注定要跟着 Elasticsearch 演进的查询类型体系。

// Term 构造精确值匹配，字段不分词。
func Term(field string, value any) types.Query {
	return types.Query{
		Term: map[string]types.TermQuery{field: {Value: value}},
	}
}

// Terms 构造多值精确匹配，命中其中任一值即可。
func Terms(field string, values ...any) types.Query {
	return types.Query{
		Terms: &types.TermsQuery{
			TermsQuery: map[string]types.TermsQueryField{field: values},
		},
	}
}

// Match 构造全文匹配，字段按其 analyzer 分词。
func Match(field, query string) types.Query {
	return types.Query{
		Match: map[string]types.MatchQuery{field: {Query: query}},
	}
}

// MatchPhrase 构造短语匹配，要求词项顺序一致。
func MatchPhrase(field, query string) types.Query {
	return types.Query{
		MatchPhrase: map[string]types.MatchPhraseQuery{field: {Query: query}},
	}
}

// MultiMatch 构造跨多字段的全文匹配。
func MultiMatch(query string, fields ...string) types.Query {
	return types.Query{
		MultiMatch: &types.MultiMatchQuery{Query: query, Fields: fields},
	}
}

// Prefix 构造前缀匹配。
func Prefix(field, value string) types.Query {
	return types.Query{
		Prefix: map[string]types.PrefixQuery{field: {Value: value}},
	}
}

// Wildcard 构造通配符匹配，value 中 * 匹配任意多个字符，? 匹配一个字符。
//
// 以通配符开头的模式需要扫描全部词项，大索引上代价很高。
func Wildcard(field, value string) types.Query {
	return types.Query{
		Wildcard: map[string]types.WildcardQuery{field: {Value: &value}},
	}
}

// Exists 匹配该字段存在且非 null 的文档。
func Exists(field string) types.Query {
	return types.Query{Exists: &types.ExistsQuery{Field: field}}
}

// MatchAll 匹配全部文档。
func MatchAll() types.Query {
	return types.Query{MatchAll: &types.MatchAllQuery{}}
}

// Range 构造数值区间匹配。gte 与 lte 传 nil 表示该侧不限。
func Range(field string, gte, lte *float64) types.Query {
	q := types.NumberRangeQuery{}
	if gte != nil {
		q.Gte = (*types.Float64)(gte)
	}
	if lte != nil {
		q.Lte = (*types.Float64)(lte)
	}
	return types.Query{
		Range: map[string]types.RangeQuery{field: q},
	}
}

// DateRange 构造时间区间匹配。gte 与 lte 为空字符串表示该侧不限，
// 取值可以是日期字符串或 ES 的日期数学表达式（如 "now-7d"）。
func DateRange(field, gte, lte string) types.Query {
	q := types.DateRangeQuery{}
	if gte != "" {
		q.Gte = &gte
	}
	if lte != "" {
		q.Lte = &lte
	}
	return types.Query{
		Range: map[string]types.RangeQuery{field: q},
	}
}
