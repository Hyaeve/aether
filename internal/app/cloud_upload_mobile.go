package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
)

type uploadReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r uploadReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func uploadHash(ctx context.Context, f *os.File, offset, size int64, h hash.Hash) (string, error) {
	n, err := io.Copy(h, uploadReader{ctx, io.NewSectionReader(f, offset, size)})
	if err != nil {
		return "", err
	}
	if n != size {
		return "", io.ErrUnexpectedEOF
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type mobileUploadPart struct {
	Number      int   `json:"partNumber"`
	Size        int64 `json:"partSize"`
	HashContext struct {
		Offset int64 `json:"partOffset"`
	} `json:"parallelHashCtx"`
}

type mobileUploadPartURL struct {
	Number int    `json:"partNumber"`
	URL    string `json:"uploadUrl"`
}

func mobileUploadParts(size int64) ([]mobileUploadPart, error) {
	if size < 0 || size > mobilePCUploadFileLimit {
		return nil, errors.New("移动上传单文件须在 0–500 GB 之间")
	}
	partSize := int64(100 << 20)
	if size > 30<<30 {
		partSize = 512 << 20
	}
	parts := []mobileUploadPart{}
	for offset := int64(0); offset < size || len(parts) == 0; offset += partSize {
		p := mobileUploadPart{Number: len(parts) + 1, Size: min(partSize, size-offset)}
		p.HashContext.Offset = offset
		parts = append(parts, p)
	}
	return parts, nil
}

func recordMobileUploadURLs(requested []mobileUploadPart, response []mobileUploadPartURL, urls map[int]string) error {
	allowed := make(map[int]bool, len(requested))
	for _, part := range requested {
		allowed[part.Number] = true
	}
	for _, part := range response {
		if !allowed[part.Number] || part.URL == "" || urls[part.Number] != "" {
			return errors.New("移动返回无效或重复的上传分片")
		}
		urls[part.Number] = part.URL
	}
	return nil
}

func (a *App) uploadMobile(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	if size < 0 || size > uploadFileLimit(s) {
		return fmt.Errorf("移动上传单文件上限 %s", uploadLimitLabel(uploadFileLimit(s)))
	}
	digest, err := uploadHash(ctx, f, 0, size, sha256.New())
	if err != nil {
		return err
	}
	return a.uploadMobileContent(ctx, s, parent, name, f, size, digest)
}

func (a *App) uploadMobileContent(ctx context.Context, s Storage, parent, name string, source io.ReaderAt, size int64, digest string) error {
	if size < 0 || size > uploadFileLimit(s) {
		return fmt.Errorf("移动上传单文件上限 %s", uploadLimitLabel(uploadFileLimit(s)))
	}
	parts, err := mobileUploadParts(size)
	if err != nil {
		return err
	}
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		return err
	}
	var created struct {
		FileID   string                `json:"fileId"`
		UploadID string                `json:"uploadId"`
		Rapid    bool                  `json:"rapidUpload"`
		Exist    bool                  `json:"exist"`
		Parts    []mobileUploadPartURL `json:"partInfos"`
	}
	err = a.mobilePost(ctx, s, host+"/file/create", map[string]any{
		"contentHash": digest, "contentHashAlgorithm": "SHA256", "contentType": "application/octet-stream",
		"fileRenameMode": "auto_rename", "name": name, "parallelUpload": false, "parentFileId": parent,
		"partInfos": parts[:min(100, len(parts))], "size": size, "type": "file",
	}, false, &created)
	if err != nil {
		return err
	}
	if created.Rapid || created.Exist {
		return nil
	}
	if created.FileID == "" || created.UploadID == "" {
		return errors.New("移动未返回上传会话")
	}
	urls := map[int]string{}
	if err := recordMobileUploadURLs(parts[:min(100, len(parts))], created.Parts, urls); err != nil {
		return err
	}
	account, _, err := mobileAccount(s)
	if err != nil {
		return err
	}
	for start := 0; start < len(parts); start += 100 {
		if size == 0 {
			break
		}
		batch := parts[start:min(start+100, len(parts))]
		missing := []mobileUploadPart{}
		for _, p := range batch {
			if urls[p.Number] == "" {
				missing = append(missing, p)
			}
		}
		if len(missing) != 0 {
			var result struct {
				Parts []mobileUploadPartURL `json:"partInfos"`
			}
			if err := a.mobilePost(ctx, s, host+"/file/getUploadUrl", map[string]any{"fileId": created.FileID, "uploadId": created.UploadID, "partInfos": missing, "commonAccountInfo": map[string]any{"account": account, "accountType": 1}}, false, &result); err != nil {
				return err
			}
			if err := recordMobileUploadURLs(missing, result.Parts, urls); err != nil {
				return err
			}
		}
		for _, p := range batch {
			if urls[p.Number] == "" {
				return errors.New("移动未返回分片地址")
			}
		}
		for _, p := range batch {
			if _, _, err := writeHTTP(ctx, "PUT", urls[p.Number], http.Header{"Content-Type": {"application/octet-stream"}, "Origin": {"https://yun.139.com"}, "Referer": {"https://yun.139.com/"}}, io.NewSectionReader(source, p.HashContext.Offset, p.Size), p.Size); err != nil {
				return err
			}
			delete(urls, p.Number)
		}
	}
	return a.mobilePost(ctx, s, host+"/file/complete", map[string]any{"fileId": created.FileID, "uploadId": created.UploadID, "contentHash": digest, "contentHashAlgorithm": "SHA256"}, false, nil)
}
