package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/net/webdav"
)

func startNativeFuse(a *App, config MountConfig) (fuseMountServer, error) {
	timeout := time.Second
	options := &fs.Options{
		UID: uint32(config.UID), GID: uint32(config.GID),
		EntryTimeout: &timeout, AttrTimeout: &timeout,
		MountOptions: fuse.MountOptions{
			Options: []string{"default_permissions"},
			FsName:  "aether:" + config.Name, Name: "aether",
			AllowOther: true, DirectMount: true, DisableXAttrs: true,
			MaxWrite: 1024 * 1024, MaxReadAhead: 1024 * 1024,
		},
	}
	if config.ReadOnly {
		options.Options = append(options.Options, "ro")
	}
	return fs.Mount(mountTarget(config), &nativeFuseNode{backend: mountFS{app: a, config: config}}, options)
}

type nativeFuseNode struct {
	fs.Inode
	backend mountFS
	mu      sync.Mutex
	writer  *nativeFuseHandle
	attrMu  sync.RWMutex
	mtime   *time.Time
	atime   *time.Time
}

func (n *nativeFuseNode) name() string { return "/" + n.Path(nil) }

// The kernel checks configured modes, including supplementary groups, through
// default_permissions. Keep read-only enforcement here for direct callbacks too.
func (n *nativeFuseNode) Access(ctx context.Context, mask uint32) syscall.Errno {
	if mask & ^uint32(7) != 0 {
		return syscall.EINVAL
	}
	if mask&2 != 0 && n.backend.config.ReadOnly {
		return syscall.EROFS
	}
	_, err := n.backend.Stat(ctx, n.name())
	return fuseErr(err)
}

// Cloud writes require local staging; report that real capacity, not a zero volume
// or a made-up cloud quota. Local pools report their backing filesystem.
func (n *nativeFuseNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	location := n.backend.app.dataDir
	s, source, _, err := n.backend.selectPath(n.name())
	if err != nil {
		return fuseErr(err)
	}
	if s.Type == "local" {
		root, err := n.backend.localRoot(s, source)
		if err != nil {
			return fuseErr(err)
		}
		location = root.Name()
		defer root.Close()
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(location, &stat); err != nil {
		return fuseErr(err)
	}
	out.FromStatfsT(&stat)
	if n.backend.config.ReadOnly {
		out.Bavail = 0
	}
	return 0
}

func fuseErr(err error) syscall.Errno {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		return syscall.EINTR
	case errors.Is(err, context.DeadlineExceeded):
		return syscall.ETIMEDOUT
	default:
		return fs.ToErrno(err)
	}
}

func (n *nativeFuseNode) attributes(info os.FileInfo, out *fuse.Attr) {
	n.attrMu.RLock()
	defer n.attrMu.RUnlock()
	out.Mode = syscall.S_IFREG | n.backend.config.Mode&0666
	out.Nlink = 1
	if info.IsDir() {
		out.Mode = syscall.S_IFDIR | n.backend.config.Mode
		out.Nlink = 2
	}
	out.Uid, out.Gid = uint32(n.backend.config.UID), uint32(n.backend.config.GID)
	out.Size = uint64(max(info.Size(), 0))
	out.Blksize, out.Blocks = 4096, (out.Size+511)/512
	if t := info.ModTime(); !t.IsZero() && t.Unix() >= 0 {
		out.Mtime, out.Ctime, out.Atime = uint64(t.Unix()), uint64(t.Unix()), uint64(t.Unix())
	}
	if n.mtime != nil {
		out.Mtime, out.Mtimensec = uint64(n.mtime.Unix()), uint32(n.mtime.Nanosecond())
	}
	if n.atime != nil {
		out.Atime, out.Atimensec = uint64(n.atime.Unix()), uint32(n.atime.Nanosecond())
	}
}

func (n *nativeFuseNode) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.writer != nil {
		n.writer.mu.Lock()
		defer n.writer.mu.Unlock()
		info, err := n.writer.file.Stat()
		if err == nil {
			n.attributes(info, &out.Attr)
		}
		return fuseErr(err)
	}
	info, err := n.backend.Stat(ctx, n.name())
	if err == nil {
		n.attributes(info, &out.Attr)
	}
	return fuseErr(err)
}

