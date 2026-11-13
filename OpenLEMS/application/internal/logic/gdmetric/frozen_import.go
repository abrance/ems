package gdmetric

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 冻结通道的**人工导入**：把电查查导出（宽表或长表）校验成 gdmetric 能直接读的
// 长表 CSV（时间戳,日期,时间,typeId,类型,单位,数值,sourceId）。
//
// 为什么需要这一层：分时研究页只读冻结快照，快照停在 2026-10-02 之后就永远是博物馆
// 展品。官方 96 点匿名拿不到（见 docs/data-source-matrix.md §2），唯一合规来源是人工
// 从电查查导出再导入。所以这里**不联网、不要 token、不进 API、不进 cron**：
// 只有人手跑一次命令才会执行，写盘前逐条校验，失败一个字节都不动目标文件。
//
// 导入约定（README「怎么把电查查导出变成这页能看的数据」同口径）：
//
//   - 一行一个 (日期, 时间, typeId)。日期 YYYY-MM-DD；时间 15 分钟一格，一天 96 点。
//   - 时间标签两种写法都收：`00:15 … 24:00`（交易中心/EMS 口径）与 `00:00 … 23:45`
//     （常见导出工具口径）。后者整体后移一格写成前者 —— 两条序列的点是同一批，
//     只是标签基准不同，页面按 `00:15 … 24:00` 画。
//   - typeId 只收 21（日前电价）与 22（实时电价）；单位只收 元/兆瓦时。
//     负荷（23）不在本约定内：96 点负荷官方没有口径，别拿价格文件冒充。
//   - 占位：**空单元格**表示「这一刻没有实际值」，写成 sourceId=999（页面会剔除，
//     不画 0 线、不连直线）。数值 999 是合法价格，不是占位符 —— 源 CSV 的占位标记
//     在 sourceId 列，不在数值列，这一点踩过就会把真实 999 元/兆瓦时吃掉。
//   - 完整性：文件覆盖的每个自然日都必须有 96 点，且区间内不许缺天。缺了就报错，
//     不做插值、不补 0、不猜。

// FrozenImportHeader 冻结通道 CSV 的表头（与 gdmetric/file.go 的解析器一致）。
const FrozenImportHeader = "时间戳,日期,时间,typeId,类型,单位,数值,sourceId"

// DefaultFrozenCachePath 冻结通道默认读取的 CSV（与 gd_metric.cache_path 的默认值一致）。
const DefaultFrozenCachePath = defaultCachePath

// frozenPriceUnit 价格单位（源站口径，不做换算）。
const frozenPriceUnit = "元/兆瓦时"

// frozenPriceTypes 可导入的价格类别：21 日前电价 / 22 实时电价。
var frozenPriceTypes = map[int]string{
	typeDayAhead: "日前电价",
	typeRealtime: "实时电价",
}

// frozenClockCount 一天的点数。96 点是这份约定的全部意义，少一点就不是同一条序列。
const frozenClockCount = 96

// frozenCanonicalClocks 规范时间轴：00:15 … 24:00（96 个）。
var frozenCanonicalClocks = frozenBuildClocks(15)

// frozenZeroBasedClocks 导出工具常用的时间轴：00:00 … 23:45（96 个）。
var frozenZeroBasedClocks = frozenBuildClocks(0)

// frozenClockRank 规范时间 → 顺序（排序与完整性检查用）。
var frozenClockRank = func() map[string]int {
	m := make(map[string]int, len(frozenCanonicalClocks))
	for i, c := range frozenCanonicalClocks {
		m[c] = i
	}
	return m
}()

func frozenBuildClocks(startMinute int) []string {
	out := make([]string, 0, frozenClockCount)
	for i := 0; i < frozenClockCount; i++ {
		total := startMinute + i*15
		out = append(out, fmt.Sprintf("%02d:%02d", total/60, total%60))
	}
	return out
}

// FrozenRow 一行冻结通道数据（校验后的规范形态）。
type FrozenRow struct {
	Date     string
	Clock    string
	TypeID   int
	Unit     string
	Value    float64
	SourceID int
}

