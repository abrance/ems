package internal

import (
	"common/c_base"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 带 tags 的 SaveSystemMetrics 走「字段名即指标名 + tags 原样做标签」，
// 这是 gdpm 这类外部指标的建模方式（不把指标名塞进 label）。
func TestSaveSystemMetricsNativeNaming(t *testing.T) {
	var body string
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Enable: true,
		Type:   "vm",
		Params: map[string]string{ParamInsertURL: srv.URL + DefaultInsertPath},
	})

	err := s.SaveSystemMetrics("gdpm_spot", map[string]string{
		"market": "dayahead",
		"side":   "gen",
		"kind":   "weighted_avg",
		"src":    "pm.gd.csg.cn",
	}, map[string]any{"gdpm_price_cny_mwh": 334.569613})
	if err != nil {
		t.Fatalf("SaveSystemMetrics 失败: %v", err)
	}

	if path != DefaultInsertPath {
		t.Errorf("写入路径 = %q, 期望 %q", path, DefaultInsertPath)
	}
	want := `gdpm_price_cny_mwh{kind="weighted_avg",market="dayahead",side="gen",src="pm.gd.csg.cn"} 334.569613 `
	if !strings.HasPrefix(body, want) {
		t.Fatalf("payload 前缀不对:\n got: %q\nwant: %q", body, want)
	}
	if strings.Contains(body, MetricNameEmsMetric) {
		t.Errorf("带 tags 时不该退化成 %s: %q", MetricNameEmsMetric, body)
	}
}

// 不带 tags 时保持 p_tsdb 的 ems_metric{type,id,field} 方案，已有图表不用改。
func TestSaveSystemMetricsCompatNaming(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Params: map[string]string{ParamInsertURL: srv.URL + DefaultInsertPath},
	})
	if err := s.SaveSystemMetrics("cpu", nil, map[string]any{"usage": 12.5}); err != nil {
		t.Fatalf("SaveSystemMetrics 失败: %v", err)
	}
	want := `ems_metric{field="usage",id="cpu",type="system"} 12.5 `
	if !strings.HasPrefix(body, want) {
		t.Fatalf("payload 前缀不对:\n got: %q\nwant: %q", body, want)
	}
}

// 查询解析：export 的 NDJSON -> ChartData，点的时间戳是真实样本时间戳，不是 start+k*step 网格。
func TestGetStorageDataParsesExport(t *testing.T) {
	const exported = `{"metric":{"__name__":"gdpm_price_cny_mwh","market":"dayahead"},"values":[334.569613,341.2],"timestamps":[1762704000000,1762790400000]}` + "\n"

	var gotMatch, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMatch = r.URL.Query().Get("match[]")
		_, _ = w.Write([]byte(exported))
	}))
	defer srv.Close()

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Params: map[string]string{ParamSelectURL: srv.URL + DefaultSelectPath},
	})

	start, end := int64(1762704000000), int64(1762876800000)
	chart, err := s.GetStorageData(
		c_base.StorageTypeSystem,
		"gdpm_spot",
		[]string{`gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}`},
		&start,
		&end,
		86400000,
	)
	if err != nil {
		t.Fatalf("GetStorageData 失败: %v", err)
	}

	if gotPath != DefaultSelectPath+DefaultExportPath {
		t.Errorf("查询路径 = %q, 期望 %q", gotPath, DefaultSelectPath+DefaultExportPath)
	}
	if gotMatch != `gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}` {
		t.Errorf("传给 vmselect 的 match[] = %q（完整选择器应原样下发）", gotMatch)
	}
	if len(chart.XAxis.Data) != 2 {
		t.Fatalf("时间点数量 = %d, 期望 2: %+v", len(chart.XAxis.Data), chart.XAxis.Data)
	}
	if chart.XAxis.Data[0] != "1762704000000" {
		t.Errorf("第一个时间戳 = %q", chart.XAxis.Data[0])
	}
	if len(chart.Series) != 1 || chart.Series[0].Name == "" {
		t.Fatalf("系列不对: %+v", chart.Series)
	}
	if got := chart.Series[0].Data[0]; got != "334.569613" {
		t.Errorf("首值 = %q, 期望 334.569613（不能丢精度）", got)
	}
	if got := chart.Series[0].Data[1]; got != "341.2" {
		t.Errorf("次值 = %q", got)
	}
}

