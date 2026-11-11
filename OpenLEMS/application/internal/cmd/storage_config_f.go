package cmd

import (
	"common/c_base"
	"common/c_enum"
	"os"
)

// storageConfigFromEnv 决定用哪个存储后端，缺省仍是本地 prometheus TSDB。
//
// 环境变量（部署时给，代码里不留地址）：
//
//	EMS_STORAGE_TYPE=vm              切到 VictoriaMetrics；不设或 tsdb 走本地 TSDB
//	EMS_VM_INSERT_URL=http://…:8480  写样本（vminsert）
//	EMS_VM_SELECT_URL=http://…:8481  查样本（vmselect）
func storageConfigFromEnv() *c_base.SStorageConfig {
	cfg := &c_base.SStorageConfig{
		Enable: true,
		Type:   c_enum.EStorageTypeTsdb,
		Params: map[string]string{},
	}
	if os.Getenv("EMS_STORAGE_TYPE") != string(c_enum.EStorageTypeVm) {
		return cfg
	}
	cfg.Type = c_enum.EStorageTypeVm
	if v := os.Getenv("EMS_VM_INSERT_URL"); v != "" {
		cfg.Params["insert_url"] = v
	}
	if v := os.Getenv("EMS_VM_SELECT_URL"); v != "" {
		cfg.Params["select_url"] = v
	}
	cfg.Url = os.Getenv("EMS_VM_BASE_URL")
	return cfg
}