// FrozenImport 一次解析 + 校验的结果。
type FrozenImport struct {
	// Format 源文件形态：long（长表）/ wide（宽表）。
	Format string
	// Rows 规范行（按 日期、时间、typeId 升序）。
	Rows []FrozenRow
	// Days 覆盖的运行日（升序）。
	Days []string
	// Placeholders 占位行数（sourceId=999，即没有实际值的点）。
	Placeholders int
}

// frozenProblems 逐条收集问题，一次报全（只报第一处会让人反复试错）。
type frozenProblems struct {
	items []string
}

func (p *frozenProblems) addf(format string, args ...any) {
	p.items = append(p.items, fmt.Sprintf(format, args...))
}

func (p *frozenProblems) err() error {
	if len(p.items) == 0 {
		return nil
	}
	const maxShow = 20
	shown := p.items
	if len(shown) > maxShow {
		shown = shown[:maxShow]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "导入文件校验未通过（%d 处）：", len(p.items))
	for _, it := range shown {
		b.WriteString("\n  - ")
		b.WriteString(it)
	}
	if len(p.items) > maxShow {
		fmt.Fprintf(&b, "\n  … 其余 %d 处已省略（先修前 %d 条）", len(p.items)-maxShow, maxShow)
	}
	return errors.New(b.String())
}

// ParseFrozenImport 读候选文件并逐条校验，通过则返回规范行。
//
// 宽表与长表都收：电查查将来的导出形状未知，约定只规定「哪些列/哪些点算数」，
// 不规定导出按钮长什么样。
func ParseFrozenImport(r io.Reader) (*FrozenImport, error) {
	reader := csv.NewReader(bufio.NewReader(r))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失败（空文件或不是 CSV）：%w", err)
	}
	for i := range header {
		header[i] = frozenHeaderCell(header[i])
	}

	// 时间基准要整份文件判定（见 frozenClockBasis），所以先把记录读进内存：
	// 一天 192 行，一年也就 7 万行，这点内存换「不会把曲线挪错一格」很值。
	records := make([][]string, 0, 256)
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("第 %d 行解析失败：%w", len(records)+2, err)
		}
		records = append(records, rec)
	}

	switch {
	case frozenLooksWide(header):
		return frozenParseWide(records, header)
	case frozenLooksLong(header):
		return frozenParseLong(records, header)
	default:
		return nil, fmt.Errorf("表头不认识，需要长表（含 日期/时间/typeId/单位/数值 列）或宽表"+
			"（首列 日期，其余 96 列为时间）。实际表头：%s", strings.Join(header, ","))
	}
}

func frozenHeaderCell(s string) string {
	return strings.ToLower(strings.TrimSpace(stripBOM(s)))
}

func frozenLooksWide(header []string) bool {
	if len(header) < frozenClockCount {
		return false
	}
	switch header[0] {
	case "日期", "date", "运行日", "交易日":
	default:
		return false
	}
	known := 0
	for _, h := range header[1:] {
		if _, ok := frozenClockRank[h]; ok {
			known++
			continue
		}
		if _, err := time.Parse("15:04", h); err == nil {
			known++
		}
	}
	return known == frozenClockCount
}

func frozenLooksLong(header []string) bool {
	have := map[string]bool{}
	for _, h := range header {
		have[h] = true
	}
	for _, need := range []string{"日期", "时间", "typeid", "单位", "数值"} {
		if !have[need] {
			return false
		}
	}
	return true
}

// frozenClockBasis 一份文件的时间标签基准。
//
// 两种写法都收：00:15 … 24:00（交易中心/EMS 口径，下称 canon）与
// 00:00 … 23:45（导出工具口径，下称 zero）。
// **必须整份文件判定，不能逐格判定**：zero 里的 `00:15` 和 canon 里的 `00:15`
// 是不同时刻，逐格归一会在 00:15 处撞车，等于把整条曲线挪错一格。
type frozenClockBasis int

const (
	frozenBasisCanon frozenClockBasis = iota
	frozenBasisZero
)

