package forecast

import (
	"context"
	"strings"
	"time"

	"application/internal/logic/gdmetric"
	"s_db"
	"s_db/s_db_model"
	"s_forecast"

	"common/c_log"
	"github.com/pkg/errors"
)

// 96 点（日内）电价预测：序列源换成电网日指标 CSV 里的 15 分钟日前/实时电价。
//
// 与 day 粒度的区别不只是步长：
//
//	day      —— 序列来自日报已确认成交价（一天一个点），只能出未来多日日均
//	intraday —— 序列来自 gd_metric.csv（15 分钟，CSV 里已有 5 年多），能出未来 5 日 × 96 点
//
// 为什么上限是 5 天：服务端 TimesFM max_horizon=512，而 96×5=480 是能整日覆盖的最大值。
const (
	IntradayStepMinutes = 15.0
	// MaxIntradayHorizonDays 96 × 5 = 480 ≤ max_horizon(512)
	MaxIntradayHorizonDays = 5
	// defaultIntradayHistoryDays 送入模型的历史天数（30 天 = 2880 点 ≤ max_context 4096）
	defaultIntradayHistoryDays = 30
)

// TPriceForecastPoint 一个预测点（含目标时刻，留痕也要用）
type TPriceForecastPoint struct {
	TargetTime time.Time
	Point      float64
	Q10        float64
	Q50        float64
	Q90        float64
}

// SPriceForecastIntraday 96 点预测结果
type SPriceForecastIntraday struct {
	Market         string
	Grain          string
	Dataset        string
	Unit           string
	Model          string
	From           string
	To             string
	HistoryTimes   []time.Time
	HistoryValues  []float64
	Points         []TPriceForecastPoint
	ContextPoints  int
	InputTruncated bool
	// InputChannels 送入模型的通道数：1 = 只用目标市场自身历史；
	// 实时电价是 3（实时价 + 日前价 + 系统负荷，实测 MAE 96.4 → 89.5 元/MWh）。
	InputChannels int
	// CoveredDays 源数据里真实覆盖的自然日数。
	// 不能用时间戳去数：CSV 的一天写作 00:15…24:00，最后一点落在次日 00:00，
	// 按时间戳数会多算一天（MissingDays 因此可能算成负数）。
	CoveredDays int
	TimeMs      float64
	GeneratedOn string
}

// latestAvailableDate 电网日指标里最新可用的日期（空串=拿不到）。
func latestAvailableDate(ctx context.Context) string {
	snap, err := gdmetric.GetSnapshot(ctx, "", "", "")
	if err != nil || snap == nil {
		return ""
	}
	return snap.LatestDate
}

// clampAnchor 把预测锚点收到「数据里最新可用的那一天」。
//
// 为什么要收：CSV 会明显滞后于现实日期（部署上数据只到 08-31，而当天是 10-07）。
// 按 asOf 直接取窗口会取到一段**完全空**的范围，然后报「数据不足（0 点）」——
// 看起来像「没有数据」，实际是取错了窗口。信息披露页的 /api/gd-metric 早就是
// 「默认取最新可用日」的口径，这里对齐它。
//
// latestDate 为空 / 解析失败 / 不早于 today 时原样返回 today：
// 锚点校正只是兜底，不该因为一个辅助信息把预测直接挡住。
func clampAnchor(today time.Time, latestDate string) time.Time {
	if latestDate == "" {
		return today
	}
	latest, err := time.ParseInLocation(priceDateLayout, latestDate, today.Location())
	if err != nil || latest.IsZero() || !latest.Before(today) {
		return today
	}
	return latest
}

// LoadPriceIntraday 用电网日指标 CSV 的 15 分钟序列做 96 点预测（以「今天」为预测起点的未来预测）。
func LoadPriceIntraday(ctx context.Context, market string, historyDays, horizonDays int, quantiles []float64) (*SPriceForecastIntraday, error) {
	return loadPriceIntradayAsOf(ctx, market, historyDays, horizonDays, quantiles, time.Now())
}