func (n *nativeFuseNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if !safeName(name) {
		return nil, syscall.EINVAL
	}
	if child := n.GetChild(name); child != nil {
		node := child.Operations().(*nativeFuseNode)
		node.mu.Lock()
		pending := node.writer != nil
		node.mu.Unlock()
		if pending {
			var attr fuse.AttrOut
			errno := node.Getattr(ctx, nil, &attr)
			out.Attr = attr.Attr
			return child, errno
		}
	}
	info, err := n.backend.Stat(ctx, path.Join(n.name(), name))
	if err != nil {
		return nil, fuseErr(err)
	}
	n.attributes(info, &out.Attr)
	child := &nativeFuseNode{backend: n.backend}
	return n.NewInode(ctx, child, fs.StableAttr{Mode: out.Mode & syscall.S_IFMT}), 0
}

func (n *nativeFuseNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	file, err := n.backend.OpenFile(ctx, n.name(), os.O_RDONLY, 0)
	if err != nil {
		return nil, fuseErr(err)
	}
	defer file.Close()
	items, err := file.Readdir(-1)
	if err != nil && err != io.EOF {
		return nil, fuseErr(err)
	}
	entries, seen := []fuse.DirEntry{}, map[string]bool{}
	for _, item := range items {
		mode := uint32(syscall.S_IFREG)
		if item.IsDir() {
			mode = syscall.S_IFDIR
		}
		entries = append(entries, fuse.DirEntry{Name: item.Name(), Mode: mode})
		seen[item.Name()] = true
	}
	for name, child := range n.Children() {
		node := child.Operations().(*nativeFuseNode)
		node.mu.Lock()
		pending := node.writer != nil
		node.mu.Unlock()
		if pending && !seen[name] {
			entries = append(entries, fuse.DirEntry{Name: name, Mode: syscall.S_IFREG})
		}
	}
	return fs.NewListDirStream(entries), 0
}

func (n *nativeFuseNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.writer != nil {
		return nil, 0, syscall.EBUSY
	}
	write := flags&syscall.O_ACCMODE != syscall.O_RDONLY
	if write && n.backend.config.ReadOnly {
		return nil, 0, syscall.EROFS
	}
	var file webdav.File
	var staged *cloudWriteFile
	var err error
	fileContext := context.WithValue(n.backend.app.ctx, transferOwnedKey{}, true)
	if write {
		s, source, rel, e := n.backend.selectPath(n.name())
		if e != nil {
			return nil, 0, fuseErr(e)
		}
		if s.Type != "local" {
			staged, err = n.backend.openCloudWrite(n.backend.app.ctx, s, source, rel, os.O_TRUNC|os.O_RDWR)
			if err == nil {
				file = staged.File
				if flags&syscall.O_TRUNC == 0 {
					old, openErr := n.backend.OpenFile(fileContext, n.name(), os.O_RDONLY, 0)
					if openErr == nil {
						_, err = io.Copy(staged.File, old)
						old.Close()
					} else if !errors.Is(openErr, os.ErrNotExist) || flags&syscall.O_CREAT == 0 {
						err = openErr
					}
				}
			}
			if err != nil && staged != nil {
				staged.File.Close()
				os.Remove(staged.File.Name())
			}
		} else {
			file, err = n.backend.OpenFile(fileContext, n.name(), int(flags)&^syscall.O_APPEND, os.FileMode(n.backend.config.Mode&0666))
		}
	} else {
		file, err = n.backend.OpenFile(fileContext, n.name(), os.O_RDONLY, 0)
	}
	if err != nil {
		n.backend.app.store.event("error", "storage", "FUSE 打开文件失败："+n.name()+"："+err.Error())
		p := n.backend.app.beginTransfer(context.WithValue(ctx, transferSourceKey{}, "FUSE"), map[bool]string{true: "upload", false: "download"}[write], Storage{Name: n.backend.config.Name}, n.name(), 0)
		p.finish(err)
		return nil, 0, fuseErr(err)
	}
	handle := &nativeFuseHandle{node: n, file: file, staged: staged, writable: write, dirty: write && flags&(syscall.O_CREAT|syscall.O_TRUNC) != 0}
	if !write {
		s, source, rel, _ := n.backend.selectPath(n.name())
		if s.ID != "" && s.Type != "local" {
			if info, statErr := file.Stat(); statErr == nil && info.Size() > 0 {
				handle.cacheKey = fmt.Sprintf("%s\x00%s\x00%s\x00%x\x00%d\x00%d", s.ID, source, rel, shareConfig(s), info.Size(), info.ModTime().UnixNano())
				handle.cacheSize = info.Size()
				handle.cacheConfig = shareConfig(s)
			}
		}
	}
	{
		s, _, _, _ := n.backend.selectPath(n.name())
		handle.progress = n.backend.app.beginTransfer(context.WithValue(ctx, transferSourceKey{}, "FUSE"), map[bool]string{true: "upload", false: "download"}[write], s, n.name(), 0)
	}
	if write {
		n.writer = handle
	}
	return handle, fuse.FOPEN_DIRECT_IO, 0
}

