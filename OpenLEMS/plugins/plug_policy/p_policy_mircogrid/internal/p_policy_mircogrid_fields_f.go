package internal

import (
	"reflect"
	"strconv"
)

// SConfigField 配置字段的表单元数据。
//
// 为什么不把前端表单写死：字段名、类型、上下限、单位、分组全部已经写在
// SPolicyMircogridConfig 的 struct tag 里（name / desc / ct / vt / min / max /
// default / unit / step / group）。再在前端抄一份必然漂移，所以用反射把它们暴露出去，
// struct 就是唯一真源。
type SConfigField struct {
	Key         string   `json:"key"`         // 配置项 key（JSON 字段名）
	Name        string   `json:"name"`        // 展示名
	Description string   `json:"description"` // 说明
	Group       string   `json:"group"`       // 表单分组
	Component   string   `json:"component"`   // 组件类型：number / switch / singleSelect / multiSelect / text
	ValueType   string   `json:"valueType"`   // 值类型：float / int / bool / string
	Unit        string   `json:"unit,omitempty"`
	Default     string   `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
}

// ConfigFields 反射 SPolicyMircogridConfig，返回带分组与校验元数据的字段列表。
// 只返回同时声明了 json 与 ct 的字段：没有 ct 的字段是内部计算/运行态，不该出现在表单里。
func ConfigFields() []*SConfigField {
	t := reflect.TypeOf(SPolicyMircogridConfig{})
	fields := make([]*SConfigField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		component := f.Tag.Get("ct")
		key := jsonKeyOf(f)
		if key == "" || component == "" || key == "-" {
			continue
		}
		fields = append(fields, &SConfigField{
			Key:         key,
			Name:        f.Tag.Get("name"),
			Description: f.Tag.Get("desc"),
			Group:       f.Tag.Get("group"),
			Component:   component,
			ValueType:   f.Tag.Get("vt"),
			Unit:        f.Tag.Get("unit"),
			Default:     f.Tag.Get("default"),
			Min:         floatTag(f, "min"),
			Max:         floatTag(f, "max"),
			Step:        floatTag(f, "step"),
		})
	}
	return fields
}

// jsonKeyOf 取 json tag 的字段名部分（去掉 omitempty 之类的选项）。
func jsonKeyOf(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	name := tag
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' {
			name = tag[:i]
			break
		}
	}
	if name == "" {
		return f.Name
	}
	return name
}

// floatTag 解析数值型 tag；缺失或非法返回 nil（表示不限制）。
func floatTag(f reflect.StructField, key string) *float64 {
	raw := f.Tag.Get(key)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}