// loadPriceIntradayAsOf 以 asOf 为「今天」做预测。
// 回测靠它把预测起点挪到历史某天：只用 asOf 之前的序列，预测 asOf 之后 horizonDays 天。
func loadPriceIntradayAsOf(
	ctx context.Context, market string, historyDays, horizonDays int, quantiles []float64, asOf time.Time,
) (*SPriceForecastIntraday, error) {
	spec, err := resolvePriceSpec(market)
	if err != nil {
		return nil, err
	}
	kind := gdmetric.KindDayAhead
	if spec.Market == "real" {
		kind = gdmetric.KindRealtime
	}
	if historyDays <= 0 {
		historyDays = defaultIntradayHistoryDays
	}
	if horizonDays <= 0 {
		horizonDays = MaxIntradayHorizonDays
	}
	if horizonDays > MaxIntradayHorizonDays {
		horizonDays = MaxIntradayHorizonDays
	}
	if len(quantiles) == 0 {
		quantiles = defaultQuantiles
	}

	baseURL, _, _, err := loadForecastConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("策略未配置预测服务地址")
	}

	loc := priceLocation()
	// 锚点先按 asOf，再收到「数据里最新可用的那一天」——CSV 常常明显滞后于现实日期
	// （部署上数据只到 08-31，而当天是 10-07）。不收的话窗口会整段为空，
	// 报出来是「15 分钟电价数据不足（0 点）」，看着像「没数据」，其实是**取错了窗口**。
	today := clampAnchor(asOf.In(loc), latestAvailableDate(ctx))
	to := today.Format(priceDateLayout)
	from := today.AddDate(0, 0, -historyDays+1).Format(priceDateLayout)

	series, err := gdmetric.GetSeries(ctx, from, to, kind)
	if err != nil {
		return nil, errors.Wrap(err, "读取电网日指标序列失败")
	}
	if len(series) < 96 {
		return nil, errors.Errorf("15 分钟%s电价数据不足（%d 点，至少需要一天 96 点）",
			marketLabel(spec.Market), len(series))
	}

	times := make([]time.Time, 0, len(series))
	values := make([]float64, 0, len(series))
	daySet := make(map[string]bool, 32)
	for _, s := range series {
		ts, err := parseSampleClock(s.Date, s.Time, loc)
		if err != nil {
			continue // 个别时刻格式异常就跳过，不因一个点废掉整段
		}
		daySet[s.Date] = true
		times = append(times, ts)
		values = append(values, s.Value)
	}

	// 输入通道：第 0 个是目标序列（要预测的那个市场），其余是协变量。
	// 只有实时电价加协变量 —— 日前预测实测无增益，见 buildPriceInputChannels。
	times, channels, covDropped := buildPriceInputChannels(ctx, from, to, spec.Market, times, values, loc)
	if len(channels[0]) < 96 {
		return nil, errors.Errorf("15 分钟%s电价数据不足（%d 点，至少需要一天 96 点）",
			marketLabel(spec.Market), len(channels[0]))
	}
	if covDropped > 0 {
		c_log.Infof(ctx, "%s电价预测：因协变量缺值丢弃 %d 个点（保证各通道时刻对齐）",
			marketLabel(spec.Market), covDropped)
	}

	// 截断到模型上下文上限（按尾部保留，最近的更重要）
	truncated := false
	if n := len(channels[0]); n > maxContextPoints {
		keep := n - maxContextPoints
		times = times[keep:]
		for i := range channels {
			channels[i] = channels[i][keep:]
		}
		truncated = true
	}

	horizon := horizonDays * 96
	client := s_forecast.NewClient(baseURL, forecastTimeout)
	series0 := &s_forecast.SSeries{Id: spec.Market}
	if len(channels) == 1 {
		series0.Values = channels[0]
	} else {
		series0.Matrix = channels
	}
	resp, err := client.Forecast(ctx, &s_forecast.SForecastRequest{
		Series:    []*s_forecast.SSeries{series0},
		Horizon:   horizon,
		Quantiles: quantiles,
	})
	if err != nil {
		return nil, errors.Wrap(err, "调用预测服务失败")
	}
	item := pickForecast(resp.Forecasts, spec.Market)
	if item == nil || len(item.Point) == 0 {
		return nil, errors.New("预测服务未返回点预测")
	}

	step := time.Duration(IntradayStepMinutes) * time.Minute
	last := times[len(times)-1]
	points := make([]TPriceForecastPoint, 0, len(item.Point))
	for i, p := range item.Point {
		points = append(points, TPriceForecastPoint{
			TargetTime: last.Add(time.Duration(i+1) * step),
			Point:      p,
			Q10:        quantileAt(item.Quantiles, "0.1", i),
			Q50:        quantileAt(item.Quantiles, "0.5", i),
			Q90:        quantileAt(item.Quantiles, "0.9", i),
		})
	}

	return &SPriceForecastIntraday{
		Market:         spec.Market,
		Grain:          s_db_model.ForecastGrainIntraday,
		Dataset:        spec.Dataset,
		Unit:           intradayPriceUnit,
		Model:          resp.Model,
		From:           series[0].Date,
		To:             series[len(series)-1].Date,
		HistoryTimes:   times,
		HistoryValues:  channels[0],
		Points:         points,
		ContextPoints:  len(channels[0]),
		InputChannels:  len(channels),
		InputTruncated: truncated,
		CoveredDays:    len(daySet),
		TimeMs:         resp.TimeMs,
		GeneratedOn:    today.Format(priceDateLayout),
	}, nil
}

// priceCovariateKinds 实时电价预测额外送的通道（除目标序列之外）。
//
// 实测（2026-09-08 ~ 10-01，24 天真实实际值，sourceId=9）MAE / 标幺：
//
//	单变量              96.4 / 0.191
//	+ 系统负荷          94.7 / 0.186
//	+ 负荷 + 日前电价    89.5 / 0.177  ← 采用
//
// 日前预测同样测过（+负荷 / +实时价 / +星期几 / +节假日标志 / 同星期几历史），
// 全部持平或更差，所以日前保持单变量。
// 注：目前只能送「历史区间内」的协变量；目标日的日前价其实提前一天就出了，
// 要利用它需要服务端支持「未来协变量」，那是后续的事。
var priceCovariateKinds = map[string][]gdmetric.SeriesKind{
	"real": {gdmetric.KindDayAhead, gdmetric.KindLoad},
}

