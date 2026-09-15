package esx

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/sortorder"
)

// defaultPageSize 是 Page 在每页条数非正时使用的兜底值。
const defaultPageSize = 20

// Search 是泛型链式查询构建器。
//
// 类型参数在类型上而不在方法上：Go 1.27 的泛型方法不能出现在接口方法集中，
// 做成方法会让持有它的类型无法被接口抽象。
//
// 并发安全：构建过程不是并发安全的，构建完成后的 Do / DoAgg / Count 可并发调用。
type Search[T any] struct {
	client *Client
	index  string

	must    []types.Query
	should  []types.Query
	mustNot []types.Query
	filter  []types.Query

	minShouldMatch *int

	sorts       []types.SortCombinations
	from        *int
	size        *int
	searchAfter []types.FieldValue

	// highlightFields 按加入顺序保存高亮字段，highlightPerField 只存显式配过的字段。
	// 两者分开是因为字段可以只出现在 Highlight 里而不带任何配置。
	highlightFields   []string
	highlightPerField map[string]*highlightConfig
	highlightGlobal   *highlightConfig

	includes []string
	excludes []string

	trackTotalHits types.TrackHits

	aggs map[string]types.Aggregations
}

// highlightConfig 是一层高亮配置，既可作全局默认，也可绑定到单个字段。
type highlightConfig struct {
	preTags           []string
	postTags          []string
	fragmentSize      *int
	numberOfFragments *int
}

// HighlightOption 配置高亮，可用于全局默认与单个字段两处。
type HighlightOption func(*highlightConfig)

// WithHighlightTags 设置包裹命中片段的标签对。Elasticsearch 默认是 <em> 与 </em>。
func WithHighlightTags(pre, post string) HighlightOption {
	return func(c *highlightConfig) {
		c.preTags = []string{pre}
		c.postTags = []string{post}
	}
}

// WithHighlightFragmentSize 设置每个高亮片段的字符数。非正值被忽略。
func WithHighlightFragmentSize(n int) HighlightOption {
	return func(c *highlightConfig) {
		if n > 0 {
			c.fragmentSize = &n
		}
	}
}

// WithHighlightFragments 设置每个字段最多返回的片段数。负值被忽略。
//
// 取 0 时 Elasticsearch 不做分片，返回整个字段内容并在其中标出命中，
// 适合标题这类本身就短的字段。
func WithHighlightFragments(n int) HighlightOption {
	return func(c *highlightConfig) {
		if n >= 0 {
			c.numberOfFragments = &n
		}
	}
}

