package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type fuseMountServer interface {
	Unmount() error
	Wait()
}

type mountProcess struct {
	server fuseMountServer
	done   chan struct{}
	point  string
}

type mountManager struct {
	app      *App
	mu       sync.Mutex
	active   map[string]*mountProcess
	failures map[string]string
}

func (m *mountManager) startAutomatic() {
	for _, mount := range m.app.store.snapshot().Mounts {
		if mount.Automount {
			if err := m.start(mount.ID); err != nil {
				m.app.store.event("error", "storage", mount.Name+" 自动挂载失败："+err.Error())
			}
		}
	}
}

func (m *mountManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.active {
		if err := m.stopLocked(key); err != nil {
			m.app.store.event("error", "storage", "退出时卸载失败："+err.Error())
			process := m.active[key]
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			detachErr := exec.CommandContext(ctx, "fusermount3", "-u", "-z", process.point).Run()
			cancel()
			if detachErr != nil {
				m.app.store.event("error", "storage", "退出时延迟卸载失败："+process.point)
			}
			if detachErr == nil {
				delete(m.active, key)
			}
		}
	}
}

func pathOverlaps(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	inside := func(base, name string) bool {
		rel, err := filepath.Rel(base, name)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return inside(a, b) || inside(b, a)
}

func mountTarget(mount MountConfig) string {
	return filepath.Join(mount.MountPoint, "AetherDrive")
}

// Only the dedicated child may be created; never hide existing user files.
func prepareMountTarget(mount MountConfig, create bool) error {
	root, err := os.OpenRoot(mount.MountPoint)
	if err != nil {
		return errors.New("挂载父目录不可访问")
	}
	defer root.Close()
	if create {
		if err := root.Mkdir("AetherDrive", 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("创建 AetherDrive 失败：%w", err)
		}
	}
	info, err := root.Lstat("AetherDrive")
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("AetherDrive 必须是普通目录，不能是文件或符号链接")
	}
	dir, err := root.Open("AetherDrive")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(1)
	if (err != nil && !errors.Is(err, io.EOF)) || len(entries) != 0 {
		return errors.New("AetherDrive 必须是可访问的空目录，不会覆盖已有文件")
	}
	return nil
}

func (a *App) validateMount(input *MountConfig) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 180 {
		return errors.New("请输入挂载名称")
	}
	if !filepath.IsAbs(input.MountPoint) || filepath.Clean(input.MountPoint) == string(filepath.Separator) {
		return errors.New("挂载点须为非根目录的绝对路径")
	}
	input.MountPoint = filepath.Clean(input.MountPoint)
	// The selected parent must already exist and contain no symlink components.
	real, err := filepath.EvalSymlinks(input.MountPoint)
	if err != nil || real != input.MountPoint {
		return errors.New("挂载点须为已存在的目录，且不能经过符号链接")
	}
	if input.UID < 0 || input.GID < 0 || uint64(input.UID) > 4294967295 || uint64(input.GID) > 4294967295 || input.Mode == 0 || input.Mode > 0777 {
		return errors.New("UID、GID 或八进制权限无效")
	}
	protected := []string{a.store.dir, a.dataDir, a.fuseCacheDirectory()}
	if runtime.GOOS == "linux" {
		protected = append(protected, "/proc", "/sys", "/dev", "/etc", "/usr", "/bin", "/sbin", "/lib", "/run", "/app")
	}
	for _, s := range a.store.snapshot().Storages {
		if s.Type == "local" && s.ID == input.StorageID {
			root, err := filepath.EvalSymlinks(s.Config["root"])
			if err == nil {
				source, sourceErr := relative(input.Source)
				if sourceErr != nil {
					return sourceErr
				}
				protected = append(protected, filepath.Join(root, filepath.FromSlash(source)))
			}
		}
	}
	for _, root := range protected {
		if pathOverlaps(root, mountTarget(*input)) {
			return fmt.Errorf("实际挂载目录 %s 与受保护目录 %s 重叠，请选择其他位置", mountTarget(*input), root)
		}
	}
	for _, old := range a.store.snapshot().Mounts {
		if old.ID != input.ID && (old.Name == input.Name || pathOverlaps(mountTarget(old), mountTarget(*input))) {
			return errors.New("挂载名称重复或挂载点相互包含")
		}
	}
	if err := prepareMountTarget(*input, false); err != nil {
		return err
	}
	if input.Source == "" {
		input.Source = "/"
	}
	if input.StorageID != "" {
		s, err := a.store.storage(input.StorageID)
		if err != nil {
			return err
		}
		if input.Source == "/" {
			input.Source = rootOf(s)
		}
		if _, err := a.rawList(a.ctx, s, input.Source); err != nil {
			return fmt.Errorf("源目录不可访问：%w", err)
		}
	}
	return nil
}

