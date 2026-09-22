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
// 关键安全行为：当图例与地图配色不一致时（实测 2026-09-18 那篇：图例是浅色系、
// 地图填充是饱和红），颜色匹配会全部失败 → 不预填任何值，交人工填写，
// 而不是按错误色板猜出一堆看似合理的档位。
const (
	DatasetNodePricePeak   = "node_price_peak"
	DatasetNodePriceValley = "node_price_valley"
	MetricPriceBucket      = "价格档位"
	// OriginCV 表示该值由图像采样预填，仍需人工确认
	OriginCV = "cv"
)

var (
	reLegendItem = regexp.MustCompile(`(?i)^[\[(]\s*(?:-?inf|\d+)\s*,\s*(?:\d+|\+?inf)\s*[\])]$`)
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
	unmatched := 0
	for _, line := range cities {
		color, share := dominantFillColor(img, line)
		if color == nil || share < 0.6 {
			unmatched++
			continue
		}
		idx, dist := nearestBucket(palette, *color)
		if idx < 0 || dist > 40 {
			unmatched++
			continue
		}
		out = append(out, CityBucket{
			City:   normalizeCity(line.Text),
			Bucket: idx + 1,
			Label:  palette[idx].Label,
			Conf:   math.Round(share*100) / 100,
		})
	}
	if unmatched > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"地图上有 %d 个地市未能判定档位（配色与图例不一致或标签跨越色块），已留空待人工填写", unmatched))
	}
	return out, warnings
}

// legendPalette 读图例：标签文本 + 左侧色块颜色，按 y 顺序即档位由低到高
func legendPalette(img image.Image, lines []OCRLine) []bucketColor {
	bounds := img.Bounds()
	w, h := float64(bounds.Dx()), float64(bounds.Dy())
	var items []legendItem
	for _, l := range lines {
		text := strings.Join(strings.Fields(l.Text), "")
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

func legendVerticalRange(lines []OCRLine, pageHeight float64) (float64, float64) {
	var tops, bottoms []float64
	for _, l := range lines {
		if reLegendItem.MatchString(strings.Join(strings.Fields(l.Text), "")) {
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
