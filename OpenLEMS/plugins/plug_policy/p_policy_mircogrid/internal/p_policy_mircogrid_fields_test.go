package internal

import (
	"reflect"
	"testing"
)

func TestConfigFieldsCoverage(t *testing.T) {
	fields := ConfigFields()
	if len(fields) == 0 {
		t.Fatal("不该返回空字段列表")
	}

	byKey := map[string]*SConfigField{}
	for _, f := range fields {
		if f.Key == "" {
			t.Errorf("字段 key 为空: %+v", f)
		}
		if _, dup := byKey[f.Key]; dup {
			t.Errorf("字段 key 重复: %s", f.Key)
		}
		byKey[f.Key] = f
		if f.Component == "" {
			t.Errorf("%s 缺 component（ct tag）", f.Key)
		}
		if f.ValueType == "" {
			t.Errorf("%s 缺 valueType（vt tag）", f.Key)
		}
		if f.Group == "" {
			t.Errorf("%s 缺 group tag，前端表单会没地方放", f.Key)
		}
		if f.Name == "" {
			t.Errorf("%s 缺 name tag", f.Key)
		}
	}

	// struct 里声明了 ct 的字段必须全部被反射出来，否则前端表单会缺项
	st := reflect.TypeOf(SPolicyMircogridConfig{})
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.Tag.Get("ct") == "" {
			continue
		}
		if _, ok := byKey[jsonKeyOf(f)]; !ok {
			t.Errorf("struct 字段 %s 带了 ct tag 却没被反射出来", f.Name)
		}
	}
}

func TestConfigFieldsKeyValues(t *testing.T) {
	byKey := map[string]*SConfigField{}
	for _, f := range ConfigFields() {
		byKey[f.Key] = f
	}

	// 储能参数页要用到的几个关键字段，逐个核对元数据
	soc := byKey["socMaxRatio"]
	if soc == nil {
		t.Fatal("缺 socMaxRatio")
	}
	if soc.Component != "number" || soc.ValueType != "float" {
		t.Errorf("socMaxRatio 类型错误: %s / %s", soc.Component, soc.ValueType)
	}
	if soc.Min == nil || *soc.Min != 0 || soc.Max == nil || *soc.Max != 1 {
		t.Errorf("socMaxRatio 上下限错误: %v ~ %v", soc.Min, soc.Max)
	}
	if soc.Default != "0.95" || soc.Group != "储能参数" {
		t.Errorf("socMaxRatio 默认值/分组错误: %q / %q", soc.Default, soc.Group)
	}

	demand := byKey["demandTarget"]
	if demand == nil || demand.Unit != "" || demand.Group != "需量控制" {
		t.Errorf("demandTarget 元数据错误: %+v", demand)
	}
	if demand.Max != nil {
		t.Errorf("demandTarget 没声明 max，不该凭空补: %v", *demand.Max)
	}

	switchField := byKey["enableDemandControl"]
	if switchField == nil || switchField.Component != "switch" || switchField.ValueType != "bool" {
		t.Errorf("开关类字段解析错误: %+v", switchField)
	}
	if switchField.Min != nil {
		t.Errorf("开关不该有 min: %v", *switchField.Min)
	}

	transformer := byKey["transformerCapacity"]
	if transformer == nil || transformer.Unit != "kVA" {
		t.Errorf("transformerCapacity 单位错误: %+v", transformer)
	}

	// 分组覆盖：储能参数 / 峰谷套利 / 需量控制 这三个是储能参数页的主分组
	for _, g := range []string{"储能参数", "峰谷套利", "需量控制"} {
		found := false
		for _, f := range byKey {
			if f.Group == g {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("分组 %s 下没有任何字段", g)
		}
	}
}