func (n *nativeFuseNode) Create(ctx context.Context, name string, flags, _ uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	if !safeName(name) {
		return nil, nil, 0, syscall.EINVAL
	}
	if n.backend.config.ReadOnly {
		return nil, nil, 0, syscall.EROFS
	}
	if _, err := n.backend.Stat(ctx, path.Join(n.name(), name)); err == nil {
		return nil, nil, 0, syscall.EEXIST
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, 0, fuseErr(err)
	}
	node := &nativeFuseNode{backend: n.backend}
	child := n.NewInode(ctx, node, fs.StableAttr{Mode: syscall.S_IFREG})
	// Attach before opening so Path resolves the new file, including aggregate pools.
	if !n.AddChild(name, child, false) {
		return nil, nil, 0, syscall.EEXIST
	}
	handle, fuseFlags, errno := node.Open(ctx, flags|syscall.O_CREAT|syscall.O_TRUNC)
	if errno != 0 {
		n.RmChild(name)
		return nil, nil, 0, errno
	}
	var attr fuse.AttrOut
	errno = node.Getattr(ctx, handle, &attr)
	out.Attr = attr.Attr
	return child, handle, fuseFlags, errno
}

func (n *nativeFuseNode) Mkdir(ctx context.Context, name string, _ uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if !safeName(name) {
		return nil, syscall.EINVAL
	}
	if err := n.backend.Mkdir(ctx, path.Join(n.name(), name), os.FileMode(n.backend.config.Mode)); err != nil {
		n.backend.app.store.event("error", "storage", "FUSE 创建目录失败："+path.Join(n.name(), name)+"："+err.Error())
		return nil, fuseErr(err)
	}
	return n.Lookup(ctx, name, out)
}

func (n *nativeFuseNode) busyTree() bool {
	n.mu.Lock()
	busy := n.writer != nil
	n.mu.Unlock()
	if busy {
		return true
	}
	for _, child := range n.Children() {
		if child.Operations().(*nativeFuseNode).busyTree() {
			return true
		}
	}
	return false
}

func (n *nativeFuseNode) remove(ctx context.Context, name string, directory bool) syscall.Errno {
	if !safeName(name) {
		return syscall.EINVAL
	}
	if child := n.GetChild(name); child != nil && child.Operations().(*nativeFuseNode).busyTree() {
		return syscall.EBUSY
	}
	target := path.Join(n.name(), name)
	info, err := n.backend.Stat(ctx, target)
	if err != nil {
		return fuseErr(err)
	}
	if info.IsDir() != directory {
		if directory {
			return syscall.ENOTDIR
		}
		return syscall.EISDIR
	}
	if directory {
		file, err := n.backend.OpenFile(ctx, target, os.O_RDONLY, 0)
		if err != nil {
			return fuseErr(err)
		}
		items, err := file.Readdir(1)
		file.Close()
		if len(items) != 0 {
			return syscall.ENOTEMPTY
		}
		if err != nil && err != io.EOF {
			return fuseErr(err)
		}
	}
	return fuseErr(n.backend.RemoveAll(ctx, target))
}

