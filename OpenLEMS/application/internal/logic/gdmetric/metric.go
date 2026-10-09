package gdmetric

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"common/c_log"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	defaultURL        = "https://file.xiaoyxq.top/gd_metric.csv"
	defaultCachePath  = "out/gd_metric.csv"
	defaultRefreshHrs = 6
	dateLayout        = "2006-01-02"
	maxRangeDays      = 400

	typeDayAhead = 21
	typeRealtime = 22
	typeLoad     = 23
)

// sourceId 口径：同一个 (日期, 时间, typeId) 只留一行 —— 该时刻有实际值就写实际来源，
// 没有就写 SourceForecastPlaceholder（电查查的预测顶上，免得曲线上有个洞）。
// **占位行不是基准真值**：拿它当「实际值」去算准确度，算出来的是两个模型谁更准。
const (
	// SourceExchange 实际值来源（广东电力交易中心），日前/实时/负荷都是它。
	SourceExchange = 9
	// SourceForecastPlaceholder 占位来源（电查查预测）：实际值还没出。
	SourceForecastPlaceholder = 999
)

// IsRealSource sourceId 是不是**实际值**来源。
//
// 0 = 文件里没写或写错（未知来源），**不算实际值**：宁可不认，
// 也不要把来源不明的数当基准真值用 —— 「拿预测当实际」这类事故就是这么来的。
func IsRealSource(sourceID int) bool {
	return sourceID != 0 && sourceID != SourceForecastPlaceholder
}

type Sample struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
	// SourceID 见上面的 sourceId 口径，0 = 文件里没写这一列
	SourceID int `json:"sourceId"`
}

type SeriesStat struct {
	Avg   float64 `json:"avg"`
	Max   float64 `json:"max"`
	Min   float64 `json:"min"`
	Last  float64 `json:"last"`
	Count int     `json:"count"`
	Unit  string  `json:"unit"`
	// SourceID 最后一条样本的来源：999 = 这天只有电查查预测占位（实际值还没出）
	SourceID int `json:"sourceId"`
}

type DayRow struct {
	Date     string     `json:"date"`
	Load     SeriesStat `json:"load"`
	DayAhead SeriesStat `json:"dayAhead"`
	Realtime SeriesStat `json:"realtime"`
}

type DayCurve struct {
	Date     string   `json:"date"`
	Load     []Sample `json:"load"`
	DayAhead []Sample `json:"dayAhead"`
	Realtime []Sample `json:"realtime"`
}

type Snapshot struct {
	LatestDate string `json:"latestDate"`
	// LatestRealDate 最近一个**有实际值**（非电查查占位）的日期。
	// 复盘/回测要用它当区间右端：CSV 常常是「最近几天只有预测占位」，
	// 拿 latestDate 当右端会得到一个算不出准确度的区间。
	LatestRealDate string `json:"latestRealDate"`
	From           string `json:"from"`
	To             string `json:"to"`
	Date           string `json:"date"`
	Source         string `json:"source"`
	UpdatedAt      string `json:"updatedAt"`
	// AvailableDays 为 CSV 内有数据的全部运行日（升序），供前端日历灰显无数据日期
	AvailableDays []string  `json:"availableDays"`
	Latest        *DayRow   `json:"latest"`
	Days          []DayRow  `json:"days"`
	Curve         *DayCurve `json:"curve"`
}

// cacheVersion 元数据缓存的结构版本。改了 DayRow/SeriesStat 的字段（比如加 sourceId）
// 就必须加一：老缓存里的逐日统计缺新字段，读进来会算错（latestRealDate 曾经因此
// 把只有电查查预测占位的那天当成「有实际值的那天」）。
const cacheVersion = 2

type cacheFile struct {
	Version      int              `json:"version"`
	URL          string           `json:"url"`
	ETag         string           `json:"etag"`
	LastModified string           `json:"lastModified"`
	FetchedAt    string           `json:"fetchedAt"`
	Days         []DayRow         `json:"days"`
	Offsets      map[string]int64 `json:"offsets"`
}

type store struct {
	mu           sync.RWMutex
	refreshMu    sync.Mutex
	url          string
	cachePath    string
	metaPath     string
	maxSize      int64
	refreshAfter time.Duration
	etag         string
	lastModified string
	fetchedAt    time.Time
	days         []DayRow
	offsets      map[string]int64
	curves       *curveLRU
}

var (
	global     = &store{}
	globalOnce sync.Once
)

func shanghaiLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func loadConfig(ctx context.Context) (url, cachePath string, refresh time.Duration) {
	url = defaultURL
	cachePath = defaultCachePath
	hours := defaultRefreshHrs
	if g.Cfg() != nil {
		if v := strings.TrimSpace(g.Cfg().MustGet(ctx, "gd_metric.url").String()); v != "" {
			url = v
		}
		if v := strings.TrimSpace(g.Cfg().MustGet(ctx, "gd_metric.cache_path").String()); v != "" {
			cachePath = v
		}
		if n := g.Cfg().MustGet(ctx, "gd_metric.refresh_hours").Int(); n > 0 {
			hours = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GD_METRIC_URL")); v != "" {
		url = v
	}
	return url, cachePath, time.Duration(hours) * time.Hour
}

func loadMaxSize(ctx context.Context) int64 {
	raw := ""
	if g.Cfg() != nil {
		raw = strings.TrimSpace(g.Cfg().MustGet(ctx, "gd_metric.max_size").String())
	}
	if v := strings.TrimSpace(os.Getenv("GD_METRIC_MAX_SIZE")); v != "" {
		raw = v
	}
	return parseSize(raw)
}

func metaPathFor(csvPath string) string {
	if strings.HasSuffix(strings.ToLower(csvPath), ".csv") {
		return csvPath[:len(csvPath)-4] + ".meta.json"
	}
	return csvPath + ".meta.json"
}

func initGlobal(ctx context.Context) {
	globalOnce.Do(func() {
		url, cachePath, refresh := loadConfig(ctx)
		global.url = url
		global.cachePath = cachePath
		global.refreshAfter = refresh
		global.metaPath = metaPathFor(cachePath)
		global.maxSize = loadMaxSize(ctx)
		global.curves = newCurveLRU(global.maxSize)
		if err := global.loadMeta(); err != nil {
			c_log.Warningf(ctx, "电网日指标本地缓存未加载: %+v", err)
		}
	})
}

// LocalDataReady 本地是否已有 gd_metric 缓存（调用前不会去下载）。
//
// 给「有数据才跑」的调用方用：GetSnapshot/GetSeries 在缓存缺失时会去拉 37MB 的文件，
// 测试与可选功能不该有这种副作用 —— 先问这个，没数据就跳过。
func LocalDataReady() bool {
	initGlobal(context.Background())
	if global.localCSVReady() {
		return true
	}
	global.mu.RLock()
	defer global.mu.RUnlock()
	return len(global.days) > 0
}

func GetSnapshot(ctx context.Context, from, to, date string) (*Snapshot, error) {
	initGlobal(ctx)
	if err := global.ensureFresh(ctx); err != nil {
		global.mu.RLock()
		empty := len(global.days) == 0
		global.mu.RUnlock()
		if empty {
			return nil, err
		}
		c_log.Warningf(ctx, "电网日指标刷新失败，沿用缓存: %+v", err)
	}
	return global.snapshot(from, to, date)
}

func (s *store) ensureFresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if !s.localCSVReady() {
		return s.refresh(ctx)
	}
	s.mu.RLock()
	needParse := len(s.days) == 0 || len(s.offsets) == 0
	s.mu.RUnlock()
	if needParse {
		if err := s.loadLocal(ctx); err != nil {
			return err
		}
	}
	s.mu.RLock()
	stale := s.fetchedAt.IsZero() || time.Since(s.fetchedAt) > s.refreshAfter
	s.mu.RUnlock()
	if !stale {
		return nil
	}
	return s.refresh(ctx)
}

func (s *store) refresh(ctx context.Context) error {
	fetchCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, s.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "OpenLEMS-gd-metric/1.0")
	s.mu.RLock()
	if s.etag != "" {
		req.Header.Set("If-None-Match", s.etag)
	}
	if s.lastModified != "" {
		req.Header.Set("If-Modified-Since", s.lastModified)
	}
	s.mu.RUnlock()

	client := &http.Client{Timeout: 180 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		s.mu.Lock()
		s.fetchedAt = time.Now()
		s.mu.Unlock()
		if err := s.saveMeta(); err != nil {
			c_log.Warningf(ctx, "电网日指标元数据写入失败: %+v", err)
		}
		s.mu.RLock()
		empty := len(s.days) == 0
		s.mu.RUnlock()
		if empty {
			return s.loadLocal(ctx)
		}
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载电网指标失败，HTTP %d", res.StatusCode)
	}

	if err := s.writeCSV(res.Body); err != nil {
		return err
	}
	s.mu.Lock()
	s.etag = res.Header.Get("ETag")
	s.lastModified = res.Header.Get("Last-Modified")
	s.fetchedAt = time.Now()
	s.mu.Unlock()
	if err := s.loadLocal(ctx); err != nil {
		return err
	}
	c_log.Infof(ctx, "电网日指标已刷新：%d 天，来源 %s", len(s.days), s.url)
	return nil
}