// frozenDetectBasis 看到 `00:00` 或 `24:00` 就能定：只有 zero 才有 00:00，
// 只有 canon 才有 24:00。两者都出现或都没有 → 报错，不猜。
func frozenDetectBasis(labels []string) (frozenClockBasis, error) {
	var hasZero, hasCanon bool
	for _, l := range labels {
		switch strings.TrimSpace(stripBOM(l)) {
		case "00:00":
			hasZero = true
		case "24:00":
			hasCanon = true
		}
	}
	switch {
	case hasZero && hasCanon:
		return 0, errors.New("时间列同时出现 00:00 与 24:00：两种时间基准混在一份文件里，请先统一导出")
	case hasZero:
		return frozenBasisZero, nil
	case hasCanon:
		return frozenBasisCanon, nil
	default:
		return 0, errors.New("时间列既没有 00:00 也没有 24:00：无法确定时间基准。" +
			"完整的一天必居其一（00:00 起 或 00:15 起），所以这份文件很可能只导了部分时段；" +
			"按整份文件判定基准是刻意的 —— 逐格猜会把整条曲线挪错一格")
	}
}

// frozenNormalizeClock 按文件基准把时间标签归一成规范时间轴上的值。
func frozenNormalizeClock(label string, basis frozenClockBasis) (string, bool) {
	label = strings.TrimSpace(stripBOM(label))
	// 24:00 是交易中心口径的末点，time.Parse 不认，先直接命中规范轴
	if label == "24:00" && basis == frozenBasisCanon {
		return label, true
	}
	t, err := time.Parse("15:04", label)
	if err != nil {
		return "", false
	}
	if basis == frozenBasisZero {
		// 后移一格：23:45 → 24:00（time.Format 会把它写成 00:00，所以按分钟算）
		shifted := t.Hour()*60 + t.Minute() + 15
		if shifted > 24*60 {
			return "", false
		}
		label := fmt.Sprintf("%02d:%02d", shifted/60, shifted%60)
		if _, ok := frozenClockRank[label]; ok {
			return label, true
		}
		return "", false
	}
	if _, ok := frozenClockRank[label]; ok {
		return label, true
	}
	return "", false
}

// frozenParseValue 解析一个价格单元格。
//
// 空 / - / N/A 是「这一刻没有实际值」→ 占位。其余必须是有限数：
// 负电价是真实存在的市场现象，不做非负限制，只拒绝 NaN/Inf 和非数字。
func frozenParseValue(raw string) (value float64, placeholder bool, err error) {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "", "-", "--", "na", "n/a", "null", "nan":
		return 0, true, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, fmt.Errorf("数值 %q 不是数字", raw)
	}
	if v != v || v > 1e12 || v < -1e12 {
		return 0, false, fmt.Errorf("数值 %q 超出合理范围", raw)
	}
	return v, false, nil
}

