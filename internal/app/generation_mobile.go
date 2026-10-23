package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Public share-web transport constant, not an account credential.
const mobileShareTransportKey = "PVGDwmcvfs1uV3d1"

func mobileShareEncrypt(raw []byte) (string, error) {
	block, err := aes.NewCipher([]byte(mobileShareTransportKey))
	if err != nil {
		return "", err
	}
	plain := mobileQRPad(raw)
	out := make([]byte, aes.BlockSize+len(plain))
	if _, err = rand.Read(out[:aes.BlockSize]); err != nil {
		return "", err
	}
	cipher.NewCBCEncrypter(block, out[:aes.BlockSize]).CryptBlocks(out[aes.BlockSize:], plain)
	return base64.StdEncoding.EncodeToString(out), nil
}

func mobileShareDecrypt(raw []byte) ([]byte, error) {
	if json.Valid(raw) && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return raw, nil
	}
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) && json.Unmarshal(raw, &value) != nil {
		return nil, errors.New("移动分享响应密文无效")
	}
	encrypted, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(encrypted) < 2*aes.BlockSize || len(encrypted)%aes.BlockSize != 0 {
		return nil, errors.New("移动分享响应密文无效")
	}
	block, err := aes.NewCipher([]byte(mobileShareTransportKey))
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(encrypted)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, encrypted[:aes.BlockSize]).CryptBlocks(plain, encrypted[aes.BlockSize:])
	plain, err = mobileQRUnpad(plain)
	if err != nil {
		return nil, errors.New("移动分享响应解密失败")
	}
	return plain, nil
}

func mobileGenerationShareJSON(ctx context.Context, method string, payload, out any) error {
	return mobileGenerationRequest(ctx, method, payload, out, nil)
}

