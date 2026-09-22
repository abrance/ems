package utils

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/guid"
)

func TestAuthBypassRoleNormalize(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"", ""},
		{"admin", "admin"},
		{"ADMIN", "admin"},
		{" user ", "user"},
		{"root", ""}, // 非法取值视为未开启
		{"true", ""}, // 不接受布尔写法，避免误配
		{"1", ""},
	}
	for _, c := range cases {
		t.Setenv(EnvAuthBypassRole, c.env)
		if got := AuthBypassRole(); got != c.want {
			t.Fatalf("AUTH_BYPASS_ROLE=%q: 期望 %q，实际 %q", c.env, c.want, got)
		}
	}
}

// startAuthServer 启动一个只挂了鉴权中间件的测试服务，返回 baseURL。
func startAuthServer(t *testing.T) string {
	t.Helper()
	s := g.Server(guid.S())
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(MiddlewareAuth)
		group.ALL("/probe", func(r *ghttp.Request) {
			r.Response.Write("ok")
		})
	})
	s.Start()
	t.Cleanup(func() { _ = s.Shutdown() })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.GetListenedPort() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	port := s.GetListenedPort()
	if port <= 0 {
		t.Fatal("测试服务未监听端口")
	}
	return "http://127.0.0.1:" + g.NewVar(port).String() + "/api/probe"
}

func callProbe(t *testing.T, method, url string) (string, int) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// 业务码在响应体里（项目约定：鉴权失败仍返回 HTTP 200，前端读 json.code）
	payload := struct {
		Code int `json:"code"`
	}{}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Code != 0 {
		return "", payload.Code
	}
	return string(body), 0
}

// 默认关闭：匿名请求必须被拒。
func TestMiddlewareAuthRejectsAnonymousByDefault(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	url := startAuthServer(t)
	body, code := callProbe(t, http.MethodGet, url)
	if code != 401 {
		t.Fatalf("期望 401（未登录），实际 code=%d body=%s", code, body)
	}
}

// bypass=admin：读写都放行。
func TestMiddlewareAuthBypassAdmin(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "admin")
	url := startAuthServer(t)
	if body, code := callProbe(t, http.MethodGet, url); code != 0 || body != "ok" {
		t.Fatalf("admin GET 期望放行，实际 code=%d body=%s", code, body)
	}
	if body, code := callProbe(t, http.MethodPost, url); code != 0 || body != "ok" {
		t.Fatalf("admin POST 期望放行，实际 code=%d body=%s", code, body)
	}
}

// bypass=user：只读放行，写接口仍按 admin 校验并拒绝。
func TestMiddlewareAuthBypassUserIsReadOnly(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "user")
	url := startAuthServer(t)
	if body, code := callProbe(t, http.MethodGet, url); code != 0 || body != "ok" {
		t.Fatalf("user GET 期望放行，实际 code=%d body=%s", code, body)
	}
	body, code := callProbe(t, http.MethodPost, url)
	if code != 403 {
		t.Fatalf("user POST 期望 403（管理类接口仍拒绝），实际 code=%d body=%s", code, body)
	}
}
