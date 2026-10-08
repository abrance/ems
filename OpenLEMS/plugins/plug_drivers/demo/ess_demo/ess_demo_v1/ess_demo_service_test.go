package ess_demo_v1

import (
	"reflect"
	"testing"

	"common/c_enum"
)

// 手动控制页的可下发指令全部来自 build.yaml 的 service 段，而执行时是反射
// 按 key 找方法（common/c_base.ExecuteCustomService）。所以「声明」和「实现」
// 必须严格对齐：声明了没实现 → 点下去才报 service not found；实现了没声明 →
// 页面上根本看不到。这里把两边钉在一起。
func TestServicesMatchDeclarations(t *testing.T) {
	info := GetDriverInfo()
	if info == nil {
		t.Fatal("driver info is nil")
	}
	if len(info.Service) == 0 {
		t.Fatal("build.yaml 没有声明任何 service，手动控制页会显示「无可用服务」")
	}

	typ := reflect.TypeOf(&sEssDemo{})
	for _, s := range info.Service {
		if s.Key == "" {
			t.Errorf("service %q 缺少 key（key 就是反射调用的方法名）", s.Name)
			continue
		}
		method, ok := typ.MethodByName(s.Key)
		if !ok {
			t.Errorf("service %s 在 build.yaml 里声明了，但没有对应方法", s.Key)
			continue
		}
		// 参数按位置下发，个数必须与方法入参一致，否则反射调用会错位。
		// Type.MethodByName 拿到的 func 签名带接收者，所以要减 1。
		if got, want := len(s.Params), method.Type.NumIn()-1; got != want {
			t.Errorf("service %s 声明了 %d 个参数，方法要 %d 个", s.Key, got, want)
		}
		for _, p := range s.Params {
			if p.Key == "" {
				t.Errorf("service %s 有参数缺少 key", s.Key)
			}
			switch p.ValueType {
			case c_enum.EConfigFieldsValueTypeString,
				c_enum.EConfigFieldsValueTypeInt,
				c_enum.EConfigFieldsValueTypeFloat,
				c_enum.EConfigFieldsValueTypeBoolean:
			default:
				t.Errorf("service %s 参数 %s 的 value_type=%q 前端不认（只能 string/int/float/bool）", s.Key, p.Key, p.ValueType)
			}
			if p.ComponentType == "" {
				t.Errorf("service %s 参数 %s 缺少 component_type", s.Key, p.Key)
			}
		}
	}
}

// 人工设定功率是手动控制的核心指令，单独钉一下它的声明口径（单位 kW、
// 带上下限、必填），因为它直接决定页面上那个数字输入框怎么渲染。
func TestSetPowerServiceDeclaration(t *testing.T) {
	info := GetDriverInfo()
	for _, s := range info.Service {
		if s.Key != "CustomSetPowerService" {
			continue
		}
		if len(s.Params) != 1 {
			t.Fatalf("CustomSetPowerService 应该有 1 个参数，实际 %d", len(s.Params))
		}
		p := s.Params[0]
		if p.ValueType != c_enum.EConfigFieldsValueTypeFloat {
			t.Errorf("功率参数应是 float，实际 %q", p.ValueType)
		}
		if p.Unit == nil || *p.Unit != "kW" {
			t.Errorf("功率参数单位应是 kW，实际 %v", p.Unit)
		}
		if !p.Required {
			t.Error("功率参数应必填")
		}
		if p.Min == nil || p.Max == nil {
			t.Errorf("功率参数应有上下限，实际 min=%v max=%v", p.Min, p.Max)
		}
		return
	}
	t.Fatal("build.yaml 没有声明 CustomSetPowerService")
}
