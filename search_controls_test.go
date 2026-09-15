package esx_test

import (
	"testing"

	"github.com/gtkit/esx"
)

// highlightFields 取出高亮的字段级配置。v9 的 Highlight.Fields 是数组包对象，
// dig 只能走对象，这里单独展开。
func highlightFields(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	arr, ok := dig(body, "highlight", "fields").([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("highlight.fields 应为非空数组，实得 %#v", dig(body, "highlight", "fields"))
	}
	fields, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("highlight.fields[0] 应为对象，实得 %#v", arr[0])
	}
	return fields
}

func TestHighlightGlobalAppliesToAllFields(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "articles").
		HighlightWith(esx.WithHighlightTags("<mark>", "</mark>"), esx.WithHighlightFragmentSize(100)).
		Highlight("title", "body").
		Do(t.Context()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_search"))
	pre, ok := dig(body, "highlight", "pre_tags").([]any)
	if !ok || len(pre) != 1 || pre[0] != "<mark>" {
		t.Errorf("顶层 pre_tags 不符，实得 %#v", dig(body, "highlight", "pre_tags"))
	}
	if got := dig(body, "highlight", "fragment_size"); got != float64(100) {
		t.Errorf("顶层 fragment_size 应为 100，实得 %#v", got)
	}

	// 全局配置放顶层，字段级留空，覆盖由 Elasticsearch 保证
	fields := highlightFields(t, body)
	for _, name := range []string{"title", "body"} {
		f, ok := fields[name].(map[string]any)
		if !ok {
			t.Fatalf("应含字段 %s，实得 %#v", name, fields)
		}
		if len(f) != 0 {
			t.Errorf("字段 %s 未单独配置时应为空对象，实得 %#v", name, f)
		}
	}
}

func TestHighlightFieldOverridesGlobal(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "articles").
		HighlightWith(esx.WithHighlightFragmentSize(100)).
		Highlight("body").
		HighlightField("title", esx.WithHighlightFragments(0)).
		Do(t.Context()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_search"))
	if got := dig(body, "highlight", "fragment_size"); got != float64(100) {
		t.Errorf("顶层 fragment_size 应为 100，实得 %#v", got)
	}
	fields := highlightFields(t, body)
	title, ok := fields["title"].(map[string]any)
	if !ok {
		t.Fatalf("应含 title 字段，实得 %#v", fields)
	}
	if got := title["number_of_fragments"]; got != float64(0) {
		t.Errorf("title 的 number_of_fragments 应为 0，实得 %#v", got)
	}
	if body2, ok := fields["body"].(map[string]any); !ok || len(body2) != 0 {
		t.Errorf("body 未单独配置时应为空对象，实得 %#v", fields["body"])
	}
}

// TestHighlightFieldDedup 覆盖同一字段经两个入口指定的情形。
func TestHighlightFieldDedup(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "articles").
		Highlight("title").
		HighlightField("title", esx.WithHighlightFragmentSize(30)).
		Do(t.Context()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	fields := highlightFields(t, decodeJSON(t, s.bodyWithSuffix("_search")))
	if len(fields) != 1 {
		t.Fatalf("同一字段应只出现一次，实得 %#v", fields)
	}
	title, _ := fields["title"].(map[string]any)
	if got := title["fragment_size"]; got != float64(30) {
		t.Errorf("后一次的配置应生效，实得 %#v", got)
	}
}

func TestHighlightGlobalOnlyEmitsNothing(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})

	if _, err := esx.NewSearch[order](c, "articles").
		HighlightWith(esx.WithHighlightTags("<b>", "</b>")).
		Do(t.Context()); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_search"))
	if got := body["highlight"]; got != nil {
		t.Errorf("未指定高亮字段时不应生成 highlight，实得 %#v", got)
	}
}

func TestTrackTotalHits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func(*esx.Search[order]) *esx.Search[order]
		want  any
	}{
		{"抬高上界", func(s *esx.Search[order]) *esx.Search[order] { return s.TrackTotalHits(50000) }, float64(50000)},
		{"全量精确", func(s *esx.Search[order]) *esx.Search[order] { return s.TrackAllHits() }, true},
		{"非正值被忽略", func(s *esx.Search[order]) *esx.Search[order] { return s.TrackTotalHits(0) }, nil},
		{"未设置", func(s *esx.Search[order]) *esx.Search[order] { return s }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, s := stubES(t, map[string]route{"_search": {body: emptyHits}})
			if _, err := tt.build(esx.NewSearch[order](c, "articles")).Do(t.Context()); err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			body := decodeJSON(t, s.bodyWithSuffix("_search"))
			if got := body["track_total_hits"]; got != tt.want {
				t.Errorf("track_total_hits 应为 %#v，实得 %#v", tt.want, got)
			}
		})
	}
}