func (n *nativeFuseNode) Unlink(ctx context.Context, name string) syscall.Errno {
	return n.remove(ctx, name, false)
}
func (n *nativeFuseNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	return n.remove(ctx, name, true)
}
func (n *nativeFuseNode) Rename(ctx context.Context, name string, parent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	target, ok := parent.(*nativeFuseNode)
	if !ok || target.backend.config.ID != n.backend.config.ID {
		return syscall.EXDEV
	}
	if !safeName(name) || !safeName(newName) || flags != 0 {
		return syscall.EINVAL
	}
	if child := n.GetChild(name); child != nil && child.Operations().(*nativeFuseNode).busyTree() {
		return syscall.EBUSY
	}
	to := path.Join(target.name(), newName)
	if _, err := n.backend.Stat(ctx, to); err == nil {
		return syscall.EEXIST
	} else if !errors.Is(err, os.ErrNotExist) {
		return fuseErr(err)
	}
	return fuseErr(n.backend.Rename(ctx, path.Join(n.name(), name), to))
}

func (n *nativeFuseNode) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if n.backend.config.ReadOnly {
		return syscall.EROFS
	}
	// Copy clients commonly reapply the current mode/owner and source timestamps.
	if in.Valid & ^uint32(fuse.FATTR_SIZE|fuse.FATTR_FH|fuse.FATTR_LOCKOWNER|fuse.FATTR_MODE|fuse.FATTR_UID|fuse.FATTR_GID|fuse.FATTR_ATIME|fuse.FATTR_MTIME|fuse.FATTR_ATIME_NOW|fuse.FATTR_MTIME_NOW) != 0 {
		return syscall.ENOTSUP
	}
	var current fuse.AttrOut
	if errno := n.Getattr(ctx, fh, &current); errno != 0 {
		return errno
	}
	if mode, ok := in.GetMode(); ok && mode != current.Mode&07777 {
		return syscall.ENOTSUP
	}
	if uid, ok := in.GetUID(); ok && uid != current.Uid {
		return syscall.ENOTSUP
	}
	if gid, ok := in.GetGID(); ok && gid != current.Gid {
		return syscall.ENOTSUP
	}
	if size, ok := in.GetSize(); ok {
		handle, valid := fh.(*nativeFuseHandle)
		if !valid {
			opened, _, errno := n.Open(ctx, syscall.O_RDWR)
			if errno != 0 {
				return errno
			}
			handle = opened.(*nativeFuseHandle)
			defer handle.Release(ctx)
		}
		handle.mu.Lock()
		file, ok := handle.file.(interface{ Truncate(int64) error })
		errno := syscall.EBADF
		if ok && handle.writable && size <= uint64(^uint64(0)>>1) {
			errno = fuseErr(file.Truncate(int64(size)))
			handle.dirty = handle.dirty || errno == 0
		}
		handle.mu.Unlock()
		if errno != 0 {
			return errno
		}
		if !valid {
			if errno := handle.Flush(ctx); errno != 0 {
				return errno
			}
		}
	}
	atime, hasA := in.GetATime()
	mtime, hasM := in.GetMTime()
	if hasA || hasM {
		if !hasA {
			atime = time.Unix(int64(current.Atime), int64(current.Atimensec))
		}
		if !hasM {
			mtime = time.Unix(int64(current.Mtime), int64(current.Mtimensec))
		}
		if atime.Unix() < 0 || mtime.Unix() < 0 {
			return syscall.EINVAL
		}
		s, source, rel, err := n.backend.selectPath(n.name())
		if err != nil {
			return fuseErr(err)
		}
		if s.Type == "local" {
			root, err := n.backend.localRoot(s, source)
			if err != nil {
				return fuseErr(err)
			}
			err = root.Chtimes(rel, atime, mtime)
			root.Close()
			if err != nil {
				return fuseErr(err)
			}
		}
		// Cloud drivers cannot set remote timestamps; retain virtual metadata for this inode.
		n.attrMu.Lock()
		n.atime, n.mtime = &atime, &mtime
		n.attrMu.Unlock()
	}
	return n.Getattr(ctx, fh, out)
}