func mobileGenerationRequest(ctx context.Context, method string, payload, out any, storage *Storage) error {
	module := "IOutLink/"
	switch method {
	case "getOutLinkGeneral", "getOutLinkInfoV6", "getContentInfoFromOutLink":
	case "createOuterLinkBatchOprTask", "queryBatchOprTaskDetail":
		if storage == nil || !nativeMobile(*storage) {
			return errors.New("移动分享转存需要已授权的原生存储")
		}
		module = "IBatchOprTask/"
	default:
		return errors.New("不支持的分享接口")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	body, err := mobileShareEncrypt(raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", mobileShareBase+module+method, strings.NewReader(body))
	if err != nil {
		return err
	}
	timestamp := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	nonce := hex.EncodeToString(random[:])
	device := "||9|12.27.0|chrome|146.0.0.0|eaec1152854e8e1a8e431bb03120c522||macos 10.15.7|1390X847|zh-CN|||"
	clientInfo := "||9|7.13.3|chrome|146.0.0.0|eaec1152854e8e1a8e431bb03120c522||macos 10.15.7||zh-CN|||dW5kZWZpbmVk||"
	for k, v := range map[string]string{
		"Accept": "application/json, text/plain, */*", "Content-Type": "application/json;charset=UTF-8",
		"Origin": "https://yun.139.com", "Referer": "https://yun.139.com/", "User-Agent": "Mozilla/5.0",
		"Cms-Device": "default", "Hcy-Cool-Flag": "1", "Inner-Hcy-Router-Https": "1",
		"Mcloud-Channel": "1000101", "Mcloud-Client": "10702", "Mcloud-Route": "001", "Mcloud-Version": "7.13.3",
		"Mcloud-Sign":  timestamp + "," + nonce + "," + strings.ToUpper(mobileMD5(body+"|"+timestamp+"|"+nonce)),
		"X-M4c-Caller": "PC", "X-M4c-Src": "10002", "X-Svctype": "1", "X-Yun-Api-Version": "v1",
		"X-Deviceinfo": device, "X-Huawei-Channelsrc": "10213406", "X-Inner-Ntwk": "2",
		"X-Yun-App-Channel": "10213406", "X-Yun-Channel-Source": "10213406",
		"X-Yun-Client-Info": clientInfo, "X-Yun-Module-Type": "100", "X-Yun-Svc-Type": "1",
	} {
		r.Header.Set(k, v)
	}
	if storage != nil {
		_, auth, err := mobileAccount(*storage)
		if err != nil {
			return err
		}
		r.Header.Set("Authorization", "Basic "+auth)
	}
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("移动分享读取连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("移动分享读取 HTTP %d", response.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return errors.New("移动分享响应过大或不完整")
	}
	raw, err = mobileShareDecrypt(raw)
	if err != nil {
		return err
	}
	var envelope struct {
		Code json.RawMessage `json:"resultCode"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return errors.New("移动分享响应格式无效")
	}
	if code := mobileQRString(envelope.Code); code != "0" && code != "0000" {
		if len(code) <= 32 && shareCodePattern.MatchString(code) {
			return fmt.Errorf("移动分享读取被拒绝（错误码 %s），请检查提取码和访问权限", code)
		}
		return errors.New("移动分享无效或读取被拒绝，请检查提取码和访问权限")
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return errors.New("移动分享未返回有效资源信息")
	}
	return nil
}

type mobileGenerationFile struct {
	ID     string      `json:"coID"`
	Name   string      `json:"coName"`
	Path   string      `json:"path"`
	CoPath string      `json:"coPath"`
	Size   json.Number `json:"coSize"`
}

func (a *App) parseMobileGenerationLinks(ctx context.Context, text string) ([]GenerationEntry, error) {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 || len(lines) > 50 {
		return nil, errors.New("链接解析支持1至50条移动分享链接，每行一条")
	}
	type link struct{ code, pass string }
	links := make([]link, 0, len(lines))
	for i, line := range lines {
		provider, code, pass, err := parseShareLink(line, "")
		addresses := shareURLPattern.FindAllString(line, -1)
		if err != nil || provider != "mobile" || len(addresses) != 1 || !strings.HasPrefix(addresses[0], "https://") {
			return nil, fmt.Errorf("第%d行不是有效的移动官方分享链接", i+1)
		}
		if pass == "" {
			u, _ := url.Parse(addresses[0])
			if f, e := url.Parse(u.Fragment); e == nil {
				for _, key := range []string{"pwd", "password", "passwd"} {
					if pass = f.Query().Get(key); pass != "" {
						break
					}
				}
			}
		}
		if pass != "" && (!shareCodePattern.MatchString(pass) || len(pass) > 32) {
			return nil, fmt.Errorf("第%d行提取码无效", i+1)
		}
		links = append(links, link{code, pass})
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	entries := []GenerationEntry{}
	requests := 0
	read := func(method string, payload, out any) error {
		requests++
		if requests > 1000 {
			return errors.New("分享扫描超过1000次只读请求上限，请缩小范围")
		}
		if err := a.waitAPI(ctx, "mobile-generation-links"); err != nil {
			return err
		}
		return mobileGenerationShareJSON(ctx, method, payload, out)
	}
	for index, item := range links {
		var general struct {
			Response struct {
				Links []struct {
					Pass string `json:"passwd"`
				} `json:"outLinkGeneral"`
			} `json:"getOutLinkGeneralResp"`
		}
		if err := read("getOutLinkGeneral", map[string]any{"getOutLinkGeneralReq": map[string]any{"linkID": item.code, "isPasswd": 1}}, &general); err != nil {
			return nil, fmt.Errorf("第%d条分享读取失败：%w", index+1, err)
		}
		if len(general.Response.Links) != 1 {
			return nil, fmt.Errorf("第%d条分享信息不完整", index+1)
		}
		if item.pass == "" {
			item.pass = general.Response.Links[0].Pass
		}
		visited := map[string]bool{}
		inspectHash := true
		var walk func(string, string, int) error
		walk = func(parent, prefix string, depth int) error {
			if depth > 32 || visited[parent] {
				return errors.New("分享目录过深或出现循环")
			}
			visited[parent] = true
			seen := map[string]bool{}
			for page := 0; page < 50; page++ {
				var data struct {
					Owner string                 `json:"owner"`
					Files []mobileGenerationFile `json:"coLst"`
					Dirs  []struct {
						ID   string `json:"caID"`
						Name string `json:"caName"`
						Path string `json:"path"`
					} `json:"caLst"`
					Next json.RawMessage `json:"nextPageCursor"`
				}
				if err := read("getOutLinkInfoV6", map[string]any{"getOutLinkInfoReq": map[string]any{"account": "", "linkID": item.code, "passwd": item.pass, "pCaID": parent, "bNum": page*100 + 1, "eNum": (page + 1) * 100, "caSrt": "0", "coSrt": "0", "srtDr": "1"}}, &data); err != nil {
					return err
				}
				for _, dir := range data.Dirs {
					if !safeName(dir.Name) || dir.ID == "" || seen[dir.ID] || len(dir.Path) > 4096 {
						return errors.New("分享目录名称、标识或分页无效")
					}
					seen[dir.ID] = true
					child := dir.Path
					if child == "" {
						child = dir.ID
					}
					if err := walk(child, path.Join(prefix, dir.Name), depth+1); err != nil {
						return err
					}
				}
				for _, file := range data.Files {
					if !safeName(file.Name) || file.ID == "" || seen[file.ID] {
						return errors.New("分享文件名称、标识或分页无效")
					}
					seen[file.ID] = true
					if len(entries) >= generationEntries {
						return errors.New("解析条目超过10000项")
					}
					var detail struct {
						Info struct {
							ID        string      `json:"contentID"`
							Name      string      `json:"contentName"`
							Size      json.Number `json:"contentSize"`
							Hash      string      `json:"contentHash"`
							Algorithm string      `json:"contentHashAlgorithm"`
							SHA256    string      `json:"sha256"`
						} `json:"contentInfo"`
					}
					if inspectHash {
						if err := read("getContentInfoFromOutLink", map[string]any{"getContentInfoFromOutLinkReq": map[string]any{"contentId": file.ID, "linkID": item.code, "passwd": item.pass, "account": ""}}, &detail); err != nil {
							return err
						}
					} else {
						// Once this share omits hashes, enumerate only; the confirmed
						// cloud copy supplies authoritative identity and SHA256 later.
						detail.Info.ID, detail.Info.Name, detail.Info.Size = file.ID, file.Name, file.Size
					}
					size, _ := file.Size.Int64()
					hash := detail.Info.SHA256
					if hash == "" && strings.EqualFold(detail.Info.Algorithm, "SHA256") {
						hash = detail.Info.Hash
					}
					info := CASInfo{Name: file.Name, Size: size, SHA256: strings.ToLower(hash)}
					entry := GenerationEntry{Path: path.Join(prefix, generationOutputName(file.Name, "cas")), Size: size}
					if detail.Info.ID != file.ID || detail.Info.Name != file.Name {
						return errors.New("分享文件身份已变化，请重新解析")
					}
					if n, err := detail.Info.Size.Int64(); err != nil || n != size {
						return errors.New("分享文件大小已变化，请重新解析")
					}
					if hash == "" && size > 0 && size <= 1<<40 {
						inspectHash = false
						token := file.Path
						if token == "" {
							token = file.CoPath
						}
						if token == "" || len(token) > 4096 {
							entry.Error = "分享未返回有效的转存资源路径"
						} else {
							entry.Mobile = &generationMobileSource{Code: item.code, Pass: item.pass, ID: file.ID, Token: token, Name: file.Name, Owner: data.Owner}
						}
					} else if err := validateCAS(info); err != nil {
						entry.Error = "移动分享文件大小或 SHA256 格式无效"
					} else {
						entry.CAS = &info
					}
					entries = append(entries, entry)
				}
				next := mobileQRString(data.Next)
				if next == "" || next == "null" || next == "false" || next == "0" {
					return nil
				}
			}
			return errors.New("分享目录分页超过上限")
		}
		if err := walk("root", "", 0); err != nil {
			return nil, fmt.Errorf("第%d条分享解析失败：%w", index+1, err)
		}
	}
	return entries, nil
}
