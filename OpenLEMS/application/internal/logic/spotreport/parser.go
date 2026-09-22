package spotreport

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var pageLabels = []string{
	"市场概况与日前申报",
	"日前成交与高峰节点电价",
	"低谷节点电价与价格曲线",
	"实时交易电价",
}

var (
	reYearMonth   = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月`)
	reMonthDay    = regexp.MustCompile(`[（(]\s*(\d{1,2})\s*[.．]\s*(\d{1,2})\s*[）)]`)
	reOgTitle     = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	reOgTitleAlt  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:title["']`)
	reActivity    = regexp.MustCompile(`(?is)id=["']activity-name["'][^>]*>(.*?)</`)
	reMsgTitle    = regexp.MustCompile(`var\s+msg_title\s*=\s*"([\s\S]*?)"\s*;`)
	reHTMLTitle   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reAuthor      = regexp.MustCompile(`(?i)<meta[^>]+name=["']author["'][^>]+content=["']([^"']+)["']`)
	reJSName      = regexp.MustCompile(`(?is)id=["']js_name["'][^>]*>(.*?)</`)
	reImgTag      = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reDataSrc     = regexp.MustCompile(`(?i)(?:data-src|src)=["']([^"']+)["']`)
	reDataW       = regexp.MustCompile(`(?i)data-w=["'](\d+)["']`)
	reDataRatio   = regexp.MustCompile(`(?i)data-ratio=["']([0-9.]+)["']`)
	rePictureList = regexp.MustCompile(`picturePageInfoList\s*=\s*"([\s\S]*?)"\s*;`)
	reStripTags   = regexp.MustCompile(`<[^>]+>`)
)

type ParsedImage struct {
	OriginURL string
	Width     int
	Height    int
	Page      int
	Label     string
}

type ParsedArticle struct {
	Title     string
	Author    string
	SourceURL string
	Year      int
	Month     int
	Day       int
	Date      string
	Images    []ParsedImage
}

type DateInfo struct {
	Year  int
	Month int
	Day   int
	Date  string
}

func AssertWechatURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("只支持微信公众号文章链接")
	}
	if parsed.Hostname() != "mp.weixin.qq.com" {
		return "", fmt.Errorf("只支持微信公众号文章链接")
	}
	return parsed.String(), nil
}

func ParseArticle(htmlBody, sourceURL string) (*ParsedArticle, error) {
	title := firstNonEmpty(
		metaContent(htmlBody, reOgTitle),
		metaContent(htmlBody, reOgTitleAlt),
		innerText(htmlBody, reActivity),
		jsString(htmlBody, reMsgTitle),
		innerText(htmlBody, reHTMLTitle),
	)
	if title == "" {
		return nil, fmt.Errorf("未能读取文章标题，请确认链接可公开访问")
	}
	dateInfo := ParseDateFromTitle(title)
	if dateInfo == nil {
		return nil, fmt.Errorf("标题未包含运行日报日期：%s", title)
	}
	images := SelectReportImages(extractImages(htmlBody))
	if len(images) != 4 {
		return nil, fmt.Errorf("文章中没有识别到日报图片")
	}
	for i := range images {
		images[i].Page = i + 1
		images[i].Label = pageLabels[i]
	}
	author := firstNonEmpty(metaContent(htmlBody, reAuthor), innerText(htmlBody, reJSName), "广东电力交易中心")
	return &ParsedArticle{
		Title:     title,
		Author:    author,
		SourceURL: sourceURL,
		Year:      dateInfo.Year,
		Month:     dateInfo.Month,
		Day:       dateInfo.Day,
		Date:      dateInfo.Date,
		Images:    images,
	}, nil
}

func ParseDateFromTitle(title string) *DateInfo {
	yearMonth := reYearMonth.FindStringSubmatch(title)
	md := reMonthDay.FindStringSubmatch(title)
	if yearMonth == nil || md == nil {
		return nil
	}
	year, _ := strconv.Atoi(yearMonth[1])
	month, _ := strconv.Atoi(yearMonth[2])
	day, _ := strconv.Atoi(md[2])
	if year == 0 || month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}
	return &DateInfo{
		Year:  year,
		Month: month,
		Day:   day,
		Date:  fmt.Sprintf("%04d-%02d-%02d", year, month, day),
	}
}

