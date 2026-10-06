package effect

import (
	"context"
	"sort"
	"time"

	"common"
	"common/c_enum"
	"common/c_type"
	"p_policy_mircogrid"
	"s_effect"
	"s_price"

	"github.com/pkg/errors"
)

// SBacktestSummary 算法回测汇总（同一段历史数据上对比各算法）
type SBacktestSummary struct {
	Start        time.Time
	End          time.Time
	Points       int
	GridDeviceId string
	EssDeviceIds []string
	DemandTarget float64
	DemandPrice  float64
	Battery      p_policy_mircogrid.SBacktestBattery
	Results      []p_policy_mircogrid.SBacktestResult
}

// Backtest 在 [start, end] 的实测数据上回测各内置算法。
//
// 做法：把实测关口/储能功率还原为「自然负荷」，再在该负荷上模拟各算法运行，
// 用同一成本模型（s_effect）结算，得到可横向比较的省电费效果。
func Backtest(ctx context.Context, start, end time.Time) (*SBacktestSummary, error) {
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

	startMs, endMs := start.UnixMilli(), end.UnixMilli()

	grid, err := loadSeries(ctx, cfg.GridDeviceId, startMs, endMs)
	if err != nil {
		return nil, errors.Wrap(err, "读取关口功率失败")
	}
	ess, err := loadSumSeries(ctx, cfg.EssDeviceIds, startMs, endMs)
	if err != nil {
		return nil, errors.Wrap(err, "读取储能功率失败")
	}

	battery, err := loadBatteryParams(cfg)
	if err != nil {
		return nil, err
	}

	points := buildBacktestPoints(grid, ess, priceSegAtFunc(ctx))
	if len(points) < 2 {
		return nil, errors.New("历史采样点不足，无法回测")
	}

	results := p_policy_mircogrid.BacktestAlgorithms(points, p_policy_mircogrid.SBacktestConfig{
		Battery:      battery,
		DemandTarget: cfg.DemandTarget,
		DemandPrice:  cfg.DemandPrice,
	})

	return &SBacktestSummary{
		Start:        start,
		End:          end,
		Points:       len(points),
		GridDeviceId: cfg.GridDeviceId,
		EssDeviceIds: cfg.EssDeviceIds,
		DemandTarget: cfg.DemandTarget,
		DemandPrice:  cfg.DemandPrice,
		Battery:      battery,
		Results:      results,
	}, nil
}

// buildBacktestPoints 将关口/储能序列还原为自然负荷点（load = 关口 − 储能）。
func buildBacktestPoints(grid, ess s_effect.SSeries, priceAt func(int64) *s_price.SPriceSegment) []p_policy_mircogrid.SBacktestPoint {
	points := make([]p_policy_mircogrid.SBacktestPoint, 0, len(grid))
	for ts, g := range grid {
		e, ok := ess[ts]
		if !ok {
			continue
		}
		var price float64
		var priceType c_enum.EPriceType
		if seg := priceAt(ts); seg != nil {
			price, priceType = seg.Price, seg.PriceType
		}
		points = append(points, p_policy_mircogrid.SBacktestPoint{
			Timestamp: ts,
			LoadKW:    g - e,
			Price:     price,
			PriceType: priceType,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	return points
}

// loadBatteryParams 由储能设备实时参数与策略配置组装等效电池参数。
func loadBatteryParams(cfg *policyConfig) (p_policy_mircogrid.SBacktestBattery, error) {
	mgr := common.GetDeviceManager()
	var ratedPower, capacity float64
	for _, id := range cfg.EssDeviceIds {
		dev := mgr.GetDeviceById(id)
		if dev == nil {
			continue
		}
		essDev, ok := dev.(c_type.IEnergyStore)
		if !ok {
			continue
		}
		if p, err := essDev.GetRatedPower(); err == nil && p != nil && *p > 0 {
			ratedPower += float64(*p)
		}
		if c, err := essDev.GetCapacity(); err == nil && c != nil && *c > 0 {
			capacity += float64(*c)
		}
	}
	if ratedPower <= 0 {
		return p_policy_mircogrid.SBacktestBattery{}, errors.New("无法读取储能额定功率")
	}
	if capacity <= 0 {
		return p_policy_mircogrid.SBacktestBattery{}, errors.New("无法读取储能额定容量")
	}

	socMin, socMax := cfg.SocMinRatio, cfg.SocMaxRatio
	return p_policy_mircogrid.SBacktestBattery{
		RatedPowerKW: ratedPower,
		CapacityKWh:  capacity,
		SocMinRatio:  socMin,
		SocMaxRatio:  socMax,
		Soc0Ratio:    (socMin + socMax) / 2, // 初始 SOC 取中点，保证各算法起点一致
		ChargeEff:    cfg.ChargeEfficiency,
		DischargeEff: cfg.DischargeEfficiency,
	}, nil
}

// priceSegAtFunc 返回按分钟缓存的电价时段查询函数。
func priceSegAtFunc(ctx context.Context) func(int64) *s_price.SPriceSegment {
	cache := make(map[string]*s_price.SPriceSegment)
	return func(ts int64) *s_price.SPriceSegment {
		t := time.UnixMilli(ts)
		key := t.Format("2006-01-02 15:04")
		if seg, ok := cache[key]; ok {
			return seg
		}
		seg, _ := s_price.PriceAt(ctx, t)
		cache[key] = seg
		return seg
	}
}
