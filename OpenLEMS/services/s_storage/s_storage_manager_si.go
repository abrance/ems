package s_storage

import (
	"common/c_base"
	"common/c_enum"
	"context"
	"s_storage/internal"
	"sync"
)

var (
	storageManagerInstance IStorageManager
	storageManagerOnce     sync.Once
)

// RegisterStorageManager 注册存储管理器实例
func RegisterStorageManager(manager IStorageManager) {
	storageManagerOnce.Do(func() {
		storageManagerInstance = manager
	})
}

// GetStorageManager 获取存储管理器实例
func GetStorageManager() IStorageManager {
	if storageManagerInstance == nil {
		panic("storage manager is nil !")
	}
	return storageManagerInstance
}

// NewStorageManagerWithConfig 根据配置创建存储管理器
func NewStorageManagerWithConfig(ctx context.Context, storageConfig *c_base.SStorageConfig) IStorageManager {
	// 按配置选后端：默认（含旧配置）仍走本地 prometheus TSDB
	var storage c_base.IStorage
	if storageConfig != nil && storageConfig.Type == c_enum.EStorageTypeVm {
		storage = internal.NewVMStorageInstance(ctx, storageConfig)
	} else {
		storage = internal.NewTSDBStorageInstance(ctx, storageConfig)
	}
	managerImpl := internal.NewStorageManagerImpl(ctx, storage)
	RegisterStorageManager(managerImpl)
	return managerImpl
}
