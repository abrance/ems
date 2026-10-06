package manifest

import (
	"io/fs"
	"testing"
)

// 悬浮层资源通过 //go:embed overlay/* 打包，新增文件应自动包含。
func TestOverlayFSContainsOverlays(t *testing.T) {
	names := []string{
		"overlay/spot-overlay.js",
		"overlay/spot-overlay.css",
		"overlay/effect-overlay.js",
		"overlay/effect-overlay.css",
	}
	for _, name := range names {
		if _, err := fs.Stat(OverlayFS, name); err != nil {
			t.Errorf("内嵌资源缺少 %s: %v", name, err)
		}
	}
}

func TestEffectOverlayNotEmpty(t *testing.T) {
	for _, name := range []string{"overlay/effect-overlay.js", "overlay/effect-overlay.css"} {
		data, err := fs.ReadFile(OverlayFS, name)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		if len(data) < 1024 {
			t.Errorf("%s 内容过小（%d 字节），疑似未正确打包", name, len(data))
		}
	}
}
