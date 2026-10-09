package v1

import "github.com/gogf/gf/v2/frame/g"

// GetRecordedForecastReq 读取预测留痕
type GetRecordedForecastReq struct {
	g.Meta `path:"/forecast/recorded" method:"get" tags:"电价预测" summary:"预测留痕（复盘用）" role:"user"`
	From   string `json:"from" v:"required" dc:"目标时刻起点：day 用 YYYY-MM-DD，intraday 用 YYYY-MM-DD HH:MM"`
	To     string `json:"to" v:"required" dc:"目标时刻终点"`
	Market string `json:"market" dc:"ahead（默认）/ real"`
	Grain  string `json:"grain" dc:"day / intraday（默认）"`
}

// RecordedForecastPoint 一条留痕
type RecordedForecastPoint struct {
	GeneratedOn string  `json:"generatedOn" dc:"生成日 YYYY-MM-DD"`
	TargetTime  string  `json:"targetTime" dc:"目标时刻"`
	Point       float64 `json:"point"`
	Q10         float64 `json:"q10"`
	Q50         float64 `json:"q50"`
	Q90         float64 `json:"q90"`
	Model       string  `json:"model"`
	Unit        string  `json:"unit"`
	Source      string  `json:"source"`
}

// GetRecordedForecastRes 留痕列表
type GetRecordedForecastRes struct {
	Points         []*RecordedForecastPoint `json:"points" dc:"按目标时刻升序"`
	GeneratedDates []string                 `json:"generatedDates" dc:"有哪些生成日，前端据此提示留痕从哪天开始"`
}

// GetPriceBacktestReq 电价预测回测
type GetPriceBacktestReq struct {
	g.Meta       `path:"/forecast/backtest" method:"get" tags:"电价预测" summary:"电价预测回测（用当前模型在历史数据上重跑）" role:"user"`
	Market       string `json:"market" dc:"ahead（默认）/ real"`
	From         string `json:"from" v:"required" dc:"目标日起点 YYYY-MM-DD"`
	To           string `json:"to" v:"required" dc:"目标日终点 YYYY-MM-DD"`
	SampleDays   int    `json:"sampleDays" dc:"抽样间隔天数，默认 3"`
	ForceRefresh bool   `json:"forceRefresh" dc:"跳过缓存，手动刷新用"`
}

// BacktestPoint 一个目标时刻的预实对比
type BacktestPoint struct {
	TargetTime string  `json:"targetTime" dc:"目标时刻 YYYY-MM-DD HH:MM"`
	Predicted  float64 `json:"predicted"`
	Actual     float64 `json:"actual"`
	Q10        float64 `json:"q10"`
	Q50        float64 `json:"q50"`
	Q90        float64 `json:"q90"`
}

// GetPriceBacktestRes 回测结果
type GetPriceBacktestRes struct {
	Market      string           `json:"market"`
	Grain       string           `json:"grain"`
	Unit        string           `json:"unit"`
	Model       string           `json:"model"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	SampleDays  int              `json:"sampleDays"`
	TargetDates []string         `json:"targetDates" dc:"实际参与回测的目标日"`
	Skipped     []string         `json:"skipped" dc:"因数据不足跳过的日期"`
	Points      []*BacktestPoint `json:"points"`
	// ActualMissing 因该时刻没有实际值而丢弃的预测点数（按时间戳配对配不上的）
	ActualMissing int     `json:"actualMissing"`
	TimeMs        float64 `json:"timeMs" dc:"推理总耗时（毫秒）"`
}
