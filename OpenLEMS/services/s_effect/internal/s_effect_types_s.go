package internal

// SInterval 一个采样点。功率单位 kW；时间戳为 Unix 毫秒。
type SInterval struct {
	Timestamp int64
	GridPower float64 // 关口功率，正=从电网取电，负=反送
	EssPower  float64 // 储能功率，正=充电，负=放电
	Price     float64 // 该采样时段电价，元/kWh
}

// SSavingsConfig 效果计算参数。
type SSavingsConfig struct {
	// DemandPrice 评估周期内的需量电价，元/kW。
	// 需量电费按计费周期（通常为月）的最大需量计收，因此评估整月时传入月度需量电价。
	DemandPrice float64

	// DemandWindowMin 需量计算窗口（分钟），<=0 时默认 15。
	DemandWindowMin int

	// MaxGapMin 允许的最大采样间隔（分钟），超过该间隔的电量不计入，<=0 时默认 60。
	// 用于避免数据中断导致的电量虚增。
	MaxGapMin int
}

// SSavings 效果分解（金额单位：元，电量单位：kWh，功率单位：kW）。
type SSavings struct {
	ChargeEnergy     float64 // 充电量
	DischargeEnergy  float64 // 放电量
	ChargeCost       float64 // 充电电费
	DischargeRevenue float64 // 放电收益（替代购电）
	ArbitrageProfit  float64 // 峰谷套利净收益 = 放电收益 − 充电电费

	ActualMaxDemand   float64 // 实际最大需量（储能介入后）
	BaselineMaxDemand float64 // 无储能反算最大需量
	DemandSaving      float64 // 需量节省 = max(0, 基线 − 实际) × 需量电价

	TotalSaving float64 // 总节省 = 套利净收益 + 需量节省
}
