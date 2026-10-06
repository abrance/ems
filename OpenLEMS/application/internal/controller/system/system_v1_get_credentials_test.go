package system

import (
	v1 "application/api/system/v1"
	"context"
	"strings"
	"testing"
)

func find(items []*v1.CredentialItem, name string) *v1.CredentialItem {
	for _, it := range items {
		if it.Name == name {
			return it
		}
	}
	return nil
}

func TestCredentialsStatusDoesNotLeakSecrets(t *testing.T) {
	t.Setenv(envQiniuAccessKey, "AKIA-SECRET-VALUE")
	t.Setenv(envQiniuSecretKey, "SUPER-SECRET-KEY")
	t.Setenv(envQiniuBucket, "tos-me")
	t.Setenv(envQiniuDomain, "http://file.example.com")
	t.Setenv(envOCRBaseURL, "http://model-ocr:8080")

	res, err := (&ControllerV1{}).GetCredentialsStatus(context.Background(), &v1.GetCredentialsStatusReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 密钥值绝不能出现在响应里
	for _, it := range res.Qiniu {
		if strings.Contains(it.Value, "SECRET") || strings.Contains(it.Value, "AKIA") {
			t.Errorf("%s 回显了密钥值：%q", it.Name, it.Value)
		}
	}
	if ak := find(res.Qiniu, "AccessKey"); ak == nil || !ak.Configured || ak.Source != "env" {
		t.Errorf("AccessKey 状态错误: %+v", ak)
	}
	if ak := find(res.Qiniu, "AccessKey"); ak != nil && ak.Value != "••••••" {
		t.Errorf("密钥应打码，实际 %q", ak.Value)
	}
	// 非敏感值照常展示（运维要靠它确认配的是哪个 bucket）
	if b := find(res.Qiniu, "Bucket"); b == nil || b.Value != "tos-me" {
		t.Errorf("Bucket 应回显非敏感值: %+v", b)
	}
	if res.StorageMode != "qiniu" {
		t.Errorf("密钥齐全时应为 qiniu，实际 %s", res.StorageMode)
	}
	if o := find(res.OCR, "服务地址"); o == nil || !o.Configured || o.Value != "http://model-ocr:8080" {
		t.Errorf("OCR 服务地址状态错误: %+v", o)
	}
}

func TestCredentialsStatusFallsBackToLocal(t *testing.T) {
	for _, k := range []string{envQiniuAccessKey, envQiniuSecretKey, envQiniuBucket, envQiniuDomain, envQiniuZone, envOCRBaseURL} {
		t.Setenv(k, "")
	}

	res, err := (&ControllerV1{}).GetCredentialsStatus(context.Background(), &v1.GetCredentialsStatusReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StorageMode != "local" {
		t.Errorf("缺密钥时应退回 local，实际 %s", res.StorageMode)
	}
	if ak := find(res.Qiniu, "AccessKey"); ak == nil || ak.Configured || ak.Source != "missing" {
		t.Errorf("缺配置时状态应为 missing: %+v", ak)
	}
	// zone 有默认值，不应被判成 missing
	if z := find(res.Qiniu, "Zone"); z == nil || !z.Configured || z.Value != "z2" {
		t.Errorf("Zone 应有默认值 z2: %+v", z)
	}
	if o := find(res.OCR, "服务地址"); o == nil || o.Configured {
		t.Errorf("OCR 未配置时应为未配置: %+v", o)
	}
}
