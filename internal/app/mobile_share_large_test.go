package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func largeMobileShareItems(count int) []shareEntry {
	items := make([]shareEntry, count)
	for i := range items {
		items[i] = shareEntry{ID: fmt.Sprint(i), Name: fmt.Sprintf("%d.mkv", i), Token: fmt.Sprintf("root/%d", i)}
	}
	return items
}

func TestMobileShareLargePlan(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "mobile", Config: map[string]string{"mode": "native"}}
	for _, count := range []int{5000, 5001, 9980, 10001, 100000, 100001, 200000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			items := largeMobileShareItems(count)
			batches, err := a.planShare(context.Background(), s, "code", "", items)
			if err != nil || len(batches) != (count+498)/499 {
				t.Fatalf("count=%d batches=%d err=%v", count, len(batches), err)
			}
			all := []shareEntry{}
			for _, batch := range batches {
				if len(batch.Items) == 0 || len(batch.Items) > 499 || batch.Directory != "" || batch.Source != "root" {
					t.Fatal("invalid batch", len(batch.Items), batch.Directory, batch.Source)
				}
				all = append(all, batch.Items...)
			}
			if !reflect.DeepEqual(all, items) {
				t.Fatal("files missing, repeated or reordered")
			}
		})
	}
	if _, err := a.planShare(context.Background(), s, "code", "", largeMobileShareItems(200001)); err == nil {
		t.Fatal("unbounded scan accepted")
	}
	for _, provider := range []string{"115", "quark"} {
		if _, err := a.planShare(context.Background(), Storage{Type: provider}, "code", "", largeMobileShareItems(5001)); err == nil {
			t.Fatal("unrelated provider limit changed", provider)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.planShare(ctx, s, "code", "", largeMobileShareItems(5001)); err != context.Canceled {
		t.Fatal("cancelled scan continued", err)
	}
}

func TestMobileShareLargeNestedPlan(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	roots := []shareEntry{{ID: "one", Name: "Season 1", IsDir: true, Token: "root/one"}, {ID: "two", Name: "Season 2", IsDir: true, Token: "root/two"}}
	reads := 0
	shareMock(t, func(r *http.Request) string {
		reads++
		var body struct {
			Request struct {
				Parent string `json:"pCaID"`
				Start  int    `json:"bNum"`
				End    int    `json:"eNum"`
			} `json:"getOutLinkInfoReq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Request.Parent != "root/one" && body.Request.Parent != "root/two" || body.Request.Start != ((reads-1)%31)*100+1 || body.Request.End != ((reads-1)%31+1)*100 {
			t.Fatal("incorrect nested pagination", body.Request)
		}
		files := []map[string]string{}
		for i := body.Request.Start; i <= min(body.Request.End, 3001); i++ {
			files = append(files, map[string]string{"coID": fmt.Sprintf("%s/%d", body.Request.Parent, i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("%s/%d", body.Request.Parent, i)})
		}
		b, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"coLst": files}})
		return string(b)
	})
	batches, err := a.planShare(context.Background(), s, "code", "", roots)
	if err != nil || reads != 62 || len(batches) != 14 {
		t.Fatal("nested share truncated", len(batches), reads, err)
	}
	seen := map[string]bool{}
	for i, batch := range batches {
		root := roots[i/7]
		if batch.Directory != root.Name || batch.Source != root.ID || len(batch.Items) > 499 || len(batch.Items) == 0 {
			t.Fatal("nested hierarchy or batch changed", batch.Directory, batch.Source, len(batch.Items))
		}
		for _, item := range batch.Items {
			if seen[item.ID] || item.Token != item.ID || !strings.HasPrefix(item.ID, root.Token+"/") {
				t.Fatal("nested files duplicated or moved", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 6002 {
		t.Fatal("nested files missing", len(seen))
	}
}

func TestMobileShareLargeExactPageEnd(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	reads := 0
	shareMock(t, func(r *http.Request) string {
		reads++
		if reads == 51 {
			return `{"code":"0","data":{"coLst":[]}}`
		}
		files := []map[string]string{}
		for i := (reads-1)*100 + 1; i <= reads*100; i++ {
			files = append(files, map[string]string{"coID": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("root/%d", i)})
		}
		b, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"coLst": files}})
		return string(b)
	})
	_, items, err := a.readShare(context.Background(), s, "code", "")
	if err != nil || reads != 51 || len(items) != 5000 {
		t.Fatal("full final page not followed by terminal read", len(items), reads, err)
	}
}

func TestMobileShareLargeSelection(t *testing.T) {
	items := largeMobileShareItems(10001)
	batches := []shareBatch{}
	for start := 0; start < len(items); start += 499 {
		batches = append(batches, shareBatch{Items: items[start:min(start+499, len(items))]})
	}
	tree := shareTree(items, batches)
	excluded := make([]string, 5001)
	for i := range excluded {
		excluded[i] = items[i].Name
	}
	selected, err := selectedShareBatches(batches, tree, excluded)
	if err != nil {
		t.Fatal(err)
	}
	all := []shareEntry{}
	for _, batch := range selected {
		if len(batch.Items) > 499 {
			t.Fatal("oversized filtered batch")
		}
		all = append(all, batch.Items...)
	}
	if !reflect.DeepEqual(all, items[5001:]) || len(batches[0].Items) != 499 {
		t.Fatal("selection lost files or mutated original plan")
	}
}

func TestMobileShareLargeSelectionBoundary(t *testing.T) {
	tree := make([]shareTreeEntry, 200000)
	excluded := make([]string, len(tree))
	for i := range tree {
		name := fmt.Sprintf("%d.mkv", i)
		tree[i] = shareTreeEntry{Path: name, Name: name}
		excluded[i] = name
	}
	if _, err := selectedShareBatches(nil, tree, excluded); err != nil {
		t.Fatal("200000 exclusions rejected", err)
	}
	tree = append(tree, shareTreeEntry{Path: "extra.mkv", Name: "extra.mkv"})
	if _, err := selectedShareBatches(nil, tree, append(excluded, "extra.mkv")); err == nil {
		t.Fatal("exclusion safety limit removed")
	}
}

func TestMobileShareLargePreviewAndTransfer(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			const count = 5201
			reads, saves := 0, 0
			transferred := []string{}
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6":
					reads++
					var body struct {
						Request struct {
							Parent string `json:"pCaID"`
							Start  int    `json:"bNum"`
							End    int    `json:"eNum"`
						} `json:"getOutLinkInfoReq"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body.Request.Parent != "root" || body.Request.Start != (reads-1)*100+1 || body.Request.End != reads*100 {
						t.Fatal("incorrect paging", body.Request)
					}
					files := []map[string]string{}
					for i := body.Request.Start - 1; i < min(body.Request.End, count); i++ {
						files = append(files, map[string]string{"coID": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("root/%d", i)})
					}
					b, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"nodNum": count, "coLst": files}})
					return string(b)
				case "/user/route/qryRoutePolicy":
					return `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case "/file/list":
					return `{"success":true,"data":{"items":[]}}`
				case "/yun-share/richlifeApp/devapp/IBatchOprTask/createOuterLinkBatchOprTask":
					var body struct {
						Request struct {
							Info struct {
								Files  []string `json:"contentInfoList"`
								Dirs   []string `json:"catalogInfoList"`
								Parent string   `json:"newCatalogID"`
							} `json:"taskInfo"`
						} `json:"createOuterLinkBatchOprTaskReq"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if len(body.Request.Info.Files) == 0 || len(body.Request.Info.Files) > 499 || len(body.Request.Info.Dirs) != 0 || body.Request.Info.Parent != "target" {
						t.Fatal("incorrect batch submission")
					}
					saves++
					transferred = append(transferred, body.Request.Info.Files...)
					if failed && saves == 2 {
						return `{"code":"1","message":"upstream rejected"}`
					}
					return fmt.Sprintf(`{"code":"0","data":{"taskID":"job-%d"}}`, saves)
				default:
					t.Fatal("unexpected upstream call", r.URL.Path)
					return ""
				}
			})
			w := request(t, h, "POST", "/api/files/share/preview", map[string]any{"storageId": s.ID, "url": "https://yun.139.com/shareweb/#/w/i/code"}, cookie)
			var result struct {
				Preview string
				Batches int
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || reads != 53 || result.Batches != 11 {
				t.Fatal(w.Code, w.Body.String(), reads)
			}
			for i := 0; i < result.Batches; i++ {
				body := map[string]any{"storageId": s.ID, "preview": result.Preview, "parent": "target", "batch": i}
				w = request(t, h, "POST", "/api/files/share/batch", body, cookie)
				if failed && i == 1 {
					if w.Code != 400 || !strings.Contains(w.Body.String(), "勿直接重复提交") {
						t.Fatal("uncertain batch not reported", w.Code, w.Body.String())
					}
					body["batch"] = i + 1
					if next := request(t, h, "POST", "/api/files/share/batch", body, cookie); next.Code != 409 || saves != 2 {
						t.Fatal("continued failed plan", next.Code, saves)
					}
					break
				}
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if replay := request(t, h, "POST", "/api/files/share/batch", body, cookie); replay.Code != 409 || saves != i+1 {
					t.Fatal("replayed batch", replay.Code, saves)
				}
			}
			if !failed {
				want := []string{}
				for _, file := range largeMobileShareItems(count) {
					want = append(want, file.Token)
				}
				if saves != 11 || !reflect.DeepEqual(transferred, want) {
					t.Fatal("large transfer missing, duplicating or reordering files", saves, len(transferred))
				}
			}
		})
	}
}
