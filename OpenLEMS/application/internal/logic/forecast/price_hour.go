package forecast

import (
	"context"
	"fmt"
	"strings"
	"time"

	"s_db"
	"s_db/s_db_model"
	"s_forecast"

	"github.com/pkg/errors"
)

// 小时粒度（24 点）电价预测：序列源换成官网主页 24 点分时（gd_home_24h）。
//
// 与另外两路的区别不只是步长：
//
//	day      —— 官方直采成交价（一天一个点），出未来多日日均
//	hour     —— 官网主页 24 整点，出**目标日** X 的 24 个整点（本文件）
//	intraday —— 电网日指标 CSV 的 15 分钟电价（已停更）
//
// 目标日语义（as-of 滚动）：
//   - 数据止于 T-6 左右，所以「锚定最新日 D 再预测 D+1」出的其实是 5 天前的日期。
//   - 这里改成「选目标日 X（默认今天），以 X 之前最新的完整 24 点日为锚点 D，
//     预测 (X-D)×24 点，取最后 24 点作为 X」。
//   - 于是「默认预测当天」成立，而滞后多少天由锚点 D 在响应里如实给出（To= D）。
const (
	// Home24hStepMinutes 24 点分时的采样步长（分钟）
	Home24hStepMinutes = 60.0
	// home24hPointsPerDay 一天 24 个整点
	home24hPointsPerDay = 24
	// defaultHourHistoryDays 默认送入模型的历史天数（14 天 = 336 点）
	defaultHourHistoryDays = 14
	// minHourHistoryDays 历史太短预测没意义（7 天 = 168 点）
	minHourHistoryDays = 7
	// maxHourHistoryDays 历史上限（30 天 = 720 点，远低于 max_context 4096）
	maxHourHistoryDays = 30
	// maxHourHorizonPoints TimesFM 单次最大步数（服务端 max_horizon=512，约 21 天）。
	// 目标日距锚点太远就超限，只能让用户把目标日往前调，不能静默截断。
	maxHourHorizonPoints = 512
	// home24hDataset 本路的 dataset 名（响应字段，非表名）
	home24hDataset = "官网主页 24 点"
)

// hourSeriesSource 24 点原始行的读取函数，便于测试替换。
type hourSeriesSource func(ctx context.Context, from, to string) ([]*s_db_model.SHome24hModel, error)

// defaultHourSeriesSource 读库里 [from, to] 的 24 点行。
func defaultHourSeriesSource(ctx context.Context, from, to string) ([]*s_db_model.SHome24hModel, error) {
	return s_db.GetHomeMarketService().ListHome24hRange(ctx, from, to)
}

// loadHourCached 读取 24 点序列并预测目标日的 24 个整点。结果按参数缓存。
func loadHourCached(ctx context.Context, req PriceRequest) (*TPriceForecastResult, error) {
	historyDays := req.HistoryDays
	if historyDays <= 0 {
		historyDays = defaultHourHistoryDays
	}
	if historyDays < minHourHistoryDays {
		historyDays = minHourHistoryDays
	}
	if historyDays > maxHourHistoryDays {
		historyDays = maxHourHistoryDays
	}
	quantiles := req.Quantiles
	if len(quantiles) == 0 {
		quantiles = defaultQuantiles
	}

	loc := priceLocation()
	targetDate, err := normalizeTargetDate(req.TargetDate, loc, time.Now())
	if err != nil {
		return nil, err
	}

	baseURL, _, _, err := loadForecastConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("策略未配置预测服务地址")
	}

	key := fmt.Sprintf("forecast:price:%s:%s:%s:%d:%s",
		s_db_model.PriceSourceHome24h, req.Market, targetDate, historyDays, quantileKey(quantiles))
	return withForecastCache(ctx, key, req.ForceRefresh, func(ctx context.Context) (*TPriceForecastResult, error) {
		client := s_forecast.NewClient(baseURL, forecastTimeout)
		return loadPriceHourWith(ctx, client, defaultHourSeriesSource, req.Market, targetDate, historyDays, quantiles, loc, time.Now())
	})
}

// normalizeTargetDate 目标日缺省为「今天」，格式必须 YYYY-MM-DD，且不能晚于今天。
//
// 晚于今天直接报错而不是回落到今天：目标日是用户明确选的，悄悄改掉会让人以为
// 自己在看未来那天的预测。
func normalizeTargetDate(raw string, loc *time.Location, now time.Time) (string, error) {
	today := now.In(loc).Format(priceDateLayout)
	if raw = strings.TrimSpace(raw); raw == "" {
		return today, nil
	}
	t, err := time.ParseInLocation(priceDateLayout, raw, loc)
	if err != nil {
		return "", errors.Errorf("目标日格式无效: %s（应为 YYYY-MM-DD）", raw)
	}
	if t.Format(priceDateLayout) > today {
		return "", errors.Errorf("目标日 %s 晚于今天 %s：还没有可用的 24 点数据可锚", raw, today)
	}
	return t.Format(priceDateLayout), nil
}

