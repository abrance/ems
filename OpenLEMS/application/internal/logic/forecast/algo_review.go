package forecast

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"s_db/s_db_model"
)

// 算法回测（留痕口径）：只读 gd_price_forecast 里 grain=hour 的当时留痕，
// 按 algo 分组，与实际值（gd_home_24h）逐点配对算误差。不重跑模型。
//
// 融合（fusion）没有自己的留痕：同一 (market, target_time) 上 TimesFM 与 GBM
// 都存在时，取两个点预测的均值（LEAR 不参与融合，只作独立第三算法对比）。
// 它没有分位带，覆盖率返回 nil 而不是编一个。

const (
	// AlgoFusion 融合算法（展示时实时算，不落库）。
	AlgoFusion = "fusion"
	// mapeMinAbsActual 电价可能为负或接近 0，|实际| 小于该值的点不计入 MAPE。
	mapeMinAbsActual = 1.0
)

var algoDisplayOrder = []string{
	s_db_model.ForecastAlgoTimesFM,
	s_db_model.ForecastAlgoGBM,
	s_db_model.ForecastAlgoLEAR,
	AlgoFusion,
}

// AlgoLabel 算法展示名。
func AlgoLabel(algo string) string {
	switch algo {
	case s_db_model.ForecastAlgoTimesFM:
		return "TimesFM"
	case s_db_model.ForecastAlgoGBM:
		return "GBM"
	case s_db_model.ForecastAlgoLEAR:
		return "LEAR"
	case AlgoFusion:
		return "融合（两者均值）"
	default:
		return algo
	}
}

// ParseAlgos 解析逗号分隔的算法列表；空串返回全部，顺序固定。
func ParseAlgos(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return append([]string(nil), algoDisplayOrder...), nil
	}
	seen := make(map[string]bool, len(algoDisplayOrder))
	for _, raw := range strings.Split(s, ",") {
		algo := strings.ToLower(strings.TrimSpace(raw))
		if algo == "" {
			continue
		}
		if !isKnownAlgo(algo) {
			return nil, fmt.Errorf("未知算法 %q（可选 timesfm、gbm、lear、fusion）", strings.TrimSpace(raw))
		}
		seen[algo] = true
	}
	if len(seen) == 0 {
		return append([]string(nil), algoDisplayOrder...), nil
	}
	out := make([]string, 0, len(seen))
	for _, algo := range algoDisplayOrder {
		if seen[algo] {
			out = append(out, algo)
		}
	}
	return out, nil
}

func isKnownAlgo(algo string) bool {
	for _, a := range algoDisplayOrder {
		if a == algo {
			return true
		}
	}
	return false
}

// SAlgoReviewSummary 单个算法的区间汇总。
type SAlgoReviewSummary struct {
	Algo         string
	Label        string
	Days         int
	Pairs        int
	MAE          float64
	RMSE         float64
	MAPE         float64
	MAPEExcluded int
	Bias         float64
	Coverage     *float64
}

// SAlgoReviewDaily 按目标日的指标。
type SAlgoReviewDaily struct {
	Algo         string
	Date         string
	Pairs        int
	MAE          float64
	MAPE         float64
	MAPEExcluded int
	Coverage     *float64
}

// SAlgoReviewHourly 按整点的误差画像。
type SAlgoReviewHourly struct {
	Algo  string
	Hour  int
	Pairs int
	MAE   float64
	MAPE  float64
}

// SAlgoReviewPoint 单日明细点。
type SAlgoReviewPoint struct {
	Algo       string
	Hour       int
	TargetTime string
	Predicted  float64
	Actual     *float64
	Q10        float64
	Q50        float64
	Q90        float64
}

// SAlgoReviewDropped 被剔除的样本。
type SAlgoReviewDropped struct {
	NoActual      int
	Backfilled    int
	IncompleteDay int
}

// SAlgoReview 一次回测的完整结果。
type SAlgoReview struct {
	Market  string
	Unit    string
	From    string
	To      string
	Algos   []string
	Summary []*SAlgoReviewSummary
	Daily   []*SAlgoReviewDaily
	Hourly  []*SAlgoReviewHourly
	Detail  []*SAlgoReviewPoint
	Dropped *SAlgoReviewDropped
}

// reviewPoint 归一化后的留痕点。
type reviewPoint struct {
	hour      int
	point     float64
	q10       float64
	q50       float64
	q90       float64
	hasBand   bool
	updatedAt int64
}

