package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type quarkShareUnavailable struct{ code string }

func (e *quarkShareUnavailable) Error() string {
	return "夸克分享已失效或被取消，请更新有效分享链接"
}

func (a *App) quarkLibraryRead(ctx context.Context, session *Storage, secret string, recursive bool) (string, string, []shareBatch, error) {
	original := *session
	_, code, pass, err := parseShareLink(secret, "")
	if err != nil {
		return "", "", nil, err
	}
	var result struct {
		Data struct {
			Token string `json:"stoken"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := a.waitAPI(ctx, session.ID); err != nil {
		return "", "", nil, err
	}
	if err = shareSessionRequest(ctx, session, "POST", "https://drive.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc",
		map[string]string{"pwd_id": code, "passcode": pass}, &result); err != nil {
		return "", "", nil, err
	}
	if result.Data.Token == "" {
		return "", "", nil, errors.New("未取得夸克分享令牌")
	}
	token, items, err := a.readShareSession(ctx, session, code, pass, "root", result.Data.Token)
	if err != nil {
		return "", "", nil, err
	}
	title := strings.TrimSpace(result.Data.Title)
	if len(items) == 1 {
		title = items[0].Name
	}
	if title == "" {
		title = "夸克分享-" + quarkLibraryStamp(code)[:8]
	}
	if !recursive {
		if err := a.persistQuarkCookies(original, *session); err != nil {
			return "", "", nil, err
		}
		return title, token, nil, nil
	}
	batches, err := a.planShareSession(ctx, session, code, pass, token, items)
	if err != nil {
		return "", "", nil, err
	}
	// The user-facing directory replaces, rather than wraps, a single root folder.
	empty := ""
	batches, err = shareDirectoryBatches(items, batches, &empty)
	if err == nil {
		err = a.persistQuarkCookies(original, *session)
	}
	return title, token, batches, err
}

func quarkLibraryRoot(card quarkLibraryCard) string {
	return filepath.Join(card.Directory, card.DirectoryName)
}

func quarkLibraryRelative(name string) bool {
	if name == "" || path.IsAbs(name) || strings.Contains(name, "\\") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if !safeName(part) {
			return false
		}
	}
	return true
}

func quarkLibraryDigest(root *os.Root, name string) (string, error) {
	if !quarkLibraryRelative(name) {
		return "", os.ErrPermission
	}
	current, err := root.OpenRoot(".")
	if err != nil {
		return "", err
	}
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		next, e := strmReplaceSubroot(current, part)
		current.Close()
		if e != nil {
			return "", e
		}
		current = next
	}
	defer current.Close()
	name = parts[len(parts)-1]
	info, err := current.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return "", errors.New("生成文件不是普通文件或超过64MiB")
	}
	file, err := current.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("文件在核验时已改变")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, 64<<20+1))
	if err != nil || n > 64<<20 {
		return "", errors.New("文件读取失败或超过64MiB")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) prepareQuarkLibraryMove(ctx context.Context, old, next quarkLibraryCard, st State) (func(bool), error) {
	nop := func(bool) {}
	if next.Directory == "" {
		for _, f := range st.QuarkLibraryRuntime.Files {
			if f.CardID == old.ID {
				return nop, errors.New("已有生成文件的卡片不能清空生成位置")
			}
		}
		return nop, nil
	}
	target, err := openStrmReplaceRoot(next.Directory)
	if err != nil {
		return nop, errors.New("生成位置不存在、无权访问或包含符号链接")
	}
	defer target.Close()
	if old.ID == "" || quarkLibraryRoot(old) == quarkLibraryRoot(next) {
		return nop, nil
	}
	from, to := filepath.Clean(quarkLibraryRoot(old)), filepath.Clean(quarkLibraryRoot(next))
	if backupPathsOverlap(from, to) || backupPathsOverlap(to, from) {
		return nop, errors.New("新旧生成目录不能互相包含")
	}
	files := []quarkLibraryFile{}
	for _, f := range st.QuarkLibraryRuntime.Files {
		if f.CardID == old.ID {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nop, nil
	}
	source, err := openStrmReplaceRoot(from)
	if err != nil {
		return nop, errors.New("原生成目录不可读取，未修改卡片")
	}
	defer source.Close()
	dest, err := virtualOutputRoot(target, next.DirectoryName)
	if err != nil {
		return nop, err
	}
	defer dest.Close()
	created := []quarkLibraryFile{}
	cleanup := func(root *os.Root, list []quarkLibraryFile) {
		for _, item := range list {
			if digest, e := quarkLibraryDigest(root, item.Relative); e == nil && digest == item.Digest {
				_ = root.Remove(item.Relative)
			}
		}
	}
	for _, item := range files {
		if err := ctx.Err(); err != nil {
			cleanup(dest, created)
			return nop, err
		}
		digest, e := quarkLibraryDigest(source, item.Relative)
		if e != nil || digest != item.Digest {
			cleanup(dest, created)
			return nop, errors.New("原生成文件已被外部修改或缺失，迁移停止，未修改卡片")
		}
		if _, e := dest.Lstat(item.Relative); !os.IsNotExist(e) {
			cleanup(dest, created)
			return nop, errors.New("迁移目标已有同名文件，不覆盖")
		}
		child, e := virtualOutputRoot(dest, path.Dir(item.Relative))
		if e != nil {
			cleanup(dest, created)
			return nop, e
		}
		in, e := source.Open(item.Relative)
		if e != nil {
			child.Close()
			cleanup(dest, created)
			return nop, e
		}
		out, e := child.OpenFile(path.Base(item.Relative), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e == nil {
			_, e = io.Copy(out, &cancelReader{ctx, in})
			if e == nil {
				e = out.Sync()
			}
			closeErr := out.Close()
			if e == nil {
				e = closeErr
			}
			created = append(created, item)
		}
		in.Close()
		child.Close()
		copied, verify := quarkLibraryDigest(dest, item.Relative)
		if e != nil || verify != nil || copied != item.Digest {
			cleanup(dest, created)
			return nop, errors.New("迁移校验失败，原文件和卡片保留")
		}
	}
	// Commit configuration before removing only unchanged, tracked old files.
	return func(committed bool) {
		rootPath, list := to, created
		if committed {
			rootPath, list = from, files
		}
		root, err := openStrmReplaceRoot(rootPath)
		if err != nil {
			a.store.event("warn", "virtual", "夸克链库迁移后旧文件未能清理："+old.Name)
			return
		}
		defer root.Close()
		cleanup(root, list)
		if committed {
			a.store.event("info", "virtual", fmt.Sprintf("夸克链库迁移完成：%s，已核验 %d 个登记文件，目标 %s", next.Name, len(files), to))
		}
	}, nil
}

func (a *App) runQuarkLibrary(ctx context.Context, card quarkLibraryCard) (result quarkLibraryResult, resultErr error) {
	if !card.Enabled || card.Directory == "" {
		return result, errors.New("请设置生成位置并启用链接")
	}
	now := time.Now()
	st := a.store.snapshotWithLogLimit(0)
	cfg := effectiveQuarkLibrary(st)
	result = st.QuarkLibraryRuntime.Results[card.ID]
	result.Status, result.Message, result.LastRun = "running", "正在读取分享", now
	result.Generated, result.Metadata, result.Skipped = 0, 0, 0
	result.NextRun = quarkLibraryNext(card, now)
	if err := a.store.update(func(st *State) error {
		initQuarkLibraryRuntime(st)
		st.QuarkLibraryRuntime.Results[card.ID] = result
		return nil
	}); err != nil {
		return result, err
	}
	a.store.event("info", "virtual", "夸克链库开始生成："+card.Name)
	defer func() {
		if resultErr != nil {
			result.Status, result.Message = "failed", resultErr.Error()
			var unavailable *quarkShareUnavailable
			if errors.As(resultErr, &unavailable) {
				result.Status = "invalid"
				if result.InvalidSince.IsZero() {
					result.InvalidSince = now
				}
			}
		} else {
			result.Status, result.Message = "completed", fmt.Sprintf("新增STRM %d 条，元数据 %d 个，跳过 %d 个", result.Generated, result.Metadata, result.Skipped)
			result.InvalidSince = time.Time{}
		}
		err := a.store.update(func(st *State) error {
			initQuarkLibraryRuntime(st)
			st.QuarkLibraryRuntime.Results[card.ID] = result
			if result.Status == "invalid" && !result.InvalidSince.Add(time.Duration(cfg.InvalidDays)*24*time.Hour).After(now) {
				for i, item := range st.QuarkLibraryCards {
					if item.ID == card.ID && item.Secret == card.Secret {
						st.QuarkLibraryCards = append(st.QuarkLibraryCards[:i], st.QuarkLibraryCards[i+1:]...)
						revokeQuarkLibraryCard(st, card.ID)
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			resultErr = errors.Join(resultErr, errors.New("生成结果持久化失败，请核对目录后重试"))
		}
		level := "info"
		if resultErr != nil {
			level = "warn"
		}
		a.store.event(level, "virtual", "夸克链库执行结果："+card.Name+"；"+result.Message)
	}()
	s, err := a.quarkLibraryStorage(st)
	if err != nil {
		return result, err
	}
	_, token, batches, err := a.quarkLibraryRead(ctx, &s, card.Secret, true)
	if err != nil {
		return result, err
	}
	// A successful read clears invalidity even if the local disk subsequently fails.
	result.InvalidSince = time.Time{}
	if err := a.quarkLibraryCheckExecution(ctx, card); err != nil {
		return result, err
	}
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		return result, errors.New("生成位置不可读取")
	}
	defer parent.Close()
	root, err := virtualOutputRoot(parent, card.DirectoryName)
	if err != nil {
		return result, err
	}
	defer root.Close()
	media, metadata := card.MediaExtensions, card.MetadataExtensions
	if media == "" {
		media = cfg.MediaExtensions
	}
	if metadata == "" {
		metadata = cfg.MetadataExtensions
	}
	known := map[string]quarkLibraryFile{}
	for _, file := range st.QuarkLibraryRuntime.Files {
		if file.CardID == card.ID {
			known[file.Relative] = file
		}
	}
	for _, batch := range batches {
		for _, item := range batch.Items {
			if err := a.quarkLibraryCheckExecution(ctx, card); err != nil {
				return result, err
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			isMedia := taskMedia(Task{MediaExtensions: media}, item.Name)
			if !isMedia && !excludedType(item.Name, metadata) {
				continue
			}
			rel := path.Join(batch.Directory, item.Name)
			if isMedia {
				rel += ".strm"
			}
			manifest := quarkLibraryFile{Key: id(), CardID: card.ID, ShareStamp: quarkLibraryStamp(card.Secret), SourceID: item.ID,
				SourceParent: batch.Source, Name: item.Name, Size: item.Size, Relative: rel, Media: isMedia}
			var previous *quarkLibraryFile
			if f, exists := known[rel]; exists {
				previous = &f
				manifest.Key = f.Key
			}
			if digest, e := quarkLibraryDigest(root, rel); e == nil {
				if previous == nil || digest != previous.Digest {
					a.store.event("warn", "virtual", "夸克链库保留非本项目或已修改文件："+card.Name+" · "+rel)
					result.Skipped++
					continue
				}
				if previous.Stage != "transfer" && previous.ShareStamp == manifest.ShareStamp && previous.SourceID == manifest.SourceID && previous.Size == manifest.Size {
					if !previous.Published {
						previous.Published = true
						previous.Stage = ""
						if err := a.store.update(func(st *State) error { st.QuarkLibraryRuntime.Files[previous.Key] = *previous; return nil }); err != nil {
							return result, err
						}
					}
					result.Skipped++
					continue
				}
			} else if !os.IsNotExist(e) {
				return result, e
			}
			var content []byte
			if isMedia {
				base := strings.TrimRight(a.currentSTRMSettings().Address, "/")
				content = []byte(base + "/q/" + manifest.Key + "/" + url.PathEscape(item.Name) + "\n")
			} else {
				if item.Size < 0 || item.Size > 64<<20 {
					return result, errors.New("元数据超过64MiB或大小无效，未转存")
				}
				manifest.Stage = "transfer"
				if previous != nil {
					manifest.Digest = previous.Digest
				}
				if err := a.saveQuarkLibraryFile(manifest); err != nil {
					return result, err
				}
				f, storage, e := a.quarkLibraryTransfer(ctx, card, manifest, &s, token, item)
				if e != nil {
					return result, e
				}
				d, e := a.downloadWithUA(ctx, storage, f.FileID, "", "Aether")
				if e != nil {
					return result, e
				}
				if cookie := d.Headers.Get("Cookie"); cookie != "" {
					s.Config["cookie"] = cookie
				}
				input := &davFile{ctx: ctx, info: davInfo{File{Name: item.Name, Size: f.Size}}, download: d}
				content, e = io.ReadAll(io.LimitReader(input, 64<<20+1))
				input.Close()
				if e != nil || len(content) > 64<<20 || int64(len(content)) != item.Size {
					return result, errors.New("元数据下载失败、不完整或超过64MiB")
				}
			}
			sum := sha256.Sum256(content)
			if err := a.quarkLibraryCheckExecution(ctx, card); err != nil {
				return result, err
			}
			manifest.Digest = hex.EncodeToString(sum[:])
			manifest.Stage = "publish"
			if err := a.quarkLibraryCheckStorage(s); err != nil {
				return result, err
			}
			if err := a.saveQuarkLibraryFile(manifest); err != nil {
				return result, err
			}
			child, e := virtualOutputRoot(root, path.Dir(rel))
			if e != nil {
				_ = a.store.update(func(st *State) error {
					if previous != nil {
						st.QuarkLibraryRuntime.Files[previous.Key] = *previous
					} else {
						delete(st.QuarkLibraryRuntime.Files, manifest.Key)
					}
					return nil
				})
				return result, e
			}
			tmp := ".aether-quark-" + id()
			out, e := child.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if e == nil {
				e = a.quarkLibraryCheckExecution(ctx, card)
				if e == nil {
					_, e = io.Copy(out, bytes.NewReader(content))
				}
				if e == nil {
					e = out.Sync()
				}
				closeErr := out.Close()
				if e == nil {
					e = closeErr
				}
			}
			if e == nil {
				e = a.quarkLibraryCheckExecution(ctx, card)
			}
			if e == nil {
				// Only a registered, unchanged previous file may be replaced.
				if _, statErr := child.Lstat(path.Base(rel)); statErr == nil {
					current, verify := quarkLibraryDigest(root, rel)
					if previous == nil || verify != nil || current != previous.Digest {
						e = errors.New("生成目标已被修改，不覆盖")
					} else {
						e = child.Rename(tmp, path.Base(rel))
					}
				} else if os.IsNotExist(statErr) {
					e = child.Link(tmp, path.Base(rel))
				} else {
					e = statErr
				}
			}
			_ = child.Remove(tmp)
			child.Close()
			if e != nil {
				_ = a.store.update(func(st *State) error {
					if previous != nil {
						st.QuarkLibraryRuntime.Files[previous.Key] = *previous
					} else {
						delete(st.QuarkLibraryRuntime.Files, manifest.Key)
					}
					return nil
				})
				return result, e
			}
			manifest.Published = true
			manifest.Stage = ""
			if err := a.store.update(func(st *State) error { st.QuarkLibraryRuntime.Files[manifest.Key] = manifest; return nil }); err != nil {
				return result, err
			}
			known[rel] = manifest
			if isMedia {
				result.Generated++
			} else {
				result.Metadata++
			}
		}
	}
	return result, nil
}

func (a *App) saveQuarkLibraryFile(file quarkLibraryFile) error {
	return a.store.update(func(st *State) error {
		initQuarkLibraryRuntime(st)
		if _, exists := st.QuarkLibraryRuntime.Files[file.Key]; !exists && len(st.QuarkLibraryRuntime.Files) >= 20000 {
			return errors.New("夸克链库生成记录已达20000项上限")
		}
		st.QuarkLibraryRuntime.Files[file.Key] = file
		return nil
	})
}
