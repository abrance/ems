package forecast

import (
	"fmt"
	"math"
	"testing"
	"time"

	"s_db/s_db_model"

	"github.com/gogf/gf/v2/os/gtime"
)

func hourRowFor(t *testing.T, algo, generatedOn, date string, hour int, point, q10, q90 float64) *s_db_model.SPriceForecastModel {
	t.Helper()
	return &s_db_model.SPriceForecastModel{
		GeneratedOn: generatedOn,
		Market:      "ahead",
		Grain:       s_db_model.ForecastGrainHour,
		Algo:        algo,
		TargetTime:  fmt.Sprintf("%s %02d:00", date, hour),
		Point:       point,
		Q10:         q10,
		Q50:         point,
		Q90:         q90,
		Unit:        "元/兆瓦时",
	}
}

func fullDay(t *testing.T, algo, generatedOn, date string, base, offset float64) []*s_db_model.SPriceForecastModel {
	t.Helper()
	rows := make([]*s_db_model.SPriceForecastModel, 0, 24)
	for h := 0; h < 24; h++ {
		p := base + float64(h)
		rows = append(rows, hourRowFor(t, algo, generatedOn, date, h, p+offset, p-10, p+10))
	}
	return rows
}

func TestBuildAlgoReviewBasic(t *testing.T) {
	rows := make([]*s_db_model.SPriceForecastModel, 0, 96)
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoTimesFM, "2026-11-09", "2026-11-10", 100, 0)...)
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoGBM, "2026-11-09", "2026-11-10", 100, 10)...)
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoTimesFM, "2026-11-10", "2026-11-11", 200, 0)...)
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoGBM, "2026-11-10", "2026-11-11", 200, 10)...)

	actual := map[string]map[int]float64{
		"2026-11-10": {},
		"2026-11-11": {},
	}
	for h := 0; h < 24; h++ {
		actual["2026-11-10"][h] = 100 + float64(h) + 5
		actual["2026-11-11"][h] = 200 + float64(h) + 5
	}

	algos, err := ParseAlgos("")
	if err != nil {
		t.Fatal(err)
	}
	got := BuildAlgoReview(rows, actual, "2026-11-10", "2026-11-11", "元/兆瓦时", algos, "")

	if len(got.Summary) != 3 {
		t.Fatalf("summary 数量 = %d, want 3", len(got.Summary))
	}
	byAlgo := map[string]*SAlgoReviewSummary{}
	for _, s := range got.Summary {
		byAlgo[s.Algo] = s
	}

	tm := byAlgo[s_db_model.ForecastAlgoTimesFM]
	if tm == nil || tm.Days != 2 || tm.Pairs != 48 {
		t.Fatalf("timesfm days/pairs = %+v", tm)
	}
	if math.Abs(tm.MAE-5) > 1e-9 {
		t.Errorf("timesfm MAE = %v, want 5", tm.MAE)
	}
	if tm.Coverage == nil || math.Abs(*tm.Coverage-100) > 1e-9 {
		t.Errorf("timesfm coverage = %v, want 100", tm.Coverage)
	}

	gbm := byAlgo[s_db_model.ForecastAlgoGBM]
	if gbm == nil || gbm.Pairs != 48 || math.Abs(gbm.MAE-5) > 1e-9 {
		t.Fatalf("gbm = %+v", gbm)
	}

	fusion := byAlgo[AlgoFusion]
	if fusion == nil || fusion.Days != 2 || fusion.Pairs != 48 {
		t.Fatalf("fusion = %+v", fusion)
	}
	if math.Abs(fusion.MAE) > 1e-9 {
		t.Errorf("fusion MAE = %v, want 0（两者误差 +5/-5 抵消）", fusion.MAE)
	}
	if fusion.Coverage != nil {
		t.Errorf("fusion coverage = %v, want nil（融合没有分位带）", *fusion.Coverage)
	}

	if len(got.Daily) != 6 { // 3 算法 × 2 天
		t.Errorf("daily 行数 = %d, want 6", len(got.Daily))
	}
	if len(got.Hourly) != 3*24 {
		t.Errorf("hourly 行数 = %d, want %d", len(got.Hourly), 3*24)
	}
	if got.Dropped.NoActual != 0 || got.Dropped.Backfilled != 0 || got.Dropped.IncompleteDay != 0 {
		t.Errorf("dropped = %+v, want 全 0", got.Dropped)
	}
}

