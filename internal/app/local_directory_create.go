package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func validLocalDirectoryName(name string) bool {
	if !safeName(name) || !filepath.IsLocal(name) || len(name) > 255 || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `<>"|?*`) {
		return false
	}
	for _, ch := range name {
		if unicode.IsControl(ch) {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return false
	}
	return true
}

// Like the browser, administrators may choose any accessible absolute local
// directory. Walk pinned roots so no symlink can redirect creation elsewhere.
func openLocalDirectoryParent(dir string) (*os.Root, error) {
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return nil, errors.New("目录须为绝对路径")
	}
	volume := filepath.VolumeName(dir)
	if volume != "" && (len(volume) != 2 || volume[1] != ':') {
		return nil, errors.New("不支持网络或设备路径")
	}
	parts := strings.FieldsFunc(strings.TrimPrefix(dir, volume), func(r rune) bool { return r == '/' || (filepath.Separator == '\\' && r == '\\') })
	for _, part := range parts {
		if part == "." || part == ".." {
			return nil, errors.New("目录不能包含相对路径段")
		}
	}
	root, err := os.OpenRoot(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		info, statErr := root.Lstat(part)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errors.New("目录不可访问或包含符号链接")
		}
		next, openErr := root.OpenRoot(part)
		root.Close()
		if openErr != nil {
			return nil, openErr
		}
		actual, statErr := next.Stat(".")
		if statErr != nil || !os.SameFile(info, actual) {
			next.Close()
			return nil, errors.New("目录已变化，请刷新后重试")
		}
		root = next
	}
	return root, nil
}

func (a *App) createLocalDirectory(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		fail(w, http.StatusUnauthorized, errors.New("请先登录"))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validLocalDirectoryName(input.Name) {
		fail(w, 400, errors.New("目录名称无效"))
		return
	}
	root, err := openLocalDirectoryParent(input.Path)
	if err != nil {
		fail(w, 400, errors.New("父目录不可访问，或路径包含符号链接/无效路径段"))
		return
	}
	defer root.Close()
	if err := root.Mkdir(input.Name, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			fail(w, 409, errors.New("同名文件或目录已存在"))
			return
		}
		fail(w, 400, errors.New("创建目录失败，请检查父目录写入权限"))
		return
	}
	jsonResponse(w, http.StatusCreated, map[string]string{"path": filepath.Join(input.Path, input.Name), "name": input.Name})
}
