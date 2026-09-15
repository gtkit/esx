# esx

Elasticsearch 通用客户端适配层，基于 [go-elasticsearch/v9](https://github.com/elastic/go-elasticsearch)。

把客户端构造、索引与别名管理、文档操作、批量写入、查询构建收敛成一组稳定契约，
使用方不必各自拼装 `elasticsearch.Config`、TLS transport、BulkIndexer 与 typedapi 请求结构。

- 面向 **Elasticsearch 9.x** 服务端
- 除 `go-elasticsearch/v9` 外无第三方直接依赖
- Go 1.27+

## 安装

```bash
go get github.com/gtkit/esx
```

## 快速开始

```go
package main

import (
	"context"
	"errors"
	"log"

	"github.com/gtkit/esx"
)

type Order struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount int64  `json:"amount"`
}

func main() {
	ctx := context.Background()

	c, err := esx.New(
		esx.WithAddresses("https://localhost:9200"),
		esx.WithBasicAuth("elastic", "••••••"),
		esx.WithPing(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close(ctx)

	res, err := esx.NewSearch[Order](c, "orders").
		Must(esx.Match("status", "paid")).
		Filter(esx.Range("amount", new(100.0), nil)). // Go 1.26 起 new 接受表达式
		Sort("created_at", false).
		Page(1, 20).
		Do(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, hit := range res.Hits {
		log.Println(hit.ID, hit.Doc.Status)
	}
	log.Println("总数", res.Total)

	if _, err := esx.GetDoc[Order](ctx, c, "orders", "missing"); errors.Is(err, esx.ErrNotFound) {
		log.Println("文档不存在")
	}
}
```

## API 概览

| 函数 / 方法 | 说明 |
|---|---|
| `New(opts...)` | 构造客户端 |
| `c.Close(ctx)` | 关闭连接，幂等 |
| `c.Typed()` / `c.ES()` | 取底层类型化 / 低级客户端 |
| `c.IndexExists(ctx, index)` | 索引是否存在，不存在返回 `(false, nil)` |
| `c.CreateIndex(ctx, index, opts...)` | 创建索引，可带 mapping 与别名 |
| `c.DeleteIndices(ctx, indices...)` | 删除索引，空列表为无操作 |
| `c.ResolveAlias(ctx, alias)` | 解析别名当前指向 |
| `c.SwitchAlias(ctx, state, newIndex)` | 单请求内原子切换别名 |
| `GetDoc[T](ctx, c, index, id)` | 读取文档并解码为 `T` |
| `c.IndexDoc(ctx, index, id, doc)` | 写入文档，已存在时整体替换 |
| `c.UpdateDoc(ctx, index, id, partial)` | 部分字段更新 |
| `c.DeleteDoc(ctx, index, id)` | 删除文档 |
| `c.DocExists(ctx, index, id)` | 文档是否存在，不存在返回 `(false, nil)` |
| `c.NewBulk(opts...)` | 构造批量写入器 |
| `b.Add/Flush/Close/Stats` | 提交 / 刷写 / 关闭 / 统计 |
| `NewSearch[T](c, index)` | 构造泛型查询 |
| `s.Do/DoAgg/Count` | 执行查询 / 只取聚合 / 只取总数 |

## Options

### 客户端 `New`

| Option | 默认值 | 说明 |
|---|---|---|
| `WithAddresses(addrs...)` | `ELASTICSEARCH_URL`，否则 `http://localhost:9200` | 节点地址，多个则轮询 |
| `WithBasicAuth(user, pass)` | 无 | 用户名密码认证 |
| `WithAPIKey(key)` | 无 | API Key 认证 |
| `WithCACert(pem)` | 无 | 校验服务端证书的 CA |
| `WithRetry(n, status...)` | 不重试；状态码默认 502/503/504 | 重试次数与触发状态码 |
| `WithRequestTimeout(d)` | 5s | 请求、拨号、TLS 握手超时 |
| `WithInsecureSkipVerify()` | 关闭 | 跳过证书校验，**仅限开发调试** |
| `WithPing()` | 关闭 | 构造时做一次连通性校验 |

### 索引 `CreateIndex`

| Option | 默认值 | 说明 |
|---|---|---|
| `WithIndexMapping(json)` | 无 | mapping 与 settings，非法 JSON 在发请求前拒绝 |
| `WithIndexAliases(aliases...)` | 无 | 创建时挂别名并设为写索引 |

### 批量写入 `NewBulk`

| Option | 默认值 | 说明 |
|---|---|---|
| `WithBulkIndex(index)` | 无 | 条目未指定索引时的默认索引 |
| `WithBulkWorkers(n)` | `runtime.NumCPU()` | 并发 worker 数 |
| `WithBulkFlushBytes(n)` | 5 MiB | 触发刷写的字节阈值 |
| `WithBulkFlushInterval(d)` | 2s | 定时刷写间隔 |
| `WithBulkOnSuccess(fn)` | 无 | 条目成功回调 |
| `WithBulkOnFailure(fn)` | 无 | 条目失败回调 |
| `WithBulkOnError(fn)` | 无 | 写入器级错误回调 |

## 客户端

`New` 一次构造出同时具备低级操作与类型化操作的客户端，两者共享同一传输层与连接池。

```go
c, err := esx.New(
	esx.WithAddresses("http://a:9200", "http://b:9200"), // 多地址轮询与故障转移
	esx.WithBasicAuth("user", "pass"),                   // 或 esx.WithAPIKey(key)
	esx.WithCACert(pem),                                 // 自签 CA
	esx.WithRetry(3, 502, 503, 504, 429),                // 重试次数与触发状态码
	esx.WithRequestTimeout(5*time.Second),               // 同时用作拨号与握手超时
	esx.WithPing(),                                      // 构造时做一次连通性校验
)
```

未指定地址时沿用 `go-elasticsearch` 的回退顺序：环境变量 `ELASTICSEARCH_URL`，其后 `http://localhost:9200`。

传输层默认值：TLS 最低版本 1.2，拨号、TLS 握手、响应头超时均非零（默认 5 秒，随
`WithRequestTimeout` 变化）。`WithInsecureSkipVerify` 可跳过证书校验，**仅限本地开发调试**。

本包未覆盖的操作用 `c.Typed()` 与 `c.ES()` 取底层客户端。

### 使用约束

- `Close(ctx)` 阻塞至传输层关闭完成或 ctx 到期。因为低级与类型化客户端共享传输层，
  它同时作用于两者；调用后不要再使用 `Typed()` 或 `ES()` 返回的客户端。
- `Close` 幂等：重复调用返回首次调用的结果。底层 `go-elasticsearch` 的 `Close` 第二次
  调用会返回 `ErrAlreadyClosed`，本包屏蔽了这一差异。
- `WithPing` 失败时 `New` 返回错误而非一个不可用的客户端，并顺手关闭已建立的连接。

## 索引与别名

```go
ok, err := c.IndexExists(ctx, "orders")                       // 不存在返回 (false, nil)
err = c.CreateIndex(ctx, "orders-20260915",
	esx.WithIndexMapping(mappingJSON),                        // 非法 JSON 在发请求前被拒绝
	esx.WithIndexAliases("orders"))                           // 同时挂别名并设为写索引
err = c.DeleteIndices(ctx, "orders-old-1", "orders-old-2")    // 空列表是无操作
```

零停机重建索引：

```go
state, err := c.ResolveAlias(ctx, "orders")     // 解析别名当前指向
// ... 建新索引、灌数据 ...
err = c.SwitchAlias(ctx, state, "orders-20260915")
err = c.DeleteIndices(ctx, state.Targets...)    // 切换成功后再清理旧索引
```

### 使用约束

- `SwitchAlias` 把移除旧目标与添加新目标放进同一个 `_aliases` 请求，别名不会出现不指向
  任何索引的中间状态——分两次请求会让该窗口内的查询报 `index_not_found_exception`。
- `ResolveAlias` 会识别「别名位置上其实是同名具体索引」这一历史遗留情形并置
  `ConcreteIndex`，`SwitchAlias` 据此改用 `remove_index`。把 `ResolveAlias` 的结果原样传给
  `SwitchAlias`，不要自行构造 `AliasState`。
- `SwitchAlias` 后新索引是该别名的写索引。

## 文档操作

```go
order, err := esx.GetDoc[Order](ctx, c, "orders", "1")   // 泛型函数，解码 _source 为 T
err = c.IndexDoc(ctx, "orders", "1", order)              // 已存在时整体替换
err = c.UpdateDoc(ctx, "orders", "1", map[string]any{"status": "shipped"})
err = c.DeleteDoc(ctx, "orders", "1")
ok, err := c.DocExists(ctx, "orders", "1")               // 文档或索引不存在均返回 (false, nil)
```

### 使用约束

- 文档不存在时 `GetDoc`、`UpdateDoc`、`DeleteDoc` 返回的错误满足 `errors.Is(err, esx.ErrNotFound)`。
  `DeleteDoc` 是否当作幂等成功由调用方决定。
- `GetDoc` 中 `_source` 解码失败返回的错误**不**匹配 `ErrNotFound`，两者可以区分。
- 索引名与文档 id 为空时在发出请求之前就被拒绝。

## 批量写入

```go
b, err := c.NewBulk(
	esx.WithBulkIndex("orders"),
	esx.WithBulkWorkers(4),
	esx.WithBulkFlushBytes(5<<20),
	esx.WithBulkFlushInterval(2*time.Second),
	esx.WithBulkOnFailure(func(ctx context.Context, item esx.BulkItem, err error) {
		log.Printf("写入失败 %s: %v", item.DocumentID, err)
	}),
)
if err != nil {
	return err
}
defer b.Close(ctx)

err = b.Add(ctx, esx.BulkItem{DocumentID: "1", Doc: order})
err = b.Add(ctx, esx.BulkItem{Action: esx.BulkUpdate, DocumentID: "2", Doc: map[string]any{"status": "paid"}})
err = b.Add(ctx, esx.BulkItem{Action: esx.BulkDelete, DocumentID: "3"})

err = b.Flush(ctx)          // 主动刷写，返回后仍可继续 Add
stats := b.Stats()          // 累计统计
```

### 使用约束

- **条目的成功与失败通过 `WithBulkOnSuccess` / `WithBulkOnFailure` 回调投递**，`Add` 的返回值
  只表示条目是否入队。想知道某条是否真的写进去了，必须注册回调。
- 回调在 worker goroutine 上执行，回调函数自身要保证并发安全。
- `Add` 与 `Flush` 可被多个 goroutine 并发调用；并发的 `Flush` 会被串行化。
- `NewBulk` 成功返回时 worker goroutine 已启动，必须调用 `Close` 回收。`Close` 幂等，
  返回后该写入器的 worker goroutine 均已退出。
- 关闭后调用 `Add` 或 `Flush` 返回满足 `errors.Is(err, esx.ErrBulkClosed)` 的错误，不 panic。
- `Action` 为空按 `BulkIndex` 处理；`BulkUpdate` 的 `Doc` 是部分字段，会被自动包进 `{"doc": ...}`。

## 查询构建

`Search[T]` 是泛型链式构建器，类型参数在类型上而非方法上——Go 1.27 的泛型方法不能出现在
接口方法集中，做成方法会让持有它的类型无法被接口抽象。

```go
s := esx.NewSearch[Order](c, "orders").
	Must(esx.Match("title", "手机")).                  // 参与打分
	Filter(esx.Term("tenant_id", 7)).                 // 不打分、可缓存
	MustNot(esx.Exists("deleted_at")).
	Should(esx.Prefix("sku", "A"), esx.Prefix("sku", "B")).
	MinimumShouldMatch(1).
	Sort("created_at", false).Sort("id", true).       // 多级排序按调用顺序
	Page(2, 20).
	Highlight("title").
	Select("id", "title", "status")

res, err := s.Do(ctx)        // 命中解码为 Order
total, err := s.Count(ctx)   // 只要总数，不受排序分页影响
aggs, err := s.Agg("by_status", statusAgg).DoAgg(ctx) // size 置 0，只取聚合
```

深度分页用 `SearchAfter`，游标取自上一页最后一条命中的排序值。

内置查询构造器：`Term`、`Terms`、`Match`、`MatchPhrase`、`MultiMatch`、`Prefix`、`Wildcard`、
`Exists`、`MatchAll`、`Range`、`DateRange`。未覆盖的查询直接构造 `types.Query` 传给
`Must` / `Filter` 即可。

### 使用约束

- 同类子句多次调用是累积而非覆盖；`Agg` 同名重复追加时后者覆盖前者。
- `Page` 对页码小于 1、每页条数非正做归一，不报错。
- 构建过程不是并发安全的；构建完成后的 `Do` / `DoAgg` / `Count` 可并发调用。
- `Result.Total` 受 ES 的 `track_total_hits` 限制，默认最多精确到 10000，
  超出时 `TotalRelation` 为 `"gte"`。

## 错误处理

```go
// 资源不存在
if errors.Is(err, esx.ErrNotFound) { ... }

// 取状态码与上下文
if e, ok := errors.AsType[*esx.Error](err); ok {
	log.Println(e.Op, e.Index, e.StatusCode)
}
```

`*Error` 携带操作名 `Op`、索引名 `Index`、HTTP 状态码 `StatusCode` 与底层错误 `Err`。
传输层失败或请求发出前就被拒绝时 `StatusCode` 为 0。上下文被取消时错误链中可用
`errors.Is` 匹配到 `context.Canceled` 或 `context.DeadlineExceeded`。

## 版本

模块路径 `github.com/gtkit/esx`，版本号见 `version.go`。
