package homemarket

// gdCityCodes 广东省 21 个地级市的 GB/T 2260 行政区划代码，源站给的中文全名 → 编码。
//
// **为什么写死在代码里**：源站 marketEntitiesNum 只回中文 name（"东莞市"/"中山市"/…），
// 一个编码都不给；21 个地市是国务院批准的固定行政区划，不是配置项，也不值得为它建表。
// 写死后端就能对外给稳定编码（前端排序、跨系统对接都不该依赖中文名）。
//
// 维护口径：源站新增/改名不在表里时，采集器 **跳过并打 Warning**，不静默写空编码
// （空编码会撞唯一键 (run_date, city_code, period_type)，把 21 个城市挤成一行）。
var gdCityCodes = map[string]string{
	"广州市": "440100",
	"韶关市": "440200",
	"深圳市": "440300",
	"珠海市": "440400",
	"汕头市": "440500",
	"佛山市": "440600",
	"江门市": "440700",
	"湛江市": "440800",
	"茂名市": "440900",
	"肇庆市": "441200",
	"惠州市": "441300",
	"梅州市": "441400",
	"汕尾市": "441500",
	"河源市": "441600",
	"阳江市": "441700",
	"清远市": "441800",
	"东莞市": "441900",
	"中山市": "442000",
	"潮州市": "445100",
	"揭阳市": "445200",
	"云浮市": "445300",
}

// CityCodeOf 中文市名 → 行政区划编码。第二个返回值为 false 表示表里没有这个市
// （源站新增/改名），调用方应跳过该条并告警，不要写空编码。
func CityCodeOf(name string) (string, bool) {
	code, ok := gdCityCodes[name]
	return code, ok
}

// CityNameOf 编码 → 中文市名（回查用，例如展示已落库的历史行）。
func CityNameOf(code string) (string, bool) {
	for name, c := range gdCityCodes {
		if c == code {
			return name, true
		}
	}
	return "", false
}
