# Changelog

本文件记录 esx 的版本变更。

## v0.1.0

### Added

- 首个版本：基于 `github.com/elastic/go-elasticsearch/v9` 的 Elasticsearch 通用客户端适配层，面向 ES 9.x 服务端。
- **客户端**：`New(opts ...Option)` 一次构造出同时具备低级操作与类型化操作的客户端，两者经 `NewTypedFrom` 共享同一传输层与连接池。选项覆盖节点地址、BasicAuth、API Key、CA 证书、重试次数与状态码、请求超时、跳过 TLS 校验（仅限开发调试）、启动连通性校验。传输层默认 TLS 最低版本 1.2，拨号、握手、响应头超时均非零。`Close(ctx)` 幂等，屏蔽底层第二次调用返回 `ErrAlreadyClosed` 的差异。
- **索引与别名**：`IndexExists`、`CreateIndex`（可带 mapping 与别名，非法 JSON 在发请求前拒绝）、`DeleteIndices`（空列表为无操作）、`ResolveAlias`、`SwitchAlias`。别名切换的 remove 与 add 在单个 `_aliases` 请求内完成，不出现别名不指向任何索引的中间状态；能识别「别名位置上其实是同名具体索引」并改用 `remove_index`。
- **文档操作**：泛型 `GetDoc[T]`，以及 `IndexDoc`、`UpdateDoc`、`DeleteDoc`、`DocExists`。索引名与文档 id 为空时在发出请求前被拒绝。
- **批量写入**：`NewBulk` 构造，透出 `Add` / `Flush` / `Close` / `Stats`。条目级结果经回调投递；`Add` 与 `Flush` 并发安全；`Close` 幂等且返回后 worker goroutine 全部退出；关闭后调用返回 `ErrBulkClosed` 而非 panic。
- **查询构建**：泛型链式 `Search[T]`，覆盖 bool 子句、多级排序、页码与偏移分页、`search_after`、高亮、聚合、`_source` 过滤，以及命中解码为 `T`。附 `Term`、`Terms`、`Match`、`MatchPhrase`、`MultiMatch`、`Prefix`、`Wildcard`、`Exists`、`MatchAll`、`Range`、`DateRange` 常用查询构造器。
- **错误模型**：哨兵错误 `ErrNotFound` 与 `ErrBulkClosed`，以及携带操作名、索引名、HTTP 状态码的 `*Error`。`typedapi` 对 404 的处理并不统一（`core/get` 与 `core/delete` 把 404 当正常响应，`core/update` 返回错误），本包统一归一到 `ErrNotFound`。

### Changed

- **⚠ module path 修正**：`githut.com/gtkit/esx` → `github.com/gtkit/esx`。`githut` 为拼写错误，修正时仓库尚未发布任何 tag，无引用方。
