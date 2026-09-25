package v1

import "github.com/gogf/gf/v2/frame/g"

type SpotPage struct {
	Page   int    `json:"page" dc:"页码"`
	Label  string `json:"label" dc:"页标签"`
	URL    string `json:"url" dc:"图片地址"`
	Key    string `json:"key" dc:"对象存储键"`
	Width  int    `json:"width" dc:"宽度"`
	Height int    `json:"height" dc:"高度"`
}

type SpotReport struct {
	Date        string      `json:"date" dc:"运行日 YYYY-MM-DD"`
	Title       string      `json:"title" dc:"文章标题"`
	SourceURL   string      `json:"sourceUrl" dc:"微信文章链接"`
	PageCount   int         `json:"pageCount" dc:"页数"`
	Pages       []*SpotPage `json:"pages" dc:"日报原图"`
	CollectedAt string      `json:"collectedAt" dc:"采集时间"`
	// 复核进度：pending=未复核（ocr+cv），confirmed=已确认（manual）；两者皆 0 表示尚未解析
	PendingCount   int `json:"pendingCount" dc:"未复核指标数"`
	ConfirmedCount int `json:"confirmedCount" dc:"已确认指标数"`
}

type ListSpotReportReq struct {
	g.Meta `path:"/spot-report" method:"get" tags:"现货日报" summary:"获取现货日报列表" role:"user"`
}

type ListSpotReportRes struct {
	List []*SpotReport `json:"list" dc:"日报列表"`
}

type GetHomeSpotReportReq struct {
	g.Meta `path:"/spot-report/home" method:"get" tags:"现货日报" summary:"获取首页默认现货日报" role:"user"`
}

type GetHomeSpotReportRes struct {
	Empty  bool        `json:"empty" dc:"档案是否为空"`
	Date   string      `json:"date" dc:"当前运行日"`
	Dates  []string    `json:"dates" dc:"可切换运行日"`
	Report *SpotReport `json:"report" dc:"当前日报"`
}

type GetSpotReportDetailReq struct {
	g.Meta `path:"/spot-report/detail" method:"get" tags:"现货日报" summary:"获取指定运行日现货日报" role:"user"`
	Date   string `json:"date" dc:"运行日 YYYY-MM-DD" v:"required"`
}

type GetSpotReportDetailRes struct {
	Report *SpotReport `json:"report" dc:"日报详情"`
}

type CollectSpotReportReq struct {
	g.Meta `path:"/spot-report/collect" method:"post" tags:"现货日报" summary:"采集微信现货运行日报" role:"admin"`
	URL    string `json:"url" dc:"微信公众号文章链接" v:"required"`
}

type CollectSpotReportRes struct {
	Report *SpotReport `json:"report" dc:"采集结果"`
}

// ---- 结构化数据提取（OCR + 模板） ----

// SpotMetric 单条指标
type SpotMetric struct {
	RunDate   string   `json:"runDate" dc:"运行日（表2 为滞后运行日）"`
	Dataset   string   `json:"dataset" dc:"数据集：day_ahead_decl/day_ahead_trade/realtime_trade/summary"`
	Scope     string   `json:"scope" dc:"对象：发电侧/燃煤/燃气/新能源/零价及以下/报价600以上/市场"`
	Metric    string   `json:"metric" dc:"指标名"`
	Value     float64  `json:"value" dc:"数值"`
	Unit      string   `json:"unit" dc:"单位"`
	AtTime    string   `json:"atTime" dc:"最高/最低电价出现时刻"`
	TextValue string   `json:"textValue" dc:"非数值载荷，如地市电价档位区间 [800,+inf)"`
	MomPct    *float64 `json:"momPct" dc:"环比%"`
	SharePct  *float64 `json:"sharePct" dc:"占比%"`
	Source    string   `json:"source" dc:"来源：表1(一)/表1(二)/表2(一)/表2(二)/正文"`
	Conf      float64  `json:"conf" dc:"OCR 置信度"`
	Origin    string   `json:"origin" dc:"复核状态：ocr/cv=未复核，manual=已确认"`
	EditedBy  string   `json:"editedBy" dc:"人工修订人"`
	EditedAt  string   `json:"editedAt" dc:"人工修订时间"`
}

