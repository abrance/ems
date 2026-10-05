package v1

import "github.com/gogf/gf/v2/frame/g"

// GetEffectSummaryReq 储能效果汇总请求
type GetEffectSummaryReq struct {
	g.Meta `path:"/effect/summary" method:"get" tags:"效果评估" summary:"储能效果汇总（峰谷套利+需量节省）" role:"user"`
	Start  string `json:"start" dc:"开始日期 2006-01-02，默认今日"`
	End    string `json:"end" dc:"结束日期 2006-01-02，默认今日"`
}

// EffectSummary 效果汇总
type EffectSummary struct {
	Start        string   `json:"start" dc:"开始时间"`
	End          string   `json:"end" dc:"结束时间"`
	Points       int      `json:"points" dc:"参与计算的采样点数"`
	GridDeviceId string   `json:"gridDeviceId" dc:"关口设备ID"`
	EssDeviceIds []string `json:"essDeviceIds" dc:"储能设备ID列表"`
	DemandPrice  float64  `json:"demandPrice" dc:"需量电价 元/kW"`

	ChargeEnergy     float64 `json:"chargeEnergy" dc:"充电量 kWh"`
	DischargeEnergy  float64 `json:"dischargeEnergy" dc:"放电量 kWh"`
	ChargeCost       float64 `json:"chargeCost" dc:"充电电费 元"`
	DischargeRevenue float64 `json:"dischargeRevenue" dc:"放电收益 元"`
	ArbitrageProfit  float64 `json:"arbitrageProfit" dc:"峰谷套利净收益 元"`

	ActualMaxDemand   float64 `json:"actualMaxDemand" dc:"实际最大需量 kW"`
	BaselineMaxDemand float64 `json:"baselineMaxDemand" dc:"无储能反算最大需量 kW"`
	DemandSaving      float64 `json:"demandSaving" dc:"需量节省 元"`

	TotalSaving float64 `json:"totalSaving" dc:"总节省 元"`
}

// GetEffectSummaryRes 储能效果汇总响应
type GetEffectSummaryRes struct {
	Summary *EffectSummary `json:"summary" dc:"效果汇总"`
}
