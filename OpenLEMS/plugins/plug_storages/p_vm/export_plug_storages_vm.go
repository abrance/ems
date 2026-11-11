package p_vm

import (
	"common/c_base"
	"context"
	"p_vm/internal"
)

// NewStorageInstance 创建 VictoriaMetrics 存储实例。
//
// 配置：Url 是 vminsert / vmselect 的基础地址；写和读不在同一个端口上，
// 所以优先用 Params：
//
//	insert_url  例如 http://vminsert:8480（也可直接带 /insert/0/prometheus/api/v1/import/prometheus）
//	select_url  例如 http://vmselect:8481（也可直接带 /select/0/prometheus）
//	timeout_ms  单次 HTTP 超时，默认 30000
func NewStorageInstance(ctx context.Context, storageConfig *c_base.SStorageConfig) c_base.IStorage {
	return internal.NewVMStorage(ctx, storageConfig)
}