func (s *store) snapshot(from, to, date string) (*Snapshot, error) {
	s.mu.RLock()
	if len(s.days) == 0 {
		url := s.url
		s.mu.RUnlock()
		return &Snapshot{Source: url, Days: []DayRow{}, AvailableDays: []string{}}, nil
	}
	latest := s.days[len(s.days)-1].Date
	from, to = clampRange(from, to, s.days[0].Date, latest)
	date = strings.TrimSpace(date)
	if date == "" {
		date = latest
	}
	out := &Snapshot{
		LatestDate:     latest,
		LatestRealDate: latestRealDate(s.days, 1),
		From:           from,
		To:             to,
		Date:           date,
		Source:         s.url,
		UpdatedAt:      s.fetchedAt.In(shanghaiLocation()).Format(time.RFC3339),
		Latest:         cloneDay(s.days[len(s.days)-1]),
		Days:           make([]DayRow, 0),
	}
	out.AvailableDays = make([]string, 0, len(s.days))
	for i := range s.days {
		out.AvailableDays = append(out.AvailableDays, s.days[i].Date)
	}
	for _, row := range s.days {
		if row.Date < from || row.Date > to {
			continue
		}
		out.Days = append(out.Days, row)
	}
	s.mu.RUnlock()
	if curve, err := s.getCurve(date); err == nil && curve != nil {
		out.Curve = curve
	}
	return out, nil
}

func cloneDay(row DayRow) *DayRow {
	copied := row
	return &copied
}

// latestRealDate 从后往前找最近一个「日前电价有实际值且不少于 minPoints 点」的日期，
// 找不到返回空串。minPoints=1 就是「有实际值就行」。
//
// 注意 SeriesStat.SourceID 是**当天最后一条样本**的来源：一个还在陆续出实际值的日子
// （比如实时电价按小时陆续出）会被判成占位日而不算「实际值日」—— 这个保守判定是对的，
// 复盘/回测本来也只认 96 点齐全的实际值日。
func latestRealDate(days []DayRow, minPoints int) string {
	for i := len(days) - 1; i >= 0; i-- {
		if days[i].DayAhead.Count >= minPoints && IsRealSource(days[i].DayAhead.SourceID) {
			return days[i].Date
		}
	}
	return ""
}

func clampRange(from, to, earliest, latest string) (string, string) {
	loc := shanghaiLocation()
	if _, err := time.ParseInLocation(dateLayout, from, loc); err != nil {
		from = addDays(latest, -29)
		if from < earliest {
			from = earliest
		}
	}
	if _, err := time.ParseInLocation(dateLayout, to, loc); err != nil {
		to = latest
	}
	if to < from {
		from, to = to, from
	}
	fromT, err1 := time.ParseInLocation(dateLayout, from, loc)
	toT, err2 := time.ParseInLocation(dateLayout, to, loc)
	if err1 == nil && err2 == nil {
		if span := int(toT.Sub(fromT).Hours()/24) + 1; span > maxRangeDays {
			from = addDays(to, -(maxRangeDays - 1))
		}
	}
	return from, to
}

func addDays(date string, n int) string {
	t, err := time.ParseInLocation(dateLayout, date, shanghaiLocation())
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(dateLayout)
}

func (s *store) loadDisk() error {
	if s.metaPath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file cacheFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return err
	}
	s.days = file.Days
	s.offsets = file.Offsets
	if s.offsets == nil {
		s.offsets = map[string]int64{}
	}
	// 结构版本不对：丢掉逐日统计与偏移（它们是会随结构变化的），但保留 etag/时间戳。
	// 这样下一次 ensureFresh 会认为「需要重新解析」而直接从本地 CSV 重建（不重下 37MB）。
	if file.Version != cacheVersion {
		s.days = nil
		s.offsets = map[string]int64{}
	}
	s.etag = file.ETag
	s.lastModified = file.LastModified
	s.url = firstNonEmpty(file.URL, s.url)
	if t, err := time.Parse(time.RFC3339, file.FetchedAt); err == nil {
		s.fetchedAt = t
	}
	if s.curves == nil {
		s.curves = newCurveLRU(s.maxSize)
	}
	return nil
}