func frozenParseLong(records [][]string, header []string) (*FrozenImport, error) {
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	problems := &frozenProblems{}
	need := func(name string) (int, bool) {
		i, ok := col[name]
		if !ok {
			problems.addf("缺少列 %q", name)
		}
		return i, ok
	}
	iDate, okDate := need("日期")
	iClock, okClock := need("时间")
	iType, okType := need("typeid")
	iUnit, okUnit := need("单位")
	iValue, okValue := need("数值")
	if !(okDate && okClock && okType && okUnit && okValue) {
		return nil, problems.err()
	}
	iSource, hasSource := col["sourceid"]

	labels := make([]string, 0, len(records))
	for _, rec := range records {
		if iClock < len(rec) {
			labels = append(labels, rec[iClock])
		}
	}
	basis, err := frozenDetectBasis(labels)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	rows := make([]FrozenRow, 0, 256)
	for i, rec := range records {
		line := i + 2
		if frozenBlankRecord(rec) {
			continue
		}
		get := func(idx int) string {
			if idx < 0 || idx >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[idx])
		}
		date := get(iDate)
		if _, err := time.ParseInLocation(dateLayout, date, time.UTC); err != nil {
			problems.addf("第 %d 行日期 %q 不是 YYYY-MM-DD", line, date)
			continue
		}
		clock, ok := frozenNormalizeClock(get(iClock), basis)
		if !ok {
			problems.addf("第 %d 行时间 %q 不是 15 分钟整点（00:15 … 24:00）", line, get(iClock))
			continue
		}
		typeID, err := strconv.Atoi(get(iType))
		if err != nil {
			problems.addf("第 %d 行 typeId %q 不是整数", line, get(iType))
			continue
		}
		if _, ok := frozenPriceTypes[typeID]; !ok {
			problems.addf("第 %d 行 typeId=%d 不在约定内（只收 21 日前电价 / 22 实时电价）", line, typeID)
			continue
		}
		if unit := get(iUnit); unit != frozenPriceUnit {
			problems.addf("第 %d 行单位 %q 不是 %s（不做换算，请先在导出里换算好）", line, unit, frozenPriceUnit)
			continue
		}
		value, placeholder, err := frozenParseValue(get(iValue))
		if err != nil {
			problems.addf("第 %d 行：%v", line, err)
			continue
		}
		sourceID := SourceExchange
		if placeholder {
			sourceID = SourceForecastPlaceholder
		}
		if hasSource {
			if raw := get(iSource); raw != "" {
				sid, err := strconv.Atoi(raw)
				if err != nil {
					problems.addf("第 %d 行 sourceId %q 不是整数", line, raw)
					continue
				}
				// 源文件的 sourceId 才是占位标记：999 = 第三方预测，0 = 未知来源（都不算实际值）
				sourceID = sid
				if !IsRealSource(sid) {
					placeholder = true
				}
			}
		}
		key := fmt.Sprintf("%s|%s|%d", date, clock, typeID)
		if seen[key] {
			problems.addf("第 %d 行重复：%s %s typeId=%d", line, date, clock, typeID)
			continue
		}
		seen[key] = true
		rows = append(rows, FrozenRow{Date: date, Clock: clock, TypeID: typeID, Unit: frozenPriceUnit, Value: value, SourceID: sourceID})
	}
	if err := problems.err(); err != nil {
		return nil, err
	}
	return frozenAssemble("long", rows, problems)
}

func frozenParseWide(records [][]string, header []string) (*FrozenImport, error) {
	// 宽表没有 typeId 列：按日前电价（21）处理。实时价请用长表 ——
	// 猜一个「第二块宽表是实时价」的规则只会让导入结果不可预测。
	const typeID = typeDayAhead
	basis, err := frozenDetectBasis(header[1:])
	if err != nil {
		return nil, err
	}
	clocks := make([]string, 0, frozenClockCount)
	for _, h := range header[1:] {
		clock, ok := frozenNormalizeClock(h, basis)
		if !ok {
			return nil, fmt.Errorf("宽表列 %q 不是时间点（需 00:15 … 24:00 共 96 列）", h)
		}
		clocks = append(clocks, clock)
	}
	if len(clocks) != frozenClockCount {
		return nil, fmt.Errorf("宽表有 %d 个时间列，需 %d 个（一天 96 点）", len(clocks), frozenClockCount)
	}
	dup := map[string]bool{}
	for _, c := range clocks {
		if dup[c] {
			return nil, fmt.Errorf("宽表时间列 %q 重复", c)
		}
		dup[c] = true
	}

	problems := &frozenProblems{}
	rows := make([]FrozenRow, 0, 96)
	for i, rec := range records {
		line := i + 2
		if frozenBlankRecord(rec) {
			continue
		}
		date := strings.TrimSpace(stripBOM(rec[0]))
		if _, err := time.ParseInLocation(dateLayout, date, time.UTC); err != nil {
			problems.addf("第 %d 行日期 %q 不是 YYYY-MM-DD", line, date)
			continue
		}
		for idx, clock := range clocks {
			cell := ""
			if idx+1 < len(rec) {
				cell = rec[idx+1]
			}
			value, placeholder, err := frozenParseValue(cell)
			if err != nil {
				problems.addf("第 %d 行 %s 列：%v", line, clock, err)
				continue
			}
			sourceID := SourceExchange
			if placeholder {
				sourceID = SourceForecastPlaceholder
			}
			rows = append(rows, FrozenRow{Date: date, Clock: clock, TypeID: typeID, Unit: frozenPriceUnit, Value: value, SourceID: sourceID})
		}
	}
	if err := problems.err(); err != nil {
		return nil, err
	}
	return frozenAssemble("wide", rows, problems)
}

