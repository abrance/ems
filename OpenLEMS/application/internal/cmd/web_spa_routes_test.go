package cmd

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

// 前端路由必须都能拿到 SPA 的 index.html。
//
// 这条守着一次真实事故：web.go 里为**旧前端**保留了 `GET:/config/*` 的静态目录绑定
// （旧产物用 config/ 放配置），而新前端的「配置」Tab 正好用 `/config/*` 路由 ——
// 于是储能参数/系统信息/存储管理/系统操作/采集与凭据/界面与语言/日志级别
// **七个页面全部 404**，页面整片空白，而且请求根本落不到 SPA 回退上。
//
// 路径清单不手写，直接读前端的 nav 源文件：手写一份迟早和新路由不一致，
// 而不一致的表现就是「新加的页面恰好撞上某个静态前缀」时没人发现。
func TestSPARoutesServeIndexHTML(t *testing.T) {
	navPath := filepath.Join("..", "..", "..", "web", "src", "nav", "index.ts")
	raw, err := os.ReadFile(navPath)
	if err != nil {
		t.Skipf("读不到前端路由清单（%s），跳过: %v", navPath, err)
	}
	// 只取页面路由；登录页不在 nav 里，单独加
	paths := regexp.MustCompile(`path: '(/[^']+)'`).FindAllStringSubmatch(string(raw), -1)
	routes := []string{"/login"}
	for _, m := range paths {
		routes = append(routes, m[1])
	}
	if len(routes) < 10 {
		t.Fatalf("解析出的路由太少（%d 个），路径可能变了: %s", len(routes), navPath)
	}

	s := g.Server("test-spa-routes")
	s.SetDumpRouterMap(false)
	s.SetAddr("127.0.0.1:0")
	s.SetSessionMaxAge(1 * time.Minute)
	setupAPIRoutes(s)
	setupStaticFiles(s, gctx.New())
	s.Start()
	defer s.Shutdown()

	addr := s.GetListenedAddress()
	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, route := range routes {
		res, err := client.Get(addr + route)
		if err != nil {
			t.Fatalf("%s 请求失败: %v", route, err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "<!doctype html") {
			t.Errorf("%s 没有返回 SPA 的 index.html（HTTP %d，前 40 字节 %q）——"+
				"多半是它撞上了某个静态目录前缀或路由跳过规则", route, res.StatusCode, string(body[:min(len(body), 40)]))
		}
	}
}
