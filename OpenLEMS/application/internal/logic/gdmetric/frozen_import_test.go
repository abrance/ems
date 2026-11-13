package gdmetric

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 人工导入的校验器：错一个字节就不许写盘。
// 这层的价值全在「报错报得准」——导入是人工操作，报错含糊会让人反复试错，
// 而一旦把半份数据写进冻结通道，页面就会把缺天当成「那天没数据」静默画空态。

const importHeader = "时间戳,日期,时间,typeId,类型,单位,数值,sourceId\n"

// importLongCSV 造一份合法长表：date 当天日前/实时各 96 点。
func importLongCSV(date string) string {
	var b strings.Builder
	b.WriteString(importHeader)
	for _, typeID := range []int{typeDayAhead, typeRealtime} {
		name := frozenPriceTypes[typeID]
		for _, clock := range frozenCanonicalClocks {
			fmt.Fprintf(&b, "%s %s:00,%s,%s,%d,%s,%s,%d,%d\n",
				date, strings.Replace(clock, "24:00", "00:00", 1), date, clock, typeID,
				name, frozenPriceUnit, 300+typeID, SourceExchange)
		}
	}
	return b.String()
}

func importWrite(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 合法长表：通过，且 96 点齐全、顺序规范、单位与 sourceId 都落到运行时口径。
func TestParseFrozenImport_LongAccepted(t *testing.T) {
	imp, err := ParseFrozenImport(strings.NewReader(importLongCSV("2026-11-10")))
	if err != nil {
		t.Fatalf("合法长表应通过：%v", err)
	}
	if imp.Format != "long" {
		t.Fatalf("形态=%s，期望 long", imp.Format)
	}
	if len(imp.Days) != 1 || imp.Days[0] != "2026-11-10" {
		t.Fatalf("天数=%v", imp.Days)
	}
	if len(imp.Rows) != 192 {
		t.Fatalf("行数=%d，期望 192（2 类 × 96 点）", len(imp.Rows))
	}
	if imp.Placeholders != 0 {
		t.Fatalf("占位=%d，期望 0", imp.Placeholders)
	}
	// 时间轴必须落在 00:15 … 24:00 上：页面按这个轴画，偏一格就整体错位
	first := imp.Rows[0]
	if first.Clock != "00:15" || first.TypeID != typeDayAhead {
		t.Fatalf("首行=%+v，期望 00:15 日前电价", first)
	}
	if last := imp.Rows[len(imp.Rows)-1]; last.Clock != "24:00" {
		t.Fatalf("末行=%+v，期望 24:00", last)
	}
}

// 少 96 点 → 报错，且错误里点出缺哪些点（人工修数据要知道缺哪儿）。
func TestParseFrozenImport_MissingPointsRejected(t *testing.T) {
	// 删掉日前电价 12:00 那一行：那一天只剩 95 点
	lines := strings.Split(importLongCSV("2026-11-10"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "2026-11-10 12:00:00,2026-11-10,12:00,21,") {
			continue
		}
		kept = append(kept, line)
	}
	body := strings.Join(kept, "\n")
	if _, err := ParseFrozenImport(strings.NewReader(body)); err == nil {
		t.Fatal("少一个点应报错")
	} else if !strings.Contains(err.Error(), "只有 95 点") || !strings.Contains(err.Error(), "12:00") {
		t.Fatalf("错误要说明点数与缺点位置，实际：%v", err)
	}
}

// 区间内缺天 → 报错（不猜那天为什么没导，也不许静默留下空洞）。
func TestParseFrozenImport_MissingDayRejected(t *testing.T) {
	body := importLongCSV("2026-11-10") + strings.SplitN(importLongCSV("2026-11-12"), "\n", 2)[1]
	if _, err := ParseFrozenImport(strings.NewReader(body)); err == nil {
		t.Fatal("缺天应报错")
	} else if !strings.Contains(err.Error(), "缺天：2026-11-11") {
		t.Fatalf("错误要点出缺的那天，实际：%v", err)
	}
}

// 列名 / 单位不符 → 报错。单位不做换算：换算悄悄做对是运气，做错是事故。
func TestParseFrozenImport_BadColumnsAndUnit(t *testing.T) {
	// 缺 sourceId 列：可以接受（老导出没有这列），占位改由空单元格表达
	body := strings.Replace(importLongCSV("2026-11-10"), "时间戳,日期,时间,typeId,类型,单位,数值,sourceId",
		"时间戳,日期,时间,typeId,类型,单位,数值", 1)
	if _, err := ParseFrozenImport(strings.NewReader(body)); err != nil {
		t.Fatalf("缺 sourceId 列应通过：%v", err)
	}

	body = strings.Replace(importLongCSV("2026-11-10"), "元/兆瓦时", "元/千瓦时", 1)
	if _, err := ParseFrozenImport(strings.NewReader(body)); err == nil {
		t.Fatal("单位不是 元/兆瓦时 应报错")
	} else if !strings.Contains(err.Error(), "元/千瓦时") {
		t.Fatalf("错误要点出实际单位，实际：%v", err)
	}

	// 列名拼错（数值 → 值）应认不出表头而不是猜
	body = strings.Replace(importLongCSV("2026-11-10"), ",单位,数值,sourceId", ",单位,值,sourceId", 1)
	if _, err := ParseFrozenImport(strings.NewReader(body)); err == nil {
		t.Fatal("缺 数值 列应报错")
	}
}

// 宽表也收：首列日期 + 96 个时间列，按日前电价处理；空单元格 = 占位。
func TestParseFrozenImport_WideAccepted(t *testing.T) {
	var b strings.Builder
	b.WriteString("日期," + strings.Join(frozenCanonicalClocks, ",") + "\n")
	cells := make([]string, 0, frozenClockCount)
	for i := range frozenCanonicalClocks {
		if i == 10 {
			cells = append(cells, "") // 空 = 没有实际值
			continue
		}
		cells = append(cells, fmt.Sprintf("%d", 400+i))
	}
	b.WriteString("2026-11-11," + strings.Join(cells, ",") + "\n")

	imp, err := ParseFrozenImport(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("宽表应通过：%v", err)
	}
	if imp.Format != "wide" || len(imp.Rows) != frozenClockCount {
		t.Fatalf("形态=%s 行数=%d", imp.Format, len(imp.Rows))
	}
	if imp.Placeholders != 1 {
		t.Fatalf("占位=%d，期望 1（空单元格）", imp.Placeholders)
	}
	for _, r := range imp.Rows {
		if r.TypeID != typeDayAhead {
			t.Fatalf("宽表应按日前电价导入，实际 typeId=%d", r.TypeID)
		}
	}
}

// 宽表的 00:00 基准标签与长表的 00:15 基准标签是同一批点：整体后移一格，不能当非法值。
func TestParseFrozenImport_ZeroBasedClockShift(t *testing.T) {
	var b strings.Builder
	b.WriteString(importHeader)
	for i := 0; i < frozenClockCount; i++ {
		total := i * 15
		clock := fmt.Sprintf("%02d:%02d", total/60, total%60)
		fmt.Fprintf(&b, "2026-11-12 %s:00,2026-11-12,%s,21,日前电价,元/兆瓦时,%d,9\n",
			clock, clock, 500+i)
	}
	imp, err := ParseFrozenImport(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("00:00 基准的时间轴应通过：%v", err)
	}
	if imp.Rows[0].Clock != "00:15" || imp.Rows[0].Value != 500 {
		t.Fatalf("首个点应归到 00:15 且值不变：%+v", imp.Rows[0])
	}
	if imp.Rows[len(imp.Rows)-1].Clock != "24:00" {
		t.Fatalf("末个点应归到 24:00：%+v", imp.Rows[len(imp.Rows)-1])
	}
}

// 数值 999 是合法价格，不是占位符：占位只看 sourceId / 空单元格。
// 这条踩过就会把真实 999 元/兆瓦时从曲线上吃掉。
func TestParseFrozenImport_Value999IsRealPrice(t *testing.T) {
	body := strings.Replace(importLongCSV("2026-11-10"), "321,9", "999,9", 1)
	imp, err := ParseFrozenImport(strings.NewReader(body))
	if err != nil {
		t.Fatalf("应通过：%v", err)
	}
	if imp.Placeholders != 0 {
		t.Fatalf("数值 999 不该算占位，占位=%d", imp.Placeholders)
	}
	found := false
	for _, r := range imp.Rows {
		if r.Value == 999 && IsRealSource(r.SourceID) {
			found = true
		}
	}
	if !found {
		t.Fatal("999 应作为实际值保留")
	}
}

// 重复行报错：同一 (日期,时间,typeId) 两行说明导出重复合并，静默取后者等于猜。
func TestParseFrozenImport_DuplicateRejected(t *testing.T) {
	line := "2026-11-10 00:15:00,2026-11-10,00:15,21,日前电价,元/兆瓦时,321,9\n"
	body := importLongCSV("2026-11-10") + line
	if _, err := ParseFrozenImport(strings.NewReader(body)); err == nil {
		t.Fatal("重复行应报错")
	} else if !strings.Contains(err.Error(), "重复") {
		t.Fatalf("错误要说重复，实际：%v", err)
	}
}

// 写盘：产物必须能被运行时解析器（ingestFile）读回，且 24:00 的时间戳归到次日。
// 这是「导入器眼里的数据」与「页面眼里的数据」同一份的唯一保证。
func TestWriteFrozenImportFile_ReadableByRuntime(t *testing.T) {
	src := importWrite(t, "export.csv", importLongCSV("2026-11-10"))
	target := filepath.Join(t.TempDir(), "out", "gd_metric.csv")

	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rep, err := WriteFrozenImportFile(f, target, false)
	if err != nil {
		t.Fatalf("写盘失败：%v", err)
	}
	if rep.Rows != 192 || len(rep.Days) != 1 {
		t.Fatalf("报告=%+v", rep)
	}

	days, _, err := ingestFile(target)
	if err != nil {
		t.Fatalf("运行时解析器读不了导入产物：%v", err)
	}
	if len(days) != 1 || days[0].DayAhead.Count != frozenClockCount || days[0].Realtime.Count != frozenClockCount {
		t.Fatalf("读回统计不对：%+v", days)
	}
	if days[0].DayAhead.Unit != frozenPriceUnit || !IsRealSource(days[0].DayAhead.SourceID) {
		t.Fatalf("单位/来源不对：%+v", days[0].DayAhead)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "2026-11-11 00:00:00,2026-11-10,24:00,21") {
		t.Fatal("24:00 的时间戳应归到次日 00:00:00（与源 CSV 同口径）")
	}
}

// 校验失败不动旧文件：这是「导入错一次就把历史数据弄没」的唯一防线。
func TestWriteFrozenImportFile_FailureKeepsOldFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gd_metric.csv")
	old := importLongCSV("2026-11-01")
	if err := os.WriteFile(target, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// 旧文件旁边有行偏移缓存：写盘成功后要删掉，否则页面按旧偏移读到错位行
	meta := metaPathFor(target)
	if err := os.WriteFile(meta, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := importWrite(t, "bad.csv", strings.Replace(importLongCSV("2026-11-02"), "元/兆瓦时", "元/千瓦时", 1))
	f, err := os.Open(bad)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := WriteFrozenImportFile(f, target, true); err == nil {
		t.Fatal("单位不符应失败")
	}

	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != old {
		t.Fatal("校验失败后旧文件被改动了")
	}
	if _, err := os.Stat(meta); err != nil {
		t.Fatalf("校验失败不该动元数据缓存：%v", err)
	}

	// 成功写盘：元数据缓存必须删掉
	good := importWrite(t, "good.csv", importLongCSV("2026-11-02"))
	g, err := os.Open(good)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if _, err := WriteFrozenImportFile(g, target, true); err != nil {
		t.Fatalf("合并导入失败：%v", err)
	}
	if _, err := os.Stat(meta); !os.IsNotExist(err) {
		t.Fatalf("写盘成功后应删除行偏移缓存，实际 err=%v", err)
	}
}

// 合并：历史行保留，同键以新文件为准。
func TestWriteFrozenImportFile_Merge(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gd_metric.csv")
	day1 := importWrite(t, "d1.csv", importLongCSV("2026-11-01"))
	f1, _ := os.Open(day1)
	defer f1.Close()
	if _, err := WriteFrozenImportFile(f1, target, true); err != nil {
		t.Fatal(err)
	}

	// 新文件是重叠区间重导：11-01 全天重导（首点改成 777）+ 11-02 全天。
	// 完整性校验要求「文件覆盖的每天都是 96 点」，所以不能只补一个点。
	body := importLongCSV("2026-11-01") + strings.SplitN(importLongCSV("2026-11-02"), "\n", 2)[1]
	body = strings.Replace(body, "2026-11-01,00:15,21,日前电价,元/兆瓦时,321,9", "2026-11-01,00:15,21,日前电价,元/兆瓦时,777,9", 1)
	day2 := importWrite(t, "d2.csv", body)
	f2, _ := os.Open(day2)
	defer f2.Close()
	rep, err := WriteFrozenImportFile(f2, target, true)
	if err != nil {
		t.Fatalf("合并失败：%v", err)
	}
	if rep.Merged != 192 {
		t.Fatalf("覆盖行数=%d，期望 192（11-01 全天重导）", rep.Merged)
	}
	if len(rep.Days) != 2 {
		t.Fatalf("合并后天数=%v，期望 2 天", rep.Days)
	}

	rows, err := readFrozenRows(target)
	if err != nil {
		t.Fatal(err)
	}
	var got float64
	for _, r := range rows {
		if r.Date == "2026-11-01" && r.Clock == "00:15" && r.TypeID == typeDayAhead {
			got = r.Value
		}
	}
	if got != 777 {
		t.Fatalf("同键应以新文件为准，实际 %v", got)
	}
}

// 文档里的示例文件必须真的能过校验：README 抄一遍就废的示例比没有更糟。
func TestParseFrozenImport_ExamplesInDocs(t *testing.T) {
	base := filepath.Join("..", "..", "..", "..", "docs", "examples", "gdmetric-import")
	for _, name := range []string{"long.csv", "wide.csv"} {
		f, err := os.Open(filepath.Join(base, name))
		if err != nil {
			t.Fatalf("示例文件缺失 %s：%v", name, err)
		}
		imp, err := ParseFrozenImport(f)
		f.Close()
		if err != nil {
			t.Fatalf("示例 %s 校验失败：%v", name, err)
		}
		if len(imp.Rows) < frozenClockCount {
			t.Fatalf("示例 %s 行数=%d", name, len(imp.Rows))
		}
	}
}
