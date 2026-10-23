package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerationMixedMultilineCAS(t *testing.T) {
	raw := generationCASFixture()
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Replace(raw, "Arrival", "Encoded", 1)))
	var pretty any
	json.Unmarshal([]byte(strings.Replace(raw, "Arrival", "Pretty", 1)), &pretty)
	indented, _ := json.MarshalIndent(pretty, "", "  ")
	for _, input := range []string{raw + "\n" + encoded + "\n" + string(indented), encoded + "\r\n" + raw + "\n" + string(indented)} {
		items := parseGenerationText("cas", input)
		if len(items) != 3 {
			t.Fatal("multiline CAS entries lost", items)
		}
		for _, item := range items {
			if item.Error != "" || !strings.HasSuffix(item.Path, ".mkv.cas.strm") {
				t.Fatal(item)
			}
		}
	}
}

func mockMobileGeneration(t *testing.T, variant string) *int {
	t.Helper()
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Host != "share-kd-njs.yun.139.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected credential or upstream request")
		}
		body, _ := io.ReadAll(r.Body)
		sign := strings.Split(r.Header.Get("Mcloud-Sign"), ",")
		if len(sign) != 3 || sign[2] != strings.ToUpper(mobileMD5(string(body)+"|"+sign[0]+"|"+sign[1])) || r.Header.Get("X-Yun-Client-Info") == "" {
			t.Fatal("share request signature or client protocol headers invalid")
		}
		plain, err := mobileShareDecrypt(body)
		if err != nil {
			t.Fatal(err)
		}
		var request map[string]map[string]any
		if json.Unmarshal(plain, &request) != nil {
			t.Fatal("invalid encrypted share payload")
		}
		var data any
		switch filepath.Base(r.URL.Path) {
		case "getOutLinkGeneral":
			data = map[string]any{"getOutLinkGeneralResp": map[string]any{"outLinkGeneral": []any{map[string]any{"passwd": "auto"}}}}
		case "getOutLinkInfoV6":
			input := request["getOutLinkInfoReq"]
			if input["passwd"] != "Ab12" && input["passwd"] != "auto" {
				t.Fatal("password not forwarded")
			}
			if input["pCaID"] == "root" {
				data = map[string]any{"caLst": []any{map[string]any{"caID": "dir", "caName": "Season 1", "path": "root/dir"}}}
			} else {
				data = map[string]any{"coLst": []any{map[string]any{"coID": "file", "coName": "Episode.mkv", "coSize": 12, "path": "root/dir/file"}}}
				if variant == "many" {
					data = map[string]any{"coLst": []any{map[string]any{"coID": "file", "coName": "Episode.mkv", "coSize": 12, "path": "root/dir/file"}, map[string]any{"coID": "file2", "coName": "Episode2.mkv", "coSize": 12, "path": "root/dir/file2"}, map[string]any{"coID": "file3", "coName": "Episode3.mkv", "coSize": 12, "path": "root/dir/file3"}}}
				}
			}
		case "getContentInfoFromOutLink":
			info := map[string]any{"contentID": "file", "contentName": "Episode.mkv", "contentSize": 12}
			switch variant {
			case "valid":
				info["contentHash"] = strings.Repeat("A", 64)
				info["contentHashAlgorithm"] = "SHA256"
			case "explicit":
				info["sha256"] = strings.Repeat("b", 64)
			case "signature":
				info["presentURL"] = "https://cdn.example/stream?X-Amz-Signature=" + strings.Repeat("a", 64)
			case "md5":
				info["digest"] = strings.Repeat("a", 32)
			case "unlabelled":
				info["contentHash"] = strings.Repeat("a", 64)
			case "invalid":
				info["sha256"] = "not-a-hash"
			case "changed":
				info["contentName"] = "changed.mkv"
			}
			data = map[string]any{"contentInfo": info}
		default:
			t.Fatal("share parser invoked a write, download or unverified API", r.URL.Path)
		}
		raw, _ := json.Marshal(map[string]any{"resultCode": "0", "data": data})
		encrypted, err := mobileShareEncrypt(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(encrypted)), Request: r}, nil
	})}
	return &calls
}

func TestMobileGenerationMissingHashEnumeratesWithoutPerFileProbes(t *testing.T) {
	a := testApp(t)
	calls := mockMobileGeneration(t, "many")
	items, err := a.parseMobileGenerationLinks(context.Background(), "https://yun.139.com/shareweb/#/w/i/one")
	if err != nil || len(items) != 3 || *calls != 4 {
		t.Fatal("missing-hash share did not enumerate efficiently", len(items), *calls, err)
	}
	for _, item := range items {
		if item.Error != "" || item.CAS != nil || item.Mobile == nil {
			t.Fatal(item)
		}
	}
}

