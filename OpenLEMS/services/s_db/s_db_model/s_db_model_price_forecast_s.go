package s_db_model

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

const (
	TablePriceForecast            = "gd_price_forecast"
	FieldPriceForecastId          = "id"
	FieldPriceForecastGeneratedOn = "generated_on"
	FieldPriceForecastMarket      = "market"
	FieldPriceForecastGrain       = "grain"
	FieldPriceForecastTargetTime  = "target_time"
	FieldPriceForecastPoint       = "point"
	FieldPriceForecastQ10         = "q10"
	FieldPriceForecastQ50         = "q50"
	FieldPriceForecastQ90         = "q90"
	FieldPriceForecastModel       = "model"
	FieldPriceForecastUnit        = "unit"
	FieldPriceForecastSource      = "source"
)

// 预测粒度
const (
	ForecastGrainDay      = "day"      // 日均（日报已确认成交价）
	ForecastGrainIntraday = "intraday" // 96 点（电网日指标 CSV）
)

// SPriceForecastModel 电价预测留痕。
//
// 为什么要留痕：复盘要回答的是「**当时**发的预测准不准」。
// 不留痕就只能事后重跑模型（回测），模型一更新历史数字就变了，那不是复盘。
//
// 唯一键 (generated_on, market, grain, target_time)：同一天对同一目标时刻只留一条，
// 重复调用（含缓存失效后的重算）覆盖而不是堆积。
type SPriceForecastModel struct {
	g.Meta      `orm:"table:gd_price_forecast"`
	Id          int64       `json:"id" orm:"id,primary,auto_increment"`
	GeneratedOn string      `json:"generatedOn" orm:"generated_on"` // 生成日 YYYY-MM-DD
	Market      string      `json:"market" orm:"market"`            // ahead / real
	Grain       string      `json:"grain" orm:"grain"`              // day / intraday
	TargetTime  string      `json:"targetTime" orm:"target_time"`   // 日粒度=YYYY-MM-DD；96 点=YYYY-MM-DD HH:MM
	Point       float64     `json:"point" orm:"point"`
	Q10         float64     `json:"q10" orm:"q10"`
	Q50         float64     `json:"q50" orm:"q50"`
	Q90         float64     `json:"q90" orm:"q90"`
	Model       string      `json:"model" orm:"model"`
	Unit        string      `json:"unit" orm:"unit"`
	Source      string      `json:"source" orm:"source"` // 序列来源说明，便于事后追责
	CreatedAt   *gtime.Time `json:"createdAt" orm:"created_at,created_at"`
	UpdatedAt   *gtime.Time `json:"updatedAt" orm:"updated_at,updated_at"`
}
