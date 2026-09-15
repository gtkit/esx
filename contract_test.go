package esx_test

// 本文件是契约反证测试：每个用例对应一条写进 README 或 GoDoc 的承诺，
// 承诺若为假，对应用例就会失败。不覆盖一般功能路径，只钉住这些声明。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtkit/esx"
)

// fakeES 起一个最小的假 Elasticsearch，把每次 bulk 与 _aliases 的请求体记下来。
func fakeES(t *testing.T) (*esx.Client, *recorder) {
	t.Helper()
	rec := &recorder{}

	// 用 NewServer 而非 NewTestServer：后者默认走内存网络，而 esx 装的是自建的
	// http.Transport（带真实 dialer），够不到内存 server。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "_bulk"):
			body, _ := io.ReadAll(r.Body)
			rec.add("bulk", string(body))
			// 必须按实际动作条数应答：esutil 假设响应条数与发送条数一致，多了会越界 panic。
			_, _ = fmt.Fprintf(w, `{"took":1,"errors":false,"items":[%s]}`,
				strings.Join(bulkItemsFor(string(body)), ","))
		case r.URL.Path == "/_aliases":
			body, _ := io.ReadAll(r.Body)
			rec.add("aliases", string(body))
			_, _ = fmt.Fprint(w, `{"acknowledged":true}`)
		default:
			_, _ = fmt.Fprint(w, `{"acknowledged":true}`)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := esx.New(esx.WithAddresses(srv.URL))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c, rec
}

// bulkItemsFor 为 NDJSON 请求体里的每个动作行造一条成功应答。
func bulkItemsFor(body string) []string {
	var items []string
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var action map[string]map[string]any
		if json.Unmarshal([]byte(line), &action) != nil {
			continue
		}
		for verb, meta := range action {
			switch verb {
			case "index", "create", "update", "delete":
				id, _ := meta["_id"].(string)
				items = append(items, fmt.Sprintf(
					`{%q:{"_id":%q,"status":200,"result":"created"}}`, verb, id))
			}
		}
	}
	return items
}

type recorder struct {
	mu   sync.Mutex
	reqs []struct{ kind, body string }
}

func (r *recorder) add(kind, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, struct{ kind, body string }{kind, body})
}

func (r *recorder) of(kind string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, q := range r.reqs {
		if q.kind == kind {
			out = append(out, q.body)
		}
	}
	return out
}

// 契约：Client.Close 幂等，屏蔽底层第二次调用返回 ErrAlreadyClosed 的差异。
// 反证：去掉 Client 里的 closeOnce，第二次调用就会带回 "already closed"，本用例失败。
func TestClientCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	c, _ := fakeES(t)
	ctx := t.Context()

	for i := range 3 {
		if err := c.Close(ctx); err != nil {
			t.Fatalf("第 %d 次 Close 返回错误: %v", i+1, err)
		}
	}
	// 底层 go-elasticsearch 的 ErrAlreadyClosed 不能漏到调用方。
	if err := c.Close(ctx); err != nil && strings.Contains(err.Error(), "already closed") {
		t.Fatalf("Close 泄漏了底层的 ErrAlreadyClosed: %v", err)
	}
}

// 契约：Bulk.Close 幂等，且关闭后 Add / Flush 返回 ErrBulkClosed 而不是 panic。
func TestBulkCloseIsIdempotentAndRejectsAfterClose(t *testing.T) {
	t.Parallel()
	c, _ := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	b, err := c.NewBulk(esx.WithBulkIndex("orders"))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	for i := range 3 {
		if err := b.Close(ctx); err != nil {
			t.Fatalf("第 %d 次 Close 返回错误: %v", i+1, err)
		}
	}

	if err := b.Add(ctx, esx.BulkItem{DocumentID: "1", Doc: map[string]any{"a": 1}}); !errors.Is(err, esx.ErrBulkClosed) {
		t.Fatalf("关闭后 Add 应返回 ErrBulkClosed，实得 %v", err)
	}
	if err := b.Flush(ctx); !errors.Is(err, esx.ErrBulkClosed) {
		t.Fatalf("关闭后 Flush 应返回 ErrBulkClosed，实得 %v", err)
	}
}

// 契约：Add 与 Flush 可被多个 goroutine 并发调用。
// 反证：本用例在 -race 下跑，Bulk 若对共享状态失去保护会被竞态检测器抓到。
func TestBulkAddAndFlushAreConcurrencySafe(t *testing.T) {
	t.Parallel()
	c, _ := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	b, err := c.NewBulk(esx.WithBulkIndex("orders"), esx.WithBulkWorkers(4))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			_ = b.Add(ctx, esx.BulkItem{
				DocumentID: fmt.Sprint(i),
				Doc:        map[string]any{"i": i},
			})
		})
	}
	for range 4 {
		wg.Go(func() { _ = b.Flush(ctx) })
	}
	wg.Go(func() { _ = b.Stats() })
	wg.Wait()

	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// 契约：Flush 返回后写入器仍可继续 Add。
