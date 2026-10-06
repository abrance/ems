package cmd

import (
	"context"
	"strings"
	"testing"

	"application/manifest"
)

// 悬浮层标签必须在 index.html 的 </head> 前注入，否则新页面不会被加载。
func TestBuildIndexHTMLInjectsOverlays(t *testing.T) {
	webfs, err := manifest.WebFS()
	if err != nil {
		t.Skipf("静态资源不可用，跳过: %v", err)
	}

	out, err := buildIndexHTMLWithRuntimeConfig(context.Background(), webfs)
	if err != nil {
		t.Fatalf("构建 index.html 失败: %v", err)
	}
	html := string(out)

	wants := []string{
		"/overlay/spot-overlay.css",
		"/overlay/spot-overlay.js",
		"/overlay/effect-overlay.css",
		"/overlay/effect-overlay.js",
	}
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("index.html 未注入 %s", want)
		}
	}
	if !strings.Contains(html, "window.__APP_RUNTIME__") {
		t.Error("index.html 未注入运行时配置")
	}
}
