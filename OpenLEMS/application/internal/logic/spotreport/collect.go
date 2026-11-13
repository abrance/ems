package spotreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"common/c_log"

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
		// 归档尺寸必须是原图实际像素，不能信 HTML 元数据：同一篇文章里微信可能
		// 给出 1080 的展示宽度（data-w 与 picturePageInfoList 都会），而 /0 原图
		// 仍是 2381 宽。模板提取把 OCR 坐标除以归档尺寸做归一化，尺寸错了整页
		// 定位就偏（实测 2026-09-20 那篇第 4 页 1080 vs 实际 2381×3367）。
		width, height := imageSize(buf, img.Width, img.Height)
		pages = append(pages, &v1.SpotPage{
			Page:   img.Page,
			Label:  img.Label,
			URL:    publicURL,
			Key:    key,
			Width:  width,
			Height: height,
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
	scheduleAutoExtract(parsed.Date)
	return ToDTO(saved), nil
}

// scheduleAutoExtract 采集成功后异步触发解析（OCR + 模板提取 + 地图采样）。
//
// 4 页 OCR 约 6-10 秒，不能挂在采集请求上；用独立 context 是因为请求结束后 ctx 会被取消。
// 失败只影响数据视图（首页会提示去解析），不影响已采集的图片档案。
// force=false：覆盖采集同一天时仍保留已确认值（复核状态优先；官方直采与人工确认值任何情况下都不覆盖）。
func scheduleAutoExtract(runDate string) {
	if !autoExtractEnabled() {
		c_log.Infof(context.Background(), "自动解析已关闭（%s=off），跳过 %s", EnvAutoExtract, runDate)
		return
	}
	go func() {
		ctx := context.Background()
		if _, err := ExtractReport(ctx, runDate, false); err != nil {
			c_log.Warningf(ctx, "采集后自动解析失败（%s）：%v；可在信息管理里手动重跑", runDate, err)
			return
		}
		c_log.Infof(ctx, "采集后自动解析完成：%s", runDate)
	}()
}

// imageSize 取已下载原图的真实像素尺寸，解码失败时回落到 HTML 元数据。
func imageSize(buf []byte, fallbackWidth, fallbackHeight int) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return fallbackWidth, fallbackHeight
	}
	return cfg.Width, cfg.Height
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

// EnvAutoExtract 关闭自动解析的开关：设为 off/false/0 时，采集后不自动解析
const EnvAutoExtract = "SPOT_AUTO_EXTRACT"

func autoExtractEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAutoExtract))) {
	case "off", "false", "0", "no":
		return false
	}
	return true
}
