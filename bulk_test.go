package esx_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtkit/esx"
)

func TestBulkActionShapes(t *testing.T) {
	t.Parallel()

	c, s := stubES(t, map[string]route{"_bulk": {body: ""}})
	b, err := c.NewBulk(
		esx.WithBulkIndex("orders"),
		esx.WithBulkWorkers(1),
		esx.WithBulkFlushInterval(time.Hour), // 只靠显式 Close 触发刷写
		esx.WithBulkFlushBytes(64<<20),
	)
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	ctx := t.Context()
	items := []esx.BulkItem{
		{DocumentID: "1", Doc: order{ID: "1", Status: "paid"}},
		{Action: esx.BulkUpdate, DocumentID: "2", Doc: map[string]any{"status": "shipped"}},
		{Action: esx.BulkDelete, DocumentID: "3"},
		{Index: "other", DocumentID: "4", Doc: order{ID: "4"}},
	}
	for _, it := range items {
		if err := b.Add(ctx, it); err != nil {
			t.Fatalf("add %s: %v", it.DocumentID, err)
		}
	}
	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	body := s.bodyWithSuffix("_bulk")
	lines := strings.Split(strings.TrimSpace(body), "\n")

	t.Run("默认 action 为 index", func(t *testing.T) {
		if !strings.Contains(body, `{"index":{"_index":"orders","_id":"1"`) &&
			!strings.Contains(body, `"index":{`) {
			t.Errorf("未生成 index 动作: %s", body)
		}
	})

	t.Run("update 包一层 doc", func(t *testing.T) {
		if !strings.Contains(body, `{"doc":{"status":"shipped"}}`) {
			t.Errorf("update 应包一层 doc: %s", body)
		}
	})

	t.Run("delete 只有动作行没有文档行", func(t *testing.T) {
		deleteIdx := -1
		for i, l := range lines {
			if strings.Contains(l, `"delete"`) {
				deleteIdx = i
			}
		}
		if deleteIdx == -1 {
			t.Fatalf("未生成 delete 动作: %s", body)
		}
		// delete 的下一行若存在，必须仍是一个动作行，而不是文档体。
		if deleteIdx+1 < len(lines) {
			var next map[string]json.RawMessage
			if err := json.Unmarshal([]byte(lines[deleteIdx+1]), &next); err != nil {
				t.Fatalf("delete 后一行无法解析: %q", lines[deleteIdx+1])
			}
			for _, verb := range []string{"index", "create", "update", "delete"} {
				if _, ok := next[verb]; ok {
					return
				}
			}
			t.Errorf("delete 不应带文档体，实得下一行 %q", lines[deleteIdx+1])
		}
	})

	t.Run("条目可覆盖默认索引", func(t *testing.T) {
		if !strings.Contains(body, `"_index":"other"`) {
			t.Errorf("条目级索引未生效: %s", body)
		}
	})
}

func TestBulkRejectsItemWithoutDoc(t *testing.T) {
	t.Parallel()

	c, _ := stubES(t, map[string]route{"_bulk": {body: ""}})
	b, err := c.NewBulk(esx.WithBulkIndex("orders"))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(t.Context()) })

	// 非删除动作缺少文档体，应在入队前被拒。
	if err := b.Add(t.Context(), esx.BulkItem{DocumentID: "1"}); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestBulkStats(t *testing.T) {
	t.Parallel()

	c, _ := stubES(t, map[string]route{"_bulk": {body: ""}})
	b, err := c.NewBulk(esx.WithBulkIndex("orders"), esx.WithBulkWorkers(2))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	ctx := t.Context()
	const n = 5
	for i := range n {
		if err := b.Add(ctx, esx.BulkItem{DocumentID: string(rune('a' + i)), Doc: map[string]any{"i": i}}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := b.Stats().NumAdded; got != n {
		t.Errorf("want NumAdded=%d, got %d", n, got)
	}
}

func TestBulkCallbacks(t *testing.T) {
	t.Parallel()

	// 第 2 条声明失败，验证成功与失败回调分别投递。
	c, _ := stubES(t, map[string]route{
		"_bulk": {body: `{"took":1,"errors":true,"items":[
			{"index":{"_id":"1","status":201,"result":"created"}},
			{"index":{"_id":"2","status":400,"error":{"type":"mapper_parsing_exception","reason":"bad field"}}}]}`},
	})

	var mu sync.Mutex
	var okIDs, failIDs []string
	var failErr error

	b, err := c.NewBulk(
		esx.WithBulkIndex("orders"),
		esx.WithBulkWorkers(1),
		esx.WithBulkOnSuccess(func(_ context.Context, it esx.BulkItem) {
			mu.Lock()
			defer mu.Unlock()
			okIDs = append(okIDs, it.DocumentID)
		}),
		esx.WithBulkOnFailure(func(_ context.Context, it esx.BulkItem, e error) {
			mu.Lock()
			defer mu.Unlock()
			failIDs = append(failIDs, it.DocumentID)
			failErr = e
		}),
	)
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	ctx := t.Context()
	for _, id := range []string{"1", "2"} {
		if err := b.Add(ctx, esx.BulkItem{DocumentID: id, Doc: map[string]any{"id": id}}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(okIDs) != 1 || okIDs[0] != "1" {
		t.Errorf("成功回调应只收到 1，实得 %#v", okIDs)
	}
	if len(failIDs) != 1 || failIDs[0] != "2" {
		t.Errorf("失败回调应只收到 2，实得 %#v", failIDs)
	}
	if failErr == nil || !strings.Contains(failErr.Error(), "mapper_parsing_exception") {
		t.Errorf("失败原因应含 ES 的错误类型，实得 %v", failErr)
	}
}

func TestBulkOnError(t *testing.T) {
	t.Parallel()

	// 整批请求被服务端拒绝，走写入器级错误回调。
	c, _ := stubES(t, map[string]route{
		"_bulk": {status: 500, body: `{"error":{"type":"internal","reason":"boom"},"status":500}`},
	})

	errCh := make(chan error, 4)
	b, err := c.NewBulk(
		esx.WithBulkIndex("orders"),
		esx.WithBulkWorkers(1),
		esx.WithBulkOnError(func(_ context.Context, e error) {
			select {
			case errCh <- e:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	ctx := t.Context()
	if err := b.Add(ctx, esx.BulkItem{DocumentID: "1", Doc: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("add: %v", err)
	}
	_ = b.Close(ctx)

	select {
	case got := <-errCh:
		if got == nil {
			t.Error("错误回调收到 nil")
		}
	default:
		t.Error("整批失败应触发写入器级错误回调")
	}
}

func TestBulkOptionsIgnoreNonPositive(t *testing.T) {
	t.Parallel()

	c, _ := stubES(t, map[string]route{"_bulk": {body: ""}})
	// 非正值应被忽略而不是产出 0 worker / 0 阈值这种不可用配置。
	b, err := c.NewBulk(
		esx.WithBulkIndex("orders"),
		esx.WithBulkWorkers(0),
		esx.WithBulkWorkers(-1),
		esx.WithBulkFlushBytes(0),
		esx.WithBulkFlushInterval(0),
	)
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}
	ctx := t.Context()
	if err := b.Add(ctx, esx.BulkItem{DocumentID: "1", Doc: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}
