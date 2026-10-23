package app

import "fmt"

const telegramDirectoryPageSize = 5

func telegramAction(kind string) string {
	if kind == "ed2k" {
		return "ED2K 离线"
	}
	return "转存"
}

func telegramDirectoryPage(st State, job telegramTransfer) (string, [][]map[string]string) {
	rows := [][]map[string]string{}
	start := job.Page * telegramDirectoryPageSize
	for i := start; i >= 0 && i < min(start+telegramDirectoryPageSize, len(job.Targets)); i++ {
		target := job.Targets[i]
		name := "存储不可用"
		for _, s := range st.Storages {
			if s.ID == target.StorageID {
				name = s.Name
				break
			}
		}
		label := fmt.Sprintf("%d. %s · %s", i+1, name, target.Path)
		if len([]rune(label)) > 90 {
			label = string([]rune(label)[:89]) + "…"
		}
		rows = append(rows, []map[string]string{{"text": label, "callback_data": fmt.Sprintf("t:%s:%d", job.ID, i)}})
	}
	navigation := []map[string]string{}
	if job.Page > 0 {
		navigation = append(navigation, map[string]string{"text": "上一页", "callback_data": fmt.Sprintf("p:%s:%d", job.ID, job.Page-1)})
	}
	if start+telegramDirectoryPageSize < len(job.Targets) {
		navigation = append(navigation, map[string]string{"text": "下一页", "callback_data": fmt.Sprintf("p:%s:%d", job.ID, job.Page+1)})
	}
	text := "选择" + telegramAction(job.Kind) + "目录"
	if len(navigation) > 0 {
		rows = append(rows, navigation)
		text += fmt.Sprintf("（%d/%d页）", job.Page+1, (len(job.Targets)+telegramDirectoryPageSize-1)/telegramDirectoryPageSize)
	}
	return text, rows
}
