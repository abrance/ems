// 冻结通道（第三方 96 点）的人工导入命令。
//
//	go run ./cmd/gdmetric-import --in 电查查导出.csv --out out/gd_metric.csv
//
// 只做三件事：读候选文件 → 逐条校验（失败一个字节都不写）→ 原子写盘。
// **不联网、不要 token、不进 API、不进 cron**：官方 96 点匿名拿不到，这是唯一合规的补数路径，
// 所以它必须是「人手动跑一次」而不是一条自动采集链路。
//
// 约定见 application/internal/logic/gdmetric/frozen_import.go 与 docs/data-source-matrix.md。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"application/internal/logic/gdmetric"
)

func main() {
	in := flag.String("in", "", "候选文件（电查查导出，长表或宽表）；- 表示标准输入")
	out := flag.String("out", gdmetric.DefaultFrozenCachePath, "目标文件（冻结通道读的那个 CSV），默认 "+gdmetric.DefaultFrozenCachePath)
	merge := flag.Bool("merge", true, "与目标文件已有数据合并（同 日期/时间/typeId 以新文件为准）")
	check := flag.Bool("check", false, "只校验不写盘")
	flag.Parse()

	if strings.TrimSpace(*in) == "" {
		fmt.Fprintln(os.Stderr, "缺少 --in：候选文件路径（或 - 表示标准输入）")
		flag.Usage()
		os.Exit(2)
	}

	var r *os.File
	if *in == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(*in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "打不开候选文件：%v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}

	if *check {
		imp, err := gdmetric.ParseFrozenImport(r)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		report("校验通过（未写盘）", &gdmetric.FrozenImportReport{
			Format: imp.Format, Days: imp.Days, Rows: len(imp.Rows), Placeholders: imp.Placeholders, Target: *out,
		})
		return
	}

	rep, err := gdmetric.WriteFrozenImportFile(r, *out, *merge)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "目标文件未改动。")
		os.Exit(1)
	}
	report("导入完成", rep)
	fmt.Printf("重启 ems（或等缓存重载）后，分时研究页即可看到 %s ~ %s。\n",
		firstOr(rep.Days, "—"), lastOr(rep.Days, "—"))
}

func report(title string, rep *gdmetric.FrozenImportReport) {
	fmt.Printf("%s：%s\n", title, rep.Target)
	fmt.Printf("  形态：%s\n", rep.Format)
	fmt.Printf("  天数：%d（%s ~ %s）\n", len(rep.Days), firstOr(rep.Days, "—"), lastOr(rep.Days, "—"))
	fmt.Printf("  行数：%d\n", rep.Rows)
	if rep.Placeholders > 0 {
		fmt.Printf("  占位（无实际值，页面剔除）：%d 行\n", rep.Placeholders)
	}
	if rep.Merged > 0 {
		fmt.Printf("  覆盖已有行：%d\n", rep.Merged)
	}
}

func firstOr(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	return items[0]
}

func lastOr(items []string, fallback string) string {
	if len(items) == 0 {
		return fallback
	}
	return items[len(items)-1]
}
