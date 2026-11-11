package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 磁盘日志清理的三条边界：超期的才删、正在写的那个不删、非日志文件不碰。
func TestCleanLogFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, size int, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatalf("造文件 %s 失败: %v", name, err)
		}
		mt := now.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatalf("改 mtime %s 失败: %v", name, err)
		}
	}

	expired := "20261001.20261001120000000101.log"
	inPeriod := "20261105.20261105120000000101.log"
	active := "20261111.20261111120000000101.log"
	write(expired, 100, 40*24*time.Hour)
	write(inPeriod, 200, 6*24*time.Hour)
	write(active, 300, 0)
	write("gd_metric.csv", 50, 40*24*time.Hour) // 同目录的非日志文件，不该被当成候选

	files, freed, notes := CleanLogFiles(dir, "{Ymd}.log", now.AddDate(0, 0, -30))
	if len(notes) != 0 {
		t.Fatalf("不该有说明，实际: %v", notes)
	}
	if files != 1 || freed != 100 {
		t.Fatalf("应只删 1 个超期文件、释放 100 字节，实际 files=%d freed=%d", files, freed)
	}
	if _, err := os.Stat(filepath.Join(dir, expired)); !os.IsNotExist(err) {
		t.Errorf("超期文件 %s 应被删除，err=%v", expired, err)
	}
	for _, keep := range []string{inPeriod, active, "gd_metric.csv"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s 不该被删: %v", keep, err)
		}
	}
}

// 即使全部都超期，也必须留下最新的那个 —— 它就是当前正在写的文件。
func TestCleanLogFilesKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	older := "20260901.20260901120000000101.log"
	newest := "20260902.20260902120000000101.log"
	for name, age := range map[string]time.Duration{
		older:  90 * 24 * time.Hour,
		newest: 60 * 24 * time.Hour,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("造文件 %s 失败: %v", name, err)
		}
		mt := now.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatalf("改 mtime %s 失败: %v", name, err)
		}
	}

	files, _, _ := CleanLogFiles(dir, "{Ymd}.log", now.AddDate(0, 0, -30))
	if files != 1 {
		t.Fatalf("应只删 1 个，实际 %d", files)
	}
	if _, err := os.Stat(filepath.Join(dir, newest)); err != nil {
		t.Errorf("最新文件必须保留: %v", err)
	}
}

// 日志目录不存在只返回说明，不算失败（容器首启、日志改到别处都会遇到）。
func TestCleanLogFilesMissingDir(t *testing.T) {
	files, freed, notes := CleanLogFiles(filepath.Join(t.TempDir(), "not-exist"), "{Ymd}.log", time.Now())
	if files != 0 || freed != 0 {
		t.Fatalf("目录不存在不该删任何东西，实际 files=%d freed=%d", files, freed)
	}
	if len(notes) != 1 {
		t.Fatalf("应返回一条说明，实际: %v", notes)
	}
}
