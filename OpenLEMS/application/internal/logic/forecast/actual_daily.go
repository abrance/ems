package forecast

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// 复盘两条腿共用的真值：官方直采的日均实际值（gd_spot_metric，发电侧加权平均电价）。
//
// 为什么抽出来：以前两条腿各读各的源 —— 日频回测读官方，留痕腿读冻结的第三方 CSV，
// 同一个页面出现两个「实际值截至」（2026-11-11 与 2026-10-02）。复盘页只能有一个真值口径，
// 否则「留痕准还是重跑准」根本没法比。
//
// 官方披露只有日频（96 点分时公开披露拿不到），所以：
//   - 日频留痕（grain=day，一天一条）能与这里 1:1 对上；
//   - 96 点留痕没有官方真值可配，只能在「96 点 · 冻结」视图里当历史演示看。
type SDailyActualSeries struct {
	Market string
	Unit   string
	// Dates 升序；Values 与之一一对应
	Dates  []string
	Values []float64
	ByDate map[string]float64
	// 新鲜度（口径同日频回测：DataAsOf = 真值本身最新到哪天，不是查询窗口右端）
	DataAsOf    string
	LagDays     int
	Stale       bool
	TruthSource string
}

// LoadDailyActuals 读 [from, to] 的官方日均实际值。
//
// 查询右端固定放到今天：DataAsOf 要说的是「真值本身最新到哪天」，页面靠它算陈旧档位。
// 只查窗口内的数据会把窗口右端当成「真值最新日」，滞后天数就永远显示 0。
func LoadDailyActuals(ctx context.Context, market, from, to string) (*SDailyActualSeries, error) {
	spec, err := resolvePriceSpec(market)
	if err != nil {
		return nil, err
	}
	loc := priceLocation()
	today := time.Now().In(loc).Format(priceDateLayout)
	end := strings.TrimSpace(to)
	if end == "" || end > today {
		end = today
	}
	start := strings.TrimSpace(from)
	if start == "" {
		start = end
	}
	if start > end {
		return nil, errors.Errorf("起始日期 %s 晚于结束日期 %s", start, end)
	}
	models, err := spotMetricSource(ctx, start, end)
	if err != nil {
		return nil, errors.Wrap(err, "查询官方电价指标失败")
	}
	dates, values, unit := buildDailySeries(models, spec)
	out := &SDailyActualSeries{
		Market:      spec.Market,
		Unit:        unit,
		Dates:       dates,
		Values:      values,
		ByDate:      make(map[string]float64, len(dates)),
		TruthSource: priceTruthSourceOfficial,
	}
	for i, d := range dates {
		out.ByDate[d] = values[i]
	}
	if len(dates) > 0 {
		out.DataAsOf = dates[len(dates)-1]
	}
	out.LagDays, out.Stale = truthFreshness(out.DataAsOf, time.Now(), loc)
	return out, nil
}
