package policy

import (
	v1 "application/api/policy/v1"
	"context"

	"p_policy_mircogrid"
)

// GetPolicyConfigFields 返回储能/微电网策略配置的表单字段元数据。
//
// 这些元数据（名称、组件、上下限、单位、分组）本来就写在 SPolicyMircogridConfig
// 的 struct tag 里。前端不复制一份，统一从这里取，struct 改动会自动反映到表单上。
func (c *ControllerV1) GetPolicyConfigFields(ctx context.Context, req *v1.GetPolicyConfigFieldsReq) (res *v1.GetPolicyConfigFieldsRes, err error) {
	fields := p_policy_mircogrid.ConfigFields()
	res = &v1.GetPolicyConfigFieldsRes{
		Fields: make([]*v1.ConfigField, 0, len(fields)),
		Groups: make([]string, 0, 8),
	}

	seen := map[string]bool{}
	for _, f := range fields {
		res.Fields = append(res.Fields, &v1.ConfigField{
			Key:         f.Key,
			Name:        f.Name,
			Description: f.Description,
			Group:       f.Group,
			Component:   f.Component,
			ValueType:   f.ValueType,
			Unit:        f.Unit,
			Default:     f.Default,
			Min:         f.Min,
			Max:         f.Max,
			Step:        f.Step,
		})
		if f.Group != "" && !seen[f.Group] {
			seen[f.Group] = true
			res.Groups = append(res.Groups, f.Group)
		}
	}
	return res, nil
}
