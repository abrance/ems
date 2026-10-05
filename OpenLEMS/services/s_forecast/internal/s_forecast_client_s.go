package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// maxResponseBytes 限制单次响应体大小，防止异常大响应耗尽内存。
const maxResponseBytes = 8 << 20 // 8MB

// IClient TimesFM 预测服务客户端接口。便于测试时替换实现。
type IClient interface {
	Forecast(ctx context.Context, req *SForecastRequest) (*SForecastResponse, error)
}

// sClient 基于 net/http 的实现。
type sClient struct {
	baseURL    string
	httpClient *http.Client
}

var _ IClient = (*sClient)(nil)

// NewClient 创建客户端。baseURL 形如 https://forecast.xiaoyxq.top；timeout<=0 时默认 30s。
func NewClient(baseURL string, timeout time.Duration) IClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &sClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Forecast 调用 POST {baseURL}/v1/forecast。
func (c *sClient) Forecast(ctx context.Context, req *SForecastRequest) (*SForecastResponse, error) {
	if c.baseURL == "" {
		return nil, errors.New("预测服务地址为空")
	}
	if req == nil || len(req.Series) == 0 {
		return nil, errors.New("预测请求序列为空")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, "序列化预测请求失败")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/forecast", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "构造预测请求失败")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, "调用预测服务失败")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, errors.Wrap(err, "读取预测响应失败")
	}

	// 无论成功失败都尝试解析，以便取用 error 字段
	var out SForecastResponse
	_ = json.Unmarshal(data, &out)

	if resp.StatusCode != http.StatusOK {
		msg := out.Error
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return &out, errors.Errorf("预测服务返回 %d: %s", resp.StatusCode, msg)
	}
	if !out.Success {
		return &out, errors.Errorf("预测失败: %s", out.Error)
	}
	if len(out.Forecasts) == 0 {
		return &out, errors.New("预测响应不含结果")
	}
	return &out, nil
}
