package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) tianyiUploadCall(ctx context.Context, s Storage, operation string, params map[string]string) (map[string]json.RawMessage, error) {
	session, err := a.tianyiSessionFor(ctx, s)
	if err != nil {
		return nil, err
	}
	if len(session.Secret) < 16 {
		return nil, errors.New("天翼上传会话密钥无效")
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]string, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, k+"="+params[k])
	}
	plain := []byte(strings.Join(fields, "&"))
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher([]byte(session.Secret[:16]))
	if err != nil {
		return nil, err
	}
	encrypted := make([]byte, len(plain))
	for i := 0; i < len(plain); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], plain[i:i+aes.BlockSize])
	}
	payload := strings.ToUpper(hex.EncodeToString(encrypted))
	date := time.Now().UTC().Format(http.TimeFormat)
	endpoint := "/person/" + operation
	mac := hmac.New(sha1.New, []byte(session.Secret))
	fmt.Fprintf(mac, "SessionKey=%s&Operate=GET&RequestURI=%s&Date=%s&params=%s", session.Key, endpoint, date, payload)
	h := http.Header{"Sessionkey": {session.Key}, "Date": {date}, "Signature": {strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))}, "X-Request-Id": {id()}}
	client := tianyiClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	raw, _, err := tianyiHTTP(ctx, client, "GET", "https://upload.cloud.189.cn"+endpoint, url.Values{"params": {payload}, "clientType": {"TELEPC"}, "version": {"7.2.4.0"}, "channelId": {"web_cloud.189.cn"}}, h)
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err := tianyiDecode(raw, &result); err != nil {
		return nil, err
	}
	if data := result["data"]; len(data) > 0 && string(data) != "null" {
		var nested map[string]json.RawMessage
		if json.Unmarshal(data, &nested) == nil {
			for k, v := range nested {
				result[k] = v
			}
		}
	}
	return result, nil
}

func rawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func (a *App) uploadTianyi(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	// Bound the number of parts without allocating a whole part in memory.
	partSize := int64(10 << 20)
	if n := (size + 1998) / 1999; n > partSize {
		partSize = ((n + (10 << 20) - 1) / (10 << 20)) * (10 << 20)
	}
	full := md5.New()
	hexes := []string{}
	parts := []string{}
	for offset := int64(0); offset < size; offset += partSize {
		partHash := md5.New()
		length := min(partSize, size-offset)
		if n, err := io.Copy(io.MultiWriter(full, partHash), uploadReader{ctx, io.NewSectionReader(f, offset, length)}); err != nil || n != length {
			if err != nil {
				return err
			}
			return io.ErrUnexpectedEOF
		}
		sum := partHash.Sum(nil)
		hexes = append(hexes, strings.ToUpper(hex.EncodeToString(sum)))
		parts = append(parts, strconv.Itoa(len(parts)+1)+"-"+base64.StdEncoding.EncodeToString(sum))
	}
	fullHash := strings.ToUpper(hex.EncodeToString(full.Sum(nil)))
	sliceHash := fullHash
	if len(parts) > 1 {
		sum := md5.Sum([]byte(strings.Join(hexes, "\n")))
		sliceHash = strings.ToUpper(hex.EncodeToString(sum[:]))
	}
	params := map[string]string{"parentFolderId": parent, "fileName": url.QueryEscape(name), "fileSize": strconv.FormatInt(size, 10), "sliceSize": strconv.FormatInt(partSize, 10)}
	if len(parts) <= 1 {
		params["fileMd5"] = fullHash
		params["sliceMd5"] = sliceHash
	} else {
		params["lazyCheck"] = "1"
	}
	result, err := a.tianyiUploadCall(ctx, s, "initMultiUpload", params)
	if err != nil {
		return err
	}
	key := rawText(result["uploadFileId"])
	if key == "" {
		return errors.New("天翼未返回上传会话")
	}
	if rawText(result["fileDataExists"]) != "1" {
		for i, part := range parts {
			result, err := a.tianyiUploadCall(ctx, s, "getMultiUploadUrls", map[string]string{"uploadFileId": key, "partInfo": part})
			if err != nil {
				return err
			}
			var urls map[string]json.RawMessage
			var selected json.RawMessage
			if json.Unmarshal(result["uploadUrls"], &urls) == nil {
				selected = urls["partNumber_"+strconv.Itoa(i+1)]
			} else {
				var list []json.RawMessage
				if json.Unmarshal(result["uploadUrls"], &list) == nil {
					for _, entry := range list {
						var item struct {
							PartNumber int `json:"partNumber"`
						}
						_ = json.Unmarshal(entry, &item)
						if item.PartNumber == i+1 {
							selected = entry
							break
						}
					}
				}
			}
			var item struct {
				URL     string `json:"requestURL"`
				Headers string `json:"requestHeader"`
			}
			if len(selected) == 0 {
				selected, _ = json.Marshal(result)
			}
			if json.Unmarshal(selected, &item) != nil || item.URL == "" {
				return errors.New("天翼未返回匹配的分片地址")
			}
			h := http.Header{}
			for _, v := range strings.Split(item.Headers, "&") {
				k, value, ok := strings.Cut(v, "=")
				if ok {
					h.Set(k, value)
				}
			}
			length := min(partSize, size-int64(i)*partSize)
			raw, _, err := writeHTTP(ctx, "PUT", item.URL, h, io.NewSectionReader(f, int64(i)*partSize, length), length)
			if err != nil {
				return err
			}
			if bytes.Contains(raw, []byte("<Error>")) || bytes.Contains(raw, []byte("errorCode")) {
				return errors.New("天翼拒绝上传分片")
			}
		}
	}
	lazy := "0"
	if len(parts) > 1 {
		lazy = "1"
	}
	_, err = a.tianyiUploadCall(ctx, s, "commitMultiUploadFile", map[string]string{"uploadFileId": key, "fileMd5": fullHash, "sliceMd5": sliceHash, "lazyCheck": lazy})
	return err
}
