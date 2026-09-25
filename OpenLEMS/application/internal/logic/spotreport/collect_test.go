package spotreport

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// 归档尺寸必须取原图实际像素：同一篇文章里微信可能给 1080 的展示宽度
// （data-w 与 picturePageInfoList 都会），而 /0 原图是 2381×3367。
// 模板提取用归档尺寸做坐标归一化，尺寸错了整页定位就偏。
func TestImageSizePrefersRealPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1080, 1527))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试图失败：%v", err)
	}
	// HTML 元数据说 1080×1527，实际原图 2381×3367 → 应取实际值
	width, height := imageSize(buf.Bytes(), 2381, 3367)
	if width != 1080 || height != 1527 {
		t.Fatalf("应取原图实际尺寸 1080x1527，实际 %dx%d", width, height)
	}

	// 解码失败时回落元数据，不能返回 0×0（0 会让归一化除零）
	width, height = imageSize([]byte("not an image"), 2381, 3367)
	if width != 2381 || height != 3367 {
		t.Fatalf("解码失败应回落元数据，实际 %dx%d", width, height)
	}
}
