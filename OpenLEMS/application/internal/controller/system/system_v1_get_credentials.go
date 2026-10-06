package system

import (
	v1 "application/api/system/v1"
	"context"
	"os"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
)

// 与 internal/logic/spotreport 的读取顺序保持一致：
// 环境变量优先，其次配置文件；OCR 只认环境变量。
const (
	envQiniuAccessKey = "QINIU_ACCESS_KEY"
	envQiniuSecretKey = "QINIU_SECRET_KEY"
	envQiniuBucket    = "QINIU_BUCKET"
	envQiniuDomain    = "QINIU_DOMAIN"
	envQiniuZone      = "QINIU_ZONE"
	envOCRBaseURL     = "OCR_BASE_URL"
)

// GetCredentialsStatus 返回凭据与外部服务的**配置状态**。
//
// 只回答「配了没有、从哪来」，不回显密钥值 —— 前端也没有改凭据的接口，
// 因为密钥存在服务端配置文件/环境变量里，改了要重启，不该做成一个网页表单。
func (c *ControllerV1) GetCredentialsStatus(ctx context.Context, req *v1.GetCredentialsStatusReq) (res *v1.GetCredentialsStatusRes, err error) {
	item := func(name, envKey, cfgKey string, sensitive bool) *v1.CredentialItem {
		envVal := strings.TrimSpace(os.Getenv(envKey))
		if envVal != "" {
			return &v1.CredentialItem{Name: name, Configured: true, Source: "env", Value: maskIf(sensitive, envVal)}
		}
		if cfgKey != "" {
			cfgVal := strings.TrimSpace(g.Cfg().MustGet(ctx, cfgKey).String())
			if cfgVal != "" {
				return &v1.CredentialItem{Name: name, Configured: true, Source: "config", Value: maskIf(sensitive, cfgVal)}
			}
		}
		return &v1.CredentialItem{Name: name, Source: "missing"}
	}

	zone := item("Zone", envQiniuZone, "qiniu.zone", false)
	if !zone.Configured {
		zone = &v1.CredentialItem{Name: "Zone", Configured: true, Source: "default", Value: "z2"}
	}

	qiniu := []*v1.CredentialItem{
		item("AccessKey", envQiniuAccessKey, "qiniu.accessKey", true),
		item("SecretKey", envQiniuSecretKey, "qiniu.secretKey", true),
		item("Bucket", envQiniuBucket, "qiniu.bucket", false),
		item("访问域名", envQiniuDomain, "qiniu.domain", false),
		zone,
	}

	ocr := []*v1.CredentialItem{item("服务地址", envOCRBaseURL, "", false)}

	mode := "local"
	if allConfigured(qiniu) {
		mode = "qiniu"
	}

	return &v1.GetCredentialsStatusRes{
		StorageMode: mode,
		Qiniu:       qiniu,
		OCR:         ocr,
		Note: "密钥保存在服务端配置文件或环境变量里，改后需重启；页面只读，不提供在线修改。" +
			"密钥值一律不回显。",
	}, nil
}

func maskIf(sensitive bool, v string) string {
	if sensitive {
		return "••••••"
	}
	return v
}

func allConfigured(items []*v1.CredentialItem) bool {
	for _, it := range items {
		if !it.Configured {
			return false
		}
	}
	return true
}
