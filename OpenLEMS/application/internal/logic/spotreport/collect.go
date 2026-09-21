package spotreport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	v1 "application/api/spotreport/v1"
	"s_db"
	"s_db/s_db_model"

	"github.com/gogf/gf/v2/os/gtime"
)

const wechatUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

func shanghaiLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func Yesterday() string {
	now := time.Now().In(shanghaiLocation())
	return now.AddDate(0, 0, -1).Format("2006-01-02")
}

func ToDTO(model *s_db_model.SSpotReportModel) *v1.SpotReport {
	if model == nil {
		return nil
	}
	var pages []*v1.SpotPage
	_ = json.Unmarshal([]byte(model.Pages), &pages)
	collected := ""
	if model.CollectedAt != nil {
		collected = model.CollectedAt.Time.In(shanghaiLocation()).Format(time.RFC3339)
	}
	return &v1.SpotReport{
		Date:        model.RunDate,
		Title:       model.Title,
		SourceURL:   model.SourceURL,
		PageCount:   len(pages),
		Pages:       pages,
		CollectedAt: collected,
	}
}

func Collect(ctx context.Context, rawURL string) (*v1.SpotReport, error) {
	sourceURL, err := AssertWechatURL(rawURL)
	if err != nil {
		return nil, err
	}
	htmlBody, err := fetchHTML(ctx, sourceURL)
	if err != nil {
		return nil, err
	}
	parsed, err := ParseArticle(htmlBody, sourceURL)
	if err != nil {
		return nil, err
	}
	cfg := loadQiniuConfig(ctx)
	if !cfg.ready() {
		return nil, fmt.Errorf("对象存储未配置")
	}
	pages := make([]*v1.SpotPage, 0, len(parsed.Images))
	for _, img := range parsed.Images {
		buf, err := downloadImage(ctx, img.OriginURL)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("gd-spot/%s/page-%d.png", parsed.Date, img.Page)
		publicURL, err := uploadToQiniu(ctx, cfg, key, buf)
		if err != nil {
			return nil, err
		}
		pages = append(pages, &v1.SpotPage{
			Page:   img.Page,
			Label:  img.Label,
			URL:    publicURL,
			Key:    key,
			Width:  img.Width,
			Height: img.Height,
		})
	}
	pagesJSON, err := json.Marshal(pages)
	if err != nil {
		return nil, err
	}
	now := gtime.Now()
	model := &s_db_model.SSpotReportModel{
		RunDate:     parsed.Date,
		Title:       parsed.Title,
		SourceURL:   parsed.SourceURL,
		Pages:       string(pagesJSON),
		CollectedAt: now,
		UpdatedAt:   now,
	}
	if err := s_db.GetSpotReportService().Upsert(ctx, model); err != nil {
		return nil, err
	}
	saved, err := s_db.GetSpotReportService().GetByDate(ctx, parsed.Date)
	if err != nil {
		return nil, err
	}
	return ToDTO(saved), nil
}

func fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", wechatUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Referer", "https://mp.weixin.qq.com/")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("拉取文章失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("拉取文章失败：HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", wechatUA)
	req.Header.Set("Referer", "https://mp.weixin.qq.com/")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败：%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
