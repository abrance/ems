package utils

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/guid"
)

const testAPIStaticToken = "test-static-token"

func startStaticTokenAuthServer(t *testing.T) string {
	t.Helper()
	s := g.Server(guid.S())
	s.SetPort(0)
	s.SetDumpRouterMap(false)
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.ALL("/seed-session", func(r *ghttp.Request) {
			r.Session.Set("role", "user")
			r.Response.Write("seeded")
		})
	})
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(MiddlewareAuth)
		group.ALL("/probe", func(r *ghttp.Request) {
			role := r.GetCtxVar("userRole").String()
			sessionRole := r.Session.MustGet("role").String()
			r.Response.Write(role + ":" + sessionRole)
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

func callStaticTokenProbe(t *testing.T, method, url string, headers map[string]string) (string, int) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload := struct {
		Code int `json:"code"`
	}{}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Code != 0 {
		return string(body), payload.Code
	}
	return string(body), 0
}

func TestMiddlewareAuthStaticTokenDisabledByDefault(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, "")
	url := startStaticTokenAuthServer(t)

	body, code := callStaticTokenProbe(t, http.MethodGet, url, nil)
	if code != 401 {
		t.Fatalf("未配置静态 token 时匿名请求期望 401，实际 code=%d body=%s", code, body)
	}
}

func TestMiddlewareAuthStaticTokenViaXAPIHeader(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	body, code := callStaticTokenProbe(t, http.MethodGet, url, map[string]string{
		"X-API-Token": testAPIStaticToken,
	})
	if code != 0 || body != "admin:admin" {
		t.Fatalf("正确 X-API-Token 应以 admin 身份放行，实际 code=%d body=%s", code, body)
	}
	if strings.Contains(body, testAPIStaticToken) {
		t.Fatalf("响应体不得包含静态 token: %s", body)
	}
}

func TestMiddlewareAuthStaticTokenViaBearerHeader(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	body, code := callStaticTokenProbe(t, http.MethodGet, url, map[string]string{
		"Authorization": "Bearer " + testAPIStaticToken,
	})
	if code != 0 || body != "admin:admin" {
		t.Fatalf("正确 Bearer token 应以 admin 身份放行，实际 code=%d body=%s", code, body)
	}
}

func TestMiddlewareAuthStaticTokenAllowsReadAndWriteMethodsAsAdmin(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	for _, method := range []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			body, code := callStaticTokenProbe(t, method, url, map[string]string{
				"X-API-Token": testAPIStaticToken,
			})
			if code != 0 || body != "admin:admin" {
				t.Fatalf("静态 token 的 %s 请求应以 admin 身份放行，实际 code=%d body=%s", method, code, body)
			}
		})
	}
}

func TestMiddlewareAuthBypassRoleTakesPrecedenceOverStaticToken(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "user")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	body, code := callStaticTokenProbe(t, http.MethodPost, url, map[string]string{
		"X-API-Token": testAPIStaticToken,
	})
	if code != 403 {
		t.Fatalf("AUTH_BYPASS_ROLE=user 应优先于静态 token，POST 期望 403，实际 code=%d body=%s", code, body)
	}
}

func TestMiddlewareAuthInvalidStaticTokenFallsBackToSessionAuth(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)
	invalidToken := "incorrect-token"

	body, code := callStaticTokenProbe(t, http.MethodGet, url, map[string]string{
		"X-API-Token": invalidToken,
	})
	if code != 401 {
		t.Fatalf("错误静态 token 且无会话时应继续原鉴权并返回 401，实际 code=%d body=%s", code, body)
	}
	if strings.Contains(body, invalidToken) || strings.Contains(body, testAPIStaticToken) {
		t.Fatalf("错误响应体不得包含 token: %s", body)
	}
}

func TestMiddlewareAuthInvalidStaticTokenPreservesValidSession(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	seedResp, err := http.Get(strings.TrimSuffix(url, "/api/probe") + "/seed-session")
	if err != nil {
		t.Fatal(err)
	}
	seedBody, err := io.ReadAll(seedResp.Body)
	_ = seedResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(seedBody) != "seeded" {
		t.Fatalf("初始化会话失败，body=%s", seedBody)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range seedResp.Cookies() {
		req.AddCookie(cookie)
	}
	req.Header.Set("X-API-Token", "incorrect-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "user:user" {
		t.Fatalf("错误静态 token 应继续使用有效会话，期望 user:user，实际 body=%s", body)
	}
}

func TestMiddlewareAuthEmptyStaticTokenHeadersDoNotAuthenticate(t *testing.T) {
	t.Setenv(EnvAuthBypassRole, "")
	t.Setenv(EnvAPIStaticToken, testAPIStaticToken)
	url := startStaticTokenAuthServer(t)

	for name, value := range map[string]string{
		"empty X-API-Token": "",
		"empty Bearer":      "Bearer ",
	} {
		t.Run(name, func(t *testing.T) {
			header := "X-API-Token"
			if name == "empty Bearer" {
				header = "Authorization"
			}
			body, code := callStaticTokenProbe(t, http.MethodGet, url, map[string]string{
				header: value,
			})
			if code != 401 {
				t.Fatalf("空静态 token 请求期望 401，实际 code=%d body=%s", code, body)
			}
		})
	}
}
