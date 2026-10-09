package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestMobileShareDirectoryResponse(t *testing.T) {
	for _, tc := range []struct {
		name               string
		count              int
		duplicate, invalid bool
		missingData        bool
		wantError          bool
	}{
		{name: "empty"},
		{name: "mixed", count: 3},
		{name: "over-page-size", count: 201},
		{name: "limit", count: 4999},
		{name: "too-large", count: 5000, wantError: true},
		{name: "duplicate", count: 3, duplicate: true, wantError: true},
		{name: "invalid", count: 3, invalid: true, wantError: true},
		{name: "missing-data", missingData: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			s := Storage{Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
			calls := 0
			shareMock(t, func(r *http.Request) string {
				calls++
				var body struct {
					Request map[string]any `json:"getOutLinkInfoReq"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if r.Method != "POST" || r.URL.String() != mobileShareBase+"IOutLink/getOutLinkInfoV6" || len(body.Request) != 4 || body.Request["pCaID"] != "folder" || body.Request["account"] != "account" || body.Request["linkID"] != "link" || body.Request["passwd"] != "pass" {
					t.Fatalf("unexpected protocol: %s %v", r.URL, body.Request)
				}
				if tc.missingData {
					return `{"code":"0","data":null}`
				}
				files := []map[string]string{}
				for i := 0; i < tc.count; i++ {
					files = append(files, map[string]string{"coId": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("root/folder/%d", i)})
				}
				if tc.duplicate {
					files[1]["coId"] = files[0]["coId"]
				}
				if tc.invalid {
					files[0]["coName"] = ""
				}
				dirs := []map[string]string{}
				if tc.count > 0 {
					dirs = append(dirs, map[string]string{"caId": "dir", "caName": "子目录"})
				}
				// nodNum is not the size of this directory; it must not drive pagination.
				b, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"nodNum": 9001, "caLst": dirs, "coLst": files}})
				return string(b)
			})
			_, items, err := a.readShareAt(context.Background(), s, "link", "pass", "folder")
			if (err != nil) != tc.wantError || calls != 1 {
				t.Fatalf("calls=%d items=%d err=%v", calls, len(items), err)
			}
			if err == nil {
				want := tc.count
				if want > 0 {
					want++
				}
				if len(items) != want {
					t.Fatalf("items=%d want=%d", len(items), want)
				}
			}
		})
	}
}
