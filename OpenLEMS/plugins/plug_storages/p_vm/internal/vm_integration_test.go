package internal

import (
	"common/c_base"
	"common/c_chart"
	"os"
	"strconv"
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
		"market": "dayahead", "side": "gen", "kind": "weighted_avg", "src": "pm.gd.csg.cn",
	}, map[string]any{field: 123.456}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	// query_range 只按 start+k*step 的网格返回点（VM 的行为），所以网格要跨过刚写的时间戳
	now := time.Now().UnixMilli()
	start, end := now-5*60*1000, now+5*60*1000
	chart, err := s.GetStorageData(c_base.StorageTypeSystem, "gdpm_spot",
		[]string{field + `{market="dayahead",side="gen",kind="weighted_avg"}`}, &start, &end, 60000)
	if err != nil {
		t.Fatalf("写后回读失败: %v", err)
	}
	if len(chart.Series) != 1 || len(chart.Series[0].Data) == 0 {
		t.Fatalf("写后回读没有数据: %+v", chart)
	}
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
