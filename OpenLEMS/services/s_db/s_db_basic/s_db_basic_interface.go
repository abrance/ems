package s_db_basic

import (
	"common/c_base"
	"common/c_enum"
	"context"
	"s_db/s_db_model"
	"time"
)

// IDeviceService 设备服务
type IDeviceService interface {
	GetDeviceConfigsWithRecursion(ctx context.Context, parentId string) ([]*c_base.SDeviceConfig, error) // 获取所有设备列表（包括enabled=false）
	GetAllDevices(ctx context.Context) ([]*s_db_model.SDeviceModel, error)                               // 获取所有设备
	GetDeviceById(ctx context.Context, id string) (*s_db_model.SDeviceModel, error)                      // 根据ID获取设备
	GetDevicesByProtocolId(ctx context.Context, protocolId string) ([]*s_db_model.SDeviceModel, error)   // 根据协议ID获取设备列表
	UpdateDevice(ctx context.Context, deviceId string, data map[string]interface{}) error
	CreateDevice(ctx context.Context, device *s_db_model.SDeviceModel) (string, error)
	DeleteDeviceById(ctx context.Context, id string) error // 根据ID删除设备
}

type ISettingService interface {
	GetAllSettings(ctx context.Context) ([]*s_db_model.SSettingModel, error)
	GetAllSettingsByGroup(ctx context.Context, group string) ([]*s_db_model.SSettingModel, error) // 根据分组获取所有设置
	GetSettingById(ctx context.Context, id string) (*s_db_model.SSettingModel, error)             // 根据ID获取设置详情
	GetSettingValueById(ctx context.Context, id string) *string                                   // 获取设置，如果获取不到，返回nil
	SetSettingValueById(ctx context.Context, id string, value string) error
	GetSettingValueBySystemSettingDefine(ctx context.Context, settingDefine *SSystemSettingDefine) *string // 通过系统设置定义获取设置值
	GetRootDeviceId(ctx context.Context) *string                                                           // 获取根设备ID
	GetRootPolicyId(ctx context.Context) *string                                                           // 获取激活的策略ID
	GetPublicEnabledSettings(ctx context.Context) ([]*s_db_model.SSettingModel, error)                     // 获取公开且启用的设置
}

type IProtocolService interface {
	GetProtocolList(ctx context.Context, type_ string) ([]*s_db_model.SProtocolModel, error)
	UpdateProtocol(ctx context.Context, protocolId string, data map[string]interface{}) error
	CreateProtocol(ctx context.Context, data map[string]interface{}) (string, error)
	DeleteProtocol(ctx context.Context, protocolId string) error
	GetAllProtocolConfigs(ctx context.Context) ([]*c_base.SProtocolConfig, error) // 获取协议列表
}

