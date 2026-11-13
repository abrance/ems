package spotreport

import (
	"context"
	"fmt"

	v1 "application/api/spotreport/v1"
)

// RecognizePages 把已归档的日报页下载下来跑一遍 OCR，返回可直接喂给 ExtractFromPages 的输入。
//
// 单独抽出来给「日报核对」（logic/reportdaily）复用：那条链路只要模板提取结果，
// 不要地市档位的图像采样、不要写库、也不要 ExtractReport 的复核语义。
// 与 ExtractReport 共用同一份下载/尺寸逻辑（imageSize 以实际图片为准，
// 归档尺寸历史上信过 HTML 元数据，偏小一倍会让整页坐标偏移）。
func RecognizePages(ctx context.Context, pages []*v1.SpotPage) ([]PageInput, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("日报没有可用页面")
	}
	client, err := NewOCRClientFromEnv()
	if err != nil {
		return nil, err
	}
	inputs := make([]PageInput, 0, len(pages))
	for _, page := range pages {
		imageBytes, err := downloadImage(ctx, page.URL)
		if err != nil {
			return nil, err
		}
		result, err := client.Recognize(ctx, imageBytes)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页识别失败：%v", page.Page, err)
		}
		width, height := imageSize(imageBytes, page.Width, page.Height)
		inputs = append(inputs, PageInput{
			Width:  float64(width),
			Height: float64(height),
			Lines:  result.Results,
		})
	}
	return inputs, nil
}
