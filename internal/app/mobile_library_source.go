package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type mobileLibraryShare struct{ Code, Pass, Name string }
type mobileLibrarySource struct {
	ID, Parent, Path, Name string
	Size                   int64
	Directory              bool
}

func mobileLibrarySecret(code, pass string) string {
	address := "https://yun.139.com/shareweb/" + code
	if pass != "" {
		address += "?pwd=" + url.QueryEscape(pass)
	}
	return address
}

func mobileLibrarySharePath(secret string) string {
	_, code, pass, _ := parseShareLink(secret, "")
	if pass != "" {
		code += "-" + pass
	}
	return code
}

func mobileLibraryURL(card quarkLibraryCard, relative, address string, encodedPath ...bool) string {
	return strings.TrimRight(address, "/") + "/139-share/" + url.PathEscape(mobileLibrarySharePath(card.Secret)) +
		"/" + shareFilenamePath(path.Base(shareSTRMSourceName(relative)), encodedPath...) + "\n"
}

func (a *App) mobileLibraryRequest(ctx context.Context, storage Storage, method string, payload, out any) error {
	if err := a.waitAPI(ctx, storage.ID); err != nil {
		return err
	}
	return mobileGenerationRequest(ctx, method, payload, out, &storage)
}

func (a *App) mobileLibraryShareInfo(ctx context.Context, storage Storage, secret string) (mobileLibraryShare, error) {
	provider, code, pass, err := parseShareLink(secret, "")
	if err != nil || provider != "mobile" {
		return mobileLibraryShare{}, errors.New("请输入有效的移动云盘官方分享链接和提取码")
	}
	var data struct {
		Response struct {
			Links []struct {
				Name string `json:"lkName"`
				Pass string `json:"passwd"`
			} `json:"outLinkGeneral"`
		} `json:"getOutLinkGeneralResp"`
	}
	err = a.mobileLibraryRequest(ctx, storage, "getOutLinkGeneral",
		map[string]any{"getOutLinkGeneralReq": map[string]any{"linkID": code, "isPasswd": 1}}, &data)
	if err != nil {
		return mobileLibraryShare{}, fmt.Errorf("读取移动分享信息：%w", err)
	}
	if len(data.Response.Links) != 1 {
		return mobileLibraryShare{}, errors.New("移动分享信息不完整，请核对链接及访问权限")
	}
	info := data.Response.Links[0]
	if pass == "" {
		pass = info.Pass
	}
	if pass != "" && (!shareCodePattern.MatchString(pass) || len(pass) > 32) {
		return mobileLibraryShare{}, errors.New("移动分享返回了无效提取码")
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = "移动分享-" + quarkLibraryStamp(code)[:8]
	}
	if len(name) > 255 {
		return mobileLibraryShare{}, errors.New("移动分享名称超过255字节，请核对分享名称")
	}
	return mobileLibraryShare{code, pass, name}, nil
}

func mobileLibraryResourcePath(parent, supplied, id string) (string, error) {
	value := strings.TrimPrefix(supplied, "/")
	if value == "" {
		if parent == "root" {
			value = id
		} else {
			value = strings.Trim(parent, "/") + "/" + id
		}
	}
	if len(value) > 4096 || !quarkLibraryRelative(value) {
		return "", errors.New("移动分享资源路径无效")
	}
	return value, nil
}