type IAlarmService interface {
	// 告警历史相关方法
	CreateAlarmHistory(ctx context.Context, deviceId, sourceDeviceId string, meta c_base.IPoint, level c_enum.EAlarmLevel, detail string, triggerAt time.Time) error
	GetAlarmHistoryByDeviceId(ctx context.Context, deviceId string) ([]*s_db_model.SAlarmHistoryModel, error)
	GetAlarmHistoryByDeviceIdAndPoint(ctx context.Context, deviceId, point string) ([]*s_db_model.SAlarmHistoryModel, error)
	DeleteAlarmHistoryByDeviceId(ctx context.Context, deviceId string) error
	DeleteAlarmHistoryByLevel(ctx context.Context, level string) error
	DeleteAlarmHistoryByDeviceIdAndLevel(ctx context.Context, deviceId, level string) error
	DeleteAlarmHistoryByFilters(ctx context.Context, deviceId, level string) error
	GetAllAlarmHistory(ctx context.Context) ([]*s_db_model.SAlarmHistoryModel, error)
	GetAlarmHistoryPage(ctx context.Context, page, pageSize int, filters map[string]interface{}) ([]*s_db_model.SAlarmHistoryModel, int, error)
	ClearAllAlarmHistory(ctx context.Context) error
	GetAlarmHistoryCount(ctx context.Context, deviceId string) int

	// 告警忽略相关方法
	CreateAlarmIgnore(ctx context.Context, deviceId, sourceDeviceId, point, pointName string) error
	GetAlarmIgnoreByDeviceId(ctx context.Context, deviceId string) ([]*s_db_model.SAlarmIgnoreModel, error)
	IsAlarmIgnored(ctx context.Context, deviceId, sourceDeviceId, point string) (bool, error)
	DeleteAlarmIgnoreByDeviceId(ctx context.Context, deviceId string) error
	DeleteAlarmIgnoreByDeviceIdAndPoint(ctx context.Context, deviceId, point string) error
	DeleteAlarmIgnoreByFilters(ctx context.Context, deviceId, point string) error
	GetAllAlarmIgnore(ctx context.Context) ([]*s_db_model.SAlarmIgnoreModel, error)
	GetAlarmIgnorePage(ctx context.Context, page, pageSize int, filters map[string]interface{}) ([]*s_db_model.SAlarmIgnoreModel, int, error)
	GetAlarmIgnoreCount(ctx context.Context, deviceId string) int
}

type ILogService interface {
	CreateLog(ctx context.Context, logType, deviceId, level, content string) error
	GetLogByDeviceId(ctx context.Context, deviceId string) ([]*s_db_model.SLogModel, error)
	GetLogByType(ctx context.Context, logType string) ([]*s_db_model.SLogModel, error)
	GetLogByLevel(ctx context.Context, level string) ([]*s_db_model.SLogModel, error)
	GetLogByDeviceIdAndType(ctx context.Context, deviceId, logType string) ([]*s_db_model.SLogModel, error)
	DeleteLogByDeviceId(ctx context.Context, deviceId string) error
	DeleteLogByType(ctx context.Context, logType string) error
	DeleteLogByFilters(ctx context.Context, filters map[string]interface{}) (int, error)
	GetAllLog(ctx context.Context) ([]*s_db_model.SLogModel, error)
	GetLogPage(ctx context.Context, page, pageSize int, filters map[string]interface{}) ([]*s_db_model.SLogModel, int, error)
	ClearAllLog(ctx context.Context) error
	// DeleteLogBefore 删除 before 之前写入的日志（日志保留策略用），返回删除行数
	DeleteLogBefore(ctx context.Context, before time.Time) (int, error)
	GetLogCount(ctx context.Context) (int, error)
}

type IAutomationService interface {
	// 基础 CRUD 方法
	CreateAutomation(ctx context.Context, name string, startTime, endTime *time.Time, timeRangeType, timeRangeValue *string, triggerRule, executeRule string, executionInterval int) (int, error)
	GetAutomationById(ctx context.Context, id int) (*s_db_model.SAutomationModel, error)
	UpdateAutomation(ctx context.Context, id int, data map[string]interface{}) error
	DeleteAutomation(ctx context.Context, id int) error

	// 查询方法
	GetAllAutomations(ctx context.Context) ([]*s_db_model.SAutomationModel, error)
	GetAutomationsByTimeRangeType(ctx context.Context, timeRangeType string) ([]*s_db_model.SAutomationModel, error)
	GetEnabledAutomations(ctx context.Context) ([]*s_db_model.SAutomationModel, error)
	GetAutomationsByFilters(ctx context.Context, deviceId string, filters map[string]interface{}) ([]*s_db_model.SAutomationModel, error)
	GetAutomationPage(ctx context.Context, page, pageSize int, deviceId string, filters map[string]interface{}) ([]*s_db_model.SAutomationModel, int, error)

	// 统计方法
	ClearAllAutomations(ctx context.Context) error
	GetAutomationCount(ctx context.Context) (int, error)
}