// 反证：若 Flush 误把写入器置为终态，这里的 Add 就会失败。
func TestBulkUsableAfterFlush(t *testing.T) {
	t.Parallel()
	c, _ := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	b, err := c.NewBulk(esx.WithBulkIndex("orders"))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(ctx) })

	if err := b.Add(ctx, esx.BulkItem{DocumentID: "1", Doc: map[string]any{"a": 1}}); err != nil {
		t.Fatalf("flush 前 Add: %v", err)
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := b.Add(ctx, esx.BulkItem{DocumentID: "2", Doc: map[string]any{"a": 2}}); err != nil {
		t.Fatalf("flush 后 Add 应仍可用，实得 %v", err)
	}
}

// 契约：Close 返回后该写入器启动的 worker goroutine 全部退出。
// 反证：按栈帧归属判定，而不是数 goroutine 总数——总数会被 httptest 的连接协程干扰，
// 那样的判据既可能漏报也可能误报。去掉 Close 里的 indexer.Close，本用例就会失败。
func TestBulkCloseLeavesNoWorkerGoroutines(t *testing.T) {
	c, _ := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	b, err := c.NewBulk(esx.WithBulkIndex("orders"), esx.WithBulkWorkers(4))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}
	for i := range 10 {
		if err := b.Add(ctx, esx.BulkItem{DocumentID: fmt.Sprint(i), Doc: map[string]any{"i": i}}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	// 关闭前先确认 worker 确实存在，否则这个用例可能因为压根没起 worker 而假绿。
	if got := workerFrames(); got == 0 {
		t.Fatalf("关闭前应存在 esutil worker goroutine，实得 0，用例无法证伪")
	}

	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	// worker 退出与 Close 返回之间可能有极短的调度延迟，给一个有界的等待窗口。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if workerFrames() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Close 返回后仍有 %d 个 esutil worker goroutine 残留", workerFrames())
}

// workerFrames 数当前存活 goroutine 里属于 esutil worker 的数量。
func workerFrames() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), "esutil.(*worker)")
}

// 契约：别名切换的 remove 与 add 在单个 _aliases 请求内完成，
// 别名不会出现不指向任何索引的中间状态。
// 反证：改成先发 remove 再发 add，请求数就会变成 2，本用例失败。
func TestSwitchAliasIsAtomicSingleRequest(t *testing.T) {
	t.Parallel()
	c, rec := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	state := esx.AliasState{Name: "orders", Targets: []string{"orders-v1"}}
	if err := c.SwitchAlias(ctx, state, "orders-v2"); err != nil {
		t.Fatalf("switch alias: %v", err)
	}

	reqs := rec.of("aliases")
	if len(reqs) != 1 {
		t.Fatalf("别名切换应只发一个 _aliases 请求，实得 %d 个", len(reqs))
	}

	var payload struct {
		Actions []map[string]json.RawMessage `json:"actions"`
	}
	if err := json.Unmarshal([]byte(reqs[0]), &payload); err != nil {
		t.Fatalf("解析请求体: %v", err)
	}

	var hasRemove, hasAdd bool
	for _, action := range payload.Actions {
		if _, ok := action["remove"]; ok {
			hasRemove = true
		}
		if _, ok := action["add"]; ok {
			hasAdd = true
		}
	}
	if !hasRemove || !hasAdd {
		t.Fatalf("同一请求内必须同时含 remove 与 add，实得 remove=%v add=%v: %s",
			hasRemove, hasAdd, reqs[0])
	}
}

// 契约：别名位置上是同名具体索引时，切换改用 remove_index 而非 remove。
// 反证：去掉 ConcreteIndex 分支就会退回 remove，本用例失败。
func TestSwitchAliasUsesRemoveIndexForConcreteIndex(t *testing.T) {
	t.Parallel()
	c, rec := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	state := esx.AliasState{Name: "orders", Targets: []string{"orders"}, ConcreteIndex: true}
	if err := c.SwitchAlias(ctx, state, "orders-v2"); err != nil {
		t.Fatalf("switch alias: %v", err)
	}

	reqs := rec.of("aliases")
	if len(reqs) != 1 {
		t.Fatalf("应只发一个 _aliases 请求，实得 %d 个", len(reqs))
	}
	if !strings.Contains(reqs[0], `"remove_index"`) {
		t.Fatalf("同名具体索引应走 remove_index，实得: %s", reqs[0])
	}
}

// 契约：BulkUpdate 的 Doc 是部分字段，会被自动包进 {"doc": ...}。
// 反证：去掉 encodeBulkBody 里的包裹，整份文档会被当作替换内容，本用例失败。
func TestBulkUpdateWrapsDoc(t *testing.T) {
	t.Parallel()
	c, rec := fakeES(t)
	ctx := t.Context()
	t.Cleanup(func() { _ = c.Close(ctx) })

	b, err := c.NewBulk(esx.WithBulkIndex("orders"))
	if err != nil {
		t.Fatalf("new bulk: %v", err)
	}
	if err := b.Add(ctx, esx.BulkItem{
		Action:     esx.BulkUpdate,
		DocumentID: "1",
		Doc:        map[string]any{"status": "paid"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := b.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	body := strings.Join(rec.of("bulk"), "")
	if !strings.Contains(body, `{"doc":{"status":"paid"}}`) {
		t.Fatalf("update 条目应包一层 doc，实得: %s", body)
	}
}
