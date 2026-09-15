package esx

import (
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/textquerytype"
)

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

// MultiMatchOption 配置跨字段匹配。它直接作用于底层查询结构，
// 本包未给出构造函数的参数，调用方可自行写一个选项设置。
type MultiMatchOption func(*types.MultiMatchQuery)

// WithMultiMatchType 指定多个字段的得分如何合成一个总分。
//
// Elasticsearch 默认是 textquerytype.Bestfields，取单个字段的最高分，适合
// 「一段话整体命中某一个字段」；textquerytype.Crossfields 把多个字段当作一个大字段
// 统一算词频，适合人名、地址这类信息分散在多个字段的检索。
func WithMultiMatchType(t textquerytype.TextQueryType) MultiMatchOption {
	return func(q *types.MultiMatchQuery) { q.Type = &t }
}

// WithMultiMatchOperator 指定查询串分词后，各词项之间是全都要命中还是命中其一。
// Elasticsearch 默认是 operator.Or。
func WithMultiMatchOperator(op operator.Operator) MultiMatchOption {
	return func(q *types.MultiMatchQuery) { q.Operator = &op }
}

// MultiMatch 构造跨多字段的全文匹配。fields 为空时由 Elasticsearch 按索引的
// 默认字段处理。
func MultiMatch(query string, fields []string, opts ...MultiMatchOption) types.Query {
	q := &types.MultiMatchQuery{Query: query, Fields: fields}
	for _, opt := range opts {
		opt(q)
	}
	return types.Query{MultiMatch: q}
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

// 以下是布尔组合器，把若干查询组合成一个可嵌套的 bool 查询。
// 它们组合的是已有查询，不是新的查询类型，因此不属于上面那条「不复制查询类型体系」
// 的范围——Search 的 Must / Should / Filter 只作用于顶层 bool，嵌套位置靠它们表达。

// Any 要求至少命中其中一个子句。
//
// 它显式设置 minimum_should_match 而不依赖 Elasticsearch 的默认值：bool 查询的该
// 默认值在同级存在 must 或 filter 子句时是 0、否则是 1，靠默认值会让同一个组合在
// 不同上下文里语义不同。写成 Must(Any(a, b)) 与 Filter 同用时，仍然要求命中 a 或 b：
//
//	esx.NewSearch[Order](c, "orders").
//		Filter(esx.Term("tenant_id", 7)).
//		Must(esx.Any(esx.Match("title", "手机"), esx.Match("body", "手机")))
//
// 不传子句时返回不施加约束的查询，使「条件列表为空」不会变成匹配不到任何文档。
func Any(queries ...types.Query) types.Query {
	b := &types.BoolQuery{Should: queries}
	if len(queries) > 0 {
		b.MinimumShouldMatch = "1"
	}
	return types.Query{Bool: b}
}

// All 要求全部子句都命中。不传子句时返回不施加约束的查询。
func All(queries ...types.Query) types.Query {
	return types.Query{Bool: &types.BoolQuery{Must: queries}}
}

// Not 要求全部子句都不命中。不传子句时返回不施加约束的查询。
func Not(queries ...types.Query) types.Query {
	return types.Query{Bool: &types.BoolQuery{MustNot: queries}}
}