// IEnergyStorageStrategyService 储能策略服务接口
type IEnergyStorageStrategyService interface {
	// 基础 CRUD 方法
	CreateEnergyStorage(ctx context.Context, model *s_db_model.SEnergyStorageModel) (int, error)
	GetEnergyStorageById(ctx context.Context, id int) (*s_db_model.SEnergyStorageModel, error)
	UpdateEnergyStorage(ctx context.Context, model *s_db_model.SEnergyStorageModel) error
	DeleteEnergyStorage(ctx context.Context, id int) error

	// 查询与分页
	GetEnergyStoragePage(ctx context.Context, page, pageSize int, filters map[string]interface{}) ([]*s_db_model.SEnergyStorageModel, int, error)
	GetEnergyStorageByIds(ctx context.Context, ids []int) ([]*s_db_model.SEnergyStorageModel, error)
	GetEnabledEnergyStorages(ctx context.Context) ([]*s_db_model.SEnergyStorageModel, error) // 获取所有启用的储能定时任务
}

// IPriceService 电价服务接口
type IPriceService interface {
	// 基础 CRUD 方法
	CreatePrice(ctx context.Context, model *s_db_model.SPriceModel) (int, error)
	GetPriceById(ctx context.Context, id int) (*s_db_model.SPriceModel, error)
	UpdatePrice(ctx context.Context, model *s_db_model.SPriceModel) error
	DeletePrice(ctx context.Context, id int) error

	// 查询与分页
	GetPricePage(ctx context.Context, page, pageSize int, filters map[string]interface{}) ([]*s_db_model.SPriceModel, int, error)
	GetAllPrices(ctx context.Context) ([]*s_db_model.SPriceModel, error)
	GetEnabledPrices(ctx context.Context) ([]*s_db_model.SPriceModel, error)                     // 获取所有启用的电价
	GetPricesByRemoteId(ctx context.Context, remoteId string) ([]*s_db_model.SPriceModel, error) // 根据远程ID获取电价

	// 统计方法
	GetPriceCount(ctx context.Context) (int, error)
}

// ISpotReportService 广东电力现货运行日报档案
type ISpotReportService interface {
	List(ctx context.Context) ([]*s_db_model.SSpotReportModel, error)
	GetByDate(ctx context.Context, runDate string) (*s_db_model.SSpotReportModel, error)
	GetLatest(ctx context.Context) (*s_db_model.SSpotReportModel, error)
	Upsert(ctx context.Context, model *s_db_model.SSpotReportModel) error
}

// SpotMetricKey 指标定位键（与 gd_spot_metric 唯一键一致）
type SpotMetricKey struct {
	ReportDate  string
	RunDate     string
	Dataset     string
	Scope       string
	Metric      string
	SourceLabel string // 来源表（如 表1(二)），仅用于提示
}

// SpotMetricOverwrite 被重跑解析覆盖的人工修订
type SpotMetricOverwrite struct {
	Key      SpotMetricKey
	OldValue float64  // 人工填写值
	NewValue *float64 // 本次 OCR 值（为 nil 表示本次未提取到该字段）
}

// SpotMetricReplaceResult 全量替换指标的结果
//
// Preserved 与 Overwritten 互斥：默认「已确认优先」时是 Preserved，显式 force 时才是 Overwritten。
type SpotMetricReplaceResult struct {
	Overwritten []SpotMetricOverwrite // force 覆盖掉的已确认值（已写审计）
	Preserved   int                   // 原样保留的已确认行数（未覆盖）
	// PreservedDiff 保留下来但本次解析算出了不同值的行（OldValue=已确认值，NewValue=本次 OCR 值）。
	// 值不变就不记，仅用于告警——否则解析器修好的 bug 永远不会落到已确认行上，而没人知道。
	PreservedDiff []SpotMetricOverwrite
}