// loadPriceHourWith 执行一次 24 点预测，不含缓存与配置解析，便于测试注入。
func loadPriceHourWith(
	ctx context.Context,
	client s_forecast.IClient,
	source hourSeriesSource,
	market, targetDate string,
	historyDays int,
	quantiles []float64,
	loc *time.Location,
	now time.Time,
) (*TPriceForecastResult, error) {
	spec, err := resolvePriceSpec(market)
	if err != nil {
		return nil, err
	}
	target, err := time.ParseInLocation(priceDateLayout, targetDate, loc)
	if err != nil {
		return nil, errors.Errorf("目标日格式无效: %s", targetDate)
	}
	if targetDate > now.In(loc).Format(priceDateLayout) {
		return nil, errors.Errorf("目标日 %s 晚于今天", targetDate)
	}

	// 下界要能覆盖「anchor 再往前 historyDays 天」；上界到目标日前一天。
	from := target.AddDate(0, 0, -(historyDays + 32)).Format(priceDateLayout)
	to := target.AddDate(0, 0, -1).Format(priceDateLayout)
	rows, err := source(ctx, from, to)
	if err != nil {
		return nil, errors.Wrap(err, "读取 24 点分时失败")
	}

	if len(rows) == 0 {
		// 空表和「窗口里没完整日」是两回事：前者是采集没配/没跑过（本地开发最常见），
		// 后者才是等源站发布。混成一句话会让人去怀疑预测逻辑。
		return nil, errors.Errorf("本库还没有 24 点分时数据（%s ~ %s 一行都没有）："+
			"先确认已配置官网主页网关（HOME_MARKET_GATEWAY_BASE），再调 POST /api/gd-metric/home-market/backfill 回补",
			from, to)
	}
	valuesByDate := groupHome24h(rows, spec.Market)
	anchor, err := latestCompleteDay(valuesByDate, targetDate, from)
	if err != nil {
		return nil, err
	}

	// 从锚点往前取连续 historyDays 个完整日；中间缺一天就报错，不跳过去凑数。
	days := make([]string, 0, historyDays)
	for i := 0; i < historyDays; i++ {
		d := anchor.AddDate(0, 0, -i).Format(priceDateLayout)
		if len(valuesByDate[d]) != home24hPointsPerDay {
			return nil, errors.Errorf("24 点电价数据不足：锚点 %s 之前的 %s 不足 24 点（需要连续 %d 个完整日）",
				anchor.Format(priceDateLayout), d, historyDays)
		}
		days = append(days, d)
	}
	// days 是倒序（锚点在前），反转成时间升序
	for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
		days[i], days[j] = days[j], days[i]
	}

	times := make([]time.Time, 0, len(days)*home24hPointsPerDay)
	values := make([]float64, 0, len(days)*home24hPointsPerDay)
	for _, d := range days {
		day, err := time.ParseInLocation(priceDateLayout, d, loc)
		if err != nil {
			continue
		}
		hours := valuesByDate[d]
		for h := 0; h < home24hPointsPerDay; h++ {
			v, ok := hours[h]
			if !ok {
				return nil, errors.Errorf("24 点电价数据不足：%s 缺 %02d:00", d, h)
			}
			times = append(times, day.Add(time.Duration(h)*time.Hour))
			values = append(values, v)
		}
	}

	// 预测步数 = 锚点到目标日的整点数，取最后 24 点作为目标日。
	steps := (daysBetween(anchor.Format(priceDateLayout), targetDate) - 1) * home24hPointsPerDay
	if steps <= 0 {
		return nil, errors.Errorf("锚点 %s 与目标日 %s 之间没有可预测的步数",
			anchor.Format(priceDateLayout), targetDate)
	}
	if steps > maxHourHorizonPoints {
		return nil, errors.Errorf("目标日距锚点 %d 天，超过模型单次上限 %d 天，请把目标日往前调",
			steps/home24hPointsPerDay, maxHourHorizonPoints/home24hPointsPerDay)
	}

	truncated := false
	if len(values) > maxContextPoints {
		keep := len(values) - maxContextPoints
		times = times[keep:]
		values = values[keep:]
		truncated = true
	}

	resp, err := client.Forecast(ctx, &s_forecast.SForecastRequest{
		Series:    []*s_forecast.SSeries{{Id: spec.Market, Values: values}},
		Horizon:   steps,
		Quantiles: quantiles,
	})
	if err != nil {
		return nil, errors.Wrap(err, "调用预测服务失败")
	}
	item := pickForecast(resp.Forecasts, spec.Market)
	if item == nil || len(item.Point) < home24hPointsPerDay {
		return nil, errors.New("预测服务未返回足够的点预测（不足 24 点）")
	}

	// 取最后 24 点：那是目标日当天的 24 个整点。目标时间直接由目标日推，不拿返回值反推日期。
	offset := len(item.Point) - home24hPointsPerDay
	points := make([]TPriceForecastPoint, 0, home24hPointsPerDay)
	for h := 0; h < home24hPointsPerDay; h++ {
		idx := offset + h
		points = append(points, TPriceForecastPoint{
			TargetTime: target.Add(time.Duration(h) * time.Hour),
			Point:      item.Point[idx],
			Q10:        quantileAt(item.Quantiles, "0.1", idx),
			Q50:        quantileAt(item.Quantiles, "0.5", idx),
			Q90:        quantileAt(item.Quantiles, "0.9", idx),
		})
	}

	return &TPriceForecastResult{
		Market:         spec.Market,
		Source:         s_db_model.PriceSourceHome24h,
		Grain:          s_db_model.ForecastGrainHour,
		TargetDate:     targetDate,
		Dataset:        home24hDataset,
		Scope:          "官网主页",
		Metric:         hourMetricLabel(spec.Market),
		Unit:           "元/MWh",
		Model:          resp.Model,
		From:           days[0],
		To:             anchor.Format(priceDateLayout),
		StepMinutes:    Home24hStepMinutes,
		HistoryDates:   formatHourTimes(times),
		HistoryValues:  values,
		Points:         points,
		Actual:         actualHours(valuesByDate[targetDate]),
		ContextPoints:  len(values),
		InputTruncated: truncated,
		MissingDays:    0, // 窗口已强制完整，缺数据直接报错
		TimeMs:         resp.TimeMs,
		GeneratedOn:    anchor.Format(priceDateLayout),
	}, nil
}