func TestMobileGenerationLinksReadOnlyAndMissingHash(t *testing.T) {
	for _, variant := range []string{"valid", "explicit", "missing", "signature", "md5", "unlabelled", "invalid", "changed"} {
		t.Run(variant, func(t *testing.T) {
			a := testApp(t)
			calls := mockMobileGeneration(t, variant)
			items, err := a.parseMobileGenerationLinks(context.Background(), "https://yun.139.com/shareweb/#/w/i/first?pwd=Ab12\nhttps://caiyun.139.com/m/i?second")
			if variant == "changed" {
				if err == nil {
					t.Fatal("accepted changed identity")
				}
				return
			}
			if err != nil || len(items) != 2 || *calls != 8 {
				t.Fatal(items, err, *calls)
			}
			for _, item := range items {
				if item.Path != "Season 1/Episode.mkv.cas.strm" {
					t.Fatal(item.Path)
				}
				valid := variant == "valid" || variant == "explicit"
				if valid != (item.CAS != nil) || (variant == "invalid") != (item.Error != "") || (!valid && variant != "invalid") != (item.Mobile != nil) {
					t.Fatal("accepted invalid or missing SHA256", item)
				}
			}
			if len(a.store.snapshot().Tasks) != 0 || len(a.store.snapshot().GenerationJobs) != 0 {
				t.Fatal("preview scheduled persistent work")
			}
		})
	}
}

func TestMobileGenerationQueueIsOneShot(t *testing.T) {
	a := testApp(t)
	calls := mockMobileGeneration(t, "valid")
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/generation/preview", map[string]string{"kind": "cas", "mode": "links", "text": "https://yun.139.com/shareweb/#/w/i/one"}, cookie)
	var preview struct {
		Preview string
		Valid   bool
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || !preview.Valid {
		t.Fatal(w.Code, w.Body.String())
	}
	target := t.TempDir()
	w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": preview.Preview, "target": target}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		jobs := a.store.snapshot().GenerationJobs
		if len(jobs) == 1 && jobs[0].Status == "success" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(target, "Season 1", "Episode.mkv.cas.strm"))
	if err != nil {
		t.Fatal(err)
	}
	generatedCASClaim(t, raw)
	if *calls != 4 || len(a.store.snapshot().Tasks) != 0 {
		t.Fatal("generation re-read share or created recurring tasks")
	}
	if w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": preview.Preview, "target": target}, cookie); w.Code != 200 || len(a.store.snapshot().GenerationJobs) != 1 {
		t.Fatal("queue replay was not idempotent")
	}
}

func TestMobileGenerationHTTPRejectsMissingHashAndUnauthenticated(t *testing.T) {
	a := testApp(t)
	calls := mockMobileGeneration(t, "missing")
	h := a.Handler(t.TempDir())
	input := map[string]string{"kind": "cas", "mode": "links", "text": "https://yun.139.com/shareweb/#/w/i/one"}
	if w := request(t, h, "POST", "/api/generation/preview", input, nil); w.Code != 401 || *calls != 0 {
		t.Fatal("unprotected link preview", w.Code)
	}
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/generation/preview", input, cookie)
	var preview struct {
		Preview string
		Valid   bool
		Items   []GenerationEntry
	}
	if w.Code != 400 || !strings.Contains(w.Body.String(), "没有可用的移动 CAS 绑定存储") {
		t.Fatal("missing hash without a pool created a usable preview", w.Code, w.Body.String())
	}
	if w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": preview.Preview, "target": t.TempDir()}, cookie); w.Code != 400 || len(a.store.snapshot().GenerationJobs) != 0 {
		t.Fatal("invalid preview entered generation queue")
	}
	logs, _ := json.Marshal(a.store.snapshot().Logs)
	if strings.Contains(string(logs), "shareweb") {
		t.Fatal("missing hash not safely logged")
	}
	input["kind"] = "ed2k"
	if w = request(t, h, "POST", "/api/generation/preview", input, cookie); w.Code != 400 {
		t.Fatal("ED2K accepted link mode")
	}
}

func TestMobileGenerationRejectsUnsafeInputsAndCiphertext(t *testing.T) {
	a := testApp(t)
	calls := mockMobileGeneration(t, "valid")
	for _, text := range []string{"https://example.com/secret", "http://yun.139.com/shareweb/#/w/i/a", "https://pan.quark.cn/s/a", strings.Repeat("https://yun.139.com/shareweb/#/w/i/a\n", 51)} {
		if _, err := a.parseMobileGenerationLinks(context.Background(), text); err == nil {
			t.Fatal("accepted unsafe or excessive input")
		}
	}
	if *calls != 0 {
		t.Fatal("invalid link reached upstream")
	}
	for _, raw := range []string{"bad", base64.StdEncoding.EncodeToString(make([]byte, 16)), base64.StdEncoding.EncodeToString(make([]byte, 32))} {
		if _, err := mobileShareDecrypt([]byte(raw)); err == nil {
			t.Fatal("accepted corrupt ciphertext")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.parseMobileGenerationLinks(ctx, "https://yun.139.com/shareweb/#/w/i/a"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
