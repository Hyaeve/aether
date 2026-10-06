package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type uploadedPart struct {
	Number int    `xml:"PartNumber"`
	ETag   string `xml:"ETag"`
}
type completedParts struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []uploadedPart `xml:"Part"`
}

func quarkWriteJSON(ctx context.Context, s Storage, endpoint string, body, out any) error {
	var result struct {
		Code, Status int
		Data         json.RawMessage
	}
	if err := uploadAPI(ctx, s, "POST", "https://drive.quark.cn/1/clouddrive"+endpoint+"?pr=ucpro&fr=pc", body, &result); err != nil {
		return err
	}
	if result.Code != 0 || result.Status >= 400 {
		return errors.New("夸克拒绝写入操作")
	}
	if out != nil {
		return json.Unmarshal(result.Data, out)
	}
	return nil
}

func (a *App) uploadQuark(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	md5sum, err := uploadHash(ctx, f, 0, size, md5.New())
	if err != nil {
		return err
	}
	sha, err := uploadHash(ctx, f, 0, size, sha1.New())
	if err != nil {
		return err
	}
	var pre struct {
		Task     string          `json:"task_id"`
		Object   string          `json:"obj_key"`
		Upload   string          `json:"upload_id"`
		Bucket   string          `json:"bucket"`
		URL      string          `json:"upload_url"`
		Auth     string          `json:"auth_info"`
		Callback json.RawMessage `json:"callback"`
	}
	err = quarkWriteJSON(ctx, s, "/file/upload/pre", map[string]any{"ccp_hash_update": true, "dir_name": "", "file_name": name, "format_type": "application/octet-stream", "l_created_at": time.Now().UnixMilli(), "l_updated_at": time.Now().UnixMilli(), "pdir_fid": parent, "size": size}, &pre)
	if err != nil {
		return err
	}
	if pre.Task == "" {
		return errors.New("夸克未返回上传任务")
	}
	var checked struct{ Finish bool }
	if err := quarkWriteJSON(ctx, s, "/file/update/hash", map[string]any{"md5": md5sum, "sha1": sha, "task_id": pre.Task}, &checked); err != nil {
		return err
	}
	if checked.Finish {
		return nil
	}
	if pre.Bucket == "" || pre.Object == "" || pre.Upload == "" || pre.URL == "" {
		return errors.New("夸克未返回完整上传凭据")
	}
	host := strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(pre.URL, "https://"), "http://"), "/")
	object := &url.URL{Scheme: "https", Host: pre.Bucket + "." + host, Path: "/" + pre.Object}
	const agent = "aliyun-sdk-js/6.6.1 Chrome 98.0.4758.80 on Windows 10 64-bit"
	auth := func(method string, h http.Header, resource string) error {
		date := time.Now().UTC().Format(http.TimeFormat)
		h.Set("x-oss-date", date)
		h.Set("x-oss-user-agent", agent)
		meta := method + "\n" + h.Get("Content-MD5") + "\n" + h.Get("Content-Type") + "\n" + date + "\n"
		if h.Get("x-oss-callback") != "" {
			meta += "x-oss-callback:" + h.Get("x-oss-callback") + "\n"
		}
		meta += "x-oss-date:" + date + "\nx-oss-user-agent:" + agent + "\n" + resource
		var result struct {
			Key string `json:"auth_key"`
		}
		if err := quarkWriteJSON(ctx, s, "/file/upload/auth", map[string]any{"task_id": pre.Task, "auth_info": pre.Auth, "auth_meta": meta}, &result); err != nil {
			return err
		}
		if result.Key == "" {
			return errors.New("夸克未返回分片签名")
		}
		h.Set("Authorization", result.Key)
		h.Set("Referer", "https://pan.quark.cn/")
		return nil
	}
	parts := completedParts{}
	const partSize = int64(16 << 20)
	for offset := int64(0); offset < size || len(parts.Parts) == 0; offset += partSize {
		number := len(parts.Parts) + 1
		length := min(partSize, size-offset)
		q := url.Values{"partNumber": {strconv.Itoa(number)}, "uploadId": {pre.Upload}}
		object.RawQuery = q.Encode()
		h := http.Header{"Content-Type": {"application/octet-stream"}}
		resource := fmt.Sprintf("/%s/%s?partNumber=%d&uploadId=%s", pre.Bucket, pre.Object, number, pre.Upload)
		if err := auth("PUT", h, resource); err != nil {
			return err
		}
		_, headers, err := writeHTTP(ctx, "PUT", object.String(), h, io.NewSectionReader(f, offset, length), length)
		if err != nil {
			return err
		}
		etag := headers.Get("ETag")
		if etag == "" {
			return errors.New("夸克未确认上传分片")
		}
		parts.Parts = append(parts.Parts, uploadedPart{number, etag})
	}
	body, err := xml.Marshal(parts)
	if err != nil {
		return err
	}
	sum := md5.Sum(body)
	callback := pre.Callback
	if len(callback) == 0 || string(callback) == "null" {
		callback = json.RawMessage("{}")
	}
	h := http.Header{"Content-Type": {"application/xml"}, "Content-MD5": {base64.StdEncoding.EncodeToString(sum[:])}, "X-Oss-Callback": {base64.StdEncoding.EncodeToString(callback)}}
	if err := auth("POST", h, "/"+pre.Bucket+"/"+pre.Object+"?uploadId="+pre.Upload); err != nil {
		return err
	}
	object.RawQuery = url.Values{"uploadId": {pre.Upload}}.Encode()
	if _, _, err := writeHTTP(ctx, "POST", object.String(), h, bytes.NewReader(body), int64(len(body))); err != nil {
		return err
	}
	return quarkWriteJSON(ctx, s, "/file/upload/finish", map[string]any{"obj_key": pre.Object, "task_id": pre.Task}, nil)
}