func newHighlightConfig(opts []HighlightOption) *highlightConfig {
	c := &highlightConfig{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// applyGlobal 把配置填到高亮的顶层，作为所有字段的默认值。
func (c *highlightConfig) applyGlobal(h *types.Highlight) {
	h.PreTags = c.preTags
	h.PostTags = c.postTags
	h.FragmentSize = c.fragmentSize
	h.NumberOfFragments = c.numberOfFragments
}

// applyField 把配置填到单个字段。Elasticsearch 以字段级优先于顶层，
// 覆盖关系由它保证，本包不在客户端侧合并两层取值。
func (c *highlightConfig) applyField(f *types.HighlightField) {
	f.PreTags = c.preTags
	f.PostTags = c.postTags
	f.FragmentSize = c.fragmentSize
	f.NumberOfFragments = c.numberOfFragments
}

// NewSearch 创建针对 index 的查询构建器。index 可以是索引名或别名。
func NewSearch[T any](c *Client, index string) *Search[T] {
	return &Search[T]{client: c, index: index}
}

// Must 追加 must 子句。多次调用累积。
func (s *Search[T]) Must(queries ...types.Query) *Search[T] {
	s.must = append(s.must, queries...)
	return s
}

// Should 追加 should 子句。多次调用累积。
func (s *Search[T]) Should(queries ...types.Query) *Search[T] {
	s.should = append(s.should, queries...)
	return s
}

// MustNot 追加 must_not 子句。多次调用累积。
func (s *Search[T]) MustNot(queries ...types.Query) *Search[T] {
	s.mustNot = append(s.mustNot, queries...)
	return s
}

// Filter 追加 filter 子句。多次调用累积。
// filter 不参与打分且可被缓存，精确匹配优先用它而非 Must。
func (s *Search[T]) Filter(queries ...types.Query) *Search[T] {
	s.filter = append(s.filter, queries...)
	return s
}

// MinimumShouldMatch 设置 should 子句至少要命中几条。
func (s *Search[T]) MinimumShouldMatch(n int) *Search[T] {
	s.minShouldMatch = &n
	return s
}

// Sort 按字段排序。多次调用按调用顺序构成多级排序。
func (s *Search[T]) Sort(field string, ascending bool) *Search[T] {
	order := sortorder.Desc
	if ascending {
		order = sortorder.Asc
	}
	s.sorts = append(s.sorts, types.SortOptions{
		SortOptions: map[string]types.FieldSort{field: {Order: &order}},
	})
	return s
}

// Page 按页码分页。页码小于 1 按 1 处理，每页条数非正时用默认值。
func (s *Search[T]) Page(page, pageSize int) *Search[T] {
	page = max(page, 1)
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	return s.Offset((page-1)*pageSize, pageSize)
}

// Offset 按偏移量分页。
func (s *Search[T]) Offset(from, size int) *Search[T] {
	s.from = &from
	s.size = &size
	return s
}

// SearchAfter 设置深度分页游标，取值来自上一页最后一条命中的排序值。
func (s *Search[T]) SearchAfter(values ...types.FieldValue) *Search[T] {
	s.searchAfter = values
	return s
}

// Highlight 指定需要高亮的字段，沿用 HighlightWith 设的全局配置。
// 高亮片段在结果的 Hit.Highlight 中返回。
func (s *Search[T]) Highlight(fields ...string) *Search[T] {
	for _, field := range fields {
		s.addHighlightField(field)
	}
	return s
}

// addHighlightField 追加一个高亮字段，已经在列的不重复追加。
// Highlight 与 HighlightField 可以指到同一个字段，去重收敛在这里。
func (s *Search[T]) addHighlightField(field string) {
	if !slices.Contains(s.highlightFields, field) {
		s.highlightFields = append(s.highlightFields, field)
	}
}

// HighlightWith 设置高亮的全局配置，作用于所有高亮字段。
//
// 它只设默认值，不会让任何字段参与高亮——字段仍要经 Highlight 或
// HighlightField 指定，一个都没有时请求中不含高亮结构。
func (s *Search[T]) HighlightWith(opts ...HighlightOption) *Search[T] {
	s.highlightGlobal = newHighlightConfig(opts)
	return s
}

// HighlightField 指定一个高亮字段并给它单独的配置，未设的项沿用全局配置。
//
// 同一字段重复指定时后一次的配置生效。
func (s *Search[T]) HighlightField(field string, opts ...HighlightOption) *Search[T] {
	if s.highlightPerField == nil {
		s.highlightPerField = make(map[string]*highlightConfig)
	}
	s.addHighlightField(field)
	s.highlightPerField[field] = newHighlightConfig(opts)
	return s
}

// Select 限定 _source 只返回这些字段。
func (s *Search[T]) Select(fields ...string) *Search[T] {
	s.includes = append(s.includes, fields...)
	return s
}

// Exclude 从 _source 中排除这些字段。
func (s *Search[T]) Exclude(fields ...string) *Search[T] {
	s.excludes = append(s.excludes, fields...)
	return s
}

// Agg 追加具名聚合。同名重复追加时后者覆盖前者。
func (s *Search[T]) Agg(name string, agg types.Aggregations) *Search[T] {
	if s.aggs == nil {
		s.aggs = make(map[string]types.Aggregations)
	}
	s.aggs[name] = agg
	return s
}

// TrackTotalHits 把精确统计命中总数的上界抬到 upTo 条。非正值被忽略。
//
// Elasticsearch 默认只精确统计到 10000 条，超出时 Result.Total 是下界而非精确值，
// Result.TotalRelation 为 "gte"。上界抬得越高，深分页的统计代价越大。
func (s *Search[T]) TrackTotalHits(upTo int) *Search[T] {
	if upTo > 0 {
		s.trackTotalHits = upTo
	}
	return s
}

// TrackAllHits 要求精确统计全部命中总数，使 Result.TotalRelation 恒为 "eq"。
//
// 代价随匹配文档数增长，大结果集上比默认行为慢。
func (s *Search[T]) TrackAllHits() *Search[T] {
	s.trackTotalHits = true
	return s
}

// Do 执行查询并把命中结果解码为 T。
func (s *Search[T]) Do(ctx context.Context) (*Result[T], error) {
	resp, err := s.client.typed.Search().
		Index(s.index).
		Request(s.buildRequest()).
		Do(ctx)
	if err != nil {
		return nil, s.searchErr(ctx, "search", err)
	}
	return decodeResult[T](s.index, resp)
}

// DoAgg 只取聚合结果，不返回文档：请求的 size 置 0。
func (s *Search[T]) DoAgg(ctx context.Context) (map[string]types.Aggregate, error) {
	req := s.buildRequest()
	req.Size = new(0)

	resp, err := s.client.typed.Search().
		Index(s.index).
		Request(req).
		Do(ctx)
	if err != nil {
		return nil, s.searchErr(ctx, "aggregate", err)
	}
	return resp.Aggregations, nil
}

// Count 只返回匹配的文档总数，复用已设置的查询条件。
// 排序、分页、高亮设置不影响结果。
func (s *Search[T]) Count(ctx context.Context) (int64, error) {
	resp, err := s.client.typed.Count().
		Index(s.index).
		Query(s.buildQuery()).
		Do(ctx)
	if err != nil {
		return 0, s.searchErr(ctx, "count", err)
	}
	return resp.Count, nil
}

// searchErr 归一查询失败。上下文被取消时要能被 errors.Is 命中，
// 而传输层返回的错误未必包裹了 ctx 的原因，这里显式并上。
func (s *Search[T]) searchErr(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return &Error{Op: op, Index: s.index, Err: errors.Join(err, ctx.Err())}
	}
	return wrapErr(op, s.index, err)
}

func (s *Search[T]) buildQuery() *types.Query {
	b := &types.BoolQuery{
		Must:    s.must,
		Should:  s.should,
		MustNot: s.mustNot,
		Filter:  s.filter,
	}
	if s.minShouldMatch != nil {
		b.MinimumShouldMatch = new(strconv.Itoa(*s.minShouldMatch))
	}
	return &types.Query{Bool: b}
}

func (s *Search[T]) buildRequest() *search.Request {
	req := search.NewRequest()
	req.Query = s.buildQuery()
	req.From = s.from
	req.Size = s.size
	req.Sort = s.sorts
	req.SearchAfter = s.searchAfter
	req.Aggregations = s.aggs
	req.TrackTotalHits = s.trackTotalHits

	if len(s.highlightFields) > 0 {
		fields := make(map[string]types.HighlightField, len(s.highlightFields))
		for _, name := range s.highlightFields {
			field := types.HighlightField{}
			if cfg := s.highlightPerField[name]; cfg != nil {
				cfg.applyField(&field)
			}
			fields[name] = field
		}
		// v9 的 Highlight.Fields 是 []map[string]HighlightField，v8 是 map[string]HighlightField。
		h := &types.Highlight{Fields: []map[string]types.HighlightField{fields}}
		if s.highlightGlobal != nil {
			s.highlightGlobal.applyGlobal(h)
		}
		req.Highlight = h
	}

	if len(s.includes) > 0 || len(s.excludes) > 0 {
		req.Source_ = &types.SourceFilter{Includes: s.includes, Excludes: s.excludes}
	}
	return req
}
