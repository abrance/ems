package internal

import (
	"common/c_base"
	"common/c_chart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 集成测试：真打 VictoriaMetrics（vminsert + vmselect）。
// 默认跳过，给了地址才跑：
//
//	EMS_VM_IT=1 \
//	EMS_VM_INSERT_URL=http://127.0.0.1:18480/insert/0/prometheus/api/v1/import/prometheus \
//	EMS_VM_SELECT_URL=http://127.0.0.1:18481/select/0/prometheus \
//	go test ./internal -run TestVMIntegration -v
func TestVMIntegration(t *testing.T) {
	if os.Getenv("EMS_VM_IT") != "1" {
		t.Skip("未设置 EMS_VM_IT=1，跳过 VictoriaMetrics 集成测试")
	}
	insertURL := os.Getenv("EMS_VM_INSERT_URL")
	selectURL := os.Getenv("EMS_VM_SELECT_URL")
	if insertURL == "" || selectURL == "" {
		t.Fatal("需要 EMS_VM_INSERT_URL 与 EMS_VM_SELECT_URL")
	}

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Type:   "vm",
		Params: map[string]string{ParamInsertURL: insertURL, ParamSelectURL: selectURL},
	})

	// 1) 插件写入路径：带 tags 的原生命名，时间戳是当前时间
	const field = "gdpm_it_probe_cny_mwh"
	if err := s.SaveSystemMetrics("gdpm_spot", map[string]string{
		"market": "dayahead", "side": "gen", "kind": "weighted_avg", "src": "official",
	}, map[string]any{field: 123.456}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// export 不加 rollup，点时间戳是真实样本时间戳；不过 vminsert→vmstorage→vmselect
	// 有可见性延迟（实测 1s 内），所以轮询等样本出现，而不是假设写完立刻能查到。
	now := time.Now().UnixMilli()
	start, end := now-5*60*1000, now+5*60*1000
	chart := pollChart(t, s, []string{field + `{market="dayahead",side="gen",kind="weighted_avg"}`}, start, end, 60000, 1)
	if got := chart.Series[0].Data[len(chart.Series[0].Data)-1]; got != "123.456" {
		t.Errorf("回读值 = %q, 期望 123.456", got)
	}

	// 2) 按日期取外部采集的日频指标（ETL 已写入的 2026-11-10）
	dayStart, dayEnd := mustMs(t, "2026-11-09T00:00:00+08:00"), mustMs(t, "2026-11-11T23:59:59+08:00")
	daily, err := s.GetStorageData(c_base.StorageTypeSystem, "gdpm_spot", []string{
		`gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}`,
		`gdpm_volume_mwh{market="dayahead",side="gen",kind="total"}`,
	}, &dayStart, &dayEnd, 86400000)
	if err != nil {
		t.Fatalf("按日期查询失败: %v", err)
	}
	if len(daily.XAxis.Data) == 0 {
		t.Fatalf("按日期查询没有数据点")
	}
	// 2026-11-10 00:00:00+08:00 = 1794240000000
	if got := valueAt(daily, 0, 1794240000000); got != "334.569613" {
		t.Errorf("2026-11-10 日前发电侧加权均价 = %q, 期望 334.569613（点: %v）", got, daily.XAxis.Data)
	}
	if got := valueAt(daily, 1, 1794240000000); got != "1540786.335" {
		t.Errorf("2026-11-10 日前发电侧总成交电量 = %q, 期望 1540786.335", got)
	}
}

