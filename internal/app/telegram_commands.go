package app

import (
	"context"
	"errors"
)

type telegramMenuCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func registerTelegramLibraryCommands(ctx context.Context, cfg telegramConfig) error {
	commands := []map[string]any{}
	if err := telegramRequest(ctx, cfg, "getMyCommands", map[string]any{}, &commands); err != nil {
		return err
	}
	for _, next := range []telegramMenuCommand{{"share", "添加或更新虚拟链库分享链接"}, {"cas", "添加或更新移动分享CAS链库"}} {
		found := false
		for i, c := range commands {
			if c["command"] == next.Command {
				commands[i]["description"] = next.Description
				found = true
				break
			}
		}
		if !found {
			commands = append(commands, map[string]any{"command": next.Command, "description": next.Description})
		}
	}
	if len(commands) > 100 {
		return errors.New("TG命令菜单已满")
	}
	var confirmed bool
	if err := telegramRequest(ctx, cfg, "setMyCommands", map[string]any{"commands": commands}, &confirmed); err != nil {
		return err
	}
	if !confirmed {
		return errors.New("TG未确认命令菜单设置")
	}
	return nil
}