// BuildAlgoReview 基于留痕与实际值计算算法对比。
//
// rows 为 grain=hour 的留痕（可含多个算法）；actual[date][hour] 为按市场选好的实际价。
// detailDate 非空时额外产出该目标日的 24 点明细。
func BuildAlgoReview(
	rows []*s_db_model.SPriceForecastModel,
	actual map[string]map[int]float64,
	from, to, unit string,
	algos []string,
	detailDate string,
) *SAlgoReview {
	out := &SAlgoReview{
		From:    from,
		To:      to,
		Unit:    unit,
		Algos:   algos,
		Dropped: &SAlgoReviewDropped{},
	}

	// byAlgo[algo][date][hour] = point
	byAlgo := make(map[string]map[string]map[int]*reviewPoint)

	for _, row := range rows {
		if row == nil || row.Grain != s_db_model.ForecastGrainHour {
			continue
		}
		date, hour, ok := splitHourTarget(row.TargetTime)
		if !ok || date < from || date > to {
			continue
		}
		// 生成日晚于目标日 = 事后回填，不是当时发出去的预测。
		if IsBackfilledForecast(row.GeneratedOn, row.TargetTime) {
			out.Dropped.Backfilled++
			continue
		}
		algo := strings.ToLower(strings.TrimSpace(row.Algo))
		if algo == "" {
			algo = s_db_model.ForecastAlgoTimesFM
		}
		if algo != s_db_model.ForecastAlgoTimesFM && algo != s_db_model.ForecastAlgoGBM &&
			algo != s_db_model.ForecastAlgoLEAR {
			continue
		}
		if byAlgo[algo] == nil {
			byAlgo[algo] = make(map[string]map[int]*reviewPoint)
		}
		if byAlgo[algo][date] == nil {
			byAlgo[algo][date] = make(map[int]*reviewPoint)
		}
		updatedAt := int64(0)
		if row.UpdatedAt != nil {
			updatedAt = row.UpdatedAt.Unix()
		}
		// 同一天、同一目标时刻可能有多次运行（锚点或数据变化）：只取最近一次运行。
		if prev := byAlgo[algo][date][hour]; prev != nil && prev.updatedAt >= updatedAt {
			continue
		}
		byAlgo[algo][date][hour] = &reviewPoint{
			hour:      hour,
			point:     row.Point,
			q10:       row.Q10,
			q50:       row.Q50,
			q90:       row.Q90,
			hasBand:   row.Q90 > row.Q10,
			updatedAt: updatedAt,
		}
	}

	// 融合：两个基础算法在同一 (date, hour) 都有值时取均值。
	// 只有用户要了 fusion 才需要算，但算出来也用于明细。
	if wantsAlgo(algos, AlgoFusion) {
		fusion := make(map[string]map[int]*reviewPoint)
		for date, tmHours := range byAlgo[s_db_model.ForecastAlgoTimesFM] {
			gbmHours := byAlgo[s_db_model.ForecastAlgoGBM][date]
			if len(gbmHours) == 0 {
				continue
			}
			for hour, tm := range tmHours {
				gbm, ok := gbmHours[hour]
				if !ok {
					continue
				}
				if fusion[date] == nil {
					fusion[date] = make(map[int]*reviewPoint)
				}
				fusion[date][hour] = &reviewPoint{
					hour:  hour,
					point: (tm.point + gbm.point) / 2,
					q50:   (tm.point + gbm.point) / 2,
					// 两个分位带的均值不是分位带，覆盖率留空。
					hasBand: false,
				}
			}
		}
		if len(fusion) > 0 {
			byAlgo[AlgoFusion] = fusion
		}
	}

	missingActual := make(map[string]bool)
	detailDate = strings.TrimSpace(detailDate)

	for _, algo := range algos {
		days := byAlgo[algo]
		dates := make([]string, 0, len(days))
		for date := range days {
			dates = append(dates, date)
		}
		sort.Strings(dates)

		total := newMetricAccum()
		hourly := make(map[int]*metricAccum)
		summary := &SAlgoReviewSummary{Algo: algo, Label: AlgoLabel(algo)}

		for _, date := range dates {
			points := days[date]
			if len(points) != 24 {
				out.Dropped.IncompleteDay++
				continue
			}
			dayAcc := newMetricAccum()
			for hour, p := range points {
				act, ok := actual[date][hour]
				if !ok {
					missingActual[date+"|"+strconv.Itoa(hour)] = true
					continue
				}
				dayAcc.add(p.point, act, p)
				total.add(p.point, act, p)
				if hourly[hour] == nil {
					hourly[hour] = newMetricAccum()
				}
				hourly[hour].add(p.point, act, p)
			}
			if dayAcc.pairs == 0 {
				continue
			}
			summary.Days++
			out.Daily = append(out.Daily, &SAlgoReviewDaily{
				Algo:         algo,
				Date:         date,
				Pairs:        dayAcc.pairs,
				MAE:          dayAcc.mae(),
				MAPE:         dayAcc.mape(),
				MAPEExcluded: dayAcc.mapeExcluded,
				Coverage:     dayAcc.coverage(),
			})
		}

		summary.Pairs = total.pairs
		summary.MAE = total.mae()
		summary.RMSE = total.rmse()
		summary.MAPE = total.mape()
		summary.MAPEExcluded = total.mapeExcluded
		summary.Bias = total.bias()
		summary.Coverage = total.coverage()
		out.Summary = append(out.Summary, summary)

		for hour := 0; hour < 24; hour++ {
			acc := hourly[hour]
			if acc == nil || acc.pairs == 0 {
				continue
			}
			out.Hourly = append(out.Hourly, &SAlgoReviewHourly{
				Algo:  algo,
				Hour:  hour,
				Pairs: acc.pairs,
				MAE:   acc.mae(),
				MAPE:  acc.mape(),
			})
		}
	}

	out.Dropped.NoActual = len(missingActual)

	if detailDate != "" {
		for _, algo := range algos {
			points := byAlgo[algo][detailDate]
			hours := make([]int, 0, len(points))
			for hour := range points {
				hours = append(hours, hour)
			}
			sort.Ints(hours)
			for _, hour := range hours {
				p := points[hour]
				dto := &SAlgoReviewPoint{
					Algo:       algo,
					Hour:       hour,
					TargetTime: fmt.Sprintf("%s %02d:00", detailDate, hour),
					Predicted:  p.point,
					Q10:        p.q10,
					Q50:        p.q50,
					Q90:        p.q90,
				}
				if act, ok := actual[detailDate][hour]; ok {
					v := act
					dto.Actual = &v
				}
				out.Detail = append(out.Detail, dto)
			}
		}
	}

	return out
}

