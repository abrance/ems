package common

import "common/c_base"

var storageInstance c_base.IStorage

func RegisterStorageInstance(storage c_base.IStorage) {
	storageInstance = storage
}

func GetStorageInstance() c_base.IStorage {
	if storageInstance == nil {
		panic("storage instance is nil !")
	}
	return storageInstance
}

// GetStorageInstanceOrNil 安全获取存储实例，未初始化时返回 nil（供可不依赖存储的可选功能使用）。
func GetStorageInstanceOrNil() c_base.IStorage {
	return storageInstance
}