func frozenBlankRecord(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// frozenAssemble 做完整性检查（每天 96 点、区间内不缺天）并排序。
func frozenAssemble(format string, rows []FrozenRow, problems *frozenProblems) (*FrozenImport, error) {
	if len(rows) == 0 {
		return nil, errors.New("导入文件没有任何数据行")
	}
	type key struct {
		date   string
		typeID int
	}
	got := map[key]map[string]bool{}
	daySet := map[string]bool{}
	for _, r := range rows {
		k := key{r.Date, r.TypeID}
		if got[k] == nil {
			got[k] = map[string]bool{}
		}
		got[k][r.Clock] = true
		daySet[r.Date] = true
	}
	dates := make([]string, 0, len(daySet))
	for d := range daySet {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	// 每天每类必须 96 点：缺一个点就不是「96 点序列」，页面会把它当成数据质量事故。
	for k, clocks := range got {
		if len(clocks) == frozenClockCount {
			continue
		}
		missing := make([]string, 0, frozenClockCount-len(clocks))
		for _, c := range frozenCanonicalClocks {
			if !clocks[c] {
				missing = append(missing, c)
			}
		}
		problems.addf("%s typeId=%d 只有 %d 点，缺 %d 点（%s）",
			k.date, k.typeID, len(clocks), len(missing), frozenJoinLimited(missing, 6))
	}

	// 区间内不许缺天：11-01、11-03 有而 11-02 没有 → 报错，不猜那天为什么没导。
	start, err := time.ParseInLocation(dateLayout, dates[0], time.UTC)
	if err == nil {
		end, err2 := time.ParseInLocation(dateLayout, dates[len(dates)-1], time.UTC)
		if err2 == nil {
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				if day := d.Format(dateLayout); !daySet[day] {
					problems.addf("区间内缺天：%s 没有任何数据", day)
				}
			}
		}
	}
	if err := problems.err(); err != nil {
		return nil, err
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date < rows[j].Date
		}
		ri, rj := frozenClockRank[rows[i].Clock], frozenClockRank[rows[j].Clock]
		if ri != rj {
			return ri < rj
		}
		return rows[i].TypeID < rows[j].TypeID
	})

	placeholders := 0
	for _, r := range rows {
		if !IsRealSource(r.SourceID) {
			placeholders++
		}
	}
	return &FrozenImport{Format: format, Rows: rows, Days: dates, Placeholders: placeholders}, nil
}

func frozenJoinLimited(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, "、")
	}
	return strings.Join(items[:max], "、") + fmt.Sprintf(" 等 %d 个", len(items))
}

// CSV 渲染成冻结通道能直接读的长表（UTF-8 BOM，与源文件一致）。
func (f *FrozenImport) CSV() []byte {
	var b strings.Builder
	b.WriteString("\ufeff")
	b.WriteString(FrozenImportHeader)
	b.WriteString("\n")
	for _, r := range f.Rows {
		fmt.Fprintf(&b, "%s,%s,%s,%d,%s,%s,%s,%d\n",
			frozenTimestamp(r.Date, r.Clock), r.Date, r.Clock, r.TypeID,
			frozenPriceTypes[r.TypeID], r.Unit, strconv.FormatFloat(r.Value, 'f', -1, 64), r.SourceID)
	}
	return []byte(b.String())
}

