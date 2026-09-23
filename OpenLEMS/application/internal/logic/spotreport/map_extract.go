package spotreport

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strings"
)

// 地市节点电价分布图（图2-1 高峰 / 图2-2 低谷）的分档采样。
//
// 这两张图的数值只存在于像素里（choropleth 填色），OCR 拿不到，因此用颜色采样：
//  1. 图例：形如 [-inf,200) … [800,+inf) 的标签，在标签左侧按比例取一条竖线，
//     切出连续色块 → 得到「档位文本 → RGB」的色板（色板随图读取，不写死）
//  2. 城市：OCR 已识别出地市名，取其标签窗口内出现最多的「有饱和度且非黑」颜色
//  3. 匹配：颜色需贴近色板某档，且该颜色在窗口内占比够高（否则标签跨在色块边界上）
//
// 两段匹配：
//  1. 严格 RGB 匹配（距离 < 40）：图例色板与地图填充一致时使用，置信度 = 采样占比
//  2. 色相回退：图例与地图配色不一致时（实测 2026-09-18：图例浅色系、地图饱和红），
//     按色系（绿/蓝/黄/橙/红）匹配，容忍饱和度与明度差异，置信度打折并给出告警
//
// 无论哪一段，都要求采样占比 ≥ 0.6（标签跨在色块边界上时留空）。
const (
	DatasetNodePricePeak   = "node_price_peak"
	DatasetNodePriceValley = "node_price_valley"
	MetricPriceBucket      = "价格档位"
	// OriginCV 表示该值由图像采样预填，仍需人工确认
	OriginCV = "cv"
)

var (
	reLegendItem = regexp.MustCompile(`(?i)^[\[(]\s*(?:-?inf|\d+)\s*,\s*(?:\d+|\+?inf)\s*[\])]$`)
	// OCR 会把图例里的连字符识别成各种 Unicode 变体（实测 2026-09-16 那篇是 U+2212 / U+2013），
	// 括号也可能变成全角；不归一化就会「图例未识别」而整篇不预填
	legendTextReplacer = strings.NewReplacer(
		"\u2212", "-", "\u2013", "-", "\u2014", "-", "\uFF0D", "-", "\u2010", "-", "\uFE63", "-",
		"（", "(", "）", ")", "［", "[", "］", "]", "【", "[", "】", "]",
	)
	// GuangdongCities 广东省 21 个地级市，用于从 OCR 结果里挑出地图标签
	GuangdongCities = []string{
		"广州", "深圳", "珠海", "汕头", "佛山", "韶关", "湛江", "肇庆", "江门", "茂名",
		"惠州", "梅州", "汕尾", "河源", "阳江", "清远", "东莞", "中山", "潮州", "揭阳", "云浮",
	}
)

type legendItem struct {
	Label string
	Left  float64
	Top   float64
	W     float64
	H     float64
}

type bucketColor struct {
	Label string
	R     int
	G     int
	B     int
}

// CityBucket 一个地市的价格档位
type CityBucket struct {
	City   string
	Bucket int // 1..N，越大电价越高
	Label  string
	Conf   float64
}

// SampleMapBuckets 从一张地图页的 OCR 结果 + 图像中采样各地市所属档位。
// 返回空切片表示无法可靠判定（色板缺失或配色不一致），调用方应提示人工填写。
func SampleMapBuckets(img image.Image, lines []OCRLine) ([]CityBucket, []string) {
	var warnings []string
	palette := legendPalette(img, lines)
	if len(palette) == 0 {
		return nil, []string{"地图图例未识别，未预填地市电价档位"}
	}
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())

	legendTop, legendBottom := legendVerticalRange(lines, h)
	cities := mapCityLines(lines, w, h, legendTop, legendBottom)
	if len(cities) == 0 {
		return nil, []string{"地图上未识别到地市标签，未预填地市电价档位"}
	}

	var out []CityBucket
	unmatched, viaHue := 0, 0
	for _, line := range cities {
		color, share := dominantFillColor(img, line)
		if color == nil || share < 0.6 {
			unmatched++
			continue
		}
		idx, dist := nearestBucket(palette, *color)
		if idx >= 0 && dist <= 40 {
			out = append(out, CityBucket{
				City:   normalizeCity(line.Text),
				Bucket: idx + 1,
				Label:  palette[idx].Label,
				Conf:   math.Round(share*100) / 100,
			})
			continue
		}
		// 图例与地图配色不一致：退化为色相（色系）匹配，置信度打折
		if idx, hueDist := nearestBucketByHue(palette, *color); idx >= 0 && hueDist <= 40 {
			viaHue++
			out = append(out, CityBucket{
				City:   normalizeCity(line.Text),
				Bucket: idx + 1,
				Label:  palette[idx].Label,
				Conf:   math.Round(share*70) / 100,
			})
			continue
		}
		unmatched++
	}
	if viaHue > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"该图填充色与图例色板不一致，%d 个地市档位由色系推断（低置信，已标记待人工确认）", viaHue))
	}
	if unmatched > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"地图上有 %d 个地市未能判定档位（标签跨越色块边界），已留空", unmatched))
	}
	return out, warnings
}

