package spotreport

import (
	"strings"
	"testing"
)

// 复现线上 2026-09-21 那篇文章（/s/pxdJ0CyMva1evGOKsCpE0w）的结构：
//
//	页眉横幅在正文 DOM 里出现两次（两张图 CDN 地址的 token 不同），其中重复出现的那张
//	不在 picturePageInfoList 里，因此既没有 data-w/data-ratio，也无法从图片列表里补尺寸。
//	合并后得到 7 张图，其中 1 张宽高为 0，导致 allSized 判定为 false，兜底分支按
//	images[1:len-1] 切出 5 张，最终报「文章中没有识别到日报图片」。
const articleWithDuplicateHeader = `
<meta property="og:title" content="通报 | 广东电力现货市场2026年9月运行日报(09.21)">
<div id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/640?wx_fmt=png" data-w="1080" data-ratio="0.2685">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8lSMlJpBUiaxnQHHS0a71uGIln10IUf/640?wx_fmt=png">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8kpxxnoWGWtr95novqyFarOibbnLquBxqQMnPk/640?wx_fmt=png">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8m1GVv9gVvtCwKzjImsMFAIPGbKtPfnl0bQTowN2/640?wx_fmt=png">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8mTrgmrw6A9KdadGfAm7ibRHqljiahxMic6k4xiau68/640?wx_fmt=png">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsnVj/640?wx_fmt=png" data-w="1080" data-ratio="0.5278">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9GficYY23ZYlftmofDg/640?wx_fmt=png">
</div>
<script>
var picturePageInfoList = "[{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":290},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8lSMlJpBUiaxnQHHS0a71uGIln10IUfQ/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8kpxxnoWGWtr95novqyFarOibbnLquBxqQMnPk/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/QIl6ABY7A8m1GVv9gVvtCwKzjImsMFAIPGbKtPfnl0bQTowN2/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8mTrgmrw6A9KdadGfAm7ibRHqljiahxMic6k4xiau68/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsnVj/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":570}]";
</script>
`

// 页眉重复出现且不在 picturePageInfoList 中时，仍应选出 4 张竖版日报页。
func TestSelectReportImagesWithUnsizedNoise(t *testing.T) {
	images := []ParsedImage{
		{OriginURL: "header", Width: 1080, Height: 290},
		{OriginURL: "p1", Width: 2381, Height: 3367},
		{OriginURL: "p2", Width: 2381, Height: 3367},
		{OriginURL: "p3", Width: 2381, Height: 3367},
		{OriginURL: "p4", Width: 2381, Height: 3367},
		{OriginURL: "footer", Width: 1080, Height: 570},
		{OriginURL: "header-dup"}, // DOM 中重复的页眉：拿不到尺寸
	}
	got := SelectReportImages(images)
	if len(got) != 4 {
		t.Fatalf("want 4 pages, got %d: %+v", len(got), got)
	}
	if got[0].OriginURL != "p1" || got[3].OriginURL != "p4" {
		t.Fatalf("unexpected pages: %+v", got)
	}
}

func TestParseArticleWithDuplicateHeader(t *testing.T) {
	article, err := ParseArticle(articleWithDuplicateHeader, "https://mp.weixin.qq.com/s/pxdJ0CyMva1evGOKsCpE0w")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if article.Date != "2026-09-21" {
		t.Fatalf("date=%s", article.Date)
	}
	if len(article.Images) != 4 {
		t.Fatalf("pages=%d", len(article.Images))
	}
	wantLabels := []string{"市场概况与日前申报", "日前成交与高峰节点电价", "低谷节点电价与价格曲线", "实时交易电价"}
	for i, img := range article.Images {
		if img.Page != i+1 || img.Label != wantLabels[i] {
			t.Fatalf("page[%d] = page:%d label:%s", i, img.Page, img.Label)
		}
		if img.Width != 2381 || img.Height != 3367 {
			t.Fatalf("page[%d] 尺寸应为 2381x3367，实际 %dx%d", i, img.Width, img.Height)
		}
		if img.OriginURL == "" {
			t.Fatalf("page[%d] 缺少图片地址", i)
		}
	}
}

// 复现线上 2026-09-20 那篇文章（/s/NITM406hoeuLthyQcdkYdg）的结构：4 页日报里
// 有一页在正文 DOM 里是 1080 宽（data-w=1080，picturePageInfoList 也写 1080×1527），
// 而 /0 原图实际是 2381×3367（已实测）。旧实现用「宽度 ≥ 1500」筛日报页：
//   - 三个 2381 页通过、那一个 1080 页被漏掉 → 第一段只得 3 页
//   - 又因为末尾有一张没尺寸的图，allSized=false，走到 images[1:len-1] 切出 5 张
//
// 两条路都凑不出 4 张，最终报「文章中没有识别到日报图片」。
const articleWithMixedPageWidth = `
<meta property="og:title" content="通报 | 广东电力现货市场2026年9月运行日报(09.20)">
<div id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/640?wx_fmt=png" data-w="1080" data-ratio="0.26851851">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8m7aQDE5r7oTbUNymhCaf5SI06g4ue9qYicfA/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8n01NGTXeJgGOlqyZCNfWRWB8gDC1fOlVLGwB/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8n3QGGB6d0xiaHdG6swcZLo6oJnRTlk676WHC/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8lUBAKr7eMUYPdCaSDCkJTib1yojkD6hziavagD/640?wx_fmt=png" data-w="1080" data-ratio="1.41388888">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsE/640?wx_fmt=png" data-w="1080" data-ratio="0.52777777">
<img data-src="http://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9GficYY23ZYlftmofF/0?wx_fmt=png">
</div>
<script>
var picturePageInfoList = "[{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":290},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8m7aQDE5r7oTbUNymhCaf5SI06g4ue9qYicfA/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":1527},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8n01NGTXeJgGOlqyZCNfWRWB8gDC1fOlVLGwB/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":1527},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8n3QGGB6d0xiaHdG6swcZLo6oJnRTlk676WHC/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":1527},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/QIl6ABY7A8lUBAKr7eMUYPdCaSDCkJTib1yojkD6hziavagD/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":1527},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsE/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":570}]";
</script>
`

