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

// SpotMetricOverwrite 被重跑解析覆盖/跳过的人工修订
type SpotMetricOverwrite struct {
	Key      SpotMetricKey
	OldValue float64  // 人工填写值
	NewValue *float64 // 本次 OCR 值（为 nil 表示本次未提取到该字段）
	Origin   string   // 原值来源（manual / pm-api / ocr / cv），仅 Skipped 使用
}

// SpotMetricReplaceResult 全量替换指标的结果
//
// Preserved 与 Overwritten 互斥：默认「已确认优先」时是 Preserved，显式 force 时才是 Overwritten。
type SpotMetricReplaceResult struct {
	Overwritten []SpotMetricOverwrite // force 覆盖掉的已确认值（已写审计）
	Preserved   int                   // 原样保留的已确认行数（未覆盖）
	// Skipped 是 force=true 时被跳过的权威行（origin 为 pm-api / manual）：force 只对
	// ocr/cv 生效，这些行不覆盖也不删，逐条转成告警回到调用方。
	Skipped []SpotMetricOverwrite
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
	// 已确认（origin 为 manual / pm-api）行任何时候都原样保留、不被重跑覆盖（force=false 计数进 Preserved）；
	// force=true 也只对 ocr/cv 生效，权威行改为逐条进 Skipped（覆盖写审计不再发生）。
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
	// ListMetricsByRunDateRange 按运行日区间查询**已确认**（origin 为 manual / pm-api）指标，供趋势聚合用。
	// 口径是「只看已确认」，未复核行在 SQL 里就剔除，不把几万行明细搬到应用层再过滤。
	// 注意：同一运行日的同一指标可能有多份日报产出（表1(二) 与滞后的 表2(一)），
	// 去重与取哪一路由调用方（logic.BuildTrend）决定。
	ListMetricsByRunDateRange(ctx context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error)
	// CountUnconfirmedByRunDateRange 统计区间内因未复核而被趋势排除的指标条数（前端角标提示用）
	CountUnconfirmedByRunDateRange(ctx context.Context, from, to string) (int, error)
	GetRun(ctx context.Context, reportDate string) (*s_db_model.SSpotExtractRunModel, error)
	UpsertRun(ctx context.Context, model *s_db_model.SSpotExtractRunModel) error
}

// IHomeMarketService 官网主页两路市场数据（24 点分时 + 地市节点价）。
//
// 为什么单独一个 service 而不是塞进 ISpotExtractService：这两张表的粒度、
// 滞后与采集任务都跟 gd_spot_metric 不同（24 点 T-6、地市 T+0、日频 T+2），
// 采集互不阻塞。混在一个接口里，日后想单独停掉一路就会牵动另一路。
type IHomeMarketService interface {
	// UpsertHome24h 覆盖写某天的 24 点（唯一键 price_date+hour，重采幂等）
	UpsertHome24h(ctx context.Context, rows []*s_db_model.SHome24hModel) (int, error)
	// ListHome24h 取某天 24 点（按 hour 升序）。该日没有数据时返回空切片而不是错误 ——
	// 「这天还没出」是正常状态，页面要显示「该日无数据」。
	ListHome24h(ctx context.Context, date string) ([]*s_db_model.SHome24hModel, error)
	// LatestHome24hDate 最近一个有数据的运行日；空串表示库里一条都没有
	LatestHome24hDate(ctx context.Context) (string, error)

	// UpsertNodePrice 覆盖写某天的地市节点价（唯一键 run_date+city_code+period_type）
	UpsertNodePrice(ctx context.Context, rows []*s_db_model.SNodePriceModel) (int, error)
	// ListNodePrice 取某天的地市节点价（按 period_type、city_code 升序）
	ListNodePrice(ctx context.Context, date string) ([]*s_db_model.SNodePriceModel, error)
	// LatestNodePriceDate 最近一个有数据的运行日
	LatestNodePriceDate(ctx context.Context) (string, error)

	// RecordFetchRun 记一次采集尝试（按 target_date+dataset 覆盖），返回落库后的行。
	// 调用方只填 TargetDate/Dataset/Status/ErrorKind/RowsWritten/RowsExpected/LastError，
	// attempt_count 与两个连续计数由实现按已有行推算。
	RecordFetchRun(ctx context.Context, run *s_db_model.SFetchRunModel) (*s_db_model.SFetchRunModel, error)
	// LatestFetchRun 某路最近一次尝试（按写入顺序倒序）；没有记录时返回 nil
	LatestFetchRun(ctx context.Context, dataset string) (*s_db_model.SFetchRunModel, error)
	// LatestOKFetchRun 某路最近一次成功的行（按 target_date 倒序）；没有成功记录时返回 nil
	LatestOKFetchRun(ctx context.Context, dataset string) (*s_db_model.SFetchRunModel, error)
}

