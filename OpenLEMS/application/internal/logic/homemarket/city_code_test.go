package homemarket

import "testing"

// gdCityCodes 是硬编码表，它错了不会有任何编译或运行错误 —— 只会让某个城市
// 采集时被静默跳过（或在补表时把两个城市写成同一个编码，撞唯一键丢一行）。
//
// 这里钉三件事：
//  1. 广东省 21 个地级市一个不少（少一个 = 该市永远没数据）；
//  2. 编码两两不同（重复 = 两城市互相覆盖）；
//  3. 编码是合法的 44 开头 6 位数字（广东省前缀）。
func TestGdCityCodesCovers21Cities(t *testing.T) {
	// 广东省 21 个地级市，按行政区划代码顺序
	wantCities := []string{
		"广州市", "韶关市", "深圳市", "珠海市", "汕头市", "佛山市", "江门市",
		"湛江市", "茂名市", "肇庆市", "惠州市", "梅州市", "汕尾市", "河源市",
		"阳江市", "清远市", "东莞市", "中山市", "潮州市", "揭阳市", "云浮市",
	}
	if len(gdCityCodes) != 21 {
		t.Fatalf("映射表有 %d 个城市，期望 21", len(gdCityCodes))
	}
	for _, name := range wantCities {
		if _, ok := CityCodeOf(name); !ok {
			t.Errorf("缺城市 %q（该市会被采集器静默跳过）", name)
		}
	}

	seen := make(map[string]string, len(gdCityCodes))
	for name, code := range gdCityCodes {
		if len(code) != 6 {
			t.Errorf("%s 的编码 %q 不是 6 位", name, code)
		}
		if code[:2] != "44" {
			t.Errorf("%s 的编码 %q 不是广东省（44 前缀）", name, code)
		}
		if prev, dup := seen[code]; dup {
			t.Errorf("编码 %q 被两个城市共用：%s / %s（会撞唯一键丢数据）", code, prev, name)
		}
		seen[code] = name
	}

	// 编码与名字必须能互相查回去
	for name, code := range gdCityCodes {
		gotName, ok := CityNameOf(code)
		if !ok || gotName != name {
			t.Errorf("CityNameOf(%q) = %q/%v，期望 %q", code, gotName, ok, name)
		}
	}
	// 不认识的名字不能凭空造编码
	if _, ok := CityCodeOf("横琴新区"); ok {
		t.Error("未知地市不该返回编码（空编码会撞唯一键）")
	}
	if _, ok := CityCodeOf(""); ok {
		t.Error("空名字不该返回编码")
	}
}
