package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"
)

var errTransferIgnored = errors.New("此扩展名已被全局传输设置忽略")
var errTransferSkipped = errors.New("目标文件已存在，已跳过且未覆盖")

type transferRequestProgressKey struct{}

type transferControls struct {
	Pause  bool `json:"pause"`
	Retry  bool `json:"retry"`
	Delete bool `json:"delete"`
}

func transferActive(status string) bool {
	return status == "running" || status == "paused" || status == "preparing" || status == "waiting" || status == "queued"
}
func transferFailed(status string) bool { return status == "failed" || status == "error" }
func controlsForTransfer(e transferEntry) transferControls {
	pausable := e.cancel != nil && (e.Kind != "copy" || e.pausable)
	return transferControls{Pause: pausable && transferActive(e.Status), Retry: e.Status == "failed" && e.retry != nil, Delete: !transferActive(e.Status) || pausable}
}
func (p *transferProgress) update(update func(*transferEntry)) {
	if p == nil || p.log == nil {
		return
	}
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	if e, ok := p.log.items[p.id]; ok && transferActive(e.Status) {
		update(&e)
		e.Updated = time.Now()
		p.log.items[p.id] = e
	}
}
func (p *transferProgress) stage(status string) {
	p.update(func(e *transferEntry) {
		e.phase = status
		if e.Status != "paused" {
			e.Status = status
		}
	})
}
func (p *transferProgress) setName(name string) { p.update(func(e *transferEntry) { e.Name = name }) }
func (p *transferProgress) controlCopy()        { p.update(func(e *transferEntry) { e.pausable = true }) }
func transferSelectionName(files []File, ids []string) string {
	names := []string{}
	for _, fid := range ids {
		for _, f := range files {
			if f.ID == fid {
				names = append(names, f.Name)
				break
			}
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("待确认项目 · %d项", len(ids))
	}
	if len(ids) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%s 等%d项", names[0], len(ids))
}
func localCopySize(ctx context.Context, root *os.Root, name string, depth int, count *int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if p, _ := ctx.Value(transferProgressResourceKey{}).(*transferProgress); p != nil {
		if err := p.wait(); err != nil {
			return 0, err
		}
	}
	(*count)++
	if depth > 128 || *count > 100000 {
		return 0, errors.New("选择范围超过100000项或128层")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return 0, err
	}
	if info.Mode().IsRegular() {
		return info.Size(), nil
	}
	if !info.IsDir() {
		return 0, errors.New("仅支持普通文件和目录")
	}
	dir, err := root.Open(name)
	if err != nil {
		return 0, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		size, err := localCopySize(ctx, root, path.Join(name, entry.Name()), depth+1, count)
		if err != nil {
			return 0, err
		}
		if size > 1<<63-1-total {
			return 0, errors.New("选择范围大小无效")
		}
		total += size
	}
	return total, nil
}

// Only a regular local source with an absent destination can be replayed.
// Browser bodies and submitted cloud operations are deliberately not retained.
func (a *App) localCopyReplay(p *transferProgress, source, target Storage, from, to string, original os.FileInfo) {
	if p == nil || original == nil || !original.Mode().IsRegular() {
		return
	}
	replay := func(ctx context.Context, next *transferProgress) error {
		for _, saved := range []Storage{source, target} {
			current, err := a.store.storage(saved.ID)
			if err != nil || !sameBackupStorage(saved, current) {
				return errors.New("存储配置已变化，请重新选择传输")
			}
		}
		src, err := os.OpenRoot(source.Config["root"])
		if err != nil {
			return err
		}
		defer src.Close()
		info, err := src.Lstat(from)
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, original) || info.Size() != original.Size() || !info.ModTime().Equal(original.ModTime()) {
			return errors.New("来源文件身份或内容已变化，请重新选择传输")
		}
		dst, err := os.OpenRoot(target.Config["root"])
		if err != nil {
			return err
		}
		defer dst.Close()
		if _, err = dst.Lstat(to); !os.IsNotExist(err) {
			return errors.New("目标已存在或无法核验，不重复写入")
		}
		a.runMu.Lock()
		defer a.runMu.Unlock()
		if len(a.running) != 0 {
			return errors.New("有任务正在执行，请稍后重试")
		}
		next.controlCopy()
		ctx = context.WithValue(ctx, copyProgressKey{}, next)
		err = copyLocalTree(ctx, src, dst, from, to, 0)
		if _, exists := dst.Lstat(to); !os.IsNotExist(exists) && err != nil {
			next.update(func(e *transferEntry) { e.retry = nil })
		}
		return err
	}
	p.update(func(e *transferEntry) { e.retry = replay })
}
