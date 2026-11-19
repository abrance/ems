package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

// GetRetentionReq 读取数据保留策略的当前状态
type GetRetentionReq struct {
	g.Meta `path:"/system/retention" method:"get" tags:"系统相关" summary:"数据保留策略状态" role:"user"`
}

// RetentionStatus 保留策略状态
type RetentionStatus struct {
	DeviceRetentionDays int    `json:"deviceRetentionDays" dc:"设备数据保留天数"`
	SystemRetentionDays int    `json:"systemRetentionDays" dc:"系统数据保留天数"`
	LogRetentionDays    int    `json:"logRetentionDays" dc:"日志保留天数"`
	IntervalHours       int    `json:"intervalHours" dc:"自动清理间隔（小时）"`
	LastRunAt           string `json:"lastRunAt" dc:"上次清理时间（本地时间，空=本次启动后还没跑过）"`
	LastRunTimeMs       int64  `json:"lastRunTimeMs" dc:"上次清理耗时（毫秒）"`
	LastLogFilesDeleted int    `json:"lastLogFilesDeleted" dc:"上次删掉的磁盘日志文件个数"`
	LastLogBytesFreed   int64  `json:"lastLogBytesFreed" dc:"上次释放的磁盘日志字节数"`
	LastDeviceBefore    string `json:"lastDeviceBefore" dc:"上次按设备保留天数算出的截止时间"`
	LastSystemBefore    string `json:"lastSystemBefore" dc:"上次按系统保留天数算出的截止时间"`
	LastLogBefore       string `json:"lastLogBefore" dc:"上次按日志保留天数算出的截止时间"`
	LastError           string `json:"lastError" dc:"上次清理的错误（空=成功）"`
}

type GetRetentionRes struct {
	Status RetentionStatus `json:"status" dc:"保留策略状态"`
}

// CleanRetentionReq 手动触发一次清理
type CleanRetentionReq struct {
	g.Meta `path:"/system/retention/clean" method:"post" tags:"系统相关" summary:"立即执行数据清理" role:"admin"`
}
type CleanRetentionRes struct {
	Status RetentionStatus `json:"status"`
}