// SpotMetricCounts 某份日报的复核进度：pending=未复核（ocr+cv），confirmed=已确认（manual）
type SpotMetricCounts struct {
	Pending   int
	Confirmed int
}

// IPriceForecastService 电价预测留痕
type IPriceForecastService interface {
	// UpsertBatch 按 (generated_on, market, grain, target_time) 覆盖写入
	UpsertBatch(ctx context.Context, rows []*s_db_model.SPriceForecastModel) error
	// ListByTargetRange 取目标时刻落在 [from, to] 内的留痕（升序）。
	// from/to 对日粒度是 YYYY-MM-DD，对 96 点是 YYYY-MM-DD HH:MM（字符串比较即可，格式对齐）。
	ListByTargetRange(ctx context.Context, from, to, market, grain string) ([]*s_db_model.SPriceForecastModel, error)
	// GeneratedDates 返回区间内有哪些生成日，前端用来提示「留痕从哪天开始有」
	GeneratedDates(ctx context.Context, market, grain string) ([]string, error)
}

// ISpotExtractService 现货日报结构化提取结果
type ISpotExtractService interface {
	// ReplaceMetrics 全量替换某份日报的提取指标（同一事务：先删后插）。
	// 已确认（origin=manual）行默认原样保留、不被重跑覆盖（force=false，Preserved 计数）；
	// force=true 时才「以 OCR 为准」覆盖它们，覆盖前逐条写审计并返回 Overwritten。
	ReplaceMetrics(ctx context.Context, reportDate string, metrics []*s_db_model.SSpotMetricModel, force bool) (*SpotMetricReplaceResult, error)
	// UpdateMetricValue 人工修订单个指标值（origin 置为 manual，并写审计）
	UpdateMetricValue(ctx context.Context, key SpotMetricKey, value float64, operator string) (*s_db_model.SSpotMetricModel, error)
	// ConfirmMetrics 把某份日报中所有未复核项（origin != manual，即 ocr + cv 预填）
	// 标记为人工已确认（origin=manual），并写审计。dataset 为空表示该日报全部数据集。返回确认条数。
	ConfirmMetrics(ctx context.Context, reportDate, dataset, operator string) (int, error)
	// CountMetricsByReport 按日报日一次性统计未复核/已确认条数（列表页用，避免 N+1）
	CountMetricsByReport(ctx context.Context) (map[string]SpotMetricCounts, error)
	// ListMetricAudit 查询某份日报的指标变更审计
	ListMetricAudit(ctx context.Context, reportDate string, limit int) ([]*s_db_model.SSpotMetricAuditModel, error)
	// ListMetrics 按日报查询指标（含该日报里滞后运行日的数据）
	ListMetrics(ctx context.Context, reportDate string) ([]*s_db_model.SSpotMetricModel, error)
	// ListMetricsByRunDate 按运行日查询指标（跨日报聚合，供趋势/交叉校验用）
	ListMetricsByRunDate(ctx context.Context, runDate string) ([]*s_db_model.SSpotMetricModel, error)
	// ListMetricsByRunDateRange 按运行日区间查询**已确认**（origin=manual）指标，供趋势聚合用。
	// 口径是「只看已确认」，未复核行在 SQL 里就剔除，不把几万行明细搬到应用层再过滤。
	// 注意：同一运行日的同一指标可能有多份日报产出（表1(二) 与滞后的 表2(一)），
	// 去重与取哪一路由调用方（logic.BuildTrend）决定。
	ListMetricsByRunDateRange(ctx context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error)
	// CountUnconfirmedByRunDateRange 统计区间内因未复核而被趋势排除的指标条数（前端角标提示用）
	CountUnconfirmedByRunDateRange(ctx context.Context, from, to string) (int, error)
	GetRun(ctx context.Context, reportDate string) (*s_db_model.SSpotExtractRunModel, error)
	UpsertRun(ctx context.Context, model *s_db_model.SSpotExtractRunModel) error
}