// legendPalette 读图例：标签文本 + 左侧色块颜色，按 y 顺序即档位由低到高
func legendPalette(img image.Image, lines []OCRLine) []bucketColor {
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())
	var items []legendItem
	for _, l := range lines {
		text := normalizeLegendText(l.Text)
		if !reLegendItem.MatchString(text) {
			continue
		}
		items = append(items, legendItem{
			Label: text, Left: l.BBox.Left, Top: l.BBox.Top, W: l.BBox.Width, H: l.BBox.Height,
		})
	}
	if len(items) == 0 {
		return nil
	}
	// 图例项应当同列（排除表格里被误读成 [ 开头的单元格）
	sort.Slice(items, func(i, j int) bool { return items[i].Left < items[j].Left })
	medianX := items[len(items)/2].Left
	filtered := items[:0]
	for _, it := range items {
		if math.Abs(it.Left-medianX) < 40*(w/2381) {
			filtered = append(filtered, it)
		}
	}
	items = filtered
	sort.Slice(items, func(i, j int) bool { return items[i].Top < items[j].Top })

	scale := w / 2381
	y0 := int(items[0].Top - 30*scale)
	y1 := int(items[len(items)-1].Top + items[len(items)-1].H + 30*scale)
	if y0 < 0 {
		y0 = 0
	}
	if float64(y1) > h {
		y1 = int(h)
	}
	// 色块在标签左侧；不同分辨率的偏移量不同，逐个候选宽度试
	for _, offset := range []float64{30, 38, 46, 54, 62, 70, 78} {
		x := int(items[0].Left - offset*scale)
		if x < 0 {
			continue
		}
		runs := colorRuns(img, x, y0, y1)
		if len(runs) != len(items) || hasDuplicateColor(runs) {
			continue
		}
		out := make([]bucketColor, len(items))
		for i := range items {
			out[i] = bucketColor{Label: items[i].Label, R: runs[i].R, G: runs[i].G, B: runs[i].B}
		}
		return out
	}
	return nil
}

type rgb struct{ R, G, B int }

func colorRuns(img image.Image, x, y0, y1 int) []rgb {
	var runs []rgb
	prev := rgb{-1, -1, -1}
	start := y0
	for y := y0; y < y1; y++ {
		c := at(img, x, y)
		key := c.quantize()
		if key != prev {
			if prev.R >= 0 && y-start >= 12 {
				mid := at(img, x, (start+y)/2)
				if mid.saturated() {
					runs = append(runs, mid)
				}
			}
			prev, start = key, y
		}
	}
	if prev.R >= 0 && y1-start >= 12 {
		mid := at(img, x, (start+y1)/2)
		if mid.saturated() {
			runs = append(runs, mid)
		}
	}
	return runs
}

func hasDuplicateColor(runs []rgb) bool {
	seen := map[rgb]bool{}
	for _, r := range runs {
		if seen[r] {
			return true
		}
		seen[r] = true
	}
	return false
}

// dominantFillColor 标签窗口内出现最多的高饱和色及其占比
func dominantFillColor(img image.Image, line OCRLine) (*rgb, float64) {
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())
	scale := w / 2381
	padX := line.BBox.Width*0.45 + 20*scale
	padY := line.BBox.Height*0.75 + 20*scale
	x0 := int(math.Max(0, line.BBox.Left-padX))
	x1 := int(math.Min(w, line.BBox.Left+line.BBox.Width+padX))
	y0 := int(math.Max(0, line.BBox.Top-padY))
	y1 := int(math.Min(h, line.BBox.Top+line.BBox.Height+padY))

	counts := map[rgb]int{}
	total := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := at(img, x, y)
			if !c.saturated() {
				continue
			}
			counts[c]++
			total++
		}
	}
	if total == 0 {
		return nil, 0
	}
	best, bestN := rgb{}, 0
	for c, n := range counts {
		if n > bestN {
			best, bestN = c, n
		}
	}
	return &best, float64(bestN) / float64(total)
}