// IReportDailyService 日报核对左列（OCR 日报原值）的读写。
//
// 口径（用户拍板）：OCR 落独立表 gd_report_daily，**绝不写 gd_spot_metric**。
// 这张表里只有 ocr / manual 两种 origin，官方值永远在 gd_spot_metric（pm-api），
// 核对页右列另取一路；两边不覆盖、不合并、无 fallback。
type IReportDailyService interface {
	// UpsertBatch 按唯一键 (run_date, dataset, scope, metric) 覆盖写。
	// force=false 时保留 manual 行，并且只在「报告日比已有行更接近运行日」时才覆盖。
	// 返回写入条数、被保留的 manual 条数与被保留的「报告日更远」条数。
	UpsertBatch(ctx context.Context, rows []*s_db_model.SReportDailyModel, force bool) (ReportDailyWriteResult, error)
	// ListByRunDateRange 取 [from, to] 区间内的日报原值行（按 run_date、dataset、scope、metric 升序）
	ListByRunDateRange(ctx context.Context, from, to string) ([]*s_db_model.SReportDailyModel, error)
	// LatestRunDate 最近一个有日报原值的运行日；空串表示一条都没有
	LatestRunDate(ctx context.Context) (string, error)
	// ListOfficialDaily 读官方直采（origin=pm-api）的日频电价与申报均价，供核对页右列使用。
	// 只读；核对页的右列永远来自这里，不来自 gd_report_daily。
	ListOfficialDaily(ctx context.Context, from, to string) ([]ReportDailyOfficialRow, error)
	// BackfillFromSpotMetric 从 gd_spot_metric 一次性回填历史 OCR 行（只读那张表，单向、幂等）。
	// 返回插入行数、跳过的 void 行数与同 run_date 多份日报的冲突计数。
	BackfillFromSpotMetric(ctx context.Context) (ReportDailyBackfillResult, error)
}

// ReportDailyWriteResult 一次写入的结果
type ReportDailyWriteResult struct {
	Written   int // 实际写入（插入 + 覆盖）
	Preserved int // 因 origin=manual 而被保留、未被本次覆盖的行数
	// SkippedFarther 因已有行的报告日更接近运行日（或距离相等）而被保留、未被本次覆盖的行数
	SkippedFarther int
}

// ReportDailyBackfillResult 历史回填结果
type ReportDailyBackfillResult struct {
	Inserted int // 新插入行数
	Skipped  int // origin=void（已被官方取代）而跳过的行数
	Conflict int // 同一 run_date 有多份日报产出同一指标、按「报告日最接近运行日」只留一行时计一次
}

// ReportDailyOfficialRow 官方直采（pm-api）的一行日频指标，供核对页右列使用。
//
// 刻意不暴露 SSpotMetricModel：核对页只该看到「运行日 + 指标名 + 值」，
// 不该能顺着模型去写那张表。读路径也因此只有一个入口。
type ReportDailyOfficialRow struct {
	RunDate string
	Dataset string
	Scope   string
	Metric  string // 官方指标原名，例如「发电侧日前加权平均电价」
	Value   float64
	Unit    string
}