func mountedAt(point string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && unescape.Replace(fields[4]) == point {
			return true
		}
	}
	return false
}

func (m *mountManager) start(key string) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if process := m.active[key]; process != nil {
		select {
		case <-process.done:
			if err := m.stopLocked(key); err != nil {
				return err
			}
		default:
			return errors.New("挂载正在运行，请先卸载")
		}
	}
	defer func() {
		if err != nil {
			m.failures[key] = err.Error()
		}
	}()
	var mount MountConfig
	for _, item := range m.app.store.snapshot().Mounts {
		if item.ID == key {
			mount = item
		}
	}
	if mount.ID == "" {
		return errors.New("挂载不存在")
	}
	if runtime.GOOS != "linux" {
		return errors.New("FUSE 挂载仅在 Linux 容器中运行")
	}
	if _, err := os.Stat("/dev/fuse"); err != nil {
		return errors.New("缺少 /dev/fuse，请检查容器特权及设备映射")
	}
	point := mountTarget(mount)
	if mountedAt(point) {
		return errors.New("挂载点已被占用")
	}
	if err := m.app.validateMount(&mount); err != nil {
		return err
	}
	if err := prepareMountTarget(mount, true); err != nil {
		return err
	}
	server, err := startNativeFuse(m.app, mount)
	if err != nil {
		return fmt.Errorf("启动挂载引擎失败：%w", err)
	}
	process := &mountProcess{server: server, done: make(chan struct{}), point: point}
	m.active[key] = process
	go func() {
		server.Wait()
		close(process.done)
		m.app.store.event("info", "storage", mount.Name+" 挂载进程已退出")
	}()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-process.done:
			_ = m.stopLocked(key)
			return errors.New("挂载引擎启动失败，请查看存储服务日志")
		case <-timeout.C:
			_ = m.stopLocked(key)
			return errors.New("等待 FUSE 挂载超时，请查看存储服务日志")
		case <-m.app.ctx.Done():
			_ = m.stopLocked(key)
			return m.app.ctx.Err()
		case <-ticker.C:
			if mountedAt(point) {
				delete(m.failures, key)
				m.app.store.event("info", "storage", mount.Name+" 已挂载到 "+point)
				return nil
			}
		}
	}
}

func (m *mountManager) stopLocked(key string) error {
	process := m.active[key]
	if process == nil {
		return nil
	}
	if mountedAt(process.point) {
		err := process.server.Unmount()
		if err != nil {
			return errors.New("卸载失败，目录可能正被占用；关闭访问它的程序后重试")
		}
	}
	select {
	case <-process.done:
	case <-time.After(3 * time.Second):
		return errors.New("等待 FUSE 卸载超时，请稍后重试")
	}
	delete(m.active, key)
	delete(m.failures, key)
	m.app.store.event("info", "storage", "挂载已卸载："+process.point)
	return nil
}

func (a *App) localDirectories(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = string(filepath.Separator)
		if runtime.GOOS == "windows" {
			dir = filepath.VolumeName(a.dataDir) + string(filepath.Separator)
		}
	}
	if !filepath.IsAbs(dir) {
		fail(w, 400, errors.New("目录须为绝对路径"))
		return
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 400, errors.New("目录不可访问"))
		return
	}
	items := []map[string]string{}
	for _, e := range entries {
		if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			items = append(items, map[string]string{"name": e.Name(), "path": filepath.Join(dir, e.Name())})
		}
	}
	jsonResponse(w, 200, map[string]any{"path": dir, "parent": filepath.Dir(dir), "items": items})
}

