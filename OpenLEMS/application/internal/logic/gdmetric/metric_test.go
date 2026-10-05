package gdmetric

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleCSV = "\ufeff时间戳,日期,时间,typeId,类型,单位,数值,sourceId\n" +
	"2026-08-30 00:15:00,2026-08-30,00:15,21,日前电价,元/兆瓦时,100,9\n" +
	"2026-08-30 00:15:00,2026-08-30,00:15,22,实时电价,元/兆瓦时,80,9\n" +
	"2026-08-30 00:15:00,2026-08-30,00:15,23,负荷,MW,1000,9\n" +
	"2026-08-30 00:30:00,2026-08-30,00:30,21,日前电价,元/兆瓦时,200,9\n" +
	"2026-08-30 00:30:00,2026-08-30,00:30,22,实时电价,元/兆瓦时,120,9\n" +
	"2026-08-30 00:30:00,2026-08-30,00:30,23,负荷,MW,1500,9\n" +
	"2026-08-31 24:00:00,2026-08-31,24:00,21,日前电价,元/兆瓦时,50,9\n" +
	"2026-08-31 24:00:00,2026-08-31,24:00,22,实时电价,元/兆瓦时,40,9\n" +
	"2026-08-31 24:00:00,2026-08-31,24:00,23,负荷,MW,800,9\n"

func TestIngestDailyStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gd_metric.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	days, offsets, err := ingestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("days=%d", len(days))
	}
	d0 := days[0]
	if d0.Date != "2026-08-30" {
		t.Fatalf("date=%s", d0.Date)
	}
	if d0.Load.Count != 2 || d0.Load.Avg != 1250 || d0.Load.Max != 1500 || d0.Load.Min != 1000 {
		t.Fatalf("load=%+v", d0.Load)
	}
	if d0.DayAhead.Avg != 150 || d0.Realtime.Avg != 100 {
		t.Fatalf("price da=%+v rt=%+v", d0.DayAhead, d0.Realtime)
	}
	if d0.Load.Unit != "MW" || d0.DayAhead.Unit != "元/兆瓦时" {
		t.Fatalf("units load=%s da=%s", d0.Load.Unit, d0.DayAhead.Unit)
	}
	if days[1].Date != "2026-08-31" || days[1].Load.Last != 800 {
		t.Fatalf("24:00 should stay on 08-31: %+v", days[1])
	}
	curve, err := readDayCurve(path, offsets["2026-08-30"], "2026-08-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(curve.Load) != 2 || curve.Load[0].Time != "00:15" || curve.Load[1].Value != 1500 {
		t.Fatalf("curve=%+v", curve.Load)
	}
}

func TestClampRangeDefaultLast30(t *testing.T) {
	from, to := clampRange("", "", "2021-05-01", "2026-08-31")
	if to != "2026-08-31" {
		t.Fatalf("to=%s", to)
	}
	if from != "2026-08-02" {
		t.Fatalf("from=%s", from)
	}
}

func TestClampRangeSwapAndCap(t *testing.T) {
	from, to := clampRange("2026-08-31", "2026-08-01", "2021-05-01", "2026-08-31")
	if from != "2026-08-01" || to != "2026-08-31" {
		t.Fatalf("swapped %s %s", from, to)
	}
	from, to = clampRange("2020-01-01", "2026-08-31", "2021-05-01", "2026-08-31")
	if to != "2026-08-31" || from != addDays("2026-08-31", -(maxRangeDays-1)) {
		t.Fatalf("capped %s %s", from, to)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":       defaultMaxSize,
		"32MB":   32 << 20,
		"32":     32 << 20,
		"1GB":    1 << 30,
		"4096":   4096,
		"2.5MB":  int64(2.5 * float64(1<<20)),
		"1024KB": 1024 << 10,
	}
	for raw, want := range cases {
		if got := parseSize(raw); got != want {
			t.Fatalf("parseSize(%q)=%d want %d", raw, got, want)
		}
	}
}

func TestCurveLRUEvictsOldest(t *testing.T) {
	lru := newCurveLRU(700)
	for i, date := range []string{"2026-08-01", "2026-08-02", "2026-08-03"} {
		samples := make([]Sample, 4)
		for j := range samples {
			samples[j] = Sample{Time: "00:15", Value: float64(i + j)}
		}
		lru.Put(date, DayCurve{Date: date, Load: samples})
	}
	if lru.Len() != 2 {
		t.Fatalf("len=%d", lru.Len())
	}
	if lru.Used() > lru.max {
		t.Fatalf("used=%d max=%d", lru.Used(), lru.max)
	}
	if _, ok := lru.Get("2026-08-01"); ok {
		t.Fatal("oldest day should be evicted")
	}
	if _, ok := lru.Get("2026-08-03"); !ok {
		t.Fatal("latest day should stay")
	}
}

func TestIngestFileAndReloadDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gd_metric.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	days, offsets, err := ingestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("days=%d", len(days))
	}
	off, ok := offsets["2026-08-30"]
	if !ok {
		t.Fatal("missing offset")
	}
	curve, err := readDayCurve(path, off, "2026-08-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(curve.Load) != 2 || curve.Load[1].Value != 1500 {
		t.Fatalf("curve=%+v", curve.Load)
	}
	s := &store{
		cachePath: path,
		metaPath:  metaPathFor(path),
		maxSize:   200,
		days:      days,
		offsets:   offsets,
		curves:    newCurveLRU(200),
	}
	got, err := s.getCurve("2026-08-31")
	if err != nil || got == nil || len(got.Load) == 0 || got.Load[0].Value != 800 {
		t.Fatalf("reload %+v err=%v", got, err)
	}
	if _, ok := s.curves.Get("2026-08-31"); !ok {
		t.Fatal("reloaded day should be cached")
	}
}

func TestSnapshotAvailableDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gd_metric.csv")
	if err := os.WriteFile(path, []byte(sampleCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	days, offsets, err := ingestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &store{
		url:       "http://example.test/gd_metric.csv",
		cachePath: path,
		maxSize:   1 << 20,
		days:      days,
		offsets:   offsets,
		curves:    newCurveLRU(1 << 20),
	}
	snap, err := s.snapshot("2026-08-01", "2026-08-31", "2026-08-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.AvailableDays) != 2 || snap.AvailableDays[0] != "2026-08-30" || snap.AvailableDays[1] != "2026-08-31" {
		t.Fatalf("availableDays=%v", snap.AvailableDays)
	}
}
