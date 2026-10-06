package v1

import "github.com/gogf/gf/v2/frame/g"

// GetPolicyConfigFieldsReq 获取策略配置的表单字段元数据
type GetPolicyConfigFieldsReq struct {
	g.Meta `path:"/policy/config-fields" method:"get" tags:"策略相关" summary:"策略配置的表单字段元数据"`
}

// ConfigField 配置字段的表单元数据（后端由 struct tag 反射得到，struct 是唯一真源）
type ConfigField struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Group       string   `json:"group"`
	Component   string   `json:"component"`
	ValueType   string   `json:"valueType"`
	Unit        string   `json:"unit,omitempty"`
	Default     string   `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Step        *float64 `json:"step,omitempty"`
}

// GetPolicyConfigFieldsRes 字段元数据 + 分组顺序
type GetPolicyConfigFieldsRes struct {
	Fields []*ConfigField `json:"fields" dc:"字段定义，按 struct 声明顺序"`
	Groups []string       `json:"groups" dc:"分组顺序（首次出现顺序）"`
}