// 页面宽度元数据混着 1080/2381 时，仍应按「竖版长图」选出 4 页日报。
func TestSelectReportImagesWithMixedPageWidth(t *testing.T) {
	images := extractImages(articleWithMixedPageWidth)
	got := SelectReportImages(images)
	if len(got) != 4 {
		t.Fatalf("want 4 pages, got %d: %+v", len(got), got)
	}
	for i, img := range got {
		if ratio := float64(img.Height) / float64(img.Width); ratio < reportPageAspect {
			t.Fatalf("第 %d 页不像日报页（%dx%d，比值 %.2f）", i+1, img.Width, img.Height, ratio)
		}
	}
}

func TestParseArticleWithMixedPageWidth(t *testing.T) {
	article, err := ParseArticle(articleWithMixedPageWidth, "https://mp.weixin.qq.com/s/NITM406hoeuLthyQcdkYdg")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if article.Date != "2026-09-20" {
		t.Fatalf("date=%s", article.Date)
	}
	if len(article.Images) != 4 {
		t.Fatalf("pages=%d", len(article.Images))
	}
	// 第 4 页是那页 1080 宽的：DOM 里带 data-w=1080，归档尺寸由采集侧实测原图覆盖
	for i, img := range article.Images {
		if img.Page != i+1 {
			t.Fatalf("page 序号错位：%d", img.Page)
		}
		if img.Width == 0 || img.Height == 0 {
			t.Fatalf("page[%d] 尺寸缺失", i)
		}
	}
}

// 复现线上 2026-09-29 补采（报 code 53「文章中没有识别到日报图片」）的结构：
// 4 页日报里第 4 页在 picturePageInfoList 里的 width/height 都是 0，正文 DOM 里也没有
// data-w/data-ratio（微信偶尔就是这么给的）。旧实现按 `img.Width <= 0` 直接把这一页
// 跳过，只剩 3 个候选，又因为「候选 > 0 就返回」而不走兜底，上层凑不满 4 页直接报错。
const articleWithUnsizedReportPage = `
<meta property="og:title" content="通报 | 广东电力现货市场2026年9月运行日报(09.29)">
<div id="js_content">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/640?wx_fmt=png" data-w="1080" data-ratio="0.26851851">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageOneAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageTwoAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageThreeAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/640?wx_fmt=png" data-w="2381" data-ratio="1.41411171">
<img data-src="https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageFourAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/640?wx_fmt=png">
<img data-src="https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsE/640?wx_fmt=png" data-w="1080" data-ratio="0.52777777">
</div>
<script>
var picturePageInfoList = "[{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8yTc1Xmlt2Uv36NibR2KLh9WTVjv/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":290},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageOneAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageTwoAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageThreeAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":2381,\"height\":3367},{\"cdn_url\":\"https://mmbiz.qpic.cn/sz_mmbiz_png/0929pageFourAaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPp/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":0,\"height\":0},{\"cdn_url\":\"https://mmbiz.qpic.cn/mmbiz_png/0EVF5ArhZk8cPHNTtib35oyYH2COnywfXA2ImRPnBzDibPdsE/0?wx_fmt=png\x26amp;from=appmsg\",\"width\":1080,\"height\":570}]";
</script>
`

// 4 页日报里 1 页缺尺寸元数据时，仍必须选出 4 张日报页，且顺序与正文 DOM 一致。
func TestSelectReportImagesWithUnsizedReportPage(t *testing.T) {
	images := extractImages(articleWithUnsizedReportPage)
	if len(images) != 6 {
		t.Fatalf("用例本身应合并出 6 张图（页眉 + 4 页日报 + 页脚），实际 %d: %+v", len(images), images)
	}
	got := SelectReportImages(images)
	if len(got) != 4 {
		t.Fatalf("want 4 pages, got %d: %+v", len(got), got)
	}
	wantTokens := []string{"0929pageOne", "0929pageTwo", "0929pageThree", "0929pageFour"}
	for i, want := range wantTokens {
		if !strings.Contains(got[i].OriginURL, want) {
			t.Fatalf("第 %d 页 URL 顺序不对：want 含 %q，实际 %q", i+1, want, got[i].OriginURL)
		}
	}
	// 前 3 页有元数据、第 4 页没有：缺尺寸不影响它被选中，仍按 DOM 顺序排在第 4。
	if got[3].Width != 0 || got[3].Height != 0 {
		t.Fatalf("第 4 页本应缺元数据，实际 %dx%d", got[3].Width, got[3].Height)
	}
}