func TestBuildAlgoReviewMAPEGuardAndMissingActual(t *testing.T) {
	// 一天 24 点：实际值 0.5（低于 MAPE 阈值），且第 23 点缺实际值。
	rows := fullDay(t, s_db_model.ForecastAlgoTimesFM, "2026-11-09", "2026-11-10", 100, 1)
	actual := map[string]map[int]float64{"2026-11-10": {}}
	for h := 0; h < 23; h++ {
		actual["2026-11-10"][h] = 0.5
	}

	got := BuildAlgoReview(rows, actual, "2026-11-10", "2026-11-10", "元/兆瓦时",
		[]string{s_db_model.ForecastAlgoTimesFM}, "")

	s := got.Summary[0]
	if s.Pairs != 23 {
		t.Fatalf("pairs = %d, want 23", s.Pairs)
	}
	if s.MAPE != 0 || s.MAPEExcluded != 23 {
		t.Errorf("mape/mapeExcluded = %v/%d, want 0/23", s.MAPE, s.MAPEExcluded)
	}
	if got.Dropped.NoActual != 1 {
		t.Errorf("dropped.noActual = %d, want 1", got.Dropped.NoActual)
	}
}

func TestBuildAlgoReviewBackfilledAndIncompleteDay(t *testing.T) {
	rows := make([]*s_db_model.SPriceForecastModel, 0, 50)
	// 回填行：生成日晚于目标日，必须剔除。
	rows = append(rows, hourRowFor(t, s_db_model.ForecastAlgoTimesFM, "2026-11-20", "2026-11-10", 0, 100, 90, 110))
	// 完整一天（24 点）。
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoTimesFM, "2026-11-09", "2026-11-11", 200, 0)...)
	// 不足 24 点的一天：整日弃用。
	rows = append(rows, fullDay(t, s_db_model.ForecastAlgoTimesFM, "2026-11-10", "2026-11-12", 300, 0)[:23]...)

	actual := map[string]map[int]float64{"2026-11-11": {}, "2026-11-12": {}}
	for h := 0; h < 24; h++ {
		actual["2026-11-11"][h] = 200 + float64(h)
		actual["2026-11-12"][h] = 300 + float64(h)
	}

	got := BuildAlgoReview(rows, actual, "2026-11-10", "2026-11-12", "元/兆瓦时",
		[]string{s_db_model.ForecastAlgoTimesFM}, "")

	if got.Dropped.Backfilled != 1 {
		t.Errorf("dropped.backfilled = %d, want 1", got.Dropped.Backfilled)
	}
	if got.Dropped.IncompleteDay != 1 {
		t.Errorf("dropped.incompleteDay = %d, want 1", got.Dropped.IncompleteDay)
	}
	if got.Summary[0].Days != 1 || got.Summary[0].Pairs != 24 {
		t.Errorf("summary = %+v, want 1 天 / 24 点", got.Summary[0])
	}
}

func TestBuildAlgoReviewUsesLatestRunPerPoint(t *testing.T) {
	// 同一天同一目标时刻跑两次（锚点或数据变化会换 generated_on/updated_at）：
	// 页面只应展示最近一次运行的结果。
	loc := priceLocation()
	oldAt := gtime.NewFromTime(time.Date(2026, 11, 10, 8, 0, 0, 0, loc))
	newAt := gtime.NewFromTime(time.Date(2026, 11, 10, 9, 0, 0, 0, loc))

	rows := make([]*s_db_model.SPriceForecastModel, 0, 48)
	for h := 0; h < 24; h++ {
		oldRow := hourRowFor(t, s_db_model.ForecastAlgoTimesFM, "2026-11-09", "2026-11-10", h, 100, 90, 110)
		oldRow.UpdatedAt = oldAt
		newRow := hourRowFor(t, s_db_model.ForecastAlgoTimesFM, "2026-11-09", "2026-11-10", h, 130, 120, 140)
		newRow.UpdatedAt = newAt
		rows = append(rows, oldRow, newRow)
	}
	actual := map[string]map[int]float64{"2026-11-10": {}}
	for h := 0; h < 24; h++ {
		actual["2026-11-10"][h] = 110
	}

	got := BuildAlgoReview(rows, actual, "2026-11-10", "2026-11-10", "元/兆瓦时",
		[]string{s_db_model.ForecastAlgoTimesFM}, "")

	if got.Summary[0].Pairs != 24 {
		t.Fatalf("pairs = %d, want 24（重复运行不能重复计数）", got.Summary[0].Pairs)
	}
	// 旧结果误差 10，新结果误差 20；取后一次 → MAE 应为 20。
	if math.Abs(got.Summary[0].MAE-20) > 1e-9 {
		t.Errorf("MAE = %v, want 20（应取 updated_at 更晚的那次）", got.Summary[0].MAE)
	}
}

func TestParseAlgos(t *testing.T) {
	all, err := ParseAlgos("")
	if err != nil || len(all) != 3 || all[0] != s_db_model.ForecastAlgoTimesFM {
		t.Fatalf("默认算法 = %v, err=%v", all, err)
	}
	got, err := ParseAlgos("FUSION, gbm,timesfm,gbm")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{s_db_model.ForecastAlgoTimesFM, s_db_model.ForecastAlgoGBM, AlgoFusion}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ParseAlgos = %v, want %v（去重且固定顺序）", got, want)
	}
	if _, err := ParseAlgos("lstm"); err == nil {
		t.Error("未知算法应当报错")
	}
}
