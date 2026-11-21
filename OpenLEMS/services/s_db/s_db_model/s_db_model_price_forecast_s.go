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
	FieldPriceForecastAlgo        = "algo"
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
	ForecastGrainDay      = "day"      // 日均（官方直采成交价）
	ForecastGrainHour     = "hour"     // 24 点（官网主页 24 点分时）
	ForecastGrainIntraday = "intraday" // 96 点（电网日指标 CSV）
)

// 预测算法。与 model 不同：model 是版本名（会随升级变化），algo 是稳定的对比维度。
// 回测按 algo 分组比较，不能拿 model 分组——升一次模型历史就被劈成两半。
const (
	ForecastAlgoTimesFM = "timesfm"
	ForecastAlgoGBM     = "gbm"
)

// 电价预测的数据源。粒度与数据源不是一回事：同一份数据只能出一种粒度，
// 但“多细的步长”与“读哪张表”混成一个参数时，加第三路就只能靠约定猜。
const (
	PriceSourceSpotMetric  = "spot_metric"   // 官方披露接口直采（pm-api）日均成交价
	PriceSourceHome24h     = "home24h"       // 官网主页网关 24 点分时
	PriceSourceGdMetricCSV = "gd_metric_csv" // 第三方电网日指标 CSV（已停更）
)

// SPriceForecastModel 电价预测留痕。
//
// 为什么要留痕：复盘要回答的是「**当时**发的预测准不准」。
// 不留痕就只能事后重跑模型（回测），模型一更新历史数字就变了，那不是复盘。
//
// 唯一键 (generated_on, market, grain, algo, target_time)：同一天同一算法对同一目标
// 时刻只留一条，重复调用（含缓存失效后的重算）覆盖而不是堆积。algo 必须进唯一键，
// 否则 TimesFM 与 GBM 对同一目标时刻会互相覆盖，算法对比就只剩最后一个写入者的数据。
type SPriceForecastModel struct {
	g.Meta      `orm:"table:gd_price_forecast"`
	Id          int64       `json:"id" orm:"id,primary,auto_increment"`
	GeneratedOn string      `json:"generatedOn" orm:"generated_on"` // 生成日 YYYY-MM-DD
	Market      string      `json:"market" orm:"market"`            // ahead / real
	Grain       string      `json:"grain" orm:"grain"`              // day / hour / intraday
	Algo        string      `json:"algo" orm:"algo"`                // timesfm / gbm：算法对比维度，稳定不变
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
