package internal

type SPolicyMircogridConfig struct {
	// 储能参数
	SocMaxRatio float64 `json:"socMaxRatio" name:"最大SOC比例" desc:"最大SOC比例" ct:"number" vt:"float" min:"0" max:"1" default:"0.95"`
	SocMinRatio float64 `json:"socMinRatio" name:"最小SOC比例" desc:"最小SOC比例" ct:"number" vt:"float" min:"0" max:"1" default:"0.1"`
	//BatteryPowerRated   float64 `json:"batteryPowerRated" name:"储能额定功率" desc:"储能额定功率" ct:"number" vt:"float" min:"0" default:"100.0" unit:"kW"`
	ChargeEfficiency    float64 `json:"chargeEfficiency" name:"充电效率" desc:"充电效率" ct:"number" vt:"float" min:"0" max:"1" default:"0.95"`
	DischargeEfficiency float64 `json:"dischargeEfficiency" name:"放电效率" desc:"放电效率" ct:"number" vt:"float" min:"0" max:"1" default:"0.95"`

	// 峰谷套利参数
	EssDevices          []string `json:"essDevices" name:"储能设备列表" desc:"参与策略的储能设备ID列表" ct:"multiSelect"`
	ChargePowerRatio    float64  `json:"chargePowerRatio" name:"充电功率比例" desc:"充电功率占额定功率的比例" ct:"number" vt:"float" min:"0" max:"1" default:"1"`
	DischargePowerRatio float64  `json:"dischargePowerRatio" name:"放电功率比例" desc:"放电功率占额定功率的比例" ct:"number" vt:"float" min:"0" max:"1" default:"1"`

	// 需量控制参数， 优先降低功率
	EnableDemandControl bool    `json:"enableDemandControl" name:"启动需量控制模式" desc:"启动需量控制模式" ct:"switch" vt:"bool" default:"false"`
	GridDeviceId        string  `json:"gridDeviceId" name:"关口电表设备ID" desc:"用于读取关口功率的设备ID" ct:"singleSelect"`
	DemandTarget        float64 `json:"demandTarget" name:"需量目标值" desc:"关口功率上限(kW)，0 表示不限制" ct:"number" vt:"float" min:"0" default:"0"`
	DemandControlPeriod int     `json:"demandControlPeriod" name:"需量控制周期" desc:"需量控制周期(分钟)" ct:"number" vt:"int" min:"1" default:"15"`

	// 负荷预测（TimesFM），用于预测型需量控制
	EnableLoadForecast bool   `json:"enableLoadForecast" name:"启用负荷预测" desc:"启用 TimesFM 负荷预测，提前削峰" ct:"switch" vt:"bool" default:"false"`
	ForecastBaseUrl    string `json:"forecastBaseUrl" name:"预测服务地址" desc:"TimesFM 预测服务地址" ct:"text" default:"https://forecast.xiaoyxq.top"`
	ForecastHorizon    int    `json:"forecastHorizon" name:"预测步数" desc:"预测未来点数（15分钟/点）" ct:"number" vt:"int" min:"1" max:"512" default:"4"`
	ForecastRefreshMin int    `json:"forecastRefreshMinutes" name:"预测刷新间隔(分钟)" desc:"负荷预测刷新间隔" ct:"number" vt:"int" min:"1" default:"15"`

	// 充电桩参数
	AllowPileReduce bool `json:"allowPileReduce" name:"允许充电桩降功率" desc:"不允许充电桩降功率（无充电桩）" ct:"switch" vt:"bool" default:"false"`

	// 负荷参数
	AllowLoadReduce    bool    `json:"allowLoadReduce" name:"允许负荷降功率" desc:"允许负荷降功率" ct:"switch" vt:"bool" default:"false"`
	LoadReduceMaxPower float64 `json:"loadReduceMaxPower" name:"负荷允许降低的最大功率" desc:"负荷允许降低的最大功率" ct:"number" vt:"float" min:"0" default:"60.0" unit:"kW"`
	LoadReducePenalty  float64 `json:"loadReducePenalty" name:"负荷降功率惩罚系数" desc:"负荷降功率惩罚系数" ct:"number" vt:"float" min:"0" default:"800.0" unit:"元/kWh"`

	// 变压器参数
	TransformerCapacity     float64 `json:"transformerCapacity" name:"变压器容量" desc:"变压器容量" ct:"number" vt:"float" min:"0" default:"200.0" unit:"kVA"`
	TransformerSafetyFactor float64 `json:"transformerSafetyFactor" name:"变压器最大安全系数" desc:"变压器最大安全系数" ct:"number" vt:"float" min:"0" max:"1" default:"0.8"`

	// 上网售电参数
	EnableGridSell bool `json:"enableGridSell" name:"上网售电" desc:"是否允许余电上网" ct:"switch" vt:"bool" default:"true"`

	TransformerMinUseFactor float64 `json:"transformerMinUseFactor" name:"变压器最小安全系数" desc:"变压器最小安全系数" ct:"number" vt:"float" min:"0" max:"1" default:"0.1"`

	// 成本参数
	CurtailmentPenalty float64 `json:"curtailmentPenalty" name:"弃光惩罚系数"  ct:"number" vt:"float" min:"0" default:"0.0" unit:"元/kWh"`
	PileReducePenalty  float64 `json:"pileReducePenalty" name:"充电桩降功率惩罚系数" ct:"number" vt:"float" min:"0" default:"0.0" unit:"元/kWh"`

	// 动态扩容参数
	EnableDynamicExpansion bool    `json:"enableDynamicExpansion" name:"关闭动态扩容" desc:"关闭动态扩容" ct:"switch" vt:"bool" default:"false"`
	ExpansionCostFactor    float64 `json:"expansionCostFactor" name:"扩容成本" desc:"扩容成本设为0（不使用）" ct:"number" vt:"float" min:"0" default:"0.0"`
	//MaxExpansionRatio      float64 `json:"maxExpansionRatio" name:"最大扩容比例" desc:"最大扩容比例设为1（不扩容）" ct:"number" vt:"float" min:"1" default:"1.0"`
	LoadPriorityWeight float64 `json:"loadPriorityWeight" name:"负荷优先级权重" desc:"负荷优先级权重" ct:"number" vt:"float" min:"0" default:"1000.0"`
	PilePriorityWeight float64 `json:"pilePriorityWeight" name:"充电桩优先级权重" desc:"充电桩优先级权重设为0（无充电桩）" ct:"number" vt:"float" min:"0" default:"0.0"`
}
