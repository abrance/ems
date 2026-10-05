package internal

import (
	"common"
	"common/c_base"
	"common/c_log"
	"common/c_type"
	"context"
	"encoding/json"
	"s_db"
	"s_forecast"
	"s_policy"
	"s_price"
	"time"

	"github.com/pkg/errors"
)

// sPolicyMircogrid 微电网策略实现
type sPolicyMircogrid struct {
	policyId   string
	policyName string
	config     *SPolicyMircogridConfig
	ctx        context.Context

	// 负荷预测（TimesFM）客户端与缓存
	forecastClient s_forecast.IClient
	forecastPowers []float64 // 最近的关口功率预测（kW）
	forecastAt     time.Time // 上次预测更新时间
}

// NewPolicyMircogrid 创建微电网策略实例
func NewPolicyMircogrid() c_base.IPolicy {
	return &sPolicyMircogrid{
		policyId:   "policy_microgrid",
		policyName: "微电网策略",
		config:     &SPolicyMircogridConfig{},
	}
}

// Init 初始化策略
func (s *sPolicyMircogrid) Init(ctx context.Context) error {
	s.ctx = ctx
	c_log.Infof(ctx, "正在初始化微电网策略...")

	// 从setting表加载配置
	configStrPtr := s_db.GetSettingService().GetSettingValueById(ctx, "policy_microgrid")
	if configStrPtr == nil {
		c_log.Warning(ctx, "微电网策略配置为空，策略将不执行")
		s.config = nil
		return nil
	}

	// 解析JSON配置到 s.config
	err := json.Unmarshal([]byte(*configStrPtr), s.config)
	if err != nil {
		return errors.Wrap(err, "解析微电网策略配置失败")
	}

	s.applyDefaults()

	// 按需初始化预测客户端
	if s.config.EnableLoadForecast {
		s.forecastClient = s_forecast.NewClient(s.config.ForecastBaseUrl, 30*time.Second)
		c_log.Infof(ctx, "已启用负荷预测，服务地址: %s", s.config.ForecastBaseUrl)
	}

	c_log.Infof(ctx, "微电网策略初始化成功")
	return nil
}

// applyDefaults 为缺失或非法的可选配置填充默认值，
// 避免因配置字段缺失导致策略静默不动作。
func (s *sPolicyMircogrid) applyDefaults() {
	if s.config.SocMaxRatio <= 0 || s.config.SocMaxRatio > 1 {
		s.config.SocMaxRatio = 0.95
	}
	if s.config.SocMinRatio <= 0 || s.config.SocMinRatio >= 1 {
		s.config.SocMinRatio = 0.1
	}
	if s.config.ChargePowerRatio <= 0 {
		s.config.ChargePowerRatio = 1
	}
	if s.config.DischargePowerRatio <= 0 {
		s.config.DischargePowerRatio = 1
	}
	if s.config.ForecastBaseUrl == "" {
		s.config.ForecastBaseUrl = "https://forecast.xiaoyxq.top"
	}
	if s.config.ForecastHorizon <= 0 {
		s.config.ForecastHorizon = 4
	}
	if s.config.ForecastRefreshMin <= 0 {
		s.config.ForecastRefreshMin = 15
	}
}

// Run 执行策略（每分钟调用一次）
//
// 峰谷套利规则版最小闭环：
//  1. 取当前电价时段
//  2. 取储能设备当前 SOC（多台取平均）
//  3. 交决策器给出动作（谷充/峰放/平待机，含 SOC 边界保护）
//  4. 换算为带符号目标功率
//  5. 下发
func (s *sPolicyMircogrid) Run(ctx context.Context) error {
	// 配置为空时不执行
	if s.config == nil {
		c_log.Debug(ctx, "微电网策略配置为空，跳过执行")
		return nil
	}
	if len(s.config.EssDevices) == 0 {
		c_log.Debug(ctx, "微电网策略未配置储能设备，跳过执行")
		return nil
	}

	// 1. 当前电价时段
	segment, err := s_price.GetCurrentPriceSegment(ctx)
	if err != nil {
		return errors.Wrap(err, "获取当前电价时段失败")
	}
	if segment == nil {
		c_log.Debug(ctx, "当前无匹配的电价时段，策略待机")
		return nil
	}

	// 2. 储能设备与当前 SOC（多台取平均，单位：百分比）
	devices := getEssDevices(s.config.EssDevices)
	if len(devices) == 0 {
		c_log.Warning(ctx, "未找到有效的储能设备，跳过执行")
		return nil
	}
	soc, ok := averageSoc(devices)
	if !ok {
		c_log.Warning(ctx, "无法获取储能 SOC，跳过执行")
		return nil
	}
	ratedPower := averageRatedPower(devices)

	// 3. 关口功率（需量控制启用时读取）
	gridPower := 0.0
	if s.config.EnableDemandControl {
		gridPower = readGridPower(s.config.GridDeviceId)
	}

	// 4. 决策（规则版实现 IDecisionMaker，可叠加需量约束，后续可替换为优化器）
	decision := s.newDecider().Decide(SDecisionInput{
		PriceType:           segment.PriceType,
		Price:               segment.Price,
		Soc:                 soc,
		SocMin:              s.config.SocMinRatio * 100,
		SocMax:              s.config.SocMaxRatio * 100,
		GridPower:           gridPower,
		DemandTarget:        s.config.DemandTarget,
		RatedPowerKW:        ratedPower,
		ForecastedGridPower: s.forecastedGridPower(ctx),
	})

	// 5. 换算目标功率（kW，正=充 负=放）
	targetPower := decision.TargetPower(ratedPower)

	// 6. 下发
	c_log.Infof(ctx, "微电网策略 - 电价类型: %s, 电价: %.4f, SOC: %.2f%%, 关口: %.1fkW, 动作: %s, 目标功率: %dkW",
		segment.PriceType, segment.Price, soc, gridPower, decision.Action, targetPower)

	return s_policy.ExecuteStrategy(ctx, targetPower, s.config.EssDevices)
}

