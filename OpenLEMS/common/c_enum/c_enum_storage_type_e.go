package c_enum

type EStorageType string

const (
	EStorageTypeInfluxDB1 EStorageType = "idb1"
	EStorageTypeInfluxDB2 EStorageType = "idb2"
	EStorageTypePebbledb  EStorageType = "pdb"
	EStorageTypeTsdb      EStorageType = "tsdb"
	// EStorageTypeVm VictoriaMetrics（集群版：vminsert 写 / vmselect 查）
	EStorageTypeVm EStorageType = "vm"
)
