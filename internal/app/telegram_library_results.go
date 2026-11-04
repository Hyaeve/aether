package app

import (
	"fmt"
	"strings"
)

func telegramLibraryLabel(provider, kind string) string {
	if kind == "cas-library" {
		return "CAS链库"
	}
	switch provider {
	case "mobile":
		return "移动链库"
	case "115":
		return "115链库"
	default:
		return "夸克链库"
	}
}

func telegramLibraryResultLines(job telegramTransfer, members []telegramTransfer) []string {
	success, failed, skipped, files := 0, 0, 0, 0
	for _, j := range members {
		files += j.Processed
		if j.Status == "completed" || j.Status == "success" {
			if j.Processed == 0 {
				skipped++
			} else {
				success++
			}
		} else {
			failed++
		}
	}
	lines := []string{telegramLibraryLabel(job.Provider, job.Kind) + "已更新",
		fmt.Sprintf("本次完成：识别 %d 条，成功 %d 条，失败 %d 条，跳过 %d 条。", len(members), success, failed, skipped),
		fmt.Sprintf("本次共处理 %d 个文件。", files), ""}
	// Telegram has a 4096 UTF-16 unit limit. Keep every link in the one result.
	budget := 3700 / max(1, len(members))
	for i, j := range members {
		name := j.Name
		if name == "" {
			name = fmt.Sprintf("链接%d", i+1)
		}
		detail := fmt.Sprint(j.Processed)
		if j.Processed == 0 && (strings.Contains(j.Result, "未启用") || strings.Contains(j.Result, "默认生成位置")) {
			detail += "；未启用：" + j.Result
		}
		if j.Status != "completed" && j.Status != "success" {
			detail += "；失败：" + telegramLimit(j.Result, max(8, budget/2))
		}
		detail = telegramLimit(detail, max(14, budget/2))
		units := 0
		for _, r := range detail {
			units++
			if r > 0xffff {
				units++
			}
		}
		lines = append(lines, "· "+telegramLimit(name, max(8, budget-units-4))+"（"+detail+"）")
	}
	return lines
}