type nativeFuseHandle struct {
	mu          sync.Mutex
	node        *nativeFuseNode
	file        webdav.File
	staged      *cloudWriteFile
	writable    bool
	dirty       bool
	offset      int64
	positioned  bool
	progress    *transferProgress
	cacheKey    string
	cacheSize   int64
	cacheConfig [32]byte
}

func (h *nativeFuseHandle) Read(ctx context.Context, data []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, fuseErr(err)
	}
	if h.file == nil {
		return nil, syscall.EBADF
	}
	if !h.positioned || h.offset != off {
		if _, err := h.file.Seek(off, io.SeekStart); err != nil {
			return nil, fuseErr(err)
		}
	}
	var n int
	var err error
	if h.cacheKey != "" && h.cacheSize > 0 {
		s, _, _, e := h.node.backend.selectPath(h.node.name())
		if e != nil {
			return nil, fuseErr(e)
		}
		if shareConfig(s) != h.cacheConfig {
			return nil, syscall.ESTALE
		}
		read := func(buf []byte, position int64) (int, error) {
			if _, e := h.file.Seek(position, io.SeekStart); e != nil {
				return 0, e
			}
			n, err := io.ReadFull(h.file, buf)
			h.progress.add(n)
			return n, err
		}
		key := fmt.Sprintf("%s:%d:%d", h.cacheKey, h.node.backend.app.cache.revision(), h.node.backend.app.fuseReadRevision.Load())
		n, err = h.node.backend.app.fuseCache().readAt(ctx, key, h.cacheSize, data, off, read)
		h.positioned = false
	} else {
		n, err = h.file.Read(data)
		h.progress.add(n)
	}
	h.offset = off + int64(n)
	h.positioned = h.cacheKey == ""
	if err != nil && err != io.EOF {
		h.progress.finish(err)
		h.node.backend.app.store.event("error", "storage", "FUSE 读取失败："+h.node.name()+"："+err.Error())
		return nil, fuseErr(err)
	}
	return fuse.ReadResultData(data[:n]), 0
}

func (h *nativeFuseHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, fuseErr(err)
	}
	if !h.writable || h.file == nil {
		return 0, syscall.EBADF
	}
	if _, err := h.file.Seek(off, io.SeekStart); err != nil {
		return 0, fuseErr(err)
	}
	n, err := h.file.Write(data)
	h.progress.add(n)
	if err != nil {
		h.progress.finish(err)
	}
	h.offset, h.positioned = off+int64(n), true
	h.dirty = h.dirty || n > 0
	return uint32(n), fuseErr(err)
}

func (h *nativeFuseHandle) Flush(ctx context.Context) syscall.Errno {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty || h.file == nil {
		return 0
	}
	if file, ok := h.file.(interface{ Sync() error }); ok {
		if err := file.Sync(); err != nil {
			return fuseErr(err)
		}
	}
	if h.staged != nil {
		f := h.staged
		err := f.publish(ctx)
		if err != nil {
			h.progress.finish(err)
			f.app.store.event("error", "storage", "FUSE 上传失败，暂存保留于 "+f.File.Name()+"："+err.Error())
			return fuseErr(err)
		}
	}
	h.dirty = false
	h.node.backend.app.cache.clear()
	return 0
}

func (h *nativeFuseHandle) Fsync(ctx context.Context, _ uint32) syscall.Errno {
	return h.Flush(ctx)
}
func (h *nativeFuseHandle) Release(_ context.Context) syscall.Errno {
	h.node.mu.Lock()
	defer h.node.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return 0
	}
	err := h.file.Close()
	if h.dirty {
		h.progress.finish(syscall.EIO)
	} else {
		h.progress.finish(err)
	}
	h.file = nil
	if h.node.writer == h {
		h.node.writer = nil
	}
	if h.staged != nil {
		if !h.dirty {
			os.Remove(h.staged.File.Name())
		} else {
			h.node.backend.app.store.event("warn", "storage", "FUSE 未完成上传，暂存保留于 "+h.staged.File.Name())
		}
	}
	return fuseErr(err)
}