func (a *App) mobileLibraryList(ctx context.Context, storage Storage, share mobileLibraryShare, parent string, firstOnly bool) ([]mobileLibrarySource, error) {
	type entry struct {
		ID          string      `json:"coID"`
		ContentID   string      `json:"contentID"`
		CatalogID   string      `json:"catalogID"`
		DirectoryID string      `json:"caID"`
		Name        string      `json:"coName"`
		ContentName string      `json:"contentName"`
		DirName     string      `json:"caName"`
		CatalogName string      `json:"catalogName"`
		Path        string      `json:"path"`
		CoPath      string      `json:"coPath"`
		CaPath      string      `json:"caPath"`
		Size        json.Number `json:"coSize"`
		ContentSize json.Number `json:"contentSize"`
	}
	items := []mobileLibrarySource{}
	seen := map[string]bool{}
	requestParent := parent
	if parent != "root" {
		requestParent = "/" + strings.TrimLeft(parent, "/")
	}
	for page := 0; page <= mobileShareScanLimit/100; page++ {
		var data struct {
			Files []entry         `json:"coLst"`
			Dirs  []entry         `json:"caLst"`
			Next  json.RawMessage `json:"nextPageCursor"`
		}
		err := a.mobileLibraryRequest(ctx, storage, "getOutLinkInfoV6", map[string]any{
			"getOutLinkInfoReq": map[string]any{"account": "", "linkID": share.Code, "passwd": share.Pass,
				"pCaID": requestParent, "bNum": strconv.Itoa(page*100 + 1), "eNum": strconv.Itoa((page + 1) * 100),
				"caSrt": "0", "coSrt": "0", "srtDr": "1"},
		}, &data)
		if err != nil {
			return nil, fmt.Errorf("读取移动分享目录：%w", err)
		}
		for _, group := range []struct {
			list []entry
			dir  bool
		}{{data.Dirs, true}, {data.Files, false}} {
			for _, raw := range group.list {
				item := mobileLibrarySource{ID: raw.ID, Name: raw.Name, Parent: parent, Directory: group.dir, Size: -1}
				supplied := raw.Path
				if group.dir {
					item.ID, item.Name = raw.DirectoryID, raw.DirName
					if item.ID == "" {
						item.ID = raw.CatalogID
					}
					if item.Name == "" {
						item.Name = raw.CatalogName
					}
					if supplied == "" {
						supplied = raw.CaPath
					}
				} else {
					if item.ID == "" {
						item.ID = raw.ContentID
					}
					if item.Name == "" {
						item.Name = raw.ContentName
					}
					if supplied == "" {
						supplied = raw.CoPath
					}
					size := raw.Size
					if size == "" {
						size = raw.ContentSize
					}
					if size != "" {
						item.Size, err = size.Int64()
						if err != nil || item.Size < 0 {
							return nil, errors.New("移动分享文件大小无效")
						}
					}
				}
				if item.ID == "" || len(item.ID) > 4096 || !safeName(item.Name) || seen[item.ID] {
					return nil, errors.New("移动分享目录包含无效或重复条目，未执行同步清理")
				}
				seen[item.ID] = true
				item.Path, err = mobileLibraryResourcePath(parent, supplied, item.ID)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
		}
		if len(items) > mobileShareScanLimit {
			return nil, errors.New("移动分享目录超过扫描上限")
		}
		cursor := mobileQRString(data.Next)
		hasNext := cursor != "" && cursor != "null" && cursor != "false" && cursor != "0"
		if firstOnly || !hasNext && len(data.Dirs)+len(data.Files) < 100 {
			return items, nil
		}
		if len(data.Dirs)+len(data.Files) == 0 {
			return nil, errors.New("移动分享分页未完成，未执行同步清理")
		}
	}
	return nil, errors.New("移动分享目录分页超过扫描上限")
}

func (a *App) mobileLibraryDownload(ctx context.Context, storage Storage, share mobileLibraryShare, file mobileLibrarySource) (Download, error) {
	account, _, err := mobileAccount(storage)
	if err != nil {
		return Download{}, err
	}
	var data struct {
		Redirect string `json:"redrUrl"`
		Download string `json:"downloadURL"`
	}
	err = a.mobileLibraryRequest(ctx, storage, "dlFromOutLinkV3", map[string]any{
		"dlFromOutLinkReqV3": map[string]any{"linkID": share.Code, "account": account, "coIDLst": map[string]any{"item": []string{file.Path}}},
		"commonAccountInfo":  map[string]any{"account": account, "accountType": 1},
	}, &data)
	if err != nil {
		return Download{}, fmt.Errorf("获取移动分享下载直链：%w", err)
	}
	address := data.Redirect
	if address == "" {
		address = data.Download
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Scheme != "https" && u.Scheme != "http" {
		return Download{}, errors.New("移动分享未返回有效下载直链")
	}
	return Download{URL: address, Headers: http.Header{}}, nil
}
