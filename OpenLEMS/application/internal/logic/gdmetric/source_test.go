package gdmetric

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceId 是第 8 列；它是「这行是不是实际值」的唯一依据，不能解析丢了。
func TestParseMetricLineSourceID(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"2026-10-01 00:15:00,2026-10-01,00:15,21,日前电价,元/兆瓦时,100.5,9", SourceExchange},
		{"2026-10-03 00:15:00,2026-10-03,00:15,21,日前电价,元/兆瓦时,307.47,999", SourceForecastPlaceholder},
		// 老文件没有这一列 → 0（未知来源），不能当 999 也不能当实际值
		{"2026-10-01 00:15:00,2026-10-01,00:15,21,日前电价,元/兆瓦时,100.5,abc", 0},
	}
	for _, c := range cases {
		_, _, _, _, value, src, ok := parseMetricLine([]byte(c.line))
		if !ok {
			t.Errorf("解析失败: %s", c.line)
			continue
		}
		if value != 100.5 && value != 307.47 {
			t.Errorf("数值错: %v", value)
		}
		if src != c.want {
			t.Errorf("sourceId = %d, want %d (%s)", src, c.want, c.line)
		}
	}
	// 少一列（老格式没有 sourceId）整行不认：宁可不读，也不要把未知来源当实际值
	if _, _, _, _, _, _, ok := parseMetricLine([]byte(
		"2026-10-01 00:15:00,2026-10-01,00:15,21,日前电价,元/兆瓦时,100.5")); ok {
		t.Error("缺 sourceId 列的行不应被接受")
	}
}

// 只有占位预测（999）的那天不是「有实际值的那天」——复盘/回测靠这个字段避开它。
func TestLatestRealDateSkipsPlaceholder(t *testing.T) {
	days := []DayRow{
		{Date: "2026-10-01", DayAhead: SeriesStat{Count: 96, SourceID: SourceExchange}},
		{Date: "2026-10-02", DayAhead: SeriesStat{Count: 92, SourceID: SourceExchange}},
		{Date: "2026-10-03", DayAhead: SeriesStat{Count: 96, SourceID: SourceForecastPlaceholder}},
		{Date: "2026-10-06", DayAhead: SeriesStat{Count: 96, SourceID: SourceForecastPlaceholder}},
	}
	if got := latestRealDate(days); got != "2026-10-02" {
		t.Errorf("latestRealDate = %s, want 2026-10-02", got)
	}
	if got := latestRealDate(nil); got != "" {
		t.Errorf("无数据应返回空串, got %s", got)
	}
	only := []DayRow{{Date: "2026-10-06", DayAhead: SeriesStat{Count: 96, SourceID: SourceForecastPlaceholder}}}
	if got := latestRealDate(only); got != "" {
		t.Errorf("全是占位应返回空串, got %s", got)
	}
}

// 真实场景：CSV 里 10-03 之后只有电查查预测占位。
const sourceCSV = "\ufeff时间戳,日期,时间,typeId,类型,单位,数值,sourceId\n" +
	"2026-10-01 00:15:00,2026-10-01,00:15,21,日前电价,元/兆瓦时,100,9\n" +
	"2026-10-01 00:30:00,2026-10-01,00:30,21,日前电价,元/兆瓦时,200,9\n" +
	"2026-10-03 00:15:00,2026-10-03,00:15,21,日前电价,元/兆瓦时,307.47,999\n" +
	"2026-10-03 00:30:00,2026-10-03,00:30,21,日前电价,元/兆瓦时,307.47,999\n"

func TestSeriesCarriesSourceAndRealOnlyDropsPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gd_metric.csv")
	if err := os.WriteFile(path, []byte(sourceCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	days, offsets, err := ingestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 逐日统计也要带来源，snapshot 靠它算 latestRealDate
	if days[0].DayAhead.SourceID != SourceExchange {
		t.Errorf("10-01 来源 = %d, want %d", days[0].DayAhead.SourceID, SourceExchange)
	}
	if days[1].DayAhead.SourceID != SourceForecastPlaceholder {
		t.Errorf("10-03 来源 = %d, want %d", days[1].DayAhead.SourceID, SourceForecastPlaceholder)
	}

	s := &store{
		url:       "http://example.test/gd_metric.csv",
		cachePath: path,
		maxSize:   1 << 20,
		days:      days,
		offsets:   offsets,
		curves:    newCurveLRU(1 << 20),
	}
	all, err := s.series("2026-10-01", "2026-10-03", KindDayAhead)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("全量序列 = %d 点, want 4", len(all))
	}
	if all[0].SourceID != SourceExchange || all[2].SourceID != SourceForecastPlaceholder {
		t.Fatalf("逐点来源没带上: %+v", all)
	}

	real := realOnly(all)
	if len(real) != 2 {
		t.Fatalf("实际值序列 = %d 点, want 2（999 占位应被剔除）", len(real))
	}
	for _, p := range real {
		if p.SourceID == SourceForecastPlaceholder || p.Date != "2026-10-01" {
			t.Errorf("混进占位行: %+v", p)
		}
	}
}

// 元数据缓存带结构版本：老缓存（没有 sourceId 那些新字段）不能直接当数据用 ——
// 2026-10-09 上线 v1.0.53 后就踩到了：老 meta 里的逐日统计缺 sourceId，
// latestRealDate 把「只有电查查预测占位」的 10-06 当成了有实际值的那天。
func TestLoadDiskDropsStaleCacheVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gd_metric.csv")
	if err := os.WriteFile(path, []byte(sourceCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	metaPath := metaPathFor(path)
	// 老缓存：没有 version 字段，逐日统计也没有 sourceId
	old := `{"url":"http://example.test/gd_metric.csv","etag":"\"abc\"",` +
		`"lastModified":"Fri, 09 Oct 2026 04:01:15 GMT","fetchedAt":"2026-10-09T12:51:45+08:00",` +
		`"days":[{"date":"2026-10-03","load":{"count":96},"dayAhead":{"count":96},"realtime":{"count":96}}],` +
		`"offsets":{"2026-10-03":0}}`
	if err := os.WriteFile(metaPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &store{url: "http://example.test/gd_metric.csv", cachePath: path, metaPath: metaPath,
		maxSize: 1 << 20, curves: newCurveLRU(1 << 20)}
	if err := s.loadDisk(); err != nil {
		t.Fatal(err)
	}
	if len(s.days) != 0 || len(s.offsets) != 0 {
		t.Fatalf("老版本的逐日统计应被丢掉: days=%d offsets=%d", len(s.days), len(s.offsets))
	}
	// etag / 时间戳要留着，否则会白白重下 37MB
	if s.etag != `"abc"` || s.fetchedAt.IsZero() {
		t.Fatalf("etag/fetchedAt 不该被清掉: etag=%q fetchedAt=%v", s.etag, s.fetchedAt)
	}

	// 重新解析后写回的是当前版本
	if err := s.loadLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version":2`) {
		t.Fatalf("写回的 meta 应带当前版本: %s", string(raw[:80]))
	}
}
