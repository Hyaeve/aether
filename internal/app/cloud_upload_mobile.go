package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

func (a *App) uploadMobile(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	digest, err := uploadHash(ctx, f, 0, size, sha256.New())
	if err != nil {
		return err
	}
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		return err
	}
	partSize := int64(100 << 20)
	if size > 30<<30 {
		partSize = 512 << 20
	}
	type part struct {
		Number      int   `json:"partNumber"`
		Size        int64 `json:"partSize"`
		HashContext struct {
			Offset int64 `json:"partOffset"`
		} `json:"parallelHashCtx"`
	}
	type partURL struct {
		Number int    `json:"partNumber"`
		URL    string `json:"uploadUrl"`
	}
	parts := []part{}
	for offset := int64(0); offset < size || len(parts) == 0; offset += partSize {
		p := part{Number: len(parts) + 1, Size: min(partSize, size-offset)}
		p.HashContext.Offset = offset
		parts = append(parts, p)
	}
	var created struct {
		FileID   string    `json:"fileId"`
		UploadID string    `json:"uploadId"`
		Rapid    bool      `json:"rapidUpload"`
		Exist    bool      `json:"exist"`
		Parts    []partURL `json:"partInfos"`
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
	for _, p := range created.Parts {
		urls[p.Number] = p.URL
	}
	account, _, err := mobileAccount(s)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if size == 0 {
			break
		}
		if urls[p.Number] == "" {
			var result struct {
				Parts []partURL `json:"partInfos"`
			}
			if err := a.mobilePost(ctx, s, host+"/file/getUploadUrl", map[string]any{"fileId": created.FileID, "uploadId": created.UploadID, "partInfos": []part{p}, "commonAccountInfo": map[string]any{"account": account, "accountType": 1}}, false, &result); err != nil {
				return err
			}
			for _, u := range result.Parts {
				urls[u.Number] = u.URL
			}
		}
		if urls[p.Number] == "" {
			return errors.New("移动未返回分片地址")
		}
		if _, _, err := writeHTTP(ctx, "PUT", urls[p.Number], http.Header{"Content-Type": {"application/octet-stream"}, "Origin": {"https://yun.139.com"}, "Referer": {"https://yun.139.com/"}}, io.NewSectionReader(f, p.HashContext.Offset, p.Size), p.Size); err != nil {
			return err
		}
	}
	return a.mobilePost(ctx, s, host+"/file/complete", map[string]any{"fileId": created.FileID, "uploadId": created.UploadID, "contentHash": digest, "contentHashAlgorithm": "SHA256"}, false, nil)
}
