package v1

import "github.com/gogf/gf/v2/frame/g"

type SpotPage struct {
	Page   int    `json:"page" dc:"页码"`
	Label  string `json:"label" dc:"页标签"`
	URL    string `json:"url" dc:"图片地址"`
	Key    string `json:"key" dc:"对象存储键"`
	Width  int    `json:"width" dc:"宽度"`
	Height int    `json:"height" dc:"高度"`
}

type SpotReport struct {
	Date        string      `json:"date" dc:"运行日 YYYY-MM-DD"`
	Title       string      `json:"title" dc:"文章标题"`
	SourceURL   string      `json:"sourceUrl" dc:"微信文章链接"`
	PageCount   int         `json:"pageCount" dc:"页数"`
	Pages       []*SpotPage `json:"pages" dc:"日报原图"`
	CollectedAt string      `json:"collectedAt" dc:"采集时间"`
}

type ListSpotReportReq struct {
	g.Meta `path:"/spot-report" method:"get" tags:"现货日报" summary:"获取现货日报列表" role:"user"`
}

type ListSpotReportRes struct {
	List []*SpotReport `json:"list" dc:"日报列表"`
}

type GetHomeSpotReportReq struct {
	g.Meta `path:"/spot-report/home" method:"get" tags:"现货日报" summary:"获取首页默认现货日报" role:"user"`
}

type GetHomeSpotReportRes struct {
	Empty  bool        `json:"empty" dc:"档案是否为空"`
	Date   string      `json:"date" dc:"当前运行日"`
	Dates  []string    `json:"dates" dc:"可切换运行日"`
	Report *SpotReport `json:"report" dc:"当前日报"`
}

type GetSpotReportDetailReq struct {
	g.Meta `path:"/spot-report/detail" method:"get" tags:"现货日报" summary:"获取指定运行日现货日报" role:"user"`
	Date   string `json:"date" dc:"运行日 YYYY-MM-DD" v:"required"`
}

type GetSpotReportDetailRes struct {
	Report *SpotReport `json:"report" dc:"日报详情"`
}

type CollectSpotReportReq struct {
	g.Meta `path:"/spot-report/collect" method:"post" tags:"现货日报" summary:"采集微信现货运行日报" role:"admin"`
	URL    string `json:"url" dc:"微信公众号文章链接" v:"required"`
}

type CollectSpotReportRes struct {
	Report *SpotReport `json:"report" dc:"采集结果"`
}
