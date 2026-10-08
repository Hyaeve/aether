package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

const taskVideoExtensions = "mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso"
const taskAudioExtensions = "mp3;flac;wav;aac;m4a;ogg;wma;ape;alac;opus"

var extensionToken = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

func normalizeExtensions(value string) (string, error) {
	if len(value) > 2048 {
		return "", errors.New("扩展名列表过长")
	}
	tokens := []string{}
	seen := map[string]bool{}
	for _, token := range strings.Split(value, ";") {
		token = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(token)), ".")
		if token == "" {
			continue
		}
		if !extensionToken.MatchString(token) {
			return "", errors.New("扩展名须以英文分号分隔，仅包含字母或数字")
		}
		if !seen[token] {
			tokens = append(tokens, token)
			seen[token] = true
		}
	}
	return strings.Join(tokens, ";"), nil
}
func taskMedia(t Task, name string) bool {
	extensions := t.MediaExtensions
	if strings.TrimSpace(extensions) == "" {
		extensions = taskVideoExtensions + ";" + taskAudioExtensions
	}
	return excludedType(name, extensions)
}
func (a *App) copyTaskMetadata(ctx context.Context, s Storage, f File, root *os.Root, name string, incremental bool) error {
	download, err := a.download(ctx, s, f.ID, f.PickCode)
	if err != nil {
		return err
	}
	var input io.ReadCloser
	if s.Type == "local" {
		source, e := os.OpenRoot(s.Config["root"])
		if e != nil {
			return e
		}
		input, err = source.Open(download.Local)
		source.Close()
	} else {
		input = &davFile{ctx: ctx, info: davInfo{f}, download: download}
	}
	if err != nil {
		return err
	}
	defer input.Close()
	if err = root.MkdirAll(path.Dir(name), 0755); err != nil {
		return err
	}
	temp := path.Join(path.Dir(name), ".aether-meta-"+id())
	out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	n, err := io.Copy(out, io.LimitReader(&cancelReader{ctx, input}, 64<<20+1))
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n > 64<<20 {
		return errors.New("元数据文件超过64MiB")
	}
	if incremental {
		return root.Link(temp, name)
	}
	return root.Rename(temp, name)
}
