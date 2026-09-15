package esx_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gtkit/esx"
)

const analyzeTokens = `{"tokens":[
	{"token":"永久","start_offset":0,"end_offset":2,"type":"CN_WORD","position":0},
	{"token":"免费","start_offset":2,"end_offset":4,"type":"CN_WORD","position":1}
]}`

func TestAnalyzeByAnalyzerName(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_analyze": {body: analyzeTokens}})

	tokens, err := c.Analyze(t.Context(), "永久免费",
		esx.WithAnalyzeIndex("articles"), esx.WithAnalyzer("ik_max_word"))
	if err != nil {
		t.Fatalf("Analyze 失败: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("应切出 2 个词项，实得 %d", len(tokens))
	}

	body := decodeJSON(t, s.bodyWithSuffix("_analyze"))
	if got := body["analyzer"]; got != "ik_max_word" {
		t.Errorf("analyzer 应为 ik_max_word，实得 %#v", got)
	}
	text, ok := body["text"].([]any)
	if !ok || len(text) != 1 || text[0] != "永久免费" {
		t.Errorf("text 不符，实得 %#v", body["text"])
	}
}

func TestAnalyzeTokenCarriesOffsets(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{"_analyze": {body: analyzeTokens}})

	tokens, err := c.Analyze(t.Context(), "永久免费", esx.WithAnalyzer("ik_max_word"))
	if err != nil {
		t.Fatalf("Analyze 失败: %v", err)
	}
	first := tokens[0]
	if first.Token != "永久" || first.Type != "CN_WORD" {
		t.Errorf("词项内容不符: %+v", first)
	}
	if first.Position != 0 || first.StartOffset != 0 || first.EndOffset != 2 {
		t.Errorf("位置与偏移不符: %+v", first)
	}
	if second := tokens[1]; second.Position != 1 || second.StartOffset != 2 {
		t.Errorf("第二个词项位置与偏移不符: %+v", second)
	}
}

func TestAnalyzeByField(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_analyze": {body: analyzeTokens}})

	if _, err := c.Analyze(t.Context(), "永久免费",
		esx.WithAnalyzeIndex("articles"), esx.WithAnalyzeField("title")); err != nil {
		t.Fatalf("Analyze 失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_analyze"))
	if got := body["field"]; got != "title" {
		t.Errorf("field 应为 title，实得 %#v", got)
	}
	if _, ok := body["analyzer"]; ok {
		t.Error("未指定 analyzer 时不应写入该字段")
	}
}

func TestAnalyzeByTokenizerAndFilters(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_analyze": {body: analyzeTokens}})

	if _, err := c.Analyze(t.Context(), "Quick <b>Fox</b>",
		esx.WithAnalyzeTokenizer("standard"),
		esx.WithAnalyzeFilters("lowercase", "stop"),
		esx.WithAnalyzeCharFilters("html_strip")); err != nil {
		t.Fatalf("Analyze 失败: %v", err)
	}

	body := decodeJSON(t, s.bodyWithSuffix("_analyze"))
	if got := body["tokenizer"]; got != "standard" {
		t.Errorf("tokenizer 应为 standard，实得 %#v", got)
	}
	filters, ok := body["filter"].([]any)
	if !ok || len(filters) != 2 || filters[0] != "lowercase" || filters[1] != "stop" {
		t.Errorf("filter 不符，实得 %#v", body["filter"])
	}
	charFilters, ok := body["char_filter"].([]any)
	if !ok || len(charFilters) != 1 || charFilters[0] != "html_strip" {
		t.Errorf("char_filter 不符，实得 %#v", body["char_filter"])
	}
}

func TestAnalyzeRejectsEmptyText(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, nil)

	if _, err := c.Analyze(t.Context(), ""); err == nil {
		t.Fatal("空文本应返回错误")
	}
	if n := s.count(); n != 0 {
		t.Errorf("空文本不应发出请求，实得 %d 次", n)
	}
}

func TestAnalyzeWithoutMethodUsesDefault(t *testing.T) {
	t.Parallel()
	c, s := stubES(t, map[string]route{"_analyze": {body: analyzeTokens}})

	if _, err := c.Analyze(t.Context(), "永久免费"); err != nil {
		t.Fatalf("不指定分析方式时应交由服务端处理: %v", err)
	}
	body := decodeJSON(t, s.bodyWithSuffix("_analyze"))
	for _, k := range []string{"analyzer", "field", "tokenizer", "filter", "char_filter"} {
		if _, ok := body[k]; ok {
			t.Errorf("未指定时不应写入 %s，实得 %#v", k, body[k])
		}
	}
}

func TestAnalyzeIndexNotFound(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"_analyze": {status: http.StatusNotFound, body: `{"error":{"type":"index_not_found_exception","reason":"no such index"},"status":404}`},
	})

	_, err := c.Analyze(t.Context(), "永久免费", esx.WithAnalyzeIndex("missing"))
	if !errors.Is(err, esx.ErrNotFound) {
		t.Fatalf("索引不存在应归一到 ErrNotFound，实得 %v", err)
	}
}

func TestAnalyzeUnknownAnalyzer(t *testing.T) {
	t.Parallel()
	c, _ := stubES(t, map[string]route{
		"_analyze": {status: http.StatusBadRequest, body: `{"error":{"type":"illegal_argument_exception","reason":"failed to find analyzer [nope]"},"status":400}`},
	})

	_, err := c.Analyze(t.Context(), "永久免费", esx.WithAnalyzer("nope"))
	if err == nil {
		t.Fatal("未定义的 analyzer 应返回错误")
	}
	var e *esx.Error
	if !errors.As(err, &e) {
		t.Fatalf("应为 *esx.Error，实得 %T", err)
	}
	if e.StatusCode != http.StatusBadRequest {
		t.Errorf("状态码应为 400，实得 %d", e.StatusCode)
	}
}
