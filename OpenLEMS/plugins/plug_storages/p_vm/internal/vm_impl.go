package internal

import (
	"bytes"
	"common/c_base"
	"common/c_chart"
	"common/c_log"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/shockerli/cvt"
)

// vmStorage 是 c_base.IStorage 在 VictoriaMetrics 上的实现。
//
// 写：vminsert 的 prometheus 文本导入接口（同一序列同一时间戳重复写是覆盖，天然幂等）。
// 读：vmselect 的 /api/v1/query_range（集群版只支持 PromQL，不支持 InfluxQL）。
type vmStorage struct {
	ctx       context.Context
	baseURL   string // vminsert 根地址，用于 /delete_series
	insertURL string // 写样本
	selectURL string // 查样本（其下接 /api/v1/query_range）
	client    *http.Client
}

// NewVMStorage 构造 VictoriaMetrics 存储实例。
func NewVMStorage(ctx context.Context, cfg *c_base.SStorageConfig) c_base.IStorage {
	params := map[string]string{}
	base := ""
	timeout := 30 * time.Second
	if cfg != nil {
		base = strings.TrimRight(cfg.Url, "/")
		for k, v := range cfg.Params {
			params[k] = v
		}
		if ms, err := strconv.Atoi(params[ParamTimeoutMS]); err == nil && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}

	insertURL := params[ParamInsertURL]
	if insertURL == "" && base != "" {
		if strings.Contains(base, "/insert/") {
			insertURL = base
		} else {
			insertURL = base + DefaultInsertPath
		}
	}
	selectURL := params[ParamSelectURL]
	if selectURL == "" && base != "" {
		if strings.Contains(base, "/select/") {
			selectURL = base
		} else {
			selectURL = base + DefaultSelectPath
		}
	}

	// vminsert 的 /delete_series 挂在根上，用基础地址
	deleteBase := base
	if u, err := url.Parse(insertURL); err == nil && deleteBase == "" {
		deleteBase = u.Scheme + "://" + u.Host
	}

	s := &vmStorage{
		ctx:       ctx,
		baseURL:   strings.TrimRight(deleteBase, "/"),
		insertURL: insertURL,
		selectURL: strings.TrimRight(selectURL, "/"),
		client:    &http.Client{Timeout: timeout},
	}
	c_log.BizInfof(ctx, "VictoriaMetrics 存储已启用：写 %s，读 %s", s.insertURL, s.selectURL)
	return s
}

// ---------------------------------------------------------------- 写入

func (v *vmStorage) SaveDevices(deviceId string, pointValues []*c_base.SPointValue) error {
	if len(pointValues) == 0 {
		return nil
	}
	ts := time.Now().UnixMilli()
	samples := make([]sample, 0, len(pointValues))
	for _, pv := range pointValues {
		if pv == nil || pv.IPoint == nil {
			continue
		}
		val, err := cvt.Float64E(pv.GetValue())
		if err != nil {
			continue // 非数值点位不入时序库，与 p_tsdb 一致
		}
		samples = append(samples, sample{
			Name: MetricNameEmsMetric,
			Labels: map[string]string{
				LabelNameType:  string(c_base.StorageTypeDevice),
				LabelNameID:    deviceId,
				LabelNameField: pv.GetKey(),
			},
			Value: val,
			TS:    ts,
		})
	}
	return v.write(samples)
}

func (v *vmStorage) SaveProtocolMetrics(protocolConfig *c_base.SProtocolConfig, deviceConfig *c_base.SDeviceConfig, metrics map[string]any) error {
	if len(metrics) == 0 {
		return nil
	}
	protocolId, deviceId := "", ""
	if protocolConfig != nil {
		protocolId = protocolConfig.Id
	}
	if deviceConfig != nil {
		deviceId = deviceConfig.Id
	}
	return v.writeMetrics(protocolId, map[string]string{
		LabelNameType:     string(c_base.StorageTypeProtocol),
		LabelNameID:       protocolId,
		LabelNameDeviceID: deviceId,
	}, metrics)
}

// SaveSystemMetrics 保存系统/业务指标。
//
// 两种命名：
//   - 不带 tags：沿用 p_tsdb 的 ems_metric{type,id,field} 方案，ems 已有图表不用改；
//   - 带 tags：字段名直接做指标名，tags 原样做标签 —— 外部采集来的指标（如 gdpm_*）用它，
//     避免把指标名塞进 label 造成高基数。
func (v *vmStorage) SaveSystemMetrics(measurement string, tags map[string]string, metrics map[string]any) error {
	if len(metrics) == 0 {
		return nil
	}
	if len(tags) > 0 {
		return v.writeMetrics("", tags, metrics)
	}
	return v.writeMetrics(measurement, map[string]string{
		LabelNameType: string(c_base.StorageTypeSystem),
		LabelNameID:   measurement,
	}, metrics)
}

