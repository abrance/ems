package policy

import (
	v1 "application/api/policy/v1"
	"context"
	"testing"
)

func TestGetPolicyConfigFields(t *testing.T) {
	res, err := (&ControllerV1{}).GetPolicyConfigFields(context.Background(), &v1.GetPolicyConfigFieldsReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Fields) == 0 {
		t.Fatal("字段列表为空")
	}
	if len(res.Groups) == 0 {
		t.Fatal("分组列表为空")
	}

	byKey := map[string]*v1.ConfigField{}
	for _, f := range res.Fields {
		byKey[f.Key] = f
		if f.Group == "" {
			t.Errorf("%s 没有分组，前端表单会没地方放", f.Key)
		}
	}

	// 储能参数页要靠这几个字段做保护校验
	for _, key := range []string{"socMinRatio", "socMaxRatio", "chargePowerRatio", "dischargePowerRatio", "demandTarget"} {
		f := byKey[key]
		if f == nil {
			t.Errorf("缺关键字段 %s", key)
			continue
		}
		if f.Component != "number" {
			t.Errorf("%s 应为 number 组件，实际 %s", key, f.Component)
		}
	}

	// 分组顺序按 struct 里首次出现的顺序，储能参数在最前
	if res.Groups[0] != "储能参数" {
		t.Errorf("分组顺序不对，首个应为「储能参数」，实际 %v", res.Groups)
	}
	// 分组不能重复
	seen := map[string]bool{}
	for _, g := range res.Groups {
		if seen[g] {
			t.Errorf("分组重复: %s", g)
		}
		seen[g] = true
	}
}
