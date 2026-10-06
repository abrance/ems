package forecast

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"common"
	"common/c_base"
	"common/c_chart"
	"s_db"
	"s_forecast"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/pkg/errors"
)

const (
	// policySettingId 微电网策略配置所在的 setting ID（与 p_policy_mircogrid 一致）
	policySettingId = "policy_microgrid"
	// powerPointKey 设备实时有功功率点位 Key
	powerPointKey = "Power"

	defaultLookbackHours = 24 * 7 // 送入模型的历史窗口
	defaultHistoryHours  = 24     // 返回给前端展示的历史窗口
	defaultHorizon       = 4      // 默认预测步数（15 分钟/点 时约 1 小时）
	defaultStepMinutes   = 15.0   // 无法推断采样间隔时的兜底值

	// maxContextPoints TimesFM 最大上下文点数（服务端 max_context=4096）
	maxContextPoints = 4096

	forecastTimeout = 30 * time.Second
)

// defaultQuantiles 默认分位点：10% / 50% / 90%
var defaultQuantiles = []float64{0.1, 0.5, 0.9}

// SLoadForecast 负荷预测结果。
// 历史窗口用于与预测曲线同图对比，预测起点为历史窗口末点的下一个步长。
type SLoadForecast struct {
	DeviceId          string
	Model             string
	StepMinutes       float64
	LookbackPoints    int  // 实际送入模型的点数
	InputTruncated    bool // 历史长度是否被本地截断
	HistoryTimes      []int64
	HistoryValues     []float64
	ForecastTimes     []int64
	ForecastPoint     []float64
	ForecastQuantiles map[string][]float64
	TimeMs            float64
}

// Load 读取设备历史有功功率并调用 TimesFM 预测，返回历史窗口与预测曲线。
func Load(ctx context.Context, deviceID string, lookbackHours, historyHours, horizon int) (*SLoadForecast, error) {
	if lookbackHours <= 0 {
		lookbackHours = defaultLookbackHours
	}
	if historyHours <= 0 {
		historyHours = defaultHistoryHours
	}

	baseURL, cfgDeviceID, cfgHorizon, err := loadForecastConfig(ctx)
	if err != nil {
		return nil, err
	}
	if deviceID == "" {
		deviceID = cfgDeviceID
	}
	if deviceID == "" {
		return nil, errors.New("未指定设备，且策略未配置关口电表")
	}
	if horizon <= 0 {
		horizon = cfgHorizon
	}
	if horizon <= 0 {
		horizon = defaultHorizon
	}
	if baseURL == "" {
		return nil, errors.New("策略未配置预测服务地址")
	}

	times, values, err := loadHistorySeries(deviceID, lookbackHours)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.Errorf("设备 %s 无历史负荷数据", deviceID)
	}

	// 限制送入模型的历史长度（TimesFM max_context）
	truncated := false
	if len(values) > maxContextPoints {
		times = times[len(times)-maxContextPoints:]
		values = values[len(values)-maxContextPoints:]
		truncated = true
	}
	lookbackPoints := len(values)
	if lookbackPoints < 2 {
		return nil, errors.New("历史数据不足两个点，无法预测")
	}

	client := s_forecast.NewClient(baseURL, forecastTimeout)
	resp, err := client.Forecast(ctx, &s_forecast.SForecastRequest{
		Series:    []*s_forecast.SSeries{{Id: deviceID, Values: values}},
		Horizon:   horizon,
		Quantiles: defaultQuantiles,
	})
	if err != nil {
		return nil, errors.Wrap(err, "调用预测服务失败")
	}
	item := resp.Forecasts[0]
	if item == nil || len(item.Point) == 0 {
		return nil, errors.New("预测服务未返回点预测")
	}

	step := medianStepMinutes(times)

	// 仅返回最近 historyHours 的历史窗口，避免响应过大
	histN := int(math.Ceil(float64(historyHours) * 60 / step))
	if histN > len(times) {
		histN = len(times)
	}
	histTimes := times[len(times)-histN:]
	histValues := values[len(values)-histN:]

	return &SLoadForecast{
		DeviceId:          deviceID,
		Model:             resp.Model,
		StepMinutes:       step,
		LookbackPoints:    lookbackPoints,
		InputTruncated:    truncated,
		HistoryTimes:      histTimes,
		HistoryValues:     histValues,
		ForecastTimes:     buildForecastTimes(times[len(times)-1], step, len(item.Point)),
		ForecastPoint:     item.Point,
		ForecastQuantiles: item.Quantiles,
		TimeMs:            resp.TimeMs,
	}, nil
}

