package spotreport

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type qiniuConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Domain    string
	Zone      string
}

func loadQiniuConfig(ctx context.Context) qiniuConfig {
	cfg := qiniuConfig{
		AccessKey: firstNonEmpty(os.Getenv("QINIU_ACCESS_KEY"), g.Cfg().MustGet(ctx, "qiniu.accessKey").String()),
		SecretKey: firstNonEmpty(os.Getenv("QINIU_SECRET_KEY"), g.Cfg().MustGet(ctx, "qiniu.secretKey").String()),
		Bucket:    firstNonEmpty(os.Getenv("QINIU_BUCKET"), g.Cfg().MustGet(ctx, "qiniu.bucket").String()),
		Domain:    firstNonEmpty(os.Getenv("QINIU_DOMAIN"), g.Cfg().MustGet(ctx, "qiniu.domain").String()),
		Zone:      firstNonEmpty(os.Getenv("QINIU_ZONE"), g.Cfg().MustGet(ctx, "qiniu.zone").String(), "z2"),
	}
	cfg.Domain = strings.TrimRight(cfg.Domain, "/")
	return cfg
}

func (c qiniuConfig) ready() bool {
	return c.AccessKey != "" && c.SecretKey != "" && c.Bucket != "" && c.Domain != ""
}

func uploadHost(zone string) string {
	switch strings.ToLower(strings.TrimSpace(zone)) {
	case "z1":
		return "https://upload-z1.qiniup.com"
	case "z2":
		return "https://upload-z2.qiniup.com"
	case "na0":
		return "https://upload-na0.qiniup.com"
	case "as0":
		return "https://upload-as0.qiniup.com"
	default:
		return "https://upload.qiniup.com"
	}
}

func makeUploadToken(cfg qiniuConfig, key string, deadline int64) string {
	policy := fmt.Sprintf(`{"scope":"%s:%s","deadline":%d}`, cfg.Bucket, key, deadline)
	encodedPolicy := base64.URLEncoding.EncodeToString([]byte(policy))
	mac := hmac.New(sha1.New, []byte(cfg.SecretKey))
	_, _ = mac.Write([]byte(encodedPolicy))
	encodedSign := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return cfg.AccessKey + ":" + encodedSign + ":" + encodedPolicy
}

func uploadToQiniu(ctx context.Context, cfg qiniuConfig, key string, data []byte) (string, error) {
	token := makeUploadToken(cfg, key, time.Now().Add(time.Hour).Unix())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("token", token)
	_ = writer.WriteField("key", key)
	part, err := writer.CreateFormFile("file", key)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadHost(cfg.Zone), &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("七牛上传失败：HTTP %d %s", resp.StatusCode, string(raw))
	}
	return cfg.Domain + "/" + key, nil
}