func nearestBucket(palette []bucketColor, c rgb) (int, float64) {
	bestIdx, bestDist := -1, math.MaxFloat64
	for i, p := range palette {
		d := math.Sqrt(float64((p.R-c.R)*(p.R-c.R) + (p.G-c.G)*(p.G-c.G) + (p.B-c.B)*(p.B-c.B)))
		if d < bestDist {
			bestIdx, bestDist = i, d
		}
	}
	return bestIdx, bestDist
}

// nearestBucketByHue 按色相（色系）匹配档位：图例与地图配色不一致时的回退。
// 绿/蓝/黄/橙/红 五个色系在两套配色里一致，只有饱和度与明度不同。
func nearestBucketByHue(palette []bucketColor, c rgb) (int, float64) {
	target := rgbHue(c)
	bestIdx, bestDist := -1, math.MaxFloat64
	for i, p := range palette {
		d := hueDistance(target, rgbHue(rgb{p.R, p.G, p.B}))
		if d < bestDist {
			bestIdx, bestDist = i, d
		}
	}
	return bestIdx, bestDist
}

// rgbHue 返回色相角（0..360）；灰度像素返回 -1
func rgbHue(c rgb) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	delta := max - min
	if delta == 0 {
		return -1
	}
	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/delta, 6)
	case g:
		h = (b-r)/delta + 2
	default:
		h = (r-g)/delta + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

func hueDistance(a, b float64) float64 {
	if a < 0 || b < 0 {
		return math.MaxFloat64
	}
	d := math.Abs(a - b)
	if d > 180 {
		d = 360 - d
	}
	return d
}

// normalizeLegendText 归一化图例文本：去空白 + 统一连字符/括号变体，便于正则匹配
func normalizeLegendText(text string) string {
	return legendTextReplacer.Replace(strings.Join(strings.Fields(text), ""))
}

func legendVerticalRange(lines []OCRLine, pageHeight float64) (float64, float64) {
	var tops, bottoms []float64
	for _, l := range lines {
		if reLegendItem.MatchString(normalizeLegendText(l.Text)) {
			tops = append(tops, l.BBox.Top)
			bottoms = append(bottoms, l.BBox.Top+l.BBox.Height)
		}
	}
	if len(tops) == 0 {
		return 0, pageHeight
	}
	sort.Float64s(tops)
	sort.Float64s(bottoms)
	return tops[0], bottoms[len(bottoms)-1]
}

// mapCityLines 取与图例同一张图的地市标签（按 y 相对图例的位置过滤，排除上方表格）
func mapCityLines(lines []OCRLine, w, h, legendTop, legendBottom float64) []OCRLine {
	var out []OCRLine
	for _, l := range lines {
		city := normalizeCity(l.Text)
		if city == "" {
			continue
		}
		y := l.BBox.Top + l.BBox.Height/2
		if y < legendTop-0.30*h || y > legendBottom+0.10*h {
			continue
		}
		l.Text = city
		out = append(out, l)
	}
	return out
}

// normalizeCity 把 OCR 文本归一化成地市名（去空白、去括号等噪声）
func normalizeCity(text string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), "")
	clean = strings.Trim(clean, "()（）[]【】")
	for _, city := range GuangdongCities {
		if clean == city {
			return city
		}
	}
	return ""
}

func at(img image.Image, x, y int) rgb {
	b := img.Bounds()
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		return rgb{}
	}
	r, g, bl, _ := img.At(x, y).RGBA()
	return rgb{int(r >> 8), int(g >> 8), int(bl >> 8)}
}

func (c rgb) quantize() rgb {
	return rgb{c.R / 10 * 10, c.G / 10 * 10, c.B / 10 * 10}
}

// saturated 有颜色（排除白底与黑字/描边）
func (c rgb) saturated() bool {
	mx, mn := c.R, c.R
	for _, v := range []int{c.G, c.B} {
		if v > mx {
			mx = v
		}
		if v < mn {
			mn = v
		}
	}
	return mx-mn > 30 && mx >= 70
}
