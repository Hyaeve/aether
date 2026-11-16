package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func (a *App) read115Inventory(ctx context.Context, s Storage, c *driver.Pan115Client, address string, query url.Values, out any) error {
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return err
	}
	res, err := c.NewRequest().SetDoNotParseResponse(true).SetQueryParamsFromValues(query).Get(address)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("115清单接口不可用")
	}
	defer res.RawBody().Close()
	if res.StatusCode() == 401 {
		return authStorageError(errors.New("115 CK已失效，请重新扫码"), true)
	}
	if res.StatusCode() != 200 {
		return fmt.Errorf("115清单接口HTTP %d", res.StatusCode())
	}
	raw, err := io.ReadAll(io.LimitReader(res.RawBody(), (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return errors.New("115清单响应无效或过大")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("115清单响应格式无效")
	}
	return ctx.Err()
}

type strm115Inventory struct{ files, folders int64 }

func (a *App) stat115Inventory(ctx context.Context, s Storage, c *driver.Pan115Client, dir, pick string) (strm115Inventory, error) {
	var result struct {
		State    *bool       `json:"state"`
		Kind     json.Number `json:"file_category"`
		PickCode string      `json:"pick_code"`
		Files    json.Number `json:"count"`
		Folders  json.Number `json:"folder_count"`
	}
	if err := a.read115Inventory(ctx, s, c, driver.ApiFileStat, url.Values{"cid": {dir}}, &result); err != nil {
		return strm115Inventory{}, err
	}
	files, fileErr := result.Files.Int64()
	folders, folderErr := result.Folders.Int64()
	if result.State != nil && !*result.State || result.PickCode != pick ||
		result.Kind != "" && result.Kind != "0" || fileErr != nil || folderErr != nil || files < 0 || folders < 0 {
		return strm115Inventory{}, errors.New("115目录独立统计缺少已核验的身份或数量")
	}
	return strm115Inventory{files, folders}, nil
}

func (want strm115Inventory) verify(dir string, files, folders int64) error {
	if want.files != files || want.folders != folders {
		return &storageRequestError{message: fmt.Sprintf("115目录 %.32q 清单与独立统计不符（文件 %d/%d，子目录 %d/%d）", dir, files, want.files, folders, want.folders)}
	}
	return nil
}

func verify115FallbackInventory(plan map[string][]File, checks map[string]strm115Inventory) error {
	for dir, want := range checks {
		var files, folders int64
		seen := map[string]bool{dir: true}
		var visit func(string) error
		visit = func(id string) error {
			entries, ok := plan[id]
			if !ok {
				return fmt.Errorf("115逐目录清单缺少独立统计已确认的目录 %.32q", id)
			}
			for _, f := range entries {
				if seen[f.ID] {
					return fmt.Errorf("115逐目录清单中项目 %.32q 重复出现", f.ID)
				}
				seen[f.ID] = true
				if f.IsDir {
					folders++
					if err := visit(f.ID); err != nil {
						return err
					}
				} else {
					files++
				}
			}
			return nil
		}
		if err := visit(dir); err != nil {
			return err
		}
		// Fresh per-directory pages can see additions made after the count
		// request. A deficit in either count still leaves gaps unexplained.
		if files < want.files || folders < want.folders {
			return want.verify(dir, files, folders)
		}
	}
	return nil
}

// The SDK represents missing fields as zero values. A confirmed STRM scan
// needs to distinguish an explicit empty listing from an incomplete response.
func (a *App) list115STRMDirectory(ctx context.Context, s Storage, dir string) ([]File, error) {
	pathGeneration := a.transferPathGeneration()
	if dir == "" || dir == "/" {
		dir = rootOf(s)
	}
	c, err := client115(ctx, s)
	if err != nil {
		return nil, err
	}
	const limit = 1150
	files := []File{}
	seen := map[string]bool{}
	var total int64
	for offset := int64(0); ; {
		fail := func(err error) ([]File, error) {
			return nil, &storageRequestError{message: fmt.Sprintf("115目录 %.32q 偏移 %d：%v", dir, offset, err), cause: err}
		}
		var page struct {
			driver.BasicResp
			CID    json.RawMessage    `json:"cid"`
			Count  json.Number        `json:"count"`
			Offset *int64             `json:"offset"`
			Data   *[]driver.FileInfo `json:"data"`
		}
		query := url.Values{
			"aid": {"1"}, "cid": {dir}, "cur": {"1"}, "show_dir": {"1"}, "fc_mix": {"1"},
			"o": {"user_ptime"}, "asc": {"1"}, "offset": {strconv.FormatInt(offset, 10)}, "limit": {strconv.Itoa(limit)}, "format": {"json"},
		}
		if err := a.read115Inventory(ctx, s, c, driver.ApiFileList, query, &page); err != nil {
			return fail(err)
		}
		if !page.State {
			if err := page.Err(); errors.Is(err, driver.ErrNotLogin) || errors.Is(err, driver.ErrBadCookie) {
				return fail(authStorageError(errors.New("115 CK已失效，请重新扫码"), true))
			}
			return fail(errors.New("115拒绝读取目录清单"))
		}
		count, err := page.Count.Int64()
		if lifeScalar(page.CID) != dir || err != nil || count < 0 || page.Data == nil || page.Offset != nil && *page.Offset != offset {
			return fail(errors.New("115目录清单缺少数量、数据，或目录身份及偏移不符"))
		}
		if offset == 0 {
			total = count
		}
		if count != total || len(*page.Data) > limit || int64(len(*page.Data)) > total-offset {
			return fail(errors.New("115目录清单数量在分页期间变化"))
		}
		for _, raw := range *page.Data {
			f := (&driver.File{}).From(&raw)
			if f.FileID == "" || f.FileID == dir || f.ParentID != dir || seen[f.FileID] {
				return fail(errors.New("115目录条目重复或父目录身份不符"))
			}
			seen[f.FileID] = true
			files = append(files, File{ID: f.FileID, Name: f.Name, IsDir: f.IsDirectory, Size: f.Size, Modified: f.UpdateTime, Created: f.CreateTime, PickCode: f.PickCode})
		}
		offset += int64(len(*page.Data))
		if offset == total {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			a.rememberTransferPaths(s, dir, files, pathGeneration)
			return files, nil
		}
		if len(*page.Data) == 0 {
			return fail(errors.New("115目录分页中断"))
		}
	}
}