func (v *vmStorage) SavePriceData(priceId int, price float64, priceType string, timestamp int64) error {
	ts := timestamp * 1000
	labels := map[string]string{
		LabelNameType: string(c_base.StorageTypeSystem),
		LabelNameID:   "price",
	}
	samples := []sample{
		{Name: MetricNameEmsMetric, Labels: withField(labels, "priceId"), Value: float64(priceId), TS: ts},
		{Name: MetricNameEmsMetric, Labels: withField(labels, "price"), Value: price, TS: ts},
		{Name: MetricNameEmsMetric, Labels: withField(labels, "priceType"), Value: float64(priceTypeCode(priceType)), TS: ts},
		{Name: MetricNameEmsMetric, Labels: withField(labels, "timestamp"), Value: float64(timestamp), TS: ts},
	}
	return v.write(samples)
}

// writeMetrics 把 metrics 里的数值字段转成样本；id 为空时按 tags 原生命名。
func (v *vmStorage) writeMetrics(id string, tags map[string]string, metrics map[string]any) error {
	ts := time.Now().UnixMilli()
	samples := make([]sample, 0, len(metrics))
	for field, raw := range metrics {
		val, err := cvt.Float64E(raw)
		if err != nil {
			continue // 非数值字段忽略
		}
		name := MetricNameEmsMetric
		labels := make(map[string]string, len(tags)+1)
		for k, val := range tags {
			labels[k] = val
		}
		if id == "" {
			name = field // tags 原生命名：字段名即指标名，不再多一个 field 标签
		} else {
			labels[LabelNameField] = field
		}
		samples = append(samples, sample{Name: name, Labels: labels, Value: val, TS: ts})
	}
	return v.write(samples)
}

func withField(labels map[string]string, field string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out[LabelNameField] = field
	return out
}

// write 批量写入，撞到 4xx/5xx 直接返回错误（不做重试，重跑同一天是幂等覆盖）。
func (v *vmStorage) write(samples []sample) error {
	if len(samples) == 0 {
		return nil
	}
	for start := 0; start < len(samples); start += writeBatchLines {
		end := start + writeBatchLines
		if end > len(samples) {
			end = len(samples)
		}
		if err := v.postLines(formatSamples(samples[start:end])); err != nil {
			return err
		}
	}
	return nil
}

