package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type upload115Reader struct {
	ctx context.Context
	io.ReadSeeker
}

func (r upload115Reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.ReadSeeker.Read(p)
}

func (a *App) upload115(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	c, err := client115(ctx, s)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := c.UploadAvailable(); err != nil {
		return err
	}
	if size > c.UploadMetaInfo.SizeLimit {
		return driver.ErrUploadTooLarge
	}
	reader := upload115Reader{ctx, f}
	digest, err := c.GetDigestResult(reader)
	if err != nil {
		return err
	}
	init, err := c.RapidUpload(size, name, parent, digest.PreID, digest.QuickID, reader)
	if err != nil {
		return err
	}
	if ok, err := init.Ok(); ok || err != nil {
		return err
	}
	token, err := c.GetOSSToken()
	if err != nil {
		return err
	}
	return upload115OSS(ctx, f, size, &init.UploadOSSParams, token)
}

func upload115OSS(ctx context.Context, f *os.File, size int64, params *driver.UploadOSSParams, token *driver.UploadOSSTokenResp) error {
	// Keep OSS traffic on HTTPS and attach cancellation, unlike the convenience
	// upload method which creates its own client without the request context.
	client, err := oss.New("https://"+driver.OSSEndpoint, token.AccessKeyID, token.AccessKeySecret,
		oss.HTTPClient(&http.Client{Transport: apiClient.Transport, Timeout: 30 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return err
	}
	bucket, err := client.Bucket(params.Bucket)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var callback []byte
	opts := append(driver.OssOption(params, token), oss.WithContext(ctx), oss.CallbackResult(&callback))
	if size <= 16<<20 {
		err = bucket.PutObject(params.Object, upload115Reader{ctx, f}, opts...)
	} else {
		partOpts := []oss.Option{oss.WithContext(ctx), oss.SetHeader(driver.OssSecurityTokenHeaderName, token.SecurityToken)}
		upload, startErr := bucket.InitiateMultipartUpload(params.Object, partOpts...)
		if startErr != nil {
			return startErr
		}
		completed := false
		defer func() {
			if !completed {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = bucket.AbortMultipartUpload(upload, oss.WithContext(cleanup), oss.SetHeader(driver.OssSecurityTokenHeaderName, token.SecurityToken))
			}
		}()
		parts := []oss.UploadPart{}
		partSize := max(int64(16<<20), (size+9998)/9999)
		for offset := int64(0); offset < size; offset += partSize {
			length := min(partSize, size-offset)
			part, err := bucket.UploadPart(upload, uploadReader{ctx, io.NewSectionReader(f, offset, length)}, length, len(parts)+1, partOpts...)
			if err != nil {
				return err
			}
			parts = append(parts, part)
		}
		_, err = bucket.CompleteMultipartUpload(upload, parts, opts...)
		completed = err == nil
	}
	if err != nil {
		return err
	}
	var result struct{ State bool }
	if json.Unmarshal(callback, &result) != nil || !result.State {
		return errors.New("115 未确认上传回调成功，请检查网盘中的目标文件")
	}
	return nil
}
