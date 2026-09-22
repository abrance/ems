package spotreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// OCR 服务（apps/model-ocr，PP-OCR + MNN）客户端。
//
// 地址由 OCR_BASE_URL 提供，默认走 cops-shared 共享网络里的 http://model-ocr:8080；
// 服务只绑宿主机回环，容器内访问不到 127.0.0.1:9101，所以必须用容器名。
const (
	EnvOCRBaseURL       = "OCR_BASE_URL"
	EnvOCRTimeoutSecond = "OCR_TIMEOUT_SECONDS"
	DefaultOCRModel     = "v6small"
	ocrMaxImageBytes    = 20 << 20
)

// OCRLine 一行识别结果，BBox 为原图像素坐标
type OCRLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	BBox       struct {
		Left   float64 `json:"left"`
		Top    float64 `json:"top"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"bbox"`
}

// OCRResult /ocr 响应
type OCRResult struct {
	Success bool      `json:"success"`
	Results []OCRLine `json:"results"`
	TimeMs  float64   `json:"time_ms"`
	Model   string    `json:"model"`
	Error   *string   `json:"error"`
}

// OCRClient OCR 服务客户端
type OCRClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewOCRClientFromEnv 从环境变量构造客户端；OCR_BASE_URL 未配置时返回错误，
// 调用方应把它转成可读提示（日报采集本身不依赖 OCR）。
func NewOCRClientFromEnv() (*OCRClient, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(EnvOCRBaseURL)), "/")
	if base == "" {
		return nil, fmt.Errorf("未配置 OCR 服务地址（%s），无法解析日报数据", EnvOCRBaseURL)
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("OCR 服务地址无效：%v", err)
	}
	timeout := 60
	if raw := strings.TrimSpace(os.Getenv(EnvOCRTimeoutSecond)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			timeout = n
		}
	}
	return &OCRClient{
		baseURL: base,
		model:   DefaultOCRModel,
		http:    &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}, nil
}

// BaseURL 便于日志与健康检查
func (c *OCRClient) BaseURL() string { return c.baseURL }

// Model 当前使用的识别档位
func (c *OCRClient) Model() string { return c.model }

// SetModel 覆盖默认档位（v6tiny / v6small / v5）
func (c *OCRClient) SetModel(model string) {
	if strings.TrimSpace(model) != "" {
		c.model = strings.TrimSpace(model)
	}
}

// Recognize 识别单张图片
func (c *OCRClient) Recognize(ctx context.Context, image []byte) (*OCRResult, error) {
	if len(image) == 0 {
		return nil, fmt.Errorf("图片内容为空")
	}
	if len(image) > ocrMaxImageBytes {
		return nil, fmt.Errorf("图片超过 OCR 服务上限（%d 字节）", ocrMaxImageBytes)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "page.png")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/ocr?model=%s", c.baseURL, url.QueryEscape(c.model))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 OCR 服务失败：%v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	var result OCRResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("OCR 服务返回无法解析（HTTP %d）：%s", resp.StatusCode, truncate(string(raw), 200))
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("OCR 服务繁忙，请稍后重试")
	}
	if result.Error != nil && *result.Error != "" {
		return nil, fmt.Errorf("OCR 识别失败：%s", *result.Error)
	}
	if !result.Success {
		return nil, fmt.Errorf("OCR 识别失败（HTTP %d）", resp.StatusCode)
	}
	return &result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
