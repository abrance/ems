package effect

import (
	"context"
	"strconv"
	"strings"
	"time"

	"common"
	"common/c_base"
	"common/c_chart"
	"s_db"
	"s_effect"
	"s_price"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/pkg/errors"
)

const (
	// policySettingId 微电网策略配置所在的 setting ID（与 p_policy_mircogrid 一致）
	policySettingId = "policy_microgrid"
	// powerPointKey 设备实时有功功率点位 Key
	powerPointKey = "Power"
	// demandWindowMin 需量计算窗口（分钟）
	demandWindowMin = 15
)

// SSummary 效果汇总（含数据来源信息，便于排查）
type SSummary struct {
	Start        time.Time
	End          time.Time
	Points       int
	Algorithm    string
	GridDeviceId string
	EssDeviceIds []string
	DemandPrice  float64
	Savings      s_effect.SSavings
}

// policyConfig 从策略配置中提取效果测算所需字段
type policyConfig struct {
	GridDeviceId        string
	EssDeviceIds        []string
	DemandPrice         float64
	EnableDemandControl bool
}

// Summary 依据实测数据计算 [start, end] 区间的储能效果。
func Summary(ctx context.Context, start, end time.Time) (*SSummary, error) {
	cfg, err := loadPolicyConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.GridDeviceId == "" {
		return nil, errors.New("微电网策略未配置关口电表设备")
	}
	if len(cfg.EssDeviceIds) == 0 {
		return nil, errors.New("微电网策略未配置储能设备")
	}
	if !end.After(start) {
		return nil, errors.New("结束时间必须晚于开始时间")
	}

	startMs := start.UnixMilli()
	endMs := end.UnixMilli()

	grid, err := loadSeries(ctx, cfg.GridDeviceId, startMs, endMs)
	if err != nil {
		return nil, errors.Wrap(err, "读取关口功率失败")
	}
	ess, err := loadSumSeries(ctx, cfg.EssDeviceIds, startMs, endMs)
	if err != nil {
		return nil, errors.Wrap(err, "读取储能功率失败")
	}

	intervals := s_effect.BuildIntervals(grid, ess, priceAtFunc(ctx))
	savings := s_effect.ComputeSavings(intervals, s_effect.SSavingsConfig{
		DemandPrice:     cfg.DemandPrice,
		DemandWindowMin: demandWindowMin,
	})

	return &SSummary{
		Start:        start,
		End:          end,
		Points:       len(intervals),
		Algorithm:    algorithmName(cfg.EnableDemandControl),
		GridDeviceId: cfg.GridDeviceId,
		EssDeviceIds: cfg.EssDeviceIds,
		DemandPrice:  cfg.DemandPrice,
		Savings:      savings,
	}, nil
}

// algorithmName 由策略配置推导当前生效的算法标识（与决策器 Name() 保持一致）。
func algorithmName(enableDemandControl bool) string {
	if enableDemandControl {
		return "tou-rule+demand"
	}
	return "tou-rule"
}

// loadPolicyConfig 读取微电网策略设置并解析效果测算所需字段。
func loadPolicyConfig(ctx context.Context) (*policyConfig, error) {
	raw := s_db.GetSettingService().GetSettingValueById(ctx, policySettingId)
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, errors.New("微电网策略配置为空")
	}
	j := gjson.New(*raw)
	cfg := &policyConfig{
		GridDeviceId:        strings.TrimSpace(j.Get("gridDeviceId").String()),
		DemandPrice:         j.Get("demandPrice").Float64(),
		EnableDemandControl: j.Get("enableDemandControl").Bool(),
	}
	for _, id := range j.Get("essDevices").Strings() {
		if id = strings.TrimSpace(id); id != "" {
			cfg.EssDeviceIds = append(cfg.EssDeviceIds, id)
		}
	}
	return cfg, nil
}

// loadSeries 读取单个设备的有功功率历史。
func loadSeries(ctx context.Context, deviceID string, startMs, endMs int64) (s_effect.SSeries, error) {
	storage := common.GetStorageInstanceOrNil()
	if storage == nil {
		return nil, errors.New("存储实例未就绪")
	}
	chart, err := storage.GetStorageData(c_base.StorageTypeDevice, deviceID, []string{powerPointKey}, &startMs, &endMs, 0)
	if err != nil {
		return nil, err
	}
	return chartToSeries(chart), nil
}

// loadSumSeries 读取多个设备的有功功率历史并按时间戳求和（多台储能合计）。
func loadSumSeries(ctx context.Context, deviceIDs []string, startMs, endMs int64) (s_effect.SSeries, error) {
	sum := s_effect.SSeries{}
	for _, id := range deviceIDs {
		series, err := loadSeries(ctx, id, startMs, endMs)
		if err != nil {
			return nil, errors.Wrapf(err, "设备 %s", id)
		}
		for ts, v := range series {
			sum[ts] += v
		}
	}
	return sum, nil
}

// chartToSeries 将图表数据转换为时间戳→值序列，跳过空值与非法点。
func chartToSeries(chart *c_chart.ChartData) s_effect.SSeries {
	out := s_effect.SSeries{}
	if chart == nil || len(chart.Series) == 0 || chart.Series[0] == nil {
		return out
	}
	values := chart.Series[0].Data
	timestamps := chart.XAxis.Data
	n := len(values)
	if len(timestamps) < n {
		n = len(timestamps)
	}
	for i := 0; i < n; i++ {
		if values[i] == "" {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(timestamps[i]), 10, 64)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(values[i]), 64)
		if err != nil {
			continue
		}
		out[ts] = v
	}
	return out
}

// priceAtFunc 返回按分钟缓存的电价查询函数。
func priceAtFunc(ctx context.Context) func(int64) float64 {
	cache := make(map[string]float64)
	return func(ts int64) float64 {
		t := time.UnixMilli(ts)
		key := t.Format("2006-01-02 15:04")
		if v, ok := cache[key]; ok {
			return v
		}
		var price float64
		if seg, err := s_price.PriceAt(ctx, t); err == nil && seg != nil {
			price = seg.Price
		}
		cache[key] = price
		return price
	}
}
