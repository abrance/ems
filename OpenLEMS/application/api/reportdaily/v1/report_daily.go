package v1

import "github.com/gogf/gf/v2/frame/g"

// ExtractDayReq 单篇日报解析（只写 gd_report_daily）
type ExtractDayReq struct {
	g.Meta `path:"/report-daily/extract" method:"post" tags:"日报核对" summary:"解析单篇日报到日报原值表" role:"admin"`
	Date   string `json:"date" dc:"日报日 YYYY-MM-DD" v:"required"`
	Force  bool   `json:"force" dc:"是否覆盖人工修订过的行（origin=manual），默认否"`
}

type ExtractDayRes struct {
	Date       string   `json:"date"`
	TradeDate  string   `json:"tradeDate" dc:"表2 承载的滞后运行日"`
	MetricRows int      `json:"metricRows" dc:"写入日报原值表的行数"`
	Preserved  int      `json:"preserved" dc:"因人工修订而被保留、未覆盖的行数"`
	// SkippedFarther 因已有行的报告日更接近运行日（或距离相等）而被保留的行数
	SkippedFarther int      `json:"skippedFarther" dc:"因已有行的报告日更接近运行日而被保留的行数"`
	Status         string   `json:"status" dc:"ok / empty"`
	Warnings       []string `json:"warnings" dc:"模板交叉校验告警，不影响入库集合"`
}

// BatchExtractReq 区间批量解析（后台串行任务，OCR 只有 2 并发槽）
type BatchExtractReq struct {
	g.Meta        `path:"/report-daily/extract/batch" method:"post" tags:"日报核对" summary:"批量解析日报到日报原值表（后台任务）" role:"admin"`
	From          string `json:"from" dc:"起始日报日 YYYY-MM-DD，留空表示最早"`
	To            string `json:"to" dc:"结束日报日 YYYY-MM-DD，留空表示最新"`
	IncludeManual bool   `json:"includeManual" dc:"是否连人工修订过的行一起覆盖，默认否"`
	DryRun        bool   `json:"dryRun" dc:"只预览计划，不执行"`
}

type BatchExtractRes struct {
	Id      string   `json:"id" dc:"任务 id"`
	Status  string   `json:"status" dc:"running / planned / done / failed"`
	Total   int      `json:"total"`
	Dates   []string `json:"dates" dc:"计划解析的日报日"`
	Skipped []string `json:"skipped" dc:"跳过的日报日及原因"`
}

type BatchStatusReq struct {
	g.Meta `path:"/report-daily/extract/batch/status" method:"get" tags:"日报核对" summary:"批量解析进度" role:"admin"`
	Id     string `json:"id" dc:"任务 id，留空取最近一次"`
}

type BatchStatusRes struct {
	Id         string   `json:"id"`
	Status     string   `json:"status" dc:"running / done / failed"`
	Total      int      `json:"total"`
	Done       int      `json:"done"`
	Succeeded  int      `json:"succeeded"`
	Failed     int      `json:"failed"`
	MetricRows int      `json:"metricRows" dc:"累计写入行数"`
	Preserved  int      `json:"preserved"`
	// SkippedFarther 累计因报告日更远（或等距）而未覆盖的行数
	SkippedFarther int      `json:"skippedFarther"`
	Current        string   `json:"current" dc:"正在处理的日报日"`
	Errors         []string `json:"errors"`
}

// BackfillReq 一次性历史回填（只读 gd_spot_metric，单向、幂等）
type BackfillReq struct {
	g.Meta `path:"/report-daily/backfill" method:"post" tags:"日报核对" summary:"从历史 OCR 行回填日报原值表" role:"admin"`
}

type BackfillRes struct {
	Inserted int `json:"inserted" dc:"新插入行数（幂等：重复执行第二次为 0）"`
	Skipped  int `json:"skipped" dc:"origin=void（已被官方取代）而跳过的行数"`
	Conflict int `json:"conflict" dc:"同一运行日多份日报产出同一指标、按报告日最接近只留一行时的冲突数"`
}

// ReconcileReq 核对页唯一数据源
type ReconcileReq struct {
	g.Meta `path:"/report-daily/reconcile" method:"get" tags:"日报核对" summary:"日报原值 vs 官网直采核对" role:"user"`
	From   string `json:"from" dc:"起始运行日 YYYY-MM-DD，缺省为最近 30 个运行日"`
	To     string `json:"to" dc:"结束运行日 YYYY-MM-DD，缺省为今天"`
}

// ReconcileItem 一行核对：左日报原值 / 右官网直采 / 差值
type ReconcileItem struct {
	// Dataset 区分同 scope+metric 的两行：日前成交与实时成交都是「发电侧 / 平均电价」，
	// 只有 dataset 不同。前端靠它给出行标签（日前 / 实时 / 申报），否则两行看起来一模一样。
	Dataset string `json:"dataset" dc:"day_ahead_trade / realtime_trade / day_ahead_decl"`
	Scope   string `json:"scope" dc:"发电侧/燃煤/燃气/新能源/零价及以下/报价600以上"`
	Metric  string `json:"metric" dc:"平均电价/申报容量/煤机容量/气机容量/新能源容量/自主申报台数/默认申报台数"`
	Unit    string `json:"unit" dc:"元/兆瓦时 | MW | 台；前端不猜单位"`
	Source  string `json:"source" dc:"日报来源表：表1(一)/表1(二)/表2(二)"`
	// ReportValue / OfficialValue 为 null 表示「没有」，**不是 0**
	ReportValue   *float64 `json:"reportValue" dc:"日报原值（OCR）；null = 该日无日报或该行未提取到"`
	OfficialValue *float64 `json:"officialValue" dc:"官网直采（pm-api）；null = 官网该日无此口径"`
	Diff          *float64 `json:"diff" dc:"日报 − 官网；仅 compare=diff 且有双值时才给，否则 null"`
	DiffPct       *float64 `json:"diffPct" dc:"diff / officialValue * 100，两位小数；仅 compare=diff"`
	Compare       string   `json:"compare" dc:"diff=真同义算差值；caliber=口径不同只并排；official_missing=官网无此口径"`
	Note          string   `json:"note" dc:"compare=caliber 时的口径说明"`
}

// ReconcileDay 一天的核对结果
type ReconcileDay struct {
	RunDate       string           `json:"runDate"`
	ReportDate    string           `json:"reportDate" dc:"产出自哪份日报；无日报时为空串"`
	MissingReport bool             `json:"missingReport" dc:"该运行日无日报（左列全 null，右列照常）"`
	Items         []*ReconcileItem `json:"items"`
}

type ReconcileRes struct {
	AsOfReport     string          `json:"asOfReport" dc:"左列最新日报日；空串 = 一条日报原值都没有"`
	AsOfOfficial   string          `json:"asOfOfficial" dc:"右列最新 pm-api 日"`
	OfficialOrigin string          `json:"officialOrigin" dc:"固定 pm-api：右列永远来自官方直采"`
	From           string          `json:"from"`
	To             string          `json:"to"`
	Days           []*ReconcileDay `json:"days" dc:"按运行日升序；整段无数据时为 []"`
	Warnings       []string        `json:"warnings" dc:"交叉校验/多份日报等提示"`
}