// newDecider 构造决策器。基础为峰谷套利规则版；启用需量控制时叠加需量约束层。
// 后续接入负荷预测优化器时，在此返回新的 IDecisionMaker 实现即可。
func (s *sPolicyMircogrid) newDecider() IDecisionMaker {
	var d IDecisionMaker = &STouRule{
		ChargeRatio:    s.config.ChargePowerRatio,
		DischargeRatio: s.config.DischargePowerRatio,
	}
	if s.config.EnableDemandControl {
		d = &SDemandLimitRule{Base: d}
	}
	return d
}

// forecastedGridPower 返回关口功率的近期预测（按刷新间隔缓存）。
// 未启用预测、无关口设备或预测失败时返回 nil（退回实时需量控制）。
//
// 注意：关口功率已包含储能自身的作用，直接预测存在反馈回路。
// 若现场具备独立的负荷计量点，宜将 GridDeviceId 指向该点，或将关口表数据
// 扣除储能功率后再输入。现场只有关口表时，仍可工作（受 SOC 下限保护约束）。
func (s *sPolicyMircogrid) forecastedGridPower(ctx context.Context) []float64 {
	if !s.config.EnableLoadForecast || s.config.GridDeviceId == "" || s.forecastClient == nil {
		return nil
	}
	if s.forecastPowers != nil && time.Since(s.forecastAt) < time.Duration(s.config.ForecastRefreshMin)*time.Minute {
		return s.forecastPowers
	}

	item, err := s_forecast.ForecastLoad(ctx, s.forecastClient, s.config.GridDeviceId, 0, s.config.ForecastHorizon, []float64{0.5})
	if err != nil {
		c_log.Warningf(ctx, "负荷预测失败，退回实时需量控制: %v", err)
		return s.forecastPowers
	}
	s.forecastPowers = item.Point
	s.forecastAt = time.Now()
	c_log.Debugf(ctx, "负荷预测已更新，未来 %d 点: %v", len(item.Point), item.Point)
	return s.forecastPowers
}

// readGridPower 读取关口功率（kW，正=从电网取电）。
// 优先使用站入口设备，回退到电表；读取失败返回 0（不干预）。
func readGridPower(deviceId string) float64 {
	if deviceId == "" {
		return 0
	}
	dev := common.GetDeviceManager().GetDeviceById(deviceId)
	if dev == nil {
		return 0
	}
	if entrance, ok := dev.(c_type.IStationEntrance); ok {
		if p, err := entrance.GetPower(); err == nil && p != nil {
			return *p
		}
	}
	if meter, ok := dev.(c_type.IAmmeter); ok {
		if p, err := meter.GetPTotal(); err == nil && p != nil {
			return float64(*p)
		}
	}
	return 0
}

// getEssDevices 按设备ID获取储能设备实例（过滤非储能设备）
func getEssDevices(ids []string) []c_type.IEnergyStore {
	mgr := common.GetDeviceManager()
	devices := make([]c_type.IEnergyStore, 0, len(ids))
	for _, id := range ids {
		dev := mgr.GetDeviceById(id)
		if dev == nil {
			continue
		}
		if ess, ok := dev.(c_type.IEnergyStore); ok {
			devices = append(devices, ess)
		}
	}
	return devices
}

// averageSoc 计算多台储能设备的平均 SOC（百分比）
func averageSoc(devices []c_type.IEnergyStore) (float64, bool) {
	var sum float64
	var n int
	for _, dev := range devices {
		soc, err := dev.GetSoc()
		if err != nil || soc == nil {
			continue
		}
		sum += float64(*soc)
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// averageRatedPower 计算多台储能设备的平均额定功率（kW）
func averageRatedPower(devices []c_type.IEnergyStore) float64 {
	var sum float64
	var n int
	for _, dev := range devices {
		p, err := dev.GetRatedPower()
		if err != nil || p == nil {
			continue
		}
		sum += float64(*p)
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// Shutdown 关闭策略
func (s *sPolicyMircogrid) Shutdown() {
	if s.ctx != nil {
		c_log.Info(s.ctx, "微电网策略关闭")
	}
}

// GetPolicyId 获取策略ID
func (s *sPolicyMircogrid) GetPolicyId() string {
	return s.policyId
}

// GetPolicyName 获取策略名称
func (s *sPolicyMircogrid) GetPolicyName() string {
	return s.policyName
}

// GetConfig 获取策略配置
func (s *sPolicyMircogrid) GetConfig() interface{} {
	return s.config
}

// GetPolicyInfo 获取策略详细信息
func (s *sPolicyMircogrid) GetPolicyInfo() *c_base.SPolicyInfo {
	fields, err := c_base.BuildConfigStructFields(s.config)
	if err != nil {
		c_log.BizErrorf(s.ctx, "加载[%s]策略失败", s.policyName)
		return nil
	}
	return &c_base.SPolicyInfo{
		PolicyId:               s.policyId,
		PolicyName:             s.policyName,
		Description:            "微电网能量管理策略，支持需量控制、削峰填谷、动态扩容等功能",
		ConfigFieldDefinitions: fields,
	}
}
