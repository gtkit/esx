// Package esx 是 Elasticsearch 的通用客户端适配层，基于 github.com/elastic/go-elasticsearch/v9。
//
// 它把客户端构造、索引与别名管理、文档操作、批量写入、查询构建收敛成一组稳定契约，
// 使用方不必各自拼装 elasticsearch.Config、TLS transport、BulkIndexer 与 typedapi 请求结构。
//
// # 服务端版本
//
// 本包面向 Elasticsearch 9.x。go-elasticsearch/v9 的 typedapi 请求结构按 9.x 的 REST spec
// 生成，对 8.x 服务端的请求体形状不保证被接受。用 [WithPing] 可以让版本或连通性问题在
// 构造时暴露，而不是拖到第一次查询。
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
// # 错误处理
//
// 资源不存在统一为哨兵错误 [ErrNotFound]，用 errors.Is 判断；其余来自 Elasticsearch
// 的失败为 [*Error]，用 errors.AsType 取状态码与描述。
package esx