// buildPriceInputChannels 组多变量输入：第 0 个通道是目标序列（就是传进来的 times/values），
// 后面按 priceCovariateKinds 补协变量。各通道按**时间戳**对齐：
// 某个时刻只要有一个通道缺值，这个时刻就从所有通道里去掉（返回丢弃的点数）。
//
// 拿不到协变量（没数据 / 读取失败）就退回单变量：协变量是增益，不是必需，
// 不能因为它把一个本来能出的预测挡住。
func buildPriceInputChannels(
	ctx context.Context, from, to, market string, times []time.Time, values []float64, loc *time.Location,
) ([]time.Time, [][]float64, int) {
	channels := [][]float64{values}
	kinds := priceCovariateKinds[market]
	if len(kinds) == 0 || len(times) == 0 {
		return times, channels, 0
	}

	covariates := make([]map[string]float64, 0, len(kinds))
	for _, kind := range kinds {
		series, err := gdmetric.GetSeries(ctx, from, to, kind)
		if err != nil {
			c_log.Warningf(ctx, "读取%s协变量失败，退回单变量预测: %+v", kind, err)
			return times, channels, 0
		}
		byTime := make(map[string]float64, len(series))
		for _, s := range series {
			ts, tsErr := parseSampleClock(s.Date, s.Time, loc)
			if tsErr != nil {
				continue
			}
			byTime[sampleKey(ts)] = s.Value
		}
		if len(byTime) == 0 {
			c_log.Warningf(ctx, "%s协变量没有数据，退回单变量预测", kind)
			return times, channels, 0
		}
		covariates = append(covariates, byTime)
	}

	keptTimes, matrix, dropped := alignChannels(times, values, covariates, loc)
	if len(keptTimes) == 0 {
		c_log.Warningf(ctx, "协变量与%s电价序列没有对齐的时刻，退回单变量预测", market)
		return times, channels, 0
	}
	return keptTimes, matrix, dropped
}

// alignChannels 把目标序列与协变量按时间戳对齐成多变量矩阵。
// 某个时刻只要有一个通道缺值，这个时刻就从所有通道里去掉（丢掉点数）——
// 通道长度必须一致，服务端按二维数组收，错位了就是拿别的时刻的值当协变量。
func alignChannels(
	times []time.Time, values []float64, covariates []map[string]float64, loc *time.Location,
) ([]time.Time, [][]float64, int) {
	keptTimes := make([]time.Time, 0, len(times))
	matrix := make([][]float64, 1+len(covariates))
	for i := range matrix {
		matrix[i] = make([]float64, 0, len(times))
	}
	for i, ts := range times {
		key := sampleKey(ts)
		ok := true
		for _, byTime := range covariates {
			if _, found := byTime[key]; !found {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		keptTimes = append(keptTimes, ts)
		matrix[0] = append(matrix[0], values[i])
		for j, byTime := range covariates {
			matrix[j+1] = append(matrix[j+1], byTime[key])
		}
	}
	return keptTimes, matrix, len(times) - len(keptTimes)
}

// parseSampleClock 把 CSV 的 (日期, "HH:MM") 组成时间点；"24:00" 归到次日 00:00。
// CSV 的时间戳是 00:15 … 24:00 这种写法，24:00 不属于当天。
func parseSampleClock(date, clock string, loc *time.Location) (time.Time, error) {
	d, err := time.ParseInLocation(priceDateLayout, date, loc)
	if err != nil {
		return time.Time{}, err
	}
	clock = strings.TrimSpace(clock)
	// CSV 把当天最后一个 15 分钟点写成 24:00，它属于次日 00:00
	if clock == "24:00" {
		return d.AddDate(0, 0, 1), nil
	}
	t, err := time.ParseInLocation("15:04", clock, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
}

func quantileAt(m map[string][]float64, key string, i int) float64 {
	if m == nil {
		return 0
	}
	series := m[key]
	if i < 0 || i >= len(series) {
		return 0
	}
	return series[i]
}

// intradayPriceUnit 电网日指标 CSV 里电价是「元/兆瓦时」，统一写成 元/MWh 与 day 粒度一致
const intradayPriceUnit = "元/MWh"

// RecordPriceForecast 写留痕。失败只记日志、不影响本次返回 ——
// 预测是主流程，留痕是附带产物，不能因为它写不进去就让用户看不到结果。
func RecordPriceForecast(ctx context.Context, rows []*s_db_model.SPriceForecastModel) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	if err := s_db.GetPriceForecastService().UpsertBatch(ctx, rows); err != nil {
		c_log.Warningf(ctx, "写入电价预测留痕失败: %+v", err)
		return 0, err
	}
	return len(rows), nil
}
