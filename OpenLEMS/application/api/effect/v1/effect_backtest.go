package v1

import "github.com/gogf/gf/v2/frame/g"

// GetEffectBacktestReq 算法回测请求
type GetEffectBacktestReq struct {
	g.Meta `path:"/effect/backtest" method:"get" tags:"效果评估" summary:"算法回测对比（同一段历史数据跑各算法）" role:"user"`
	Start  string `json:"start" dc:"开始日期 2006-01-02，默认今日"`
	End    string `json:"end" dc:"结束日期 2006-01-02，默认今日"`
}

// BacktestAlgorithmResult 单算法回测结果
type BacktestAlgorithmResult struct {
	Algorithm         string  `json:"algorithm" dc:"算法标识"`
	ChargeEnergy      float64 `json:"chargeEnergy" dc:"充电量 kWh"`
	DischargeEnergy   float64 `json:"dischargeEnergy" dc:"放电量 kWh"`
	ChargeCost        float64 `json:"chargeCost" dc:"充电电费 元"`
	DischargeRevenue  float64 `json:"dischargeRevenue" dc:"放电收益 元"`
	ArbitrageProfit   float64 `json:"arbitrageProfit" dc:"峰谷套利净收益 元"`
	ActualMaxDemand   float64 `json:"actualMaxDemand" dc:"实际最大需量 kW"`
	BaselineMaxDemand float64 `json:"baselineMaxDemand" dc:"无储能最大需量 kW"`
	DemandSaving      float64 `json:"demandSaving" dc:"需量节省 元"`
	TotalSaving       float64 `json:"totalSaving" dc:"总节省 元"`
	FinalSocPct       float64 `json:"finalSocPct" dc:"终止 SOC %"`
}

// EffectBacktest 回测汇总
type EffectBacktest struct {
	Start        string                    `json:"start" dc:"开始时间"`
	End          string                    `json:"end" dc:"结束时间"`
	Points       int                       `json:"points" dc:"采样点数"`
	GridDeviceId string                    `json:"gridDeviceId" dc:"关口设备ID"`
	EssDeviceIds []string                  `json:"essDeviceIds" dc:"储能设备ID列表"`
	DemandTarget float64                   `json:"demandTarget" dc:"需量目标 kW（0=未启用）"`
	DemandPrice  float64                   `json:"demandPrice" dc:"需量电价 元/kW"`
	RatedPowerKW float64                   `json:"ratedPowerKw" dc:"等效额定功率 kW"`
	CapacityKWh  float64                   `json:"capacityKwh" dc:"等效额定容量 kWh"`
	Results      []BacktestAlgorithmResult `json:"results" dc:"各算法结果（首个为 idle 基线）"`
}

// GetEffectBacktestRes 算法回测响应
type GetEffectBacktestRes struct {
	Backtest *EffectBacktest `json:"backtest" dc:"回测汇总"`
}