func wantsAlgo(algos []string, want string) bool {
	for _, a := range algos {
		if a == want {
			return true
		}
	}
	return false
}

// splitHourTarget 解析 "YYYY-MM-DD HH:MM" → (date, hour)。
func splitHourTarget(target string) (string, int, bool) {
	target = strings.TrimSpace(target)
	if len(target) < 13 || target[10] != ' ' {
		return "", 0, false
	}
	hour, err := strconv.Atoi(target[11:13])
	if err != nil || hour < 0 || hour > 23 {
		return "", 0, false
	}
	return target[:10], hour, true
}

// metricAccum 指标累加器。
type metricAccum struct {
	pairs        int
	sumAbs       float64
	sumSq        float64
	sumErr       float64
	mapeSum      float64
	mapeN        int
	mapeExcluded int
	bandN        int
	bandCovered  int
}

func newMetricAccum() *metricAccum { return &metricAccum{} }

func (a *metricAccum) add(pred, act float64, p *reviewPoint) {
	err := pred - act
	a.pairs++
	a.sumAbs += math.Abs(err)
	a.sumSq += err * err
	a.sumErr += err
	if math.Abs(act) >= mapeMinAbsActual {
		a.mapeSum += math.Abs(err) / math.Abs(act)
		a.mapeN++
	} else {
		a.mapeExcluded++
	}
	if p != nil && p.hasBand {
		a.bandN++
		if act >= p.q10 && act <= p.q90 {
			a.bandCovered++
		}
	}
}

func (a *metricAccum) mae() float64 {
	if a.pairs == 0 {
		return 0
	}
	return a.sumAbs / float64(a.pairs)
}

func (a *metricAccum) rmse() float64 {
	if a.pairs == 0 {
		return 0
	}
	return math.Sqrt(a.sumSq / float64(a.pairs))
}

func (a *metricAccum) bias() float64 {
	if a.pairs == 0 {
		return 0
	}
	return a.sumErr / float64(a.pairs)
}

func (a *metricAccum) mape() float64 {
	if a.mapeN == 0 {
		return 0
	}
	return a.mapeSum / float64(a.mapeN) * 100
}

// coverage P10~P90 覆盖率（%）；没有分位带时返回 nil。
func (a *metricAccum) coverage() *float64 {
	if a.bandN == 0 {
		return nil
	}
	v := float64(a.bandCovered) / float64(a.bandN) * 100
	return &v
}
