package esx

import (
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/calendarinterval"
)

// 本文件提供最常用聚合的构造器，省掉调用方手拼 types.Aggregations 的样板。
// 与查询构造器同理，这里没覆盖的聚合直接构造 types.Aggregations 传给 Agg 即可，
// 本包不复制一套注定要跟着 Elasticsearch 演进的聚合类型体系。

// TermsAgg 按字段的词项分组统计。size 为每次返回的分组条数，非正值沿用
// Elasticsearch 的默认值。
//
// 字段须是不分词的类型（如 keyword），对 text 字段分组得到的是词项而非原值。
func TermsAgg(field string, size int) types.Aggregations {
	agg := &types.TermsAggregation{Field: &field}
	if size > 0 {
		agg.Size = &size
	}
	return types.Aggregations{Terms: agg}
}

// DateHistogramAgg 按日历间隔把时间字段分桶，间隔取 calendarinterval 中的常量
// （如 calendarinterval.Day）。
//
// 日历间隔跟随自然日历，月与年的长度不固定；需要固定长度的桶时自行构造
// types.DateHistogramAggregation 并设置 FixedInterval。
func DateHistogramAgg(field string, interval calendarinterval.CalendarInterval) types.Aggregations {
	return types.Aggregations{
		DateHistogram: &types.DateHistogramAggregation{
			Field:            &field,
			CalendarInterval: &interval,
		},
	}
}

// AvgAgg 求字段的平均值。
func AvgAgg(field string) types.Aggregations {
	return types.Aggregations{Avg: &types.AverageAggregation{Field: &field}}
}

// SumAgg 求字段的和。
func SumAgg(field string) types.Aggregations {
	return types.Aggregations{Sum: &types.SumAggregation{Field: &field}}
}

// MinAgg 求字段的最小值。
func MinAgg(field string) types.Aggregations {
	return types.Aggregations{Min: &types.MinAggregation{Field: &field}}
}

// MaxAgg 求字段的最大值。
func MaxAgg(field string) types.Aggregations {
	return types.Aggregations{Max: &types.MaxAggregation{Field: &field}}
}

// CardinalityAgg 求字段的去重计数。
//
// 结果是基数估算而非精确值：Elasticsearch 用 HyperLogLog++ 实现，超出精度阈值后
// 有误差，用于量级判断而非对账。
func CardinalityAgg(field string) types.Aggregations {
	return types.Aggregations{Cardinality: &types.CardinalityAggregation{Field: &field}}
}
