package esx

import (
	"encoding/json"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// Result 是一次查询的结果。
type Result[T any] struct {
	// Total 是匹配的文档总数。ES 默认最多精确统计到 10000，
	// 超出时 TotalRelation 为 "gte"。
	Total int64
	// TotalRelation 说明 Total 是精确值（"eq"）还是下界（"gte"）。
	TotalRelation string
	// Hits 是本页命中，按 ES 返回顺序。
	Hits []Hit[T]
	// Aggregations 是聚合结果，未请求聚合时为 nil。
	Aggregations map[string]types.Aggregate
}

// Docs 返回本页命中的文档，丢弃 id、score、高亮等元信息。
func (r *Result[T]) Docs() []T {
	docs := make([]T, len(r.Hits))
	for i, h := range r.Hits {
		docs[i] = h.Doc
	}
	return docs
}

// Hit 是一条命中。
type Hit[T any] struct {
	// ID 是文档 id。
	ID string
	// Score 是相关性得分；查询不打分（如只用 filter）时为 nil。
	Score *float64
	// Doc 是解码后的文档。
	Doc T
	// Highlight 是高亮片段，键为字段名。未请求高亮时为 nil。
	Highlight map[string][]string
}

func decodeResult[T any](index string, resp *search.Response) (*Result[T], error) {
	out := &Result[T]{
		Hits:         make([]Hit[T], 0, len(resp.Hits.Hits)),
		Aggregations: resp.Aggregations,
	}
	if resp.Hits.Total != nil {
		out.Total = resp.Hits.Total.Value
		out.TotalRelation = resp.Hits.Total.Relation.String()
	}

	for _, h := range resp.Hits.Hits {
		hit := Hit[T]{Highlight: h.Highlight}
		if h.Id_ != nil {
			hit.ID = *h.Id_
		}
		if h.Score_ != nil {
			score := float64(*h.Score_)
			hit.Score = &score
		}
		// _source 被完全过滤掉时为空，此时保留 T 的零值，不当作解码失败。
		if len(h.Source_) > 0 {
			if err := json.Unmarshal(h.Source_, &hit.Doc); err != nil {
				return nil, &Error{Op: "decode hit", Index: index, Err: err}
			}
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}
