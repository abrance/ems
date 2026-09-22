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
	Dataset  string   `json:"dataset" dc:"数据集：day_ahead_decl/day_ahead_trade/realtime_trade/summary"`
	Scope    string   `json:"scope" dc:"对象：发电侧/燃煤/燃气/新能源/零价及以下/报价600以上/市场"`
	Metric   string   `json:"metric" dc:"指标名"`
	Value    float64  `json:"value" dc:"数值"`
	Unit     string   `json:"unit" dc:"单位"`
	AtTime   string   `json:"atTime" dc:"最高/最低电价出现时刻"`
	MomPct   *float64 `json:"momPct" dc:"环比%"`
	SharePct *float64 `json:"sharePct" dc:"占比%"`
	Source   string   `json:"source" dc:"来源：表1(一)/表1(二)/表2(一)/表2(二)/正文"`
	Conf     float64  `json:"conf" dc:"OCR 置信度"`
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
}

type ExtractSpotReportRes struct {
	Date        string   `json:"date"`
	TradeDate   string   `json:"tradeDate" dc:"表2 承载的滞后运行日"`
	MetricCount int      `json:"metricCount"`
	Status      string   `json:"status"`
	Warnings    []string `json:"warnings"`
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