func SelectReportImages(images []ParsedImage) []ParsedImage {
	if len(images) == 0 {
		return images
	}
	allSized := true
	for _, img := range images {
		if img.Width == 0 || img.Height == 0 {
			allSized = false
			break
		}
	}
	if allSized {
		var reports []ParsedImage
		for _, img := range images {
			if img.Height > img.Width && img.Width >= 1500 {
				reports = append(reports, img)
			}
		}
		if len(reports) >= 4 {
			return reports[:4]
		}
		if len(reports) > 0 {
			return reports
		}
	}
	// 正文 DOM 里常出现重复的页眉/页脚图片：它们不在 picturePageInfoList 中，
	// 拿不到宽高，会让 allSized 判定为 false。只要已带尺寸的竖版长图够 4 张，
	// 就直接采用，避免落到下面按张数切片的兜底分支——切出来的张数不等于 4 时
	// ParseArticle 会误报「文章中没有识别到日报图片」。
	var portrait []ParsedImage
	for _, img := range images {
		if img.Width > 0 && img.Width >= 1500 && img.Height > img.Width {
			portrait = append(portrait, img)
		}
	}
	if len(portrait) >= 4 {
		return portrait[:4]
	}
	if len(images) >= 6 {
		return images[1 : len(images)-1]
	}
	if len(images) >= 4 {
		return images[:4]
	}
	return images
}

func NormalizeImageURL(raw string) string {
	out := strings.ReplaceAll(raw, `\x26amp;`, "&")
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "/640?", "/0?")
	if strings.HasPrefix(out, "http://") {
		out = "https://" + strings.TrimPrefix(out, "http://")
	}
	return out
}

func extractImages(htmlBody string) []ParsedImage {
	fromDom := extractDomImages(htmlBody)
	fromList := parsePictureList(htmlBody)
	if len(fromDom) == 0 {
		return fromList
	}
	if len(fromList) == 0 {
		return fromDom
	}
	merged := make([]ParsedImage, 0, len(fromDom))
	for i, img := range fromDom {
		hit := findSameImage(fromList, img.OriginURL)
		if hit == nil && i < len(fromList) {
			hit = &fromList[i]
		}
		if hit != nil {
			if img.Width == 0 {
				img.Width = hit.Width
			}
			if img.Height == 0 {
				img.Height = hit.Height
			}
		}
		merged = append(merged, img)
	}
	return merged
}

func extractDomImages(htmlBody string) []ParsedImage {
	var images []ParsedImage
	for _, tag := range reImgTag.FindAllString(htmlBody, -1) {
		srcMatch := reDataSrc.FindStringSubmatch(tag)
		if srcMatch == nil {
			continue
		}
		src := NormalizeImageURL(html.UnescapeString(srcMatch[1]))
		if !strings.Contains(src, "mmbiz.qpic.cn") {
			continue
		}
		width := 0
		height := 0
		if wMatch := reDataW.FindStringSubmatch(tag); wMatch != nil {
			width, _ = strconv.Atoi(wMatch[1])
		}
		if rMatch := reDataRatio.FindStringSubmatch(tag); rMatch != nil {
			ratio, _ := strconv.ParseFloat(rMatch[1], 64)
			if width > 0 && ratio > 0 {
				height = int(float64(width)*ratio + 0.5)
			}
		}
		images = append(images, ParsedImage{OriginURL: src, Width: width, Height: height})
	}
	return images
}

func parsePictureList(htmlBody string) []ParsedImage {
	match := rePictureList.FindStringSubmatch(htmlBody)
	if match == nil {
		return nil
	}
	raw := match[1]
	raw = strings.ReplaceAll(raw, `\x26amp;`, "&")
	raw = strings.ReplaceAll(raw, "&amp;", "&")
	raw = strings.ReplaceAll(raw, `\"`, `"`)
	raw = strings.ReplaceAll(raw, `'`, `"`)
	reTrailingComma := regexp.MustCompile(`,(\s*])`)
	raw = reTrailingComma.ReplaceAllString(raw, "$1")
	var list []map[string]any
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	var images []ParsedImage
	for _, item := range list {
		cdn, _ := item["cdn_url"].(string)
		origin := NormalizeImageURL(cdn)
		if origin == "" {
			continue
		}
		images = append(images, ParsedImage{
			OriginURL: origin,
			Width:     jsonInt(item["width"]),
			Height:    jsonInt(item["height"]),
		})
	}
	return images
}

func findSameImage(images []ParsedImage, target string) *ParsedImage {
	want := stripQuery(target)
	for i := range images {
		if stripQuery(images[i].OriginURL) == want {
			return &images[i]
		}
	}
	return nil
}

func stripQuery(raw string) string {
	base := strings.Split(raw, "?")[0]
	base = strings.TrimSuffix(base, "/0")
	base = strings.TrimSuffix(base, "/640")
	return strings.TrimSuffix(base, "/")
}

func jsonInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func metaContent(htmlBody string, re *regexp.Regexp) string {
	match := re.FindStringSubmatch(htmlBody)
	if match == nil {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func innerText(htmlBody string, re *regexp.Regexp) string {
	match := re.FindStringSubmatch(htmlBody)
	if match == nil {
		return ""
	}
	text := reStripTags.ReplaceAllString(match[1], "")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

func jsString(htmlBody string, re *regexp.Regexp) string {
	match := re.FindStringSubmatch(htmlBody)
	if match == nil {
		return ""
	}
	out := strings.ReplaceAll(match[1], `\"`, `"`)
	out = strings.ReplaceAll(out, "&amp;", "&")
	return strings.TrimSpace(html.UnescapeString(out))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
