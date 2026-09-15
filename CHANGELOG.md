# Changelog

本文件记录 esx 的版本变更。

## [v0.2.0] - 2026-09-15

### Added

- **文本分析**：`Analyze` 把一段文本按指定方式切成词项返回，携带词项文本、类型、位置与起止偏移。分析方式有三种指定途径，可按需组合：`WithAnalyzer` 给出分析器名称、`WithAnalyzeField` 沿用某字段 mapping 的配置、`WithAnalyzeTokenizer` 与 `WithAnalyzeFilters` 临时拼一条分析链；`WithAnalyzeIndex` 让索引 settings 中定义的自定义分析器可以按名字引用。待分析文本为空时在发出请求前被拒绝，索引不存在归一到 `ErrNotFound`。
- **高亮两层配置**：`HighlightWith` 设全局默认，`HighlightField` 给单个字段单独配置，两层都支持标签对（`WithHighlightTags`）、片段大小（`WithHighlightFragmentSize`）、片段数量（`WithHighlightFragments`）。字段级优先于全局，覆盖关系由 Elasticsearch 保证，本包不在客户端侧合并两层取值。原 `Highlight(fields...)` 的签名与行为不变，此时该字段沿用全局配置。
- **命中总数统计精度**：`TrackTotalHits(upTo)` 把精确统计的上界抬到指定条数，`TrackAllHits()` 要求精确统计全部命中。未设置时不生成该结构，保持 Elasticsearch 默认只精确统计到 10000 条的行为。

- **布尔组合器**：`Any`、`All`、`Not` 把若干查询组合成一个可嵌套的 bool 查询，分别表达「至少命中其一」「全部命中」「全部不命中」。`Any` 显式设置 `minimum_should_match` 而不依赖 Elasticsearch 的默认值——该默认值在同级存在 `must` 或 `filter` 时为 0、否则为 1，靠默认值会让同一个组合在不同上下文里语义不同。三者不传子句时生成不施加约束的查询，使「条件列表为空」不会变成匹配不到任何文档。

### Changed

- **依赖**：`github.com/elastic/elastic-transport-go/v8` 由 v8.9.0 升到 v8.11.0。本包只用到其 `WithTransport`，两版行为一致（都是把自建 transport 装进 `Config.Transport`），v8.11.0 仅为选项补了名称元信息。`go-elasticsearch/v9` 仍为 v9.5.2，间接依赖无增减。

- **⚠ `MultiMatch` 签名变更**：`MultiMatch(query string, fields ...string)` 改为 `MultiMatch(query string, fields []string, opts ...MultiMatchOption)`。变参 `fields` 占住末位，无法再追加变参选项，跨字段匹配的 `type` 与 `operator` 因此一直取 Elasticsearch 默认值（`best_fields` 与 `or`），跨字段中文检索常用的 `cross_fields` 取不到。改切片让选项有位置可放，导出面保持单一入口而不是留一对双胞胎函数。

  迁移：字段列表由变参改为切片，其余不变。

  ```go
  // 旧
  esx.MultiMatch("手机", "title", "body")
  // 新
  esx.MultiMatch("手机", []string{"title", "body"})
  // 新增能力
  esx.MultiMatch("张三 北京", []string{"name", "city", "address"},
      esx.WithMultiMatchType(textquerytype.Crossfields),
      esx.WithMultiMatchOperator(operator.And),
  )
  ```

## [v0.1.1] - 2026-09-15

### Fixed

- **文档**：删除 `WithPing` 关于「服务端大版本不匹配会在构造时暴露」的表述。`ping` 只调 `Info()`，而 `go-elasticsearch` 的产品检查只比对响应头 `X-Elastic-Product` 是否为 `Elasticsearch`，不读版本号，该头自 ES 7.14 起各大版本均返回。GoDoc、`doc.go` 与 README 改为陈述实际覆盖范围：连通性、认证、对端是否为 Elasticsearch。行为未变。
- **文档**：`doc.go` 与 README 的服务端版本说明改为实测结论。原措辞「对 8.x 服务端的请求体形状不保证被接受」偏轻：v9 的 typedapi 在生成代码里无条件设置 `compatible-with=9`，8.x 只接受 7 或 8，收到 9 时以 `media_type_header_exception` 返回 400（实测 `elasticsearch:8.15.0`），请求体不会被解析。

## [v0.1.0] - 2026-09-15

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