func (s *store) loadMeta() error {
	return s.loadDisk()
}

func (s *store) saveMeta() error {
	if s.metaPath == "" {
		return nil
	}
	s.mu.RLock()
	file := cacheFile{
		Version:      cacheVersion,
		URL:          s.url,
		ETag:         s.etag,
		LastModified: s.lastModified,
		FetchedAt:    s.fetchedAt.Format(time.RFC3339),
		Days:         s.days,
		Offsets:      s.offsets,
	}
	s.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(s.metaPath), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	tmp := s.metaPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath)
}

func (s *store) localCSVReady() bool {
	if s.cachePath == "" {
		return false
	}
	info, err := os.Stat(s.cachePath)
	return err == nil && info.Size() > 0
}

func (s *store) writeCSV(r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return err
	}
	tmp := s.cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, s.cachePath)
}

func (s *store) loadLocal(ctx context.Context) error {
	days, offsets, err := ingestFile(s.cachePath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.days = days
	s.offsets = offsets
	s.curves = newCurveLRU(s.maxSize)
	if s.fetchedAt.IsZero() {
		if info, statErr := os.Stat(s.cachePath); statErr == nil {
			s.fetchedAt = info.ModTime()
		} else {
			s.fetchedAt = time.Now()
		}
	}
	s.mu.Unlock()
	s.warmCurves()
	if err := s.saveMeta(); err != nil {
		c_log.Warningf(ctx, "电网日指标元数据写入失败: %+v", err)
	}
	return nil
}

func (s *store) warmCurves() {
	s.mu.RLock()
	days := s.days
	path := s.cachePath
	offsets := s.offsets
	s.mu.RUnlock()
	warmed := 0
	for i := len(days) - 1; i >= 0; i-- {
		if warmed >= 31 {
			break
		}
		s.mu.Lock()
		full := s.curves != nil && s.curves.Used() >= s.maxSize && s.curves.Len() > 0
		s.mu.Unlock()
		if full {
			break
		}
		date := days[i].Date
		off, ok := offsets[date]
		if !ok {
			continue
		}
		curve, err := readDayCurve(path, off, date)
		if err != nil {
			continue
		}
		s.mu.Lock()
		s.curves.Put(date, curve)
		s.mu.Unlock()
		warmed++
	}
}

func (s *store) getCurve(date string) (*DayCurve, error) {
	s.mu.Lock()
	if s.curves != nil {
		if curve, ok := s.curves.Get(date); ok {
			copied := curve
			s.mu.Unlock()
			return &copied, nil
		}
	}
	off, ok := s.offsets[date]
	path := s.cachePath
	s.mu.Unlock()
	if !ok || path == "" {
		return nil, nil
	}
	curve, err := readDayCurve(path, off, date)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.curves == nil {
		s.curves = newCurveLRU(s.maxSize)
	}
	s.curves.Put(date, curve)
	copied := curve
	s.mu.Unlock()
	return &copied, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type acc struct {
	sum, max, min, last float64
	count               int
	unit                string
	lastSource          int
	samples             []Sample
}

func newAcc() *acc {
	return &acc{max: math.Inf(-1), min: math.Inf(1)}
}

func (a *acc) add(timeStr string, value float64, unit string, sourceID int, keepSample bool) {
	if a.count == 0 {
		a.unit = unit
	}
	a.lastSource = sourceID
	a.sum += value
	a.last = value
	if value > a.max {
		a.max = value
	}
	if value < a.min {
		a.min = value
	}
	a.count++
	if keepSample {
		a.samples = append(a.samples, Sample{Time: timeStr, Value: value, SourceID: sourceID})
	}
}

func (a *acc) stat() SeriesStat {
	if a == nil || a.count == 0 {
		return SeriesStat{}
	}
	return SeriesStat{
		Avg:      round2(a.sum / float64(a.count)),
		Max:      round2(a.max),
		Min:      round2(a.min),
		Last:     round2(a.last),
		Count:    a.count,
		Unit:     a.unit,
		SourceID: a.lastSource,
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

type dayAcc struct {
	load, da, rt *acc
}

func (d *dayAcc) bucket(typeID int) *acc {
	switch typeID {
	case typeLoad:
		if d.load == nil {
			d.load = newAcc()
		}
		return d.load
	case typeDayAhead:
		if d.da == nil {
			d.da = newAcc()
		}
		return d.da
	case typeRealtime:
		if d.rt == nil {
			d.rt = newAcc()
		}
		return d.rt
	default:
		return nil
	}
}
