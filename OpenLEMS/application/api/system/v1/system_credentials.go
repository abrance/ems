package v1

import "github.com/gogf/gf/v2/frame/g"

// GetCredentialsStatusReq 凭据与外部服务状态
type GetCredentialsStatusReq struct {
	g.Meta `path:"/system/credentials" method:"get" tags:"系统相关" summary:"凭据与外部服务配置状态（不回显密钥）" role:"admin"`
}

// CredentialItem 单项凭据状态：只说有没有、从哪来，不回显值
type CredentialItem struct {
	Name       string `json:"name" dc:"配置项名称"`
	Configured bool   `json:"configured" dc:"是否已配置"`
	Source     string `json:"source" dc:"来源：env / config / missing"`
	Value      string `json:"value,omitempty" dc:"非敏感值（如 bucket、域名、服务地址）"`
}

// GetCredentialsStatusRes 凭据状态
type GetCredentialsStatusRes struct {
	// StorageMode qiniu=密钥齐全走七牛；local=缺密钥退回本地
	StorageMode string            `json:"storageMode" dc:"存储方式：qiniu / local"`
	Qiniu       []*CredentialItem `json:"qiniu" dc:"七牛配置项状态"`
	OCR         []*CredentialItem `json:"ocr" dc:"OCR 配置项状态"`
	// Note 说明为什么这里不能改
	Note string `json:"note" dc:"说明"`
}
