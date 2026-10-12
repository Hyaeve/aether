package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

const generationLimit = 32 << 20
const generationEntries = 10000

type GenerationEntry struct {
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	CAS   *CASInfo  `json:"cas,omitempty"`
	ED2K  *ED2KInfo `json:"ed2k,omitempty"`
	Error string    `json:"error,omitempty"`
}
type generationManifest struct {
	User    string            `json:"user"`
	Kind    string            `json:"kind"`
	Entries []GenerationEntry `json:"entries"`
	Created time.Time         `json:"created"`
}
type GenerationJob struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Processed int       `json:"processed"`
	Total     int       `json:"total"`
	Success   int       `json:"success"`
	Failed    int       `json:"failed"`
	Created   time.Time `json:"created"`
	Preview   string    `json:"preview"`
}

var ed2kTextPattern = regexp.MustCompile(`(?i)ed2k://\|file\|[^\r\n]+?\|/`)

func parseED2KText(raw string) (ED2KInfo, error) {
	parts := strings.Split(strings.TrimSpace(raw), "|")
	if len(parts) != 6 || !strings.EqualFold(parts[0], "ed2k://") || !strings.EqualFold(parts[1], "file") || parts[5] != "/" {
		return ED2KInfo{}, errors.New("无效的 ED2K 链接")
	}
	name, err := url.PathUnescape(parts[2])
	if err != nil {
		return ED2KInfo{}, err
	}
	size, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return ED2KInfo{}, errors.New("ED2K 大小无效")
	}
	info := ED2KInfo{Name: name, Size: size, Hash: strings.ToLower(parts[4])}
	return info, validED2KInfo(info)
}
func parseCASContent(raw []byte) (CASInfo, error) {
	raw = bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))
	decoded := raw
	if len(decoded) > 0 && decoded[0] != '{' {
		var e error
		decoded, e = base64.StdEncoding.DecodeString(string(raw))
		if e != nil {
			decoded, e = base64.RawStdEncoding.DecodeString(string(raw))
		}
		if e != nil {
			return CASInfo{}, errors.New("CAS 内容不是 JSON 或 Base64")
		}
	}
	var name struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(decoded, &name) != nil || !safeName(name.Name) {
		return CASInfo{}, errors.New("CAS 缺少有效文件名称")
	}
	return decodeCAS(decoded, name.Name+".cas")
}
func generationOutputName(name, kind string) string {
	return strings.TrimSuffix(name, path.Ext(name)) + "." + kind + ".strm"
}
func parseGenerationText(kind, text string) []GenerationEntry {
	entries := []GenerationEntry{}
	add := func(raw string) {
		if len(entries) > generationEntries {
			return
		}
		if len(entries) == generationEntries {
			entries = append(entries, GenerationEntry{Path: "无效内容", Error: "解析条目超过10000项"})
			return
		}
		entry := GenerationEntry{}
		if kind == "ed2k" {
			i, e := parseED2KText(raw)
			if e != nil {
				entry.Error = e.Error()
			} else {
				entry.ED2K = &i
				entry.Size = i.Size
				entry.Path = generationOutputName(i.Name, kind)
			}
		} else {
			i, e := parseCASContent([]byte(raw))
			if e != nil {
				entry.Error = e.Error()
			} else {
				i.Provider, i.PlaybackURL, i.RetentionHours = "", "", 0
				entry.CAS = &i
				entry.Size = i.Size
				entry.Path = generationOutputName(i.Name, kind)
			}
		}
		if entry.Error != "" {
			entry.Path = fmt.Sprintf("无效内容 %d", len(entries)+1)
		}
		entries = append(entries, entry)
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(text, "{") && kind == "cas" {
		d := json.NewDecoder(strings.NewReader(text))
		for {
			var raw json.RawMessage
			e := d.Decode(&raw)
			if e == io.EOF {
				break
			}
			if e != nil {
				add("invalid")
				break
			}
			add(string(raw))
			if len(entries) > generationEntries {
				break
			}
		}
	} else {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if kind == "ed2k" {
				matches := ed2kTextPattern.FindAllString(line, -1)
				if len(matches) == 0 {
					add(line)
				} else {
					for _, m := range matches {
						add(m)
					}
				}
			} else {
				add(line)
			}
			if len(entries) > generationEntries {
				break
			}
		}
	}
	return entries
}
func collapseGenerationPath(name string) string {
	parts := strings.Split(name, "/")
	// Remove only identical wrapper directories; do not strip distinct parents.
	for len(parts) > 2 && strings.EqualFold(parts[0], parts[1]) {
		parts = parts[1:]
	}
	return strings.Join(parts, "/")
}
func parseGenerationArchive(ctx context.Context, kind, name string, data []byte) ([]GenerationEntry, error) {
	entries := []GenerationEntry{}
	count := 0
	total := int64(0)
	names := map[string]bool{}
	add := func(name string, mode fs.FileMode, size int64, r io.Reader) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		count++
		if count > generationEntries || !extractName(strings.TrimPrefix(name, "./")) || mode.Type() != 0 && !mode.IsDir() || size < 0 || size > casLimit || total+size > 64<<20 {
			return errors.New("压缩包路径、文件大小或展开限额无效（每文件1MiB，合计64MiB，10000项）")
		}
		name = strings.TrimPrefix(name, "./")
		key := strings.ToLower(strings.TrimSuffix(name, "/"))
		if names[key] {
			return errors.New("压缩包包含重复路径")
		}
		names[key] = true
		if mode.IsDir() {
			return nil
		}
		total += size
		raw, e := io.ReadAll(io.LimitReader(&cancelReader{ctx, r}, size+1))
		if e != nil {
			return e
		}
		if int64(len(raw)) != size {
			return errors.New("压缩包内容不完整")
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ".txt" && ext != ".cas" && ext != ".ed2k" {
			return nil
		}
		parsed := parseGenerationText(kind, string(raw))
		for i := range parsed {
			if parsed[i].Error == "" {
				parsed[i].Path = path.Join(path.Dir(collapseGenerationPath(name)), parsed[i].Path)
			}
		}
		entries = append(entries, parsed...)
		if len(entries) > generationEntries {
			return errors.New("解析条目超过10000项")
		}
		return nil
	}
	switch archiveFormat(name) {
	case ".zip":
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range z.File {
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			e = add(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
			ce := r.Close()
			if e != nil {
				return nil, e
			}
			if ce != nil {
				return nil, ce
			}
		}
	case ".rar":
		z, e := rardecode.NewReader(bytes.NewReader(data), rardecode.MaxDictionarySize(64<<20))
		if e != nil {
			return nil, e
		}
		for {
			h, e := z.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.LinkType != rardecode.LinkTypeNone || h.Mode()&fs.ModeSymlink != 0 {
				return nil, errors.New("压缩包不支持链接成员")
			}
			if e = add(h.Name, h.Mode(), h.UnPackedSize, z); e != nil {
				return nil, e
			}
		}
	case ".7z":
		z, e := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range z.File {
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			e = add(f.Name, f.Mode(), int64(f.UncompressedSize), r)
			r.Close()
			if e != nil {
				return nil, e
			}
		}
	default:
		return nil, errors.New("支持 ZIP、RAR、7z 压缩包")
	}
	return entries, nil
}
func (a *App) generationManifestPath(key string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("预览编号无效")
	}
	for _, c := range key {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_", c) {
			return "", errors.New("预览编号无效")
		}
	}
	return filepath.Join(a.dataDir, "generation-preview", key+".enc"), nil
}
func (a *App) writeGenerationManifest(key string, m generationManifest) error {
	a.generationPreviewMu.Lock()
	defer a.generationPreviewMu.Unlock()
	name, e := a.generationManifestPath(key)
	if e != nil {
		return e
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	if len(b) > generationLimit-64 {
		return errors.New("预览内容过大")
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		return e
	}
	files, e := os.ReadDir(filepath.Dir(name))
	if e != nil {
		return e
	}
	active := map[string]bool{}
	for _, job := range a.store.snapshotWithLogLimit(0).GenerationJobs {
		if job.Status == "queued" || job.Status == "running" {
			active[job.Preview+".enc"] = true
		}
	}
	count, total := 0, int64(len(b)+64)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".enc") || f.Name() == key+".enc" {
			continue
		}
		info, err := f.Info()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !active[f.Name()] && time.Since(info.ModTime()) > 30*time.Minute {
			if err := os.Remove(filepath.Join(filepath.Dir(name), f.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		count++
		total += info.Size()
	}
	if count >= 64 || total > 256<<20 {
		return errors.New("解析预览暂存已满，请等待预览过期后重试")
	}
	return atomicWrite(name, a.store.aead.Seal(nonce, nonce, b, []byte("generation:"+key)))
}
func (a *App) readGenerationManifest(key string) (generationManifest, error) {
	var m generationManifest
	name, e := a.generationManifestPath(key)
	if e != nil {
		return m, e
	}
	b, e := os.ReadFile(name)
	if e != nil {
		return m, e
	}
	n := a.store.aead.NonceSize()
	if len(b) < n || len(b) > generationLimit {
		return m, errors.New("预览数据无效")
	}
	b, e = a.store.aead.Open(nil, b[:n], b[n:], []byte("generation:"+key))
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	if m.User != a.store.snapshotWithLogLimit(0).Username || time.Since(m.Created) > 30*time.Minute {
		return m, errors.New("解析预览已过期，请重新解析")
	}
	return m, nil
}
func (a *App) generationPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, generationLimit)
	kind := ""
	entries := []GenerationEntry{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if e := r.ParseMultipartForm(2 << 20); e != nil {
			fail(w, 400, errors.New("文件解析请求过大或无效"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		kind = r.FormValue("kind")
		if kind != "cas" && kind != "ed2k" {
			fail(w, 400, errors.New("解析类型无效"))
			return
		}
		files := r.MultipartForm.File["files"]
		if len(files) == 0 || len(files) > 100 {
			fail(w, 400, errors.New("请选择1至100个文件"))
			return
		}
		for _, f := range files {
			if f.Size > generationLimit {
				fail(w, 400, errors.New("单文件超过32MiB"))
				return
			}
			in, e := f.Open()
			if e != nil {
				fail(w, 400, e)
				return
			}
			data, e := io.ReadAll(io.LimitReader(in, generationLimit+1))
			in.Close()
			if e != nil || len(data) > generationLimit {
				fail(w, 400, errors.New("文件读取失败或过大"))
				return
			}
			var parsed []GenerationEntry
			if archiveFormat(f.Filename) != "" {
				parsed, e = parseGenerationArchive(r.Context(), kind, f.Filename, data)
			} else if ext := strings.ToLower(path.Ext(f.Filename)); ext == ".txt" || ext == ".cas" && kind == "cas" || ext == ".ed2k" && kind == "ed2k" {
				parsed = parseGenerationText(kind, string(data))
			} else {
				e = errors.New("文件类型不支持")
			}
			if e != nil {
				entries = append(entries, GenerationEntry{Path: f.Filename, Error: e.Error()})
			} else {
				entries = append(entries, parsed...)
			}
			if len(entries) > generationEntries {
				fail(w, 400, errors.New("解析条目超过10000项"))
				return
			}
		}
	} else {
		var in struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		kind = in.Kind
		if kind != "cas" && kind != "ed2k" {
			fail(w, 400, errors.New("解析类型无效"))
			return
		}
		entries = parseGenerationText(kind, in.Text)
	}
	if len(entries) == 0 {
		entries = append(entries, GenerationEntry{Path: "无效内容", Error: "没有找到有效内容"})
	}
	names := map[string]bool{}
	valid := true
	for i := range entries {
		entry := &entries[i]
		if entry.Error == "" {
			if !extractName(entry.Path) || names[strings.ToLower(entry.Path)] {
				entry.Error = "生成路径无效或重复"
			}
			names[strings.ToLower(entry.Path)] = true
		}
		if entry.Error != "" {
			valid = false
		}
	}
	key := ""
	m := generationManifest{User: a.store.snapshotWithLogLimit(0).Username, Kind: kind, Entries: entries, Created: time.Now()}
	if valid {
		key = id()
		if e := a.writeGenerationManifest(key, m); e != nil {
			fail(w, 500, e)
			return
		}
	}
	jsonResponse(w, 200, map[string]any{"preview": key, "items": entries, "valid": valid})
}
func (a *App) generationJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, a.store.snapshotWithLogLimit(0).GenerationJobs)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Preview string `json:"preview"`
		Target  string `json:"target"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	for _, job := range a.store.snapshotWithLogLimit(0).GenerationJobs {
		if job.Preview == in.Preview {
			jsonResponse(w, 200, job)
			return
		}
	}
	m, e := a.readGenerationManifest(in.Preview)
	if e != nil {
		fail(w, 400, e)
		return
	}
	for _, entry := range m.Entries {
		if entry.Error != "" {
			fail(w, 400, errors.New("预览含无效内容，不能生成"))
			return
		}
	}
	base, _, e := a.outputLocation(in.Target)
	if e != nil {
		fail(w, 400, e)
		return
	}
	if filepath.Clean(base) == filepath.Clean(a.outputDir) {
		if e = os.MkdirAll(base, 0755); e != nil {
			fail(w, 400, e)
			return
		}
	}
	if _, e = os.Stat(base); e != nil {
		fail(w, 400, errors.New("生成目录不存在"))
		return
	}
	job := GenerationJob{ID: id(), Preview: in.Preview, Kind: m.Kind, Name: strings.ToUpper(m.Kind) + " 解析生成", Target: in.Target, Total: len(m.Entries), Status: "queued", Created: time.Now()}
	if e = a.store.update(func(st *State) error {
		active := 0
		for _, j := range st.GenerationJobs {
			if j.Status == "running" || j.Status == "queued" {
				active++
			}
		}
		if active >= 8 {
			return errors.New("生成队列已满，请稍后再试")
		}
		if len(st.GenerationJobs) >= 100 {
			cut := -1
			for i, j := range st.GenerationJobs {
				if j.Status != "running" && j.Status != "queued" {
					cut = i
					break
				}
			}
			if cut >= 0 {
				st.GenerationJobs = append(st.GenerationJobs[:cut], st.GenerationJobs[cut+1:]...)
			}
		}
		st.GenerationJobs = append(st.GenerationJobs, job)
		return nil
	}); e != nil {
		fail(w, 400, e)
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.generationMu.Lock()
		defer a.generationMu.Unlock()
		for {
			var pending *GenerationJob
			for _, j := range a.store.snapshotWithLogLimit(0).GenerationJobs {
				if j.Status == "queued" {
					copy := j
					pending = &copy
					break
				}
			}
			if pending == nil {
				return
			}
			manifest, e := a.readGenerationManifest(pending.Preview)
			if e != nil {
				_ = a.generationUpdate(pending.ID, func(j *GenerationJob) { j.Status = "error"; j.Message = e.Error(); j.Failed = j.Total })
				continue
			}
			base, rel, e := a.outputLocation(pending.Target)
			if e != nil {
				_ = a.generationUpdate(pending.ID, func(j *GenerationJob) { j.Status = "error"; j.Message = e.Error(); j.Failed = j.Total })
				continue
			}
			a.executeGeneration(a.ctx, *pending, manifest, base, rel)
		}
	}()
	jsonResponse(w, 202, job)
}
func (a *App) generationUpdate(key string, fn func(*GenerationJob)) error {
	return a.store.update(func(st *State) error {
		for i := range st.GenerationJobs {
			if st.GenerationJobs[i].ID == key {
				fn(&st.GenerationJobs[i])
				return nil
			}
		}
		return errors.New("生成队列记录不存在")
	})
}
func (a *App) executeGeneration(ctx context.Context, job GenerationJob, m generationManifest, base, rel string) {
	if e := a.generationUpdate(job.ID, func(j *GenerationJob) { j.Status = "running" }); e != nil {
		return
	}
	root, e := os.OpenRoot(base)
	if e == nil {
		defer root.Close()
	}
	success := 0
	if e == nil {
		for _, entry := range m.Entries {
			if e = ctx.Err(); e != nil {
				break
			}
			filename := path.Join(rel, entry.Path)
			claim := streamClaim{CAS: entry.CAS, ED2K: entry.ED2K, Redirect: true}
			link := a.generatedStreamURL(claim)
			if _, statErr := root.Stat(filename); statErr == nil {
				e = fmt.Errorf("生成文件已存在，未覆盖：%s", entry.Path)
				break
			} else if !os.IsNotExist(statErr) {
				e = statErr
				break
			}
			if e = writeCASOutput(root, filename, []byte(link+"\n"), true, true); e != nil {
				break
			}
			success++
			if e = a.generationUpdate(job.ID, func(j *GenerationJob) { j.Processed = success; j.Success = success; j.Message = entry.Path }); e != nil {
				break
			}
		}
	}
	status, message := "success", fmt.Sprintf("生成完成：%d 项", success)
	failed := 0
	if e != nil {
		status, message, failed = "error", e.Error(), job.Total-success
	}
	if errors.Is(e, context.Canceled) {
		status = "interrupted"
	}
	_ = a.generationUpdate(job.ID, func(j *GenerationJob) {
		j.Status = status
		j.Message = message
		j.Processed = success
		j.Success = success
		j.Failed = failed
	})
	a.store.event(status, "tasks", job.Name+"："+message)
	if name, e := a.generationManifestPath(job.Preview); e == nil {
		_ = os.Remove(name)
	}
}
