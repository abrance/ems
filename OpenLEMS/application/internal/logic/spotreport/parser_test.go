package spotreport

import "testing"

func TestParseDateFromTitle(t *testing.T) {
	info := ParseDateFromTitle("通报 | 广东电力现货市场2026年9月运行日报(09.21)")
	if info == nil {
		t.Fatal("expected date")
	}
	if info.Date != "2026-09-21" || info.Year != 2026 || info.Month != 9 || info.Day != 21 {
		t.Fatalf("unexpected date: %+v", info)
	}
	if ParseDateFromTitle("随便一篇文章") != nil {
		t.Fatal("expected nil")
	}
}

func TestAssertWechatURL(t *testing.T) {
	ok, err := AssertWechatURL("https://mp.weixin.qq.com/s/demo")
	if err != nil || ok == "" {
		t.Fatalf("expected wechat url, err=%v", err)
	}
	if _, err := AssertWechatURL("https://example.com/x"); err == nil {
		t.Fatal("expected host error")
	}
	if _, err := AssertWechatURL("not-a-url"); err == nil {
		t.Fatal("expected invalid url error")
	}
}

func TestSelectReportImages(t *testing.T) {
	images := []ParsedImage{
		{OriginURL: "header", Width: 800, Height: 200},
		{OriginURL: "p1", Width: 2381, Height: 3367},
		{OriginURL: "p2", Width: 2381, Height: 3367},
		{OriginURL: "p3", Width: 2381, Height: 3367},
		{OriginURL: "p4", Width: 2381, Height: 3367},
		{OriginURL: "footer", Width: 800, Height: 200},
	}
	got := SelectReportImages(images)
	if len(got) != 4 {
		t.Fatalf("want 4 pages, got %d", len(got))
	}
	if got[0].OriginURL != "p1" || got[3].OriginURL != "p4" {
		t.Fatalf("unexpected pages: %+v", got)
	}
}

func TestParseArticle(t *testing.T) {
	htmlBody := `
	<meta property="og:title" content="通报 | 广东电力现货市场2026年9月运行日报(09.21)">
	<div id="js_content">
	<img data-src="https://mmbiz.qpic.cn/header/640?wx_fmt=png" data-w="800" data-ratio="0.25">
	<img data-src="https://mmbiz.qpic.cn/p1/640?wx_fmt=png" data-w="2381" data-ratio="1.414">
	<img data-src="https://mmbiz.qpic.cn/p2/640?wx_fmt=png" data-w="2381" data-ratio="1.414">
	<img data-src="https://mmbiz.qpic.cn/p3/640?wx_fmt=png" data-w="2381" data-ratio="1.414">
	<img data-src="https://mmbiz.qpic.cn/p4/640?wx_fmt=png" data-w="2381" data-ratio="1.414">
	<img data-src="https://mmbiz.qpic.cn/footer/640?wx_fmt=png" data-w="800" data-ratio="0.25">
	</div>
	`
	article, err := ParseArticle(htmlBody, "https://mp.weixin.qq.com/s/demo")
	if err != nil {
		t.Fatal(err)
	}
	if article.Date != "2026-09-21" {
		t.Fatalf("date=%s", article.Date)
	}
	if len(article.Images) != 4 {
		t.Fatalf("pages=%d", len(article.Images))
	}
	if article.Images[0].Label != "市场概况与日前申报" {
		t.Fatalf("label=%s", article.Images[0].Label)
	}
	if article.Images[0].OriginURL != "https://mmbiz.qpic.cn/p1/0?wx_fmt=png" {
		t.Fatalf("url=%s", article.Images[0].OriginURL)
	}
}