// frozenTimestamp 与源 CSV 同口径：24:00 的点时间戳归到次日 00:00:00。
func frozenTimestamp(date, clock string) string {
	d, err := time.ParseInLocation(dateLayout, date, time.UTC)
	if err != nil {
		return date + " " + clock + ":00"
	}
	if clock == "24:00" {
		return d.AddDate(0, 0, 1).Format(dateLayout) + " 00:00:00"
	}
	return date + " " + clock + ":00"
}

// FrozenImportReport 一次写盘的结果（给 CLI 打印）。
type FrozenImportReport struct {
	Format       string
	Days         []string
	Rows         int
	Placeholders int
	Merged       int
	Target       string
}

// WriteFrozenImportFile 校验 → 合并（可选）→ 原子写盘。
//
// 任何校验失败都发生在写盘之前：目标文件（以及它旁边的 .meta.json）一个字节都不动。
// 写盘成功后删掉 .meta.json —— 那是旧 CSV 的行偏移缓存，继续留着会让页面按旧偏移
// 读到新文件里的错位行。
func WriteFrozenImportFile(r io.Reader, target string, merge bool) (*FrozenImportReport, error) {
	imp, err := ParseFrozenImport(r)
	if err != nil {
		return nil, err
	}
	merged := 0
	rows := imp.Rows
	if merge {
		existing, err := readFrozenRows(target)
		if err != nil {
			return nil, err
		}
		if len(existing) > 0 {
			rows, merged = frozenMergeRows(existing, imp.Rows)
		}
	}
	payload := &FrozenImport{Format: imp.Format, Rows: rows, Days: imp.Days, Placeholders: imp.Placeholders}
	if merge && merged > 0 {
		// 合并后按规范顺序重排（历史行也一起排），天数与占位按合并结果重算
		days := map[string]bool{}
		placeholders := 0
		for _, r := range rows {
			days[r.Date] = true
			if !IsRealSource(r.SourceID) {
				placeholders++
			}
		}
		payload.Days = make([]string, 0, len(days))
		for d := range days {
			payload.Days = append(payload.Days, d)
		}
		sort.Strings(payload.Days)
		payload.Placeholders = placeholders
	}

	if err := frozenWriteAtomic(target, payload.CSV()); err != nil {
		return nil, err
	}
	_ = os.Remove(metaPathFor(target))
	return &FrozenImportReport{
		Format:       imp.Format,
		Days:         payload.Days,
		Rows:         len(payload.Rows),
		Placeholders: payload.Placeholders,
		Merged:       merged,
		Target:       target,
	}, nil
}

// readFrozenRows 读已有目标文件（不存在 = 空）。用与运行时同一个解析器，
// 保证「导入器眼里的旧文件」和「页面眼里的旧文件」是同一份。
func readFrozenRows(path string) ([]FrozenRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	rows := make([]FrozenRow, 0, 1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if date, clock, typeID, unit, value, sourceID, ok := parseMetricLine(line); ok {
				rows = append(rows, FrozenRow{Date: date, Clock: clock, TypeID: typeID, Unit: unit, Value: value, SourceID: sourceID})
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return rows, nil
}

// frozenMergeRows 历史行 + 新行（同键新行优先），返回合并结果与被覆盖的行数。
func frozenMergeRows(existing, incoming []FrozenRow) ([]FrozenRow, int) {
	key := func(r FrozenRow) string { return fmt.Sprintf("%s|%s|%d", r.Date, r.Clock, r.TypeID) }
	byKey := make(map[string]FrozenRow, len(existing)+len(incoming))
	for _, r := range existing {
		byKey[key(r)] = r
	}
	overwritten := 0
	for _, r := range incoming {
		if _, ok := byKey[key(r)]; ok {
			overwritten++
		}
		byKey[key(r)] = r
	}
	out := make([]FrozenRow, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		ri, rj := frozenClockRank[out[i].Clock], frozenClockRank[out[j].Clock]
		if ri != rj {
			return ri < rj
		}
		return out[i].TypeID < out[j].TypeID
	})
	return out, overwritten
}

// frozenWriteAtomic 先写临时文件再 rename：中途失败不会留下半个文件。
func frozenWriteAtomic(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".import-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
