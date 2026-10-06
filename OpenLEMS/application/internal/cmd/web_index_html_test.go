package cmd

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"application/internal/utils"
	"application/manifest"
)

// index.html 必须留出 </head> 注入点，否则运行时配置与会话注入都进不去。
func TestBuildIndexHTMLInjectionPoint(t *testing.T) {
	// 会话引导脚本只在开了鉴权绕过时注入；这正是它唯一有价值的场景
	t.Setenv(utils.EnvAuthBypassRole, "admin")

	webfs, err := manifest.WebFS()
	if err != nil {
		t.Skipf("静态资源不可用，跳过: %v", err)
	}

	out, err := buildIndexHTMLWithRuntimeConfig(context.Background(), webfs)
	if err != nil {
		t.Fatalf("构建 index.html 失败: %v", err)
	}
	html := string(out)

	if !strings.Contains(html, "window.__APP_RUNTIME__") {
		t.Error("index.html 未注入运行时配置")
	}
	if !strings.Contains(html, "ems:user:token") {
		t.Error("index.html 未注入会话引导脚本（AUTH_BYPASS_ROLE 场景下这是唯一会话来源）")
	}
	// 会话脚本必须排在 SPA 入口之前执行，否则首屏请求会 401
	scriptAt := strings.Index(html, "ems:user:token")
	entryAt := strings.Index(html, `type="module"`)
	if scriptAt < 0 || entryAt < 0 || scriptAt > entryAt {
		t.Errorf("会话脚本应在 SPA 入口之前：script@%d entry@%d", scriptAt, entryAt)
	}
}

// 悬浮层已下线：产物里不该再出现它的资源引用。
func TestBuildIndexHTMLHasNoOverlay(t *testing.T) {
	webfs, err := manifest.WebFS()
	if err != nil {
		t.Skipf("静态资源不可用，跳过: %v", err)
	}

	out, err := buildIndexHTMLWithRuntimeConfig(context.Background(), webfs)
	if err != nil {
		t.Fatalf("构建 index.html 失败: %v", err)
	}
	if strings.Contains(string(out), "/overlay/") {
		t.Error("index.html 仍在引用已下线的悬浮层资源")
	}
}

// 内嵌产物本身要是一份可用的新前端：有入口脚本与样式，且没有残留悬浮层引用。
func TestEmbeddedBundleIsNewFrontend(t *testing.T) {
	webfs, err := manifest.WebFS()
	if err != nil {
		t.Fatalf("静态资源不可用: %v", err)
	}

	index, err := fs.ReadFile(webfs, "index.html")
	if err != nil {
		t.Fatalf("读取 index.html 失败: %v", err)
	}
	html := string(index)
	if strings.Contains(html, "/overlay/") {
		t.Error("内嵌 index.html 仍在引用悬浮层")
	}
	if !strings.Contains(html, "/assets/") {
		t.Error("内嵌 index.html 未引用构建产物（/assets/）")
	}
	if !strings.Contains(html, `id="app"`) {
		t.Error("内嵌 index.html 缺少挂载点 #app")
	}

	// 引用的资源必须真的在包里 —— 断链是「构建产物没打全」最常见的表现
	for _, ref := range assetRefs(html) {
		if _, err := fs.Stat(webfs, strings.TrimPrefix(ref, "/")); err != nil {
			t.Errorf("index.html 引用的资源不在内嵌包内：%s (%v)", ref, err)
		}
	}
}

// assetRefs 从 index.html 里取出 /assets/... 形式的引用。
func assetRefs(html string) []string {
	var out []string
	for _, tag := range []string{"src=\"", "href=\""} {
		for at := 0; ; {
			i := strings.Index(html[at:], tag)
			if i < 0 {
				break
			}
			start := at + i + len(tag)
			end := strings.Index(html[start:], "\"")
			if end < 0 {
				break
			}
			ref := html[start : start+end]
			if strings.HasPrefix(ref, "/assets/") {
				out = append(out, ref)
			}
			at = start + end
		}
	}
	return out
}