func (a *App) mountAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		mounts := a.store.snapshot().Mounts
		a.mounts.mu.Lock()
		for i := range mounts {
			mounts[i].Status, mounts[i].LastError = "stopped", a.mounts.failures[mounts[i].ID]
			if process := a.mounts.active[mounts[i].ID]; process != nil {
				select {
				case <-process.done:
					mounts[i].LastError = "挂载进程已退出，请查看日志"
				default:
					mounts[i].Status = "mounted"
				}
			}
			if mounts[i].LastError != "" {
				mounts[i].Status = "error"
			}
		}
		a.mounts.mu.Unlock()
		if mounts == nil {
			mounts = []MountConfig{}
		}
		jsonResponse(w, 200, mounts)
		return
	}
	key := r.PathValue("id")
	a.mounts.mu.Lock()
	defer a.mounts.mu.Unlock()
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	if r.Method != http.MethodPost {
		found := false
		for _, item := range a.store.snapshot().Mounts {
			found = found || item.ID == key
		}
		if !found {
			fail(w, 404, errors.New("挂载不存在"))
			return
		}
		if a.mounts.active[key] != nil {
			fail(w, 409, errors.New("请先卸载，再编辑或删除挂载配置"))
			return
		}
	}
	var input MountConfig
	if r.Method != http.MethodDelete {
		if !decode(w, r, &input) {
			return
		}
		if r.Method == http.MethodPost {
			key = id()
		}
		input.ID = key
		input.Status, input.LastError = "", ""
		if err := a.validateMount(&input); err != nil {
			fail(w, 400, err)
			return
		}
	}
	err := a.store.update(func(st *State) error {
		for i := range st.Mounts {
			if st.Mounts[i].ID == key {
				if r.Method == http.MethodDelete {
					st.Mounts = append(st.Mounts[:i], st.Mounts[i+1:]...)
				} else {
					st.Mounts[i] = input
				}
				return nil
			}
		}
		st.Mounts = append(st.Mounts, input)
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	delete(a.mounts.failures, key)
	a.store.event("info", "storage", "保存挂载配置："+key)
	jsonResponse(w, 200, map[string]any{"id": key, "ok": true})
}

func (a *App) mountAction(w http.ResponseWriter, r *http.Request) {
	found := false
	for _, mount := range a.store.snapshot().Mounts {
		found = found || mount.ID == r.PathValue("id")
	}
	if !found {
		fail(w, 404, errors.New("挂载不存在"))
		return
	}
	var err error
	switch r.PathValue("action") {
	case "test":
		for _, mount := range a.store.snapshot().Mounts {
			if mount.ID != r.PathValue("id") {
				continue
			}
			pools := a.store.snapshot().Storages
			tested := 0
			for _, pool := range pools {
				if !pool.Enabled || (mount.StorageID != "" && pool.ID != mount.StorageID) {
					continue
				}
				source := rootOf(pool)
				if mount.StorageID != "" && mount.Source != "" {
					source = mount.Source
				}
				if _, err = a.rawList(r.Context(), pool, source); err != nil {
					break
				}
				tested++
			}
			if err == nil && tested == 0 {
				err = errors.New("没有可访问的源存储池")
			}
			if err == nil {
				_, err = os.Stat(mount.MountPoint)
			}
		}
	case "start":
		err = a.mounts.start(r.PathValue("id"))
	case "stop":
		a.mounts.mu.Lock()
		err = a.mounts.stopLocked(r.PathValue("id"))
		a.mounts.mu.Unlock()
	default:
		err = errors.New("不支持的挂载操作")
	}
	if err != nil {
		a.store.event("error", "storage", "挂载操作失败："+err.Error())
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