// groupHome24h 按日期分组，再按小时存电价。market=real 取实时列。
func groupHome24h(rows []*s_db_model.SHome24hModel, market string) map[string]map[int]float64 {
	out := make(map[string]map[int]float64, len(rows)/home24hPointsPerDay+1)
	for _, r := range rows {
		if r == nil || r.PriceDate == "" {
			continue
		}
		if out[r.PriceDate] == nil {
			out[r.PriceDate] = make(map[int]float64, home24hPointsPerDay)
		}
		if market == "real" {
			out[r.PriceDate][r.Hour] = r.RealtimePrice
		} else {
			out[r.PriceDate][r.Hour] = r.AheadPrice
		}
	}
	return out
}

// latestCompleteDay 目标日之前（严格小于）最新的 24 点齐全日。
// 扫描范围限于已取回的窗口 [from, target-1]，而不是凭空往回数固定天数。
func latestCompleteDay(valuesByDate map[string]map[int]float64, targetDate, from string) (time.Time, error) {
	target, err := time.ParseInLocation(priceDateLayout, targetDate, priceLocation())
	if err != nil {
		return time.Time{}, errors.Errorf("目标日格式无效: %s", targetDate)
	}
	span := daysBetween(from, targetDate)
	for i := 1; i < span; i++ {
		d := target.AddDate(0, 0, -i)
		if len(valuesByDate[d.Format(priceDateLayout)]) == home24hPointsPerDay {
			return d, nil
		}
	}
	return time.Time{}, errors.Errorf("24 点电价数据不足：目标日 %s 之前没有完整的 24 点运行日", targetDate)
}

// actualHours 目标日的 24 点实际值；不满 24 点返回空切片（不补 0，否则图上会画成全天 0 元）。
func actualHours(hours map[int]float64) []float64 {
	if len(hours) != home24hPointsPerDay {
		return []float64{}
	}
	out := make([]float64, home24hPointsPerDay)
	for h := 0; h < home24hPointsPerDay; h++ {
		out[h] = hours[h]
	}
	return out
}

func formatHourTimes(times []time.Time) []string {
	out := make([]string, 0, len(times))
	for _, t := range times {
		out = append(out, t.Format("2006-01-02 15:04"))
	}
	return out
}

func hourMetricLabel(market string) string {
	if market == "real" {
		return "实时电价（整点）"
	}
	return "日前电价（整点）"
}
