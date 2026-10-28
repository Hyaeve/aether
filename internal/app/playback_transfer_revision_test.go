package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCASMobilePoolTriesNextAccount(t *testing.T) {
	a := testApp(t)
	storages := []Storage{}
	for _, account := range []string{"a", "b"} {
		storages = append(storages, Storage{ID: account, Name: account, Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:" + account + ":token")), "root": "/"}})
	}
	if err := a.store.update(func(st *State) error {
		st.Storages = storages
		st.CASPool = normalizedCASPool(CASPoolConfig{Mobile: CASPoolGroup{StorageIDs: []string{"a", "b"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	writes := []string{}
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		auth := r.Header.Get("Authorization")
		data := map[string]any{}
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			data["routePolicyList"] = []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}
		case "/file/list":
			name, fid := "Aether", "folder"
			if payload["parentFileId"] == "folder" {
				name, fid = "CAS", "cas-folder"
			}
			data["items"] = []any{map[string]string{"fileId": fid, "name": name, "type": "folder"}}
		case "/file/create":
			writes = append(writes, auth)
			data = map[string]any{"fileId": "file", "rapidUpload": len(writes) > 1}
		case "/file/getDownloadUrl":
			data = map[string]any{"cdnUrl": "https://media.example/movie", "cdnSwitch": true}
		default:
			t.Fatal("unexpected endpoint", r.URL.Path)
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	info := CASInfo{Name: "movie.mkv", Size: 123, SHA256: strings.Repeat("a", 64)}
	d, release, err := a.casDownload(context.Background(), Storage{}, streamClaim{CAS: &info})
	if err != nil || d.URL == "" || len(writes) != 2 || writes[0] == writes[1] {
		t.Fatal("did not try a different mobile account", d, err, writes)
	}
	release()
}

func TestLocalCopyDoesNotUseCloud100GiBGate(t *testing.T) {
	target := Storage{Type: "local", Config: map[string]string{"root": t.TempDir()}}
	if uploadFileLimit(target) <= 100<<30 {
		t.Fatal("local filesystem still restricted by cloud upload limit", uploadFileLimit(target))
	}
}

func Test115RedirectRetainsCredentialsOnlyWithinOfficialHTTPS(t *testing.T) {
	original := apiClient
	defer func() { apiClient = original }()
	for _, host := range []string{"cdn2.115cdn.com", "other.invalid"} {
		calls := 0
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://" + host + "/movie"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			}
			want := host == "cdn2.115cdn.com"
			if strings.Contains(r.Header.Get("Cookie"), "SEID=login") != want {
				t.Fatal("redirect credential scope incorrect", host)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
		})}
		f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 2}}, download: Download{URL: "https://cdn1.115cdn.com/movie", Headers: http.Header{"Cookie": {"UID=123_A1; CID=x; SEID=login"}}}}
		if _, err := f.Read(make([]byte, 2)); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func TestMobileCASMembershipLimitsAndUnavailableMetadata(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct {
		tier, client string
		limit        int64
	}{
		{"", "pc", 5_000_000_000}, {"silver", "pc", 8_000_000_000}, {"gold", "pc", 20_000_000_000}, {"diamond", "pc", 500_000_000_000}, {"diamond", "web", 50_000_000_000},
	} {
		s := mobilePCFixture(tc.client)
		if got := mobileCASTierLimit(s, tc.tier); got != tc.limit {
			t.Fatal(tc, got)
		}
		if tc.tier != "" {
			s.Usage = &storageUsageState{Stamp: storageRevision(s), storageUsage: storageUsage{Membership: membership(tc.tier, "member", time.Time{})}}
		}
		old := apiClient
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"userSubMemberList":[]}}`)), Request: r}, nil
		})}
		err := a.checkMobileCASSize(a.ctx, s, tc.limit)
		over := a.checkMobileCASSize(a.ctx, s, tc.limit+1)
		apiClient = old
		if err != nil || over == nil {
			t.Fatal(tc, err, over)
		}
	}
	original := apiClient
	defer func() { apiClient = original }()
	for _, body := range []string{`{"success":true,"data":{}}`, `{"success":true,"data":{"userSubMemberList":null}}`} {
		calls := 0
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if !strings.Contains(r.URL.Path, "queryUserBenefits") {
				t.Fatal("unexpected membership request", r.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		s := mobilePCFixture("web")
		s.ID = body
		if a.checkMobileCASSize(a.ctx, s, 30_000_000_000) == nil || calls != 1 {
			t.Fatal("unknown membership authorized big file", calls)
		}
		if err := a.checkMobileCASSize(a.ctx, s, 123); err != nil || calls != 1 {
			t.Fatal("small file needs no membership request")
		}
	}
}

func TestCASMobileTierPreflightSkipsWrites(t *testing.T) {
	for _, diamond := range []bool{false, true} {
		a := testApp(t)
		pools := []Storage{}
		for _, tier := range []string{"silver", "gold", "diamond"} {
			if tier == "diamond" && !diamond {
				continue
			}
			s := mobilePCFixture("pc")
			s.ID = tier
			s.Name = tier
			s.Config["authorization"] = base64.StdEncoding.EncodeToString([]byte("pc:" + tier + ":token"))
			s.Usage = &storageUsageState{Stamp: storageRevision(s), storageUsage: storageUsage{Membership: membership(tier, tier, time.Time{})}}
			pools = append(pools, s)
		}
		a.store.update(func(st *State) error {
			st.Storages = pools
			st.CASPool = normalizedCASPool(CASPoolConfig{Mobile: CASPoolGroup{StorageIDs: []string{"silver", "gold", "diamond"}}})
			return nil
		})
		writes := 0
		original := apiClient
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			data := map[string]any{}
			switch r.URL.Path {
			case "/user/route/qryRoutePolicy":
				data["routePolicyList"] = []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}
			case "/file/list":
				name, fid := "Aether", "folder"
				if payload["parentFileId"] == "folder" {
					name, fid = "CAS", "cas-folder"
				}
				data["items"] = []any{map[string]string{"fileId": fid, "name": name, "type": "folder"}}
			case "/file/create":
				writes++
				data = map[string]any{"fileId": "movie", "rapidUpload": true}
			case "/file/getDownloadUrl":
				data = map[string]any{"cdnUrl": "https://media.example/movie", "cdnSwitch": true}
			default:
				t.Fatal("unneeded membership or write request", r.URL.Path)
			}
			raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
		})}
		info := CASInfo{Name: "large.mkv", Size: 30_000_000_000, SHA256: strings.Repeat("a", 64)}
		_, release, err := a.casDownload(a.ctx, Storage{}, streamClaim{CAS: &info})
		apiClient = original
		if (err == nil) != diamond || writes != map[bool]int{false: 0, true: 1}[diamond] {
			t.Fatal(diamond, writes, err)
		}
		release()
	}
}

func TestDAVRangesShareOneEntryAndUniqueByteProgress(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "movie.mkv"), []byte("0123456789"), 0600)
	pool := Storage{ID: "local", Name: "local", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	a.store.update(func(st *State) error { st.Storages = []Storage{pool}; return nil })
	ctx := context.WithValue(context.WithValue(a.ctx, transferSourceKey{}, "WebDAV"), davReadClientKey{}, "one-client")
	read := func(offset int64, want string) {
		t.Helper()
		f, err := (davFS{a}).OpenFile(ctx, "/local/movie.mkv", os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.Seek(offset, io.SeekStart)
		buf := make([]byte, len(want))
		if _, err := io.ReadFull(f, buf); err != nil || string(buf) != want {
			t.Fatal(string(buf), err)
		}
		f.Close()
	}
	read(0, "0123")
	read(0, "01")
	if len(a.transfers.items) != 1 {
		t.Fatal("range generated duplicate rows", a.transfers.items)
	}
	for _, e := range a.transfers.items {
		if e.Done != 4 || e.Status != "running" {
			t.Fatal("partial read marked full success", e)
		}
	}
	read(4, "456789")
	for _, e := range a.transfers.items {
		if e.Done != 10 || e.Total != 10 || e.Status != "completed" {
			t.Fatal("wrong unique coverage", e)
		}
	}
	read(0, "0123")
	if len(a.transfers.items) != 1 {
		t.Fatal("recent complete range created duplicate")
	}
}

func TestDAVHTTPRangesKeepRealProgressAndSeparateClients(t *testing.T) {
	a := testApp(t)
	pool := addLocal(t, a)
	writeTest(t, filepath.Join(pool.Config["root"], "movie.mkv"), "0123456789")
	handler := a.Handler(t.TempDir())
	if res := request(t, handler, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil); res.Code != http.StatusCreated {
		t.Fatal(res.Code)
	}
	a.store.update(func(st *State) error { st.Settings.WebDAVEnabled = true; return nil })
	get := func(rng, agent string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/dav/"+pool.ID+"/movie.mkv", nil)
		r.SetBasicAuth("owner", "secret")
		r.Header.Set("Range", rng)
		r.Header.Set("User-Agent", agent)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 206 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	get("bytes=0-3", "Client/1")
	get("bytes=0-1", "Client/1")
	if len(a.transfers.items) != 1 {
		t.Fatal("duplicate HTTP range rows")
	}
	for _, entry := range a.transfers.items {
		if entry.Done != 4 || entry.Status != "running" {
			t.Fatal(entry)
		}
	}
	get("bytes=4-9", "Client/1")
	for _, entry := range a.transfers.items {
		if entry.Done != 10 || entry.Status != "completed" {
			t.Fatal(entry)
		}
	}
	get("bytes=0-9", "Client/2")
	if len(a.transfers.items) != 2 {
		t.Fatal("different clients merged")
	}
}

func TestDAVSessionFailureCooldownAndUnionCoverage(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	pool := Storage{ID: "p", Name: "pool", Type: "115", Config: map[string]string{"cookie": test115Cookie}}
	file := File{ID: "fid", Name: "movie.mkv", Size: 100}
	s := a.attachDAVRead(ctx, pool, file)
	s.add(30, 20)
	s.add(0, 40)
	s.add(10, 5)
	if s.covered != 50 {
		t.Fatal("overlap double counted", s.covered)
	}
	s.fail(&storageRequestError{message: "115 上游拒绝读取 HTTP 403，已刷新票据"})
	s.close()
	next := a.attachDAVRead(ctx, pool, file)
	if next != s || len(a.transfers.items) != 1 {
		t.Fatal("failure storm created repeated rows")
	}
	next.close()
	for _, entry := range a.transfers.items {
		if entry.Status != "failed" || entry.Done != 50 || !strings.Contains(entry.Message, "403") {
			t.Fatal("safe failure detail or coverage lost", entry)
		}
	}
}

func Test115RedirectNeverRestoresCredentialsAfterUntrustedHop(t *testing.T) {
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	for _, target := range []string{"https://cdn1.115cdn.com/movie", "https://cdn2.115cdn.com/movie"} {
		calls := 0
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			location := "https://untrusted.invalid/redirect"
			if calls == 2 {
				if r.Header.Get("Cookie") != "" {
					t.Fatal("login sent to an unrelated host")
				}
				location = target
			}
			if calls == 3 {
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Fatal("redirect through an untrusted hop restored credentials", target)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})}
		f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 2}}, download: Download{
			URL: "https://cdn1.115cdn.com/movie", Headers: http.Header{"Cookie": {"UID=uid; SEID=login; download_ticket=ticket"}, "Authorization": {"private"}},
		}}
		if _, err := io.ReadAll(f); err != nil || calls != 3 {
			t.Fatal(calls, err)
		}
		f.Close()
	}
}

func TestDAVFailureCooldownDoesNotExtendOnEveryRetry(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	pool := Storage{ID: "p", Type: "local"}
	file := File{ID: "movie", Name: "movie.mkv", Size: 10}
	s := a.attachDAVRead(ctx, pool, file)
	s.fail(errors.New("failed"))
	s.close()
	s.mu.Lock()
	s.last = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	// Active handles retain the failure; a retry cannot extend its terminal time.
	s.mu.Lock()
	s.refs = 1
	s.mu.Unlock()
	again := a.attachDAVRead(ctx, pool, file)
	if again != s {
		t.Fatal("active failed handle detached from its shared session")
	}
	again.close()
	s.mu.Lock()
	s.refs = 0
	s.mu.Unlock()
	next := a.attachDAVRead(ctx, pool, file)
	if next == s {
		t.Fatal("failed session cooldown was extended by retry")
	}
	next.fail(errors.New("done"))
	next.close()
}

func TestCASMobileAliasesAndUnknownSubmissionNeverReplay(t *testing.T) {
	a := testApp(t)
	pools := []Storage{}
	for _, name := range []string{"one", "one-alias", "two"} {
		account := name
		if name == "one-alias" {
			account = "one"
		}
		pools = append(pools, Storage{ID: name, Name: name, Type: "mobile", Enabled: true, Config: map[string]string{
			"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:" + account + ":token")), "root": "/",
		}})
	}
	if err := a.store.update(func(st *State) error {
		st.Storages = pools
		st.CASPool = normalizedCASPool(CASPoolConfig{Mobile: CASPoolGroup{StorageIDs: []string{"one", "one-alias", "two"}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writes := map[string]int{}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		data := map[string]any{}
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			data["routePolicyList"] = []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}
		case "/file/list":
			name, fid := "Aether", "folder"
			if payload["parentFileId"] == "folder" {
				name, fid = "CAS", "cas-folder"
			}
			data["items"] = []any{map[string]string{"fileId": fid, "name": name, "type": "folder"}}
		case "/file/create":
			writes[r.Header.Get("Authorization")]++
			return nil, errors.New("unknown create result")
		default:
			t.Fatal("unexpected endpoint", r.URL.Path)
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	info := CASInfo{Name: "movie.mkv", Size: 123, SHA256: strings.Repeat("a", 64)}
	for range 2 {
		_, release, err := a.casDownload(a.ctx, Storage{}, streamClaim{CAS: &info})
		release()
		if err == nil {
			t.Fatal("unknown submissions reported success")
		}
	}
	if len(writes) != 2 || len(a.store.snapshot().CASPlaybackAttempts) != 2 {
		t.Fatal("account aliases were retried or unknown intent lost", len(writes))
	}
	for _, count := range writes {
		if count != 1 {
			t.Fatal("unknown write replayed", count)
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(reopened.snapshot().CASPlaybackAttempts) != 2 {
		t.Fatal("restart lost unknown submission protection")
	}
	raw, _ := json.Marshal(a.store.jsonValues())
	if strings.Contains(string(raw), "casPlaybackAttempts") {
		t.Fatal("playback intents leaked into configuration modules")
	}
}

func TestMobileCASMembershipCacheCredentialAndExpiryIsolation(t *testing.T) {
	a := testApp(t)
	pool := mobilePCFixture("pc")
	calls := 0
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw := `{"success":true,"data":{"userSubMemberList":[{"memberLvName":"钻石会员"}]}}`
		if calls > 1 {
			raw = `{"success":true,"data":{"userSubMemberList":[]}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	if err := a.checkMobileCASSize(a.ctx, pool, 30_000_000_000); err != nil {
		t.Fatal(err)
	}
	if err := a.checkMobileCASSize(a.ctx, pool, 30_000_000_000); err != nil || calls != 1 {
		t.Fatal("fresh membership not reused", calls, err)
	}
	pool.Config["authorization"] = base64.StdEncoding.EncodeToString([]byte("pc:other:token"))
	if err := a.checkMobileCASSize(a.ctx, pool, 30_000_000_000); err == nil || calls != 2 {
		t.Fatal("membership crossed credentials", calls, err)
	}
	a.usageMu.Lock()
	cached := a.mobileCASMembers[pool.ID]
	cached.expires = time.Now().Add(-time.Second)
	a.mobileCASMembers[pool.ID] = cached
	a.usageMu.Unlock()
	if err := a.checkMobileCASSize(a.ctx, pool, 30_000_000_000); err == nil || calls != 3 {
		t.Fatal("expired member cache skipped refresh", calls, err)
	}
}

func TestDAVIdlePartialAndRequestCancellationDoNotFakeCompletion(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	pool := Storage{ID: "p", Type: "local"}
	file := File{ID: "movie", Name: "movie.mkv", Size: 10}
	s := a.attachDAVRead(ctx, pool, file)
	s.add(0, 4)
	s.fail(context.Canceled)
	s.close()
	s.expireIdle()
	if s.finished {
		t.Fatal("stale timer ended a recently closed range")
	}
	s.mu.Lock()
	s.last = time.Now().Add(-davReadSessionIdle)
	s.mu.Unlock()
	s.expireIdle()
	for _, entry := range a.transfers.items {
		if entry.Status != "skipped" || entry.Done != 4 || entry.Total != 10 {
			t.Fatal("partial read reported full completion", entry)
		}
	}
}

func TestDAVSharedPauseAndDeletionRespectRequestContexts(t *testing.T) {
	a := testApp(t)
	pool := addLocal(t, a)
	writeTest(t, filepath.Join(pool.Config["root"], "movie.mkv"), "0123456789")
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	one, err := (davFS{a}).OpenFile(ctx, "/"+pool.ID+"/movie.mkv", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	if _, err := one.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	session := one.(*davFile).readSession
	a.transfers.mu.Lock()
	entry := a.transfers.items[session.progress.id]
	entry.Status = "paused"
	a.transfers.items[entry.ID] = entry
	a.transfers.mu.Unlock()
	requestCtx, cancel := context.WithCancel(ctx)
	two, err := (davFS{a}).OpenFile(requestCtx, "/"+pool.ID+"/movie.mkv", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := two.Read(make([]byte, 1)); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("paused read ignored request cancellation")
	}
	two.Close()
	if session.progress.ctx.Err() != nil {
		t.Fatal("one request canceled sibling ranges")
	}
	a.transfers.mu.Lock()
	entry.Status = "running"
	a.transfers.items[entry.ID] = entry
	a.transfers.mu.Unlock()
	if _, err := one.Read(make([]byte, 1)); err != nil {
		t.Fatal("sibling could not resume", err)
	}
	session.progress.cancel()
	if _, err := one.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal("task cancellation did not stop shared read", err)
	}
}

func TestDAVRemoteRangeCoverageAnd403CooldownUseOneTransfer(t *testing.T) {
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	for _, rejected := range []bool{false, true} {
		a := testApp(t)
		pool := Storage{ID: "remote", Name: "115-media", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "device": "web"}}
		if err := a.store.update(func(st *State) error { st.Storages = []Storage{pool}; return nil }); err != nil {
			t.Fatal(err)
		}
		cdnCalls, refreshes := 0, 0
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			status, body := 200, ""
			header := http.Header{"Content-Type": {"application/json"}}
			switch r.URL.Path {
			case "/files":
				body = `{"state":true,"cid":"0","count":1,"data":[{"fid":"file","cid":"0","n":"movie.mkv","s":"10","pc":"pick"}]}`
			case "/movie":
				cdnCalls++
				if rejected {
					status = 403
					break
				}
				if r.Header.Get("Range") == "bytes=4-" {
					status, body = 206, "456789"
					header.Set("Content-Range", "bytes 4-9/10")
				} else {
					body = "0123456789"
				}
			case "/android/2.0/ufile/download":
				refreshes++
				body = `{"state":false,"errno":987654,"error":"private-ticket-error"}`
			default:
				t.Fatal("unexpected remote request", r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
		_, file, err := (davFS{a}).resolve(ctx, "/115-media/movie.mkv")
		if err != nil {
			apiClient = original
			t.Fatal(err)
		}
		session := a.attachDAVRead(ctx, pool, file)
		session.download, session.linkAt = cdn115TestDownload(t, "ticket"), time.Now()
		address, _ := url.Parse(session.download.URL)
		address.Path = "/movie"
		session.download.URL = address.String()
		session.close()
		for i, offset := range []int64{0, 0, 4} {
			f, err := (davFS{a}).OpenFile(ctx, "/115-media/movie.mkv", os.O_RDONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			f.Seek(offset, io.SeekStart)
			n := 4
			if offset == 4 {
				n = 6
			}
			_, err = io.ReadFull(f, make([]byte, n))
			f.Close()
			if rejected && (err == nil || strings.Contains(err.Error(), "private-")) {
				t.Fatal("missing or unsafe read failure", err)
			}
			if !rejected && err != nil {
				t.Fatal(err)
			}
			if len(a.transfers.items) != 1 {
				t.Fatal("remote ranges generated duplicate transfer rows")
			}
			for _, entry := range a.transfers.items {
				if rejected && entry.Status != "failed" || !rejected && i < 2 && (entry.Status != "running" || entry.Done != 4) || !rejected && i == 2 && (entry.Status != "completed" || entry.Done != 10) {
					t.Fatal(entry)
				}
			}
		}
		apiClient = original
		if rejected && (cdnCalls != 1 || refreshes != 1) {
			t.Fatal("403 generated repeated ticket refreshes", cdnCalls, refreshes)
		}
	}
}

func TestDAVSharedRejectsMalformedAndTruncatedRanges(t *testing.T) {
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	for _, malformed := range []bool{false, true} {
		a := testApp(t)
		pool := Storage{ID: "p", Name: "pool", Type: "115"}
		file := File{ID: "movie", Name: "movie.mkv", Size: 10}
		ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
		session := a.attachDAVRead(ctx, pool, file)
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			header := http.Header{"Content-Range": {"bytes 4-9/10"}}
			body := "45"
			if malformed {
				header.Set("Content-Range", "bytes 5-9/10")
				body = "56789"
			}
			return &http.Response{StatusCode: 206, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		f := &davFile{ctx: ctx, app: a, info: davInfo{file}, offset: 4, readSession: session, progress: session.progress,
			download: Download{URL: "https://cdn.115cdn.com/movie"}}
		_, err := io.ReadAll(f)
		f.Close()
		if err == nil || !malformed && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatal("invalid or incomplete content accepted", malformed, err)
		}
		for _, entry := range a.transfers.items {
			if entry.Status != "failed" || entry.Done >= entry.Total {
				t.Fatal("invalid range reported complete", entry)
			}
		}
	}
}
