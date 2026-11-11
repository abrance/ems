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

// 查询解析：matrix 响应 -> ChartData，多序列按时间戳对齐，缺值留空。
func TestGetStorageDataParsesQueryRange(t *testing.T) {
	const matrix = `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"__name__":"gdpm_price_cny_mwh","market":"dayahead"},"values":[[1762704000,"334.569613"],[1762790400,"341.2"]]}
	]}}`

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/0/prometheus/api/v1/query_range" {
			t.Errorf("查询路径 = %q", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(matrix))
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

	if gotQuery != `gdpm_price_cny_mwh{market="dayahead",side="gen",kind="weighted_avg"}` {
		t.Errorf("传给 vmselect 的 query = %q（完整选择器应原样下发）", gotQuery)
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

// 单点 vector 响应也要能解析（query_range 对某些步长会返回 vector）。
func TestParseQueryRangeVector(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"__name__":"x"},"value":[1762704000,"7.5"]}]}}`
	pts, err := parseQueryRange([]byte(body))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(pts) != 1 || pts[0].TS != 1762704000000 || pts[0].Value != 7.5 {
		t.Fatalf("解析结果不对: %+v", pts)
	}
}

// 查询报错要带出上游原因，不能静默返回空图。
func TestParseQueryRangeError(t *testing.T) {
	body := `{"status":"error","error":"bad_data: invalid parameter"}`
	if _, err := parseQueryRange([]byte(body)); err == nil {
		t.Fatal("期望返回错误")
	}
}
