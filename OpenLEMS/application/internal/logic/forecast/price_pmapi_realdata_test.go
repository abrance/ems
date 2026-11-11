package forecast

import (
	"context"
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"s_db/s_db_model"
	"s_forecast"
)

// 官方接口直采数据的 7 天日前电价预测。
//
// 为什么单独一条：日频序列的取数口径换过源（OCR 日报 -> pm-api 直采），指标名也从
// 「平均电价」变成源站原名。这条用例用抓下来的官方长表（tools/gdpm 的产物）跑完整
// 预测，守住「名字对不上 -> 序列变空」那个静默失效。
//
// 需要两个外部条件，缺一就跳过（CI 上跳过）：
//
//	GDPM_CSV            data/gdpm/gdpm_long.csv
//	FORECAST_BASE_URL   可达的 TimesFM 服务地址
func TestLoadPricePMApiForecastSevenDays(t *testing.T) {
	csvPath := os.Getenv("GDPM_CSV")
	baseURL := os.Getenv("FORECAST_BASE_URL")
	if csvPath == "" || baseURL == "" {
		t.Skip("需要 GDPM_CSV 与 FORECAST_BASE_URL，跳过")
	}
	source := csvSource(t, csvPath)

	spec, err := resolvePriceSpec("ahead")
	if err != nil {
		t.Fatalf("resolvePriceSpec: %v", err)
	}
	client := s_forecast.NewClient(baseURL, forecastTimeout)
	res, err := loadPriceWith(context.Background(), client, source, spec,
		60, defaultPriceHorizonDays, nil, priceLocation(), time.Now())
	if err != nil {
		t.Fatalf("官方数据 7 天预测失败: %v", err)
	}
	if len(res.ForecastDates) != 7 {
		t.Fatalf("预测天数 = %d, want 7", len(res.ForecastDates))
	}
	for i, d := range res.ForecastDates {
		t.Logf("%s  %.2f %s", d, res.ForecastPoint[i], res.Unit)
	}
	if res.Unit != "元/兆瓦时" && res.Unit != "元/MWh" {
		t.Errorf("单位异常: %q", res.Unit)
	}
}

// csvSource 用官方长表 CSV 顶替 DB 读取（字段与 gd_spot_metric 一一对应）。
func csvSource(t *testing.T, path string) priceSeriesSource {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", path, err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	models := make([]*s_db_model.SSpotMetricModel, 0, len(rows))
	for i, r := range rows {
		if i == 0 || len(r) < 6 {
			continue
		}
		v, err := strconv.ParseFloat(r[4], 64)
		if err != nil {
			continue
		}
		models = append(models, &s_db_model.SSpotMetricModel{
			RunDate: r[0], ReportDate: r[0], Dataset: r[1], Scope: r[2], Metric: r[3],
			Value: v, Unit: r[5], Source: "gdpm-api", Origin: "pm-api", Conf: 1,
		})
	}
	if len(models) == 0 {
		t.Fatalf("%s 没有可用行", path)
	}
	return func(_ context.Context, from, to string) ([]*s_db_model.SSpotMetricModel, error) {
		out := make([]*s_db_model.SSpotMetricModel, 0, len(models))
		for _, m := range models {
			if m.RunDate >= from && m.RunDate <= to {
				out = append(out, m)
			}
		}
		return out, nil
	}
}
