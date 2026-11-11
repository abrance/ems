package internal

import (
	"common/c_base"
	"context"
	"p_vm"
)

// NewVMStorageInstance 创建 VictoriaMetrics 存储实例
func NewVMStorageInstance(ctx context.Context, config *c_base.SStorageConfig) c_base.IStorage {
	return p_vm.NewStorageInstance(ctx, config)
}
