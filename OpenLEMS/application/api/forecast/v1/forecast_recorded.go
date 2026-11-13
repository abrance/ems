package v1

import (
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/frame/g"
)

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
	// Actual 该目标日的官方实际日均价（gd_spot_metric，发电侧加权平均电价）。
	// 指针：0 元/MWh 是合法电价，不能拿 0 当「没有」；没有实际值时为 null。
	Actual *float64 `json:"actual" dc:"官方日均实际值，没有则为 null"`
}

// GetRecordedForecastRes 留痕列表
type GetRecordedForecastRes struct {
	Points         []*RecordedForecastPoint `json:"points" dc:"按目标时刻升序"`
	GeneratedDates []string                 `json:"generatedDates" dc:"有哪些生成日，前端据此提示留痕从哪天开始"`
	// 真值口径：与日频回测腿同一个来源，页面才不会出现两个「实际值截至」
	DataAsOf    string `json:"dataAsOf" dc:"官方实际值最新日期 YYYY-MM-DD"`
	LagDays     int    `json:"lagDays" dc:"实际值滞后天数；-1 表示拿不到"`
	Stale       bool   `json:"stale" dc:"滞后超过阈值"`
	TruthSource string `json:"truthSource" dc:"真值来源说明，页面原样展示"`
	// Backfilled 生成日晚于目标日、按「事后回填」剔除的条数（那不是当时发出的预测）
	Backfilled int `json:"backfilled"`
	// ActualMissing 有留痕但官方实际值还没出的点数
	ActualMissing int `json:"actualMissing"`
	// ActualUnavailable 真值读不到/不可配对的原因；空字符串表示正常
	ActualUnavailable string `json:"actualUnavailable" dc:"真值不可用的原因，页面原样展示"`
}

// GetPriceBacktestReq 电价预测回测
type GetPriceBacktestReq struct {
	g.Meta `path:"/forecast/backtest" method:"get" tags:"电价预测" summary:"电价预测回测（用当前模型在历史数据上重跑）" role:"user"`
	Market string `json:"market" dc:"ahead（默认）/ real"`
	// Grain day（默认，官方日频日均价）/ intraday（96 点，真值来自已停更的第三方 CSV，仅历史演示）
	Grain        string `json:"grain" dc:"day（默认）/ intraday"`
	From         string `json:"from" v:"required" dc:"目标日起点 YYYY-MM-DD"`
	To           string `json:"to" v:"required" dc:"目标日终点 YYYY-MM-DD"`
	SampleDays   int    `json:"sampleDays" dc:"抽样间隔天数，默认 3"`
	ForceRefresh bool   `json:"forceRefresh" dc:"跳过缓存，手动刷新用"`
	// AllowStale 实际值滞后超过阈值时仍要结果。不传就报错（带明确滞后天数），
	// 避免页面/脚本拿几周前的真值算出一个看着像「最近表现」的数字。
	AllowStale bool `json:"allowStale" dc:"实际值滞后超阈值时仍返回历史区间结果"`
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
	// HistoryDays 每次 as-of 预测送入模型的历史天数（day 粒度有意义）
	HistoryDays int `json:"historyDays"`
	// ActualMissing 因该时刻没有实际值而丢弃的预测点数（按时间戳配对配不上的）
	ActualMissing int     `json:"actualMissing"`
	TimeMs        float64 `json:"timeMs" dc:"推理总耗时（毫秒）"`
	// DataAsOf 本次用到的实际值最新日期；LagDays 该日期距今天数；
	// Stale=true 表示滞后超阈值，结果只能代表历史，页面必须明确告知。
	DataAsOf    string `json:"dataAsOf" dc:"实际值最新日期 YYYY-MM-DD"`
	LagDays     int    `json:"lagDays" dc:"实际值滞后天数"`
	Stale       bool   `json:"stale" dc:"实际值滞后超过阈值"`
	TruthSource string `json:"truthSource" dc:"真值来源说明，页面原样展示"`
}

// CodePriceTruthStale 回测真值滞后超阈值（服务端不静默返回旧区间）。
//
// 单独一个码，前端据此区分「数据陈旧，要不要看历史区间」和普通失败（不必按文本猜）。
var CodePriceTruthStale = gcode.New(6001, "电价数据陈旧", nil)
