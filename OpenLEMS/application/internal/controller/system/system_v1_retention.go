package system

import (
	v1 "application/api/system/v1"
	retentionlogic "application/internal/logic/retention"
	"context"
	"time"

	"s_db/s_db_basic"
)

// GetRetention 保留策略状态：三条设置的实际值 + 上次清理结果 + 自动清理间隔。
//
// 界面要能回答「上次到底跑没跑、删了什么」—— 保留了却不清理，正是这个功能之前的毛病。
func (c *ControllerV1) GetRetention(ctx context.Context, _ *v1.GetRetentionReq) (res *v1.GetRetentionRes, err error) {
	return &v1.GetRetentionRes{Status: buildRetentionStatus(ctx)}, nil
}

// CleanRetention 立即执行一次清理（管理员）。让运维不用等定时任务。
func (c *ControllerV1) CleanRetention(ctx context.Context, _ *v1.CleanRetentionReq) (res *v1.CleanRetentionRes, err error) {
	retentionlogic.RunOnce(ctx)
	return &v1.CleanRetentionRes{Status: buildRetentionStatus(ctx)}, nil
}

func buildRetentionStatus(ctx context.Context) v1.RetentionStatus {
	const layout = "2006-01-02 15:04:05"
	status := v1.RetentionStatus{
		DeviceRetentionDays: retentionlogic.SettingDays(ctx, s_db_basic.SystemSettingDeviceRetentionDays),
		SystemRetentionDays: retentionlogic.SettingDays(ctx, s_db_basic.SystemSettingSystemRetentionDays),
		LogRetentionDays:    retentionlogic.SettingDays(ctx, s_db_basic.SystemSettingLogRetentionDays),
		IntervalHours:       int(retentionlogic.DefaultInterval / time.Hour),
	}

	if last := retentionlogic.Last(); last != nil {
		status.LastRunAt = last.At.Format(layout)
		status.LastRunTimeMs = last.TimeMs
		status.LastLogFilesDeleted = last.LogFilesDeleted
		status.LastLogBytesFreed = last.LogBytesFreed
		status.LastDeviceBefore = last.DeviceBefore.Format(layout)
		status.LastSystemBefore = last.SystemBefore.Format(layout)
		status.LastLogBefore = last.LogBefore.Format(layout)
		status.LastError = last.Err
	}
	return status
}
