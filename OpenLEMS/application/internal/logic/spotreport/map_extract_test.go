package spotreport

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// 合成一张图例 + 地市色块的图，确定性地验证颜色采样逻辑（无需真实大图 fixture）。
//
// 图例 5 档色板与真实日报一致（浅绿/浅蓝/浅黄/浅橙/浅红），地市色块用其中几档填充。

var testPalette = []color.RGBA{
	{183, 214, 163, 255},
	{173, 205, 234, 255},
	{255, 255, 126, 255},
	{255, 224, 126, 255},
	{255, 126, 126, 255},
}

var testLegendLabels = []string{"[-inf,200)", "[200,400)", "[400,600)", "[600,800)", "[800,+inf)"}

type cityBox struct {
	name string
	box  image.Rectangle
}

func buildMapImage(fill []color.RGBA) (image.Image, []OCRLine, []cityBox) {
	const w, h = 1600, 2200
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	// 地市色块（先画，图例后画，互不重叠）
	boxes := []cityBox{
		{"韶关", image.Rect(200, 1200, 400, 1320)},
		{"清远", image.Rect(450, 1250, 650, 1370)},
		{"梅州", image.Rect(700, 1150, 900, 1270)},
		{"河源", image.Rect(950, 1300, 1150, 1420)},
	}
	for i, b := range boxes {
		c := fill[i%len(fill)]
		for y := b.box.Min.Y; y < b.box.Max.Y; y++ {
			for x := b.box.Min.X; x < b.box.Max.X; x++ {
				img.Set(x, y, c)
			}
		}
	}

	var lines []OCRLine
	// 图例：标签在 x=1400，左侧 1340~1390 是色块
	for i, label := range testLegendLabels {
		top := 1600 + i*70
		for y := top; y < top+50; y++ {
			for x := 1330; x < 1390; x++ {
				img.Set(x, y, testPalette[i])
			}
		}
		lines = append(lines, testOCRLine(label, 1400, float64(top), 150, 40))
	}
	// 地市标签放在各自色块中心
	for _, b := range boxes {
		cx := b.box.Min.X + b.box.Dx()/2
		cy := b.box.Min.Y + b.box.Dy()/2
		lines = append(lines, testOCRLine(b.name, float64(cx-40), float64(cy-20), 80, 40))
	}
	return img, lines, boxes
}

func testOCRLine(text string, left, top, w, h float64) OCRLine {
	var l OCRLine
	l.Text = text
	l.Confidence = 1
	l.BBox.Left = left
	l.BBox.Top = top
	l.BBox.Width = w
	l.BBox.Height = h
	return l
}

func TestSampleMapBuckets(t *testing.T) {
	fill := []color.RGBA{testPalette[4], testPalette[2], testPalette[0], testPalette[3]}
	img, lines, boxes := buildMapImage(fill)
	got, warnings := SampleMapBuckets(img, lines)
	if len(warnings) != 0 {
		t.Fatalf("不应有告警：%v", warnings)
	}
	if len(got) != len(boxes) {
		t.Fatalf("期望识别 %d 个地市，实际 %d：%+v", len(boxes), len(got), got)
	}
	want := map[string]int{"韶关": 5, "清远": 3, "梅州": 1, "河源": 4}
	for _, b := range got {
		if want[b.City] != b.Bucket {
			t.Fatalf("%s 档位期望 %d，实际 %d（%s）", b.City, want[b.City], b.Bucket, b.Label)
		}
		if b.Label == "" || !strings.HasPrefix(b.Label, "[") {
			t.Fatalf("%s 档位区间文本缺失：%q", b.City, b.Label)
		}
		if b.Conf <= 0 {
			t.Fatalf("%s 采样置信度缺失", b.City)
		}
	}
}

// 配色与图例不一致时必须拒绝预填（实测 2026-09-18 那篇就是这种情况：
// 图例是浅色系、地图填充是饱和红），而不是按错误色板猜出一堆看似合理的档位。
func TestSampleMapBucketsRejectsMismatchedPalette(t *testing.T) {
	saturated := []color.RGBA{
		{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 0, 255},
	}
	img, lines, _ := buildMapImage(saturated)
	got, warnings := SampleMapBuckets(img, lines)
	if len(got) != 0 {
		t.Fatalf("配色不一致时不应预填，实际得到 %+v", got)
	}
	if len(warnings) == 0 {
		t.Fatal("配色不一致时应给出告警")
	}
}

// 图例缺失时也不预填
func TestSampleMapBucketsWithoutLegend(t *testing.T) {
	img, lines, _ := buildMapImage([]color.RGBA{testPalette[0]})
	var cityOnly []OCRLine
	for _, l := range lines {
		if !strings.HasPrefix(l.Text, "[") {
			cityOnly = append(cityOnly, l)
		}
	}
	got, warnings := SampleMapBuckets(img, cityOnly)
	if len(got) != 0 || len(warnings) == 0 {
		t.Fatalf("无图例应不预填并告警，got=%+v warnings=%v", got, warnings)
	}
}

// 同一地市高峰档位低于低谷档位应产生告警
func TestValidateDetectsBucketInversion(t *testing.T) {
	res := &ExtractResult{
		RunDate: "2026-09-21",
		Prose:   map[string]*float64{},
		Metrics: []Metric{
			{Dataset: DatasetNodePricePeak, Scope: "阳江", Metric: MetricPriceBucket, Value: 3, Conf: 1},
			{Dataset: DatasetNodePriceValley, Scope: "阳江", Metric: MetricPriceBucket, Value: 4, Conf: 1},
		},
	}
	warnings := validate(res)
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "阳江") && strings.Contains(w, "低于低谷档位") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未检出档位倒挂：%v", warnings)
	}
}