// SpotMetricAudit 指标变更审计条目
type SpotMetricAudit struct {
	RunDate   string   `json:"runDate"`
	Dataset   string   `json:"dataset"`
	Scope     string   `json:"scope"`
	Metric    string   `json:"metric"`
	OldValue  *float64 `json:"oldValue"`
	NewValue  *float64 `json:"newValue"`
	Operation string   `json:"operation" dc:"edit=人工修订，overwritten_by_extract=被重跑覆盖"`
	Operator  string   `json:"operator"`
	CreatedAt string   `json:"createdAt"`
}

type UpdateSpotMetricReq struct {
	g.Meta   `path:"/spot-report/metric" method:"post" tags:"现货日报" summary:"人工修订单个指标" role:"admin"`
	Date     string  `json:"date" v:"required" dc:"日报日 YYYY-MM-DD"`
	RunDate  string  `json:"runDate" v:"required" dc:"运行日"`
	Dataset  string  `json:"dataset" v:"required"`
	Scope    string  `json:"scope"`
	Metric   string  `json:"metric" v:"required"`
	Value    float64 `json:"value"`
	Operator string  `json:"operator" dc:"修订人备注，缺省为当前角色"`
}

type UpdateSpotMetricRes struct {
	Metric *SpotMetric `json:"metric"`
}

type BatchExtractReq struct {
	g.Meta `path:"/spot-report/extract/batch" method:"post" tags:"现货日报" summary:"批量重跑解析（后台任务）" role:"admin"`
	From   string `json:"from" dc:"起始日报日 YYYY-MM-DD，留空表示最早"`
	To     string `json:"to" dc:"结束日报日 YYYY-MM-DD，留空表示最新"`
	// IncludeConfirmed 为 true 时连「已全部人工复核」的日报一起重跑（会覆盖复核状态，审计留痕）
	IncludeConfirmed bool `json:"includeConfirmed" dc:"是否连已人工复核的日报一起重跑，默认否"`
	// DryRun 只返回计划不执行，便于启动前确认会覆盖哪些日报
	DryRun bool `json:"dryRun" dc:"只预览计划，不执行"`
}

type BatchExtractRes struct {
	Id               string   `json:"id"`
	Total            int      `json:"total" dc:"本次将重跑的日报数"`
	Status           string   `json:"status"`
	Dates            []string `json:"dates" dc:"将重跑的日报日"`
	SkippedConfirmed []string `json:"skippedConfirmed" dc:"已跳过（已人工复核）的日报日"`
}

type BatchExtractStatusReq struct {
	g.Meta `path:"/spot-report/extract/batch/status" method:"get" tags:"现货日报" summary:"批量重跑进度" role:"admin"`
	Id     string `json:"id" dc:"任务 id，留空表示最近一次"`
}

type BatchExtractStatusRes struct {
	Id               string   `json:"id"`
	Status           string   `json:"status" dc:"running/done/failed"`
	Total            int      `json:"total"`
	Done             int      `json:"done"`
	Succeeded        int      `json:"succeeded"`
	Failed           int      `json:"failed"`
	Overwritten      int      `json:"overwritten" dc:"被本次重跑覆盖的人工值条数（已写审计）"`
	Current          string   `json:"current" dc:"当前处理中的日报日"`
	SkippedConfirmed []string `json:"skippedConfirmed" dc:"本次跳过（已人工复核）的日报日"`
	Errors           []string `json:"errors"`
	StartedAt        string   `json:"startedAt"`
	FinishedAt       string   `json:"finishedAt"`
}

type ConfirmSpotMetricReq struct {
	g.Meta   `path:"/spot-report/confirm" method:"post" tags:"现货日报" summary:"确认未复核的提取/预填值" role:"admin"`
	Date     string `json:"date" v:"required" dc:"日报日 YYYY-MM-DD"`
	Dataset  string `json:"dataset" dc:"留空表示确认该日报全部数据集"`
	Operator string `json:"operator" dc:"确认人，缺省为当前角色"`
}

type ConfirmSpotMetricRes struct {
	Confirmed int `json:"confirmed" dc:"确认条数"`
}

type GetSpotReportAuditReq struct {
	g.Meta `path:"/spot-report/audit" method:"get" tags:"现货日报" summary:"指标变更审计" role:"admin"`
	Date   string `json:"date" v:"required"`
	Limit  int    `json:"limit" dc:"默认 50，最大 200"`
}