func (v *vmStorage) postLines(body string) error {
	if v.insertURL == "" {
		return errors.New("VictoriaMetrics 写入地址为空")
	}
	req, err := http.NewRequest(http.MethodPost, v.insertURL, bytes.NewBufferString(body))
	if err != nil {
		return errors.Wrap(err, "构造写入请求失败")
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := v.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "写入 VictoriaMetrics 失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.Errorf("写入 VictoriaMetrics 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// ---------------------------------------------------------------- 读取

// GetStorageData 按 pointKey 取序列。
//
// pointKey 每一项可以是：
//   - 完整的 PromQL 序列选择器，例如 gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}
//     （外部采集指标用它，多个维度不进 id）
//   - 纯字段名，例如 cpu —— 走 ems_metric{type,id,field} 兼容方案
//
// step 单位毫秒；不传（<=0）时按日频取，步长 1 天。
func (v *vmStorage) GetStorageData(storageType c_base.StorageType, id string, pointKey []string, startTime, endTime *int64, step int) (*c_chart.ChartData, error) {
	if len(pointKey) == 0 {
		return c_chart.NewChartData(0), nil
	}
	if len(pointKey) > maxQueryKeys {
		return nil, errors.Errorf("一次最多查询 %d 条序列，收到 %d 条", maxQueryKeys, len(pointKey))
	}

	endMs := time.Now().UnixMilli()
	if endTime != nil && *endTime > 0 {
		endMs = *endTime
	}
	startMs := endMs - 7*24*3600*1000 // 默认最近 7 天（日频指标的典型跨度）
	if startTime != nil && *startTime > 0 {
		startMs = *startTime
	}

	stepSec := 86400
	if step > 0 {
		stepSec = step / 1000
		if stepSec < 1 {
			stepSec = 1
		}
	}

	seriesMap := make(map[string]*c_chart.Series, len(pointKey))
	data := make(map[int64]map[string]float64)
	for _, k := range pointKey {
		selector := buildSelector(storageType, id, k)
		points, err := v.queryRange(selector, startMs, endMs, stepSec)
		if err != nil {
			return nil, errors.Wrapf(err, "查询 %s 失败", selector)
		}
		seriesMap[k] = c_chart.NewSeries(k, c_chart.ChartTypeLine, len(points))
		for _, p := range points {
			if _, ok := data[p.TS]; !ok {
				data[p.TS] = make(map[string]float64, len(pointKey))
			}
			data[p.TS][k] = p.Value
		}
	}

	timestamps := make([]int64, 0, len(data))
	for ts := range data {
		timestamps = append(timestamps, ts)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })

	chart := c_chart.NewChartData(len(pointKey))
	for _, ts := range timestamps {
		chart.AddTimestamp(ts)
		for _, k := range pointKey {
			if val, ok := data[ts][k]; ok {
				seriesMap[k].AppendData(strconv.FormatFloat(val, 'f', -1, 64))
			} else {
				seriesMap[k].AppendData("")
			}
		}
	}
	for _, k := range pointKey {
		chart.AddSeries(seriesMap[k])
	}
	return chart, nil
}

// buildSelector 把 pointKey 变成 PromQL 序列选择器。
func buildSelector(storageType c_base.StorageType, id, key string) string {
	if strings.Contains(key, "{") {
		return key // 调用方给的就是完整选择器
	}
	return fmt.Sprintf(`%s{type=%q,id=%q,field=%q}`, MetricNameEmsMetric, string(storageType), id, key)
}

type point struct {
	TS    int64
	Value float64
}

// queryRange 打 vmselect 的 /api/v1/query_range，返回原始样本。
func (v *vmStorage) queryRange(selector string, startMs, endMs int64, stepSec int) ([]point, error) {
	if v.selectURL == "" {
		return nil, errors.New("VictoriaMetrics 查询地址为空")
	}
	q := url.Values{}
	q.Set("query", selector)
	q.Set("start", strconv.FormatInt(startMs/1000, 10))
	q.Set("end", strconv.FormatInt(endMs/1000, 10))
	q.Set("step", strconv.Itoa(stepSec))

	req, err := http.NewRequest(http.MethodGet, v.selectURL+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return nil, errors.Wrap(err, "构造查询请求失败")
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "查询 VictoriaMetrics 失败")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "读取查询响应失败")
	}
	if resp.StatusCode/100 != 2 {
		return nil, errors.Errorf("查询返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseQueryRange(body)
}

type queryRangeResp struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][2]json.RawMessage `json:"values"`
			Value  []json.RawMessage    `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// parseQueryRange 解析 query_range 响应（matrix 为主，单点 vector 也兼容）。
func parseQueryRange(body []byte) ([]point, error) {
	var r queryRangeResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, errors.Wrap(err, "解析查询响应失败")
	}
	if r.Status != "success" {
		return nil, errors.Errorf("VictoriaMetrics 查询失败: %s", r.Error)
	}
	out := make([]point, 0, len(r.Data.Result))
	appendSample := func(raw [2]json.RawMessage) error {
		var sec float64
		if err := json.Unmarshal(raw[0], &sec); err != nil {
			return err
		}
		var valStr string
		if err := json.Unmarshal(raw[1], &valStr); err != nil {
			return err
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return err
		}
		out = append(out, point{TS: int64(sec * 1000), Value: val})
		return nil
	}
	for _, s := range r.Data.Result {
		if len(s.Value) == 2 {
			if err := appendSample([2]json.RawMessage{s.Value[0], s.Value[1]}); err != nil {
				return nil, errors.Wrap(err, "解析查询样本失败")
			}
			continue
		}
		for _, vals := range s.Values {
			if len(vals) != 2 {
				continue
			}
			if err := appendSample(vals); err != nil {
				return nil, errors.Wrap(err, "解析查询样本失败")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, nil
}

// ---------------------------------------------------------------- 统计与清理

// GetStorageStats 从 vmselect 的 status/tsdb 取序列数；取不到就返回零值而不是报错，
// 免得「存储管理」页因为统计不可用而整页失败。
func (v *vmStorage) GetStorageStats() (*c_base.StorageStats, error) {
	stats := &c_base.StorageStats{}
	if v.selectURL == "" {
		return stats, nil
	}
	resp, err := v.client.Get(v.selectURL + "/api/v1/status/tsdb")
	if err != nil {
		return stats, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return stats, nil
	}
	var r struct {
		Data struct {
			TotalSeries int64 `json:"totalSeries"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return stats, nil
	}
	stats.TotalSeries = r.Data.TotalSeries
	return stats, nil
}

// ClearDeviceHistoryAll 用 vminsert 的 /delete_series 删掉某设备的全部序列。
func (v *vmStorage) ClearDeviceHistoryAll(deviceId string) error {
	if deviceId == "" {
		return errors.New("设备ID不能为空")
	}
	if v.baseURL == "" {
		return errors.New("VictoriaMetrics 删除地址为空")
	}
	selector := fmt.Sprintf(`%s{type=%q,id=%q}`, MetricNameEmsMetric, string(c_base.StorageTypeDevice), deviceId)
	q := url.Values{}
	q.Set("match[]", selector)
	req, err := http.NewRequest(http.MethodPost, v.baseURL+"/delete_series?"+q.Encode(), nil)
	if err != nil {
		return errors.Wrap(err, "构造删除请求失败")
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "删除 VictoriaMetrics 序列失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.Errorf("删除序列返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// DeleteBefore VictoriaMetrics 的保留期由服务端 --retentionPeriod 决定，客户端不删数据。
func (v *vmStorage) DeleteBefore(before time.Time, types ...c_base.StorageType) error {
	return nil
}

func (v *vmStorage) Close() {}
