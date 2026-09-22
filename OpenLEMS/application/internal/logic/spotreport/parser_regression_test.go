package spotreport

import "testing"

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
