// Package esx 是 Elasticsearch 的通用客户端适配层，基于 github.com/elastic/go-elasticsearch/v9。
//
// 它把客户端构造、索引与别名管理、文档操作、批量写入、查询构建收敛成一组稳定契约，
// 使用方不必各自拼装 elasticsearch.Config、TLS transport、BulkIndexer 与 typedapi 请求结构。
//
// # 服务端版本
//
// 本包面向 Elasticsearch 9.x。go-elasticsearch/v9 的 typedapi 在每个 API 的生成代码里
// 无条件设置 Accept 与 Content-Type 为 application/vnd.elasticsearch+json;compatible-with=9，
// 该头不受 elasticsearch.Config 或本包的选项控制。8.x 服务端只接受 compatible-with 为 7 或 8，
// 收到 9 时以 media_type_header_exception 返回 400（实测 8.15.0），请求体不会被解析。
// 因此本包所有走 typedapi 的操作对 8.x 服务端都失败，[WithPing] 在构造阶段即返回错误。
//
// [WithPing] 校验的是连通性、认证与对端是否为 Elasticsearch，不比对服务端版本。
//
// # 快速开始
//
//	c, err := esx.New(
//		esx.WithAddresses("https://localhost:9200"),
//		esx.WithBasicAuth("elastic", pass),
//		esx.WithPing(),
//	)
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	res, err := esx.NewSearch[Order](c, "orders").
//		Must(esx.Match("status", "paid")).
//		Sort("created_at", false).
//		Page(1, 20).
//		Do(ctx)
//
// # 分词验证
//
// 检索结果不符预期时，先看文本被切成了什么词：
//
//	tokens, err := c.Analyze(ctx, "永久免费的搜索引擎",
//		esx.WithAnalyzeIndex("articles"),
//		esx.WithAnalyzer("ik_max_word"),
//	)
//
// # 错误处理
//
// 资源不存在统一为哨兵错误 [ErrNotFound]，用 errors.Is 判断；其余来自 Elasticsearch
// 的失败为 [*Error]，用 errors.AsType 取状态码与描述。
package esx
