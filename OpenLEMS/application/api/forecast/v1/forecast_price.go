package v1

import "github.com/gogf/gf/v2/frame/g"

// GetPriceForecastReq 现货电价预测请求。
//
// grain 决定序列源，不只是步长：
//
//	source=spot_metric   grain=day      —— 官方直采（origin=pm-api）的日均成交价，一天一个点
//	source=home24h       grain=hour     —— 官网主页 24 点分时，出目标日 24 个整点
//	source=gd_metric_csv grain=intraday —— 电网日指标 CSV（gd_metric.csv）的 15 分钟电价（已停更）
//
// 两者交叉校验，不一致直接报参数错误（见 logic.ParsePriceSource）；只给 grain 时按上表反推 source。
type GetPriceForecastReq struct {
	g.Meta       `path:"/forecast/price" method:"get" tags:"电价预测" summary:"现货电价预测（TimesFM）" role:"user"`
	Market       string `json:"market" dc:"市场：ahead=日前（默认）/ real=实时"`
	Source       string `json:"source" dc:"数据源：spot_metric（默认）/ home24h / gd_metric_csv，与 grain 交叉校验"`
	Grain        string `json:"grain" dc:"粒度：day=日均（默认）/ hour=24 点 / intraday=96 点"`
	TargetDate   string `json:"targetDate" dc:"目标日 YYYY-MM-DD（仅 grain=hour，默认今天，晚于今天报错）"`
	HistoryDays  int    `json:"historyDays" dc:"送入模型的历史天数，默认 60（hour 默认 14，intraday 默认 30）"`
	HorizonDays  int    `json:"horizonDays" dc:"预测天数，默认 5；intraday 上限 5；hour 忽略（步数由目标日与锚点推出）"`
	Quantiles    string `json:"quantiles" dc:"分位点，逗号分隔，默认 0.1,0.5,0.9"`
	ForceRefresh bool   `json:"forceRefresh" dc:"跳过缓存，手动刷新用"`
	Compare      bool   `json:"compare" dc:"仅 grain=hour：额外跑 GBM（modelman price-gbm）做算法对比"`
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
	Market string `json:"market" dc:"市场：ahead / real"`
	// Source 数据源：spot_metric / home24h / gd_metric_csv。前端据此出角标，不靠猜 grain。
	Source        string                `json:"source" dc:"数据源"`
	Grain         string                `json:"grain" dc:"粒度：day / hour / intraday"`
	TargetDate    string                `json:"targetDate" dc:"目标日 YYYY-MM-DD（仅 grain=hour）"`
	StepMinutes   float64               `json:"stepMinutes" dc:"采样步长（分钟）：day=1440，hour=60，intraday=15"`
	Recorded      int                   `json:"recorded" dc:"本次写入留痕的条数（复盘用）"`
	GeneratedOn   string                `json:"generatedOn" dc:"留痕生成日 YYYY-MM-DD"`
	Dataset       string                `json:"dataset" dc:"数据来源数据集"`
	Scope         string                `json:"scope" dc:"对象（发电侧）"`
	Metric        string                `json:"metric" dc:"指标名（平均电价）"`
	Unit          string                `json:"unit" dc:"单位"`
	Model         string                `json:"model" dc:"预测模型"`
	HistoryDates  []string              `json:"historyDates" dc:"历史运行日，升序"`
	HistoryValues []float64             `json:"historyValues" dc:"历史日均成交价"`
	Forecast      []*PriceForecastPoint `json:"forecast" dc:"未来多日预测，按日升序"`
	// Actual 仅 hour：目标日 24 点实际值；该日不满 24 点时为**空数组**（不是补 0）。
	Actual         []float64 `json:"actual" dc:"目标日 24 点实际值（仅 grain=hour；不满 24 点为空数组）"`
	ContextPoints  int       `json:"contextPoints" dc:"送入模型的点数"`
	InputChannels  int       `json:"inputChannels" dc:"送入模型的通道数：1=只用本市场历史；实时电价=3（实时价+日前价+负荷）"`
	InputTruncated bool      `json:"inputTruncated" dc:"历史是否被截断至模型上限"`
	MissingDays    int       `json:"missingDays" dc:"区间内无已确认数据的运行日数"`
	From           string    `json:"from" dc:"历史起点运行日"`
	To             string    `json:"to" dc:"历史终点运行日"`
	TimeMs         float64   `json:"timeMs" dc:"预测耗时（毫秒）"`
	// GBM 仅 hour + compare：第二算法（LightGBM 分位回归）对同一目标日的 24 点预测。
	GBM *PriceForecastGBM `json:"gbm,omitempty" dc:"第二算法预测（仅 hour + compare）"`
	// GBMError 对比不可用的原因（未配置/调用失败）。有它时 TimesFM 结果仍然有效。
	GBMError string `json:"gbmError,omitempty" dc:"对比算法不可用的原因"`
}

// PriceForecastGBM 第二算法（modelman price-gbm / LightGBM）的结果。
//
// 与 TimesFM 的 forecast 同构，前端可以并排画也能逐点算误差；分开一个对象是
// 为了让"哪个模型出的数"在响应里不靠猜。
type PriceForecastGBM struct {
	Model       string                `json:"model" dc:"模型标识"`
	TargetDate  string                `json:"targetDate" dc:"目标日 YYYY-MM-DD"`
	HorizonDays int                   `json:"horizonDays" dc:"目标日距锚点天数"`
	HistoryDays int                   `json:"historyDays" dc:"送入的连续完整日数"`
	Forecast    []*PriceForecastPoint `json:"forecast" dc:"目标日 24 点预测，按整点升序"`
	TimeMs      float64               `json:"timeMs" dc:"GBM 推理耗时（毫秒）"`
}

// GetPriceForecastRes 电价预测响应
type GetPriceForecastRes struct {
	Forecast *PriceForecast `json:"forecast" dc:"预测结果"`
}