// 回归（真打 VM）：两天有数据、中间那天空着，查中间那天必须返回空。
// 旧读路径 query_range + step=86400 会把前一天的值冒充成当天，这条测试就是为了钉住它。
func TestVMIntegrationGapDayStaysEmpty(t *testing.T) {
	if os.Getenv("EMS_VM_IT") != "1" {
		t.Skip("未设置 EMS_VM_IT=1，跳过 VictoriaMetrics 集成测试")
	}
	insertURL := os.Getenv("EMS_VM_INSERT_URL")
	selectURL := os.Getenv("EMS_VM_SELECT_URL")
	if insertURL == "" || selectURL == "" {
		t.Fatal("需要 EMS_VM_INSERT_URL 与 EMS_VM_SELECT_URL")
	}

	// 用独立的探针指标名，不碰 gdpm_* 真实序列；不依赖 retention 窗口外的历史日期。
	const probe = "gdpm_it_gap_probe_cny_mwh"
	labels := map[string]string{"it": "gap", "src": "official"}
	day := func(offsetDays int) int64 {
		n := time.Now().UTC().AddDate(0, 0, offsetDays)
		tm := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
		return tm.UnixMilli()
	}
	// 前两天、今天各写一个样本，昨天故意空着
	hasData, gap, dayBefore := day(-2), day(-1), day(-3)

	// 写入需要指定时间戳，插件的 SaveSystemMetrics 用 now，所以直接用同一套 payload 格式写。
	body := formatSamples([]sample{
		{Name: probe, Labels: labels, Value: 111.5, TS: hasData},
		{Name: probe, Labels: labels, Value: 222.5, TS: dayBefore},
	})
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, insertURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造写入请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("写入返回 %d", resp.StatusCode)
	}

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Type:   "vm",
		Params: map[string]string{ParamInsertURL: insertURL, ParamSelectURL: selectURL},
	})
	selector := probe + `{it="gap"}`

	// 有数据的那天：能查回真值（轮询等可见性）
	start, end := hasData, hasData+24*3600*1000-1
	chart := pollChart(t, s, []string{selector}, start, end, 86400000, 1)
	if got := valueAt(chart, 0, hasData); got != "111.5" {
		t.Fatalf("%s 的值 = %q, 期望 111.5（点: %v）", time.UnixMilli(hasData), got, chart.XAxis.Data)
	}

	// 空着的那天：必须是空，不能是前一天的值（查两次，排除可见性抖动）
	start, end = gap, gap+24*3600*1000-1
	for i := 0; i < 2; i++ {
		chart, err := s.GetStorageData(c_base.StorageTypeSystem, "gdpm_spot", []string{selector}, &start, &end, 86400000)
		if err != nil {
			t.Fatalf("查空日失败: %v", err)
		}
		if len(chart.XAxis.Data) != 0 {
			t.Fatalf("空日返回了 %d 个点: %v（值 %v）", len(chart.XAxis.Data), chart.XAxis.Data, seriesData(chart))
		}
		for _, se := range chart.Series {
			if len(se.Data) != 0 {
				t.Fatalf("空日序列 %s 带回了值 %v（就是 lookbehind 把前一天冒充成当天）", se.Name, se.Data)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// pollChart 轮询取数直到拿到至少 min 个时间点（VM 写入有可见性延迟），超时才失败。
// 用「至少」而不是「等于」：同一探针序列会被重跑写到不同时间戳，窗口内点数会累积。
func pollChart(t *testing.T, s c_base.IStorage, keys []string, start, end int64, step int, min int) *c_chart.ChartData {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last *c_chart.ChartData
	for time.Now().Before(deadline) {
		chart, err := s.GetStorageData(c_base.StorageTypeSystem, "gdpm_spot", keys, &start, &end, step)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		last = chart
		if len(chart.XAxis.Data) >= min {
			return chart
		}
		time.Sleep(300 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("查询始终没有返回结果")
	}
	t.Fatalf("等了 15s 仍只有 %d 个时间点, 期望至少 %d: %v", len(last.XAxis.Data), min, last.XAxis.Data)
	return last
}

// seriesData 把各序列的值摊平，只用于失败时的可读输出。
func seriesData(chart *c_chart.ChartData) []string {
	out := make([]string, 0, len(chart.Series))
	for _, s := range chart.Series {
		out = append(out, s.Data...)
	}
	return out
}

func mustMs(t *testing.T, s string) int64 {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("时间解析失败: %v", err)
	}
	return tm.UnixMilli()
}

// valueAt 取某个时间戳上第 i 条序列的值（没这个点就返回空串）。
func valueAt(chart *c_chart.ChartData, i int, ts int64) string {
	want := strconv.FormatInt(ts, 10)
	for idx, x := range chart.XAxis.Data {
		if x == want && i < len(chart.Series) && idx < len(chart.Series[i].Data) {
			return chart.Series[i].Data[idx]
		}
	}
	return ""
}