type GetSpotReportAuditRes struct {
	List []*SpotMetricAudit `json:"list"`
}

// SpotExtractInfo 提取运行信息
type SpotExtractInfo struct {
	Status     string   `json:"status" dc:"ok/partial/failed"`
	OcrModel   string   `json:"ocrModel" dc:"OCR 档位"`
	PageCount  int      `json:"pageCount" dc:"识别页数"`
	OcrMs      int      `json:"ocrMs" dc:"OCR 总耗时(ms)"`
	Warnings   []string `json:"warnings" dc:"校验告警"`
	UpdatedAt  string   `json:"updatedAt" dc:"最近提取时间"`
	ExtractorV string   `json:"extractorVersion" dc:"提取器版本"`
}

type ExtractSpotReportReq struct {
	g.Meta `path:"/spot-report/extract" method:"post" tags:"现货日报" summary:"解析现货日报数据（OCR+模板）" role:"admin"`
	Date   string `json:"date" v:"required" dc:"运行日 YYYY-MM-DD"`
	// Force 为 true 时才以 OCR 为准覆盖已确认值；默认保留已确认行（复核状态优先）
	Force bool `json:"force" dc:"是否覆盖已确认值，默认否（保留已确认，只更新未复核项）"`
}

type ExtractSpotReportRes struct {
	Date        string   `json:"date"`
	TradeDate   string   `json:"tradeDate" dc:"表2 承载的滞后运行日"`
	MetricCount int      `json:"metricCount"`
	Status      string   `json:"status"`
	Warnings    []string `json:"warnings"`
	Preserved   int      `json:"preserved" dc:"本次未覆盖、原样保留的已确认条数"`
	Pending     int      `json:"pending" dc:"解析后该日报仍待人工确认的条数"`
}

type GetSpotReportDataReq struct {
	g.Meta `path:"/spot-report/data" method:"get" tags:"现货日报" summary:"获取现货日报结构化数据" role:"user"`
	Date   string `json:"date" v:"required" dc:"运行日 YYYY-MM-DD"`
}

type GetSpotReportDataRes struct {
	Date      string           `json:"date"`
	TradeDate string           `json:"tradeDate"`
	Metrics   []*SpotMetric    `json:"metrics"`
	Extract   *SpotExtractInfo `json:"extract"`
}

// SpotTrendPoint 趋势点：一个桶内一条序列的聚合结果
type SpotTrendPoint struct {
	Bucket  string  `json:"bucket" dc:"桶键：2026-09-20 / 2026-W39 / 2026-09"`
	From    string  `json:"from" dc:"桶内实际最早运行日"`
	To      string  `json:"to" dc:"桶内实际最晚运行日"`
	Dataset string  `json:"dataset"`
	Scope   string  `json:"scope"`
	Metric  string  `json:"metric"`
	Avg     float64 `json:"avg"`
	Max     float64 `json:"max"`
	Min     float64 `json:"min"`
	Sum     float64 `json:"sum"`
	Days    int     `json:"days"`
	Unit    string  `json:"unit"`
	// SharePct 仅申报容量类：Σ容量 ÷ Σ申报容量 × 100（桶内重算，不是各天占比平均）
	SharePct *float64 `json:"sharePct,omitempty"`
	// Value / TextValue / Count 仅地市档位：档位序号、区间文本、命中该档位的城市-天数
	Value     float64 `json:"value"`
	TextValue string  `json:"textValue,omitempty"`
	Count     int     `json:"count,omitempty"`
}

type GetSpotReportTrendReq struct {
	g.Meta `path:"/spot-report/trend" method:"get" tags:"现货日报" summary:"现货指标趋势（按运行日分桶聚合）" role:"user"`
	From   string `json:"from" dc:"起始运行日 YYYY-MM-DD，缺省为本月 1 日"`
	To     string `json:"to" dc:"结束运行日 YYYY-MM-DD，缺省为今天"`
	Grain  string `json:"grain" dc:"分桶粒度：day/week/month，缺省按区间跨度推断"`
}

type GetSpotReportTrendRes struct {
	From   string            `json:"from"`
	To     string            `json:"to"`
	Grain  string            `json:"grain"`
	Points []*SpotTrendPoint `json:"points"`
	// ExcludedPending 区间内因未复核（origin != manual）被排除的指标条数
	ExcludedPending int `json:"excludedPending"`
}