// buildForecastTimes 依据历史末点与采样步长生成预测时间轴。
func buildForecastTimes(lastTs int64, stepMinutes float64, n int) []int64 {
	stepMs := int64(stepMinutes * 60 * 1000)
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		out[i] = lastTs + int64(i+1)*stepMs
	}
	return out
}

// medianStepMinutes 由历史时间戳推断采样间隔（取相邻差的中位数，单位分钟）。
// 点数不足或间隔异常时返回默认 15 分钟。
func medianStepMinutes(times []int64) float64 {
	if len(times) < 2 {
		return defaultStepMinutes
	}
	diffs := make([]int64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		if d := times[i] - times[i-1]; d > 0 {
			diffs = append(diffs, d)
		}
	}
	if len(diffs) == 0 {
		return defaultStepMinutes
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i] < diffs[j] })
	median := diffs[len(diffs)/2]
	if median <= 0 {
		return defaultStepMinutes
	}
	return float64(median) / 60000.0
}

// loadForecastConfig 从微电网策略设置中读取预测相关配置。
func loadForecastConfig(ctx context.Context) (baseURL, gridDeviceID string, horizon int, err error) {
	raw := s_db.GetSettingService().GetSettingValueById(ctx, policySettingId)
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", "", 0, errors.New("微电网策略配置为空")
	}
	j := gjson.New(*raw)
	return strings.TrimSpace(j.Get("forecastBaseUrl").String()),
		strings.TrimSpace(j.Get("gridDeviceId").String()),
		j.Get("forecastHorizon").Int(),
		nil
}

// loadHistorySeries 读取设备历史有功功率，返回升序的时间戳（毫秒）与数值。
func loadHistorySeries(deviceID string, lookbackHours int) ([]int64, []float64, error) {
	storage := common.GetStorageInstanceOrNil()
	if storage == nil {
		return nil, nil, errors.New("存储实例未就绪")
	}
	end := time.Now().UnixMilli()
	start := end - int64(lookbackHours)*3600*1000

	chart, err := storage.GetStorageData(c_base.StorageTypeDevice, deviceID, []string{powerPointKey}, &start, &end, 0)
	if err != nil {
		return nil, nil, errors.Wrap(err, "查询设备历史负荷失败")
	}
	return parseChartPoints(chart)
}

// parseChartPoints 解析图表数据为升序的时间戳/数值对，跳过空值与非法点。
func parseChartPoints(chart *c_chart.ChartData) ([]int64, []float64, error) {
	if chart == nil || len(chart.Series) == 0 || chart.Series[0] == nil {
		return nil, nil, nil
	}
	values := chart.Series[0].Data
	timestamps := chart.XAxis.Data
	n := len(values)
	if len(timestamps) < n {
		n = len(timestamps)
	}

	times := make([]int64, 0, n)
	nums := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if strings.TrimSpace(values[i]) == "" {
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
		times = append(times, ts)
		nums = append(nums, v)
	}

	sort.Sort(byTime{times: times, values: nums})
	return times, nums, nil
}

// byTime 按时间戳对「时间戳/数值」平行切片排序。
type byTime struct {
	times  []int64
	values []float64
}

func (b byTime) Len() int           { return len(b.times) }
func (b byTime) Less(i, j int) bool { return b.times[i] < b.times[j] }
func (b byTime) Swap(i, j int) {
	b.times[i], b.times[j] = b.times[j], b.times[i]
	b.values[i], b.values[j] = b.values[j], b.values[i]
}
