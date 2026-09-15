package esx

import (
	"context"
	"errors"

	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/analyze"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// AnalyzeToken 是一次文本分析切出的一个词项。
type AnalyzeToken struct {
	// Token 是词项文本，即最终进入倒排索引的形态。
	Token string
	// Type 是词项类型，取值由 tokenizer 决定，如 standard 分词器的 "<ALPHANUM>"。
	Type string
	// Position 是词项在序列中的位置序号，从 0 开始。短语匹配按它判断词序。
	Position int64
	// StartOffset 是词项在原文中的起始字符偏移。
	StartOffset int64
	// EndOffset 是词项在原文中的结束字符偏移。高亮据此定位原文片段。
	EndOffset int64
}

// AnalyzeOption 配置一次文本分析。
type AnalyzeOption func(*analyzeConfig)

type analyzeConfig struct {
	index       string
	analyzer    string
	field       string
	tokenizer   string
	filters     []types.TokenFilter
	charFilters []types.CharFilter
}

// WithAnalyzeIndex 在指定索引的上下文中分析，使该索引 settings 里定义的自定义
// 分析器、分词器与过滤器可以按名字引用。
func WithAnalyzeIndex(index string) AnalyzeOption {
	return func(c *analyzeConfig) { c.index = index }
}

// WithAnalyzer 指定分析器名称，如 "standard"、"ik_max_word"。
func WithAnalyzer(name string) AnalyzeOption {
	return func(c *analyzeConfig) { c.analyzer = name }
}

// WithAnalyzeField 按索引中该字段 mapping 所配的分析器分析。
//
// 字段名要经索引解析，需与 [WithAnalyzeIndex] 同用。
func WithAnalyzeField(field string) AnalyzeOption {
	return func(c *analyzeConfig) { c.field = field }
}

// WithAnalyzeTokenizer 指定分词器名称，用于在不落地分析器定义的前提下试验组合。
func WithAnalyzeTokenizer(name string) AnalyzeOption {
	return func(c *analyzeConfig) { c.tokenizer = name }
}

// WithAnalyzeFilters 指定词项过滤器名称，按给定顺序作用于分词结果，如 "lowercase"。
func WithAnalyzeFilters(names ...string) AnalyzeOption {
	return func(c *analyzeConfig) {
		for _, name := range names {
			c.filters = append(c.filters, name)
		}
	}
}

// WithAnalyzeCharFilters 指定字符过滤器名称，在分词之前作用于原文，如 "html_strip"。
func WithAnalyzeCharFilters(names ...string) AnalyzeOption {
	return func(c *analyzeConfig) {
		for _, name := range names {
			c.charFilters = append(c.charFilters, name)
		}
	}
}

// Analyze 分析文本，返回切出的词项序列。
//
// 分析方式有三种指定途径，可按需组合：[WithAnalyzer] 给出分析器名称、
// [WithAnalyzeField] 沿用某字段 mapping 的配置、[WithAnalyzeTokenizer] 与
// [WithAnalyzeFilters] 临时拼一条分析链。都不指定时由 Elasticsearch 用默认分析器。
//
// 它的用途是让 mapping 里配的分析器对具体文本的实际效果可见——检索结果不符预期时，
// 先看文本被切成了什么词，比对着查询语句猜更直接：
//
//	tokens, err := c.Analyze(ctx, "永久免费的搜索引擎",
//		esx.WithAnalyzeIndex("articles"),
//		esx.WithAnalyzer("ik_max_word"),
//	)
func (c *Client) Analyze(ctx context.Context, text string, opts ...AnalyzeOption) ([]AnalyzeToken, error) {
	if text == "" {
		return nil, invalid("analyze", errNoAnalyzeText)
	}

	cfg := &analyzeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	req := analyze.NewRequest()
	req.Text = []string{text}
	if cfg.analyzer != "" {
		req.Analyzer = &cfg.analyzer
	}
	if cfg.field != "" {
		req.Field = &cfg.field
	}
	if cfg.tokenizer != "" {
		req.Tokenizer = cfg.tokenizer
	}
	req.Filter = cfg.filters
	req.CharFilter = cfg.charFilters

	builder := c.typed.Indices.Analyze()
	if cfg.index != "" {
		builder = builder.Index(cfg.index)
	}

	resp, err := builder.Request(req).Do(ctx)
	if err != nil {
		return nil, wrapErr("analyze", cfg.index, err)
	}

	tokens := make([]AnalyzeToken, len(resp.Tokens))
	for i, t := range resp.Tokens {
		tokens[i] = AnalyzeToken{
			Token:       t.Token,
			Type:        t.Type,
			Position:    t.Position,
			StartOffset: t.StartOffset,
			EndOffset:   t.EndOffset,
		}
	}
	return tokens, nil
}

var errNoAnalyzeText = errors.New("text to analyze is required")