// 回归：区间内没有样本必须返回空，不能把邻近日期的值带回来。
// 这是旧读路径（query_range + step=86400 的 lookbehind）会把前一天变成当天的触发点。
func TestGetStorageDataEmptyRangeStaysEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // export 无样本 = 空 body
	}))
	defer srv.Close()

	s := NewVMStorage(t.Context(), &c_base.SStorageConfig{
		Params: map[string]string{ParamSelectURL: srv.URL + DefaultSelectPath},
	})

	// 只查 2026-11-11 这一天（该天没有样本）
	start, end := int64(1794326400000), int64(1794412799000)
	chart, err := s.GetStorageData(c_base.StorageTypeSystem, "gdpm_spot",
		[]string{`gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}`},
		&start, &end, 86400000)
	if err != nil {
		t.Fatalf("GetStorageData 失败: %v", err)
	}
	if len(chart.XAxis.Data) != 0 {
		t.Fatalf("无样本日期返回了 %d 个点: %+v", len(chart.XAxis.Data), chart.XAxis.Data)
	}
	for _, s := range chart.Series {
		if len(s.Data) != 0 {
			t.Errorf("序列 %s 无样本却带回了值 %v", s.Name, s.Data)
		}
	}
}

// export 一行一个序列；同一序列多个样本按 step 分桶，每桶只留最新一个。
func TestParseExportAndDownsample(t *testing.T) {
	const body = `{"metric":{"__name__":"x"},"values":[1,2,3,4],"timestamps":[1794153600000,1794196800000,1794240000000,1794283200000]}` + "\n"
	pts, err := parseExport(strings.NewReader(body))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(pts) != 4 {
		t.Fatalf("原始样本 = %d, 期望 4", len(pts))
	}

	// 按 1 天分桶：4 个半天间隔的样本 -> 2 个桶，各留桶内最新
	got := downsample(pts, 1794153600000, 86400000)
	if len(got) != 2 {
		t.Fatalf("降采样后 = %d 个点, 期望 2: %+v", len(got), got)
	}
	if got[0].Value != 2 || got[1].Value != 4 {
		t.Errorf("分桶取值不对: %+v", got)
	}
	// 时间戳保留真实样本时间戳，不是桶起点
	if got[0].TS != 1794196800000 {
		t.Errorf("桶内时间戳 = %d, 期望真实样本时间戳 1794196800000", got[0].TS)
	}
}

// export 的空响应是真「无数据」，解析成空切片而不是错误。
func TestParseExportEmpty(t *testing.T) {
	pts, err := parseExport(strings.NewReader(""))
	if err != nil {
		t.Fatalf("空响应不该报错: %v", err)
	}
	if len(pts) != 0 {
		t.Fatalf("空响应解析出 %d 个点", len(pts))
	}
}

// 纯字段名走 ems_metric 兼容选择器。
func TestBuildSelectorCompat(t *testing.T) {
	got := buildSelector(c_base.StorageTypeDevice, "dev-1", "Power")
	want := `ems_metric{type="device",id="dev-1",field="Power"}`
	if got != want {
		t.Errorf("buildSelector = %q, want %q", got, want)
	}
	if got := buildSelector(c_base.StorageTypeSystem, "gdpm_spot", `gdpm_volume_mwh{side="gen"}`); got != `gdpm_volume_mwh{side="gen"}` {
		t.Errorf("完整选择器被改写: %q", got)
	}
}

// export 报错要带出上游原因，不能静默返回空图。
func TestParseExportError(t *testing.T) {
	body := `{"status":"error","error":"bad_data: invalid parameter"}` + "\n"
	if _, err := parseExport(strings.NewReader(body)); err == nil {
		t.Fatal("期望返回错误")
	}
}
