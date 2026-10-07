package internal

import (
	"testing"

	"common/c_enum"
)

// 枚举 → 存储标签必须与消费侧的解析互逆。
//
// 这里曾经写的是 string(currentSegment.PriceType)：int 枚举被当成码点转字符，
// Valley 变成 "\x01"，存储侧 getPriceTypeValue → ParsePriceType 匹配不上任何分支，
// 一律取默认的「平电」——于是所有落库的电价类型都是平电，且没有任何报错。
func TestPriceTypeLabelRoundTrips(t *testing.T) {
	all := []c_enum.EPriceType{
		c_enum.EPriceTypeValley,
		c_enum.EPriceTypePeak,
		c_enum.EPriceTypeFlat,
		c_enum.EPriceTypeSharp,
		c_enum.EPriceTypeDeepValley,
	}
	for _, want := range all {
		label := priceTypeLabel(want)
		// 复现历史 bug：string(枚举) 得到的是单字符控制符，这里必须不是它
		if label == string(rune(int(want))) {
			t.Errorf("%v 的标签是单字符控制符 %q，说明又写成了 string(枚举)", want, label)
		}
		if got := c_enum.ParsePriceType(label); got != want {
			t.Errorf("%v → 标签 %q → 解析回 %v，不互逆（消费侧会把它当成别的类型）", want, label, got)
		}
	}

	// 把历史 bug 的后果钉住：string(枚举) 经消费侧解析后等于平电
	if got := c_enum.ParsePriceType(string(rune(int(c_enum.EPriceTypeValley)))); got != c_enum.EPriceTypeFlat {
		t.Logf("提示：消费侧对无法识别的标签取默认 %v", got)
	}
}
