package v1

import "github.com/gogf/gf/v2/frame/g"

// GetPriceForecastReq 现货电价预测请求。
//
// grain 决定序列源，不只是步长：
//
//	day（默认）—— 日报已确认（origin=manual）的日均成交价，一天一个点
//	intraday    —— 电网日指标 CSV（gd_metric.csv）的 15 分钟电价，出未来 5 日 × 96 点
type GetPriceForecastReq struct {
	g.Meta       `path:"/forecast/price" method:"get" tags:"电价预测" summary:"现货电价预测（TimesFM）" role:"user"`
	Market       string `json:"market" dc:"市场：ahead=日前（默认）/ real=实时"`
	Grain        string `json:"grain" dc:"粒度：day=日均（默认）/ intraday=96 点"`
	HistoryDays  int    `json:"historyDays" dc:"送入模型的历史天数，默认 60（intraday 默认 30）"`
	HorizonDays  int    `json:"horizonDays" dc:"预测天数，默认 5；intraday 上限 5（96×5=480 ≤ max_horizon 512）"`
	Quantiles    string `json:"quantiles" dc:"分位点，逗号分隔，默认 0.1,0.5,0.9"`
	ForceRefresh bool   `json:"forceRefresh" dc:"跳过缓存，手动刷新用"`
}

// PriceForecastPoint 单点预测结果
type PriceForecastPoint struct {
	RunDate string `json:"runDate" dc:"运行日 YYYY-MM-DD（intraday 时取目标时刻的日期部分）"`
	// TargetTime 目标时刻：day 粒度 = "YYYY-MM-DD"；intraday = "YYYY-MM-DD HH:MM"
	TargetTime string             `json:"targetTime" dc:"目标时刻"`
	Point      float64            `json:"point" dc:"点预测（中位）"`
	Quantiles  map[string]float64 `json:"quantiles" dc:"分位预测，键为分位字符串（如 0.5）"`
	AtTime     string             `json:"atTime,omitempty" dc:"intraday 时的 HH:MM，便于按天分组绘图"`
}

// PriceForecast 电价预测结果
type PriceForecast struct {
	Market         string                `json:"market" dc:"市场：ahead / real"`
	Grain          string                `json:"grain" dc:"粒度：day / intraday"`
	StepMinutes    float64               `json:"stepMinutes" dc:"采样步长（分钟）：day=1440，intraday=15"`
	Recorded       int                   `json:"recorded" dc:"本次写入留痕的条数（复盘用）"`
	GeneratedOn    string                `json:"generatedOn" dc:"留痕生成日 YYYY-MM-DD"`
	Dataset        string                `json:"dataset" dc:"数据来源数据集"`
	Scope          string                `json:"scope" dc:"对象（发电侧）"`
	Metric         string                `json:"metric" dc:"指标名（平均电价）"`
	Unit           string                `json:"unit" dc:"单位"`
	Model          string                `json:"model" dc:"预测模型"`
	HistoryDates   []string              `json:"historyDates" dc:"历史运行日，升序"`
	HistoryValues  []float64             `json:"historyValues" dc:"历史日均成交价"`
	Forecast       []*PriceForecastPoint `json:"forecast" dc:"未来多日预测，按日升序"`
	ContextPoints  int                   `json:"contextPoints" dc:"送入模型的点数"`
	InputChannels  int                   `json:"inputChannels" dc:"送入模型的通道数：1=只用本市场历史；实时电价=3（实时价+日前价+负荷）"`
	InputTruncated bool                  `json:"inputTruncated" dc:"历史是否被截断至模型上限"`
	MissingDays    int                   `json:"missingDays" dc:"区间内无已确认数据的运行日数"`
	From           string                `json:"from" dc:"历史起点运行日"`
	To             string                `json:"to" dc:"历史终点运行日"`
	TimeMs         float64               `json:"timeMs" dc:"预测耗时（毫秒）"`
}

// GetPriceForecastRes 电价预测响应
type GetPriceForecastRes struct {
	Forecast *PriceForecast `json:"forecast" dc:"预测结果"`
}
