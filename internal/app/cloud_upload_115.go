package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	n, err := r.ReadSeeker.Read(p)
	if stage, ok := r.ctx.Value(rapid115ReadStageKey{}).(rapid115ReadStage); ok {
		if offset, seekErr := r.ReadSeeker.Seek(0, io.SeekCurrent); seekErr == nil {
			rapid115Report(r.ctx, stage.phase, min(offset, stage.total), stage.total)
		}
	}
	return n, err
}

func (a *App) upload115(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) (resultErr error) {
	phase := "读取上传凭据"
	defer func() { resultErr = upload115Failure(phase, resultErr) }()
	c, err := client115(ctx, s)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	phase = "检查上传权限"
	if _, err := c.UploadAvailable(); err != nil {
		return err
	}
	if c.UploadMetaInfo == nil || c.UploadMetaInfo.SizeLimit <= 0 {
		return &storageRequestError{message: "115 未返回有效的账号上传额度，未提交文件"}
	}
	if !c.UploadMetaInfo.UploadAllowed {
		return &storageRequestError{message: "115 明确返回当前账号不允许上传，请核对 CK、账号权益和上传权限"}
	}
	if size > c.UploadMetaInfo.SizeLimit {
		return &storageRequestError{message: fmt.Sprintf("115 账号单文件上传上限为 %d 字节，当前文件 %d 字节", c.UploadMetaInfo.SizeLimit, size), cause: driver.ErrUploadTooLarge}
	}
	phase = "计算文件摘要"
	reader := upload115Reader{ctx, f}
	digest, err := c.GetDigestResult(reader)
	if err != nil {
		return err
	}
	phase = "秒传握手"
	init, err := c.RapidUpload(size, name, parent, digest.PreID, digest.QuickID, reader)
	if err != nil {
		return err
	}
	if ok, err := init.Ok(); ok || err != nil {
		return err
	}
	phase = "获取 OSS 上传票据"
	token, err := c.GetOSSToken()
	if err != nil {
		return err
	}
	return upload115OSS(ctx, f, size, &init.UploadOSSParams, token)
}

func upload115OSS(ctx context.Context, f *os.File, size int64, params *driver.UploadOSSParams, token *driver.UploadOSSTokenResp) (resultErr error) {
	phase := "准备 OSS 上传"
	defer func() { resultErr = upload115Failure(phase, resultErr) }()
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
		phase = "上传文件正文"
		err = bucket.PutObject(params.Object, controlledUploadReader(ctx, rapid115ReadProgress(ctx, upload115Reader{ctx, f}, "本地传输", 0, size)), opts...)
	} else {
		partOpts := []oss.Option{oss.WithContext(ctx), oss.SetHeader(driver.OssSecurityTokenHeaderName, token.SecurityToken), oss.UserAgentHeader(driver.OSSUserAgent)}
		phase = "初始化分片上传"
		initOpts := append(append([]oss.Option{}, partOpts...), oss.Sequential(), oss.EnableSha1())
		upload, startErr := bucket.InitiateMultipartUpload(params.Object, initOpts...)
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
			phase = fmt.Sprintf("上传第 %d 个分片", len(parts)+1)
			length := min(partSize, size-offset)
			part, err := bucket.UploadPart(upload, controlledUploadReader(ctx, rapid115ReadProgress(ctx, uploadReader{ctx, io.NewSectionReader(f, offset, length)}, "本地传输", offset, size)), length, len(parts)+1, partOpts...)
			if err != nil {
				return err
			}
			parts = append(parts, part)
		}
		phase = "合并分片并确认回调"
		_, err = bucket.CompleteMultipartUpload(upload, parts, opts...)
		completed = err == nil
	}
	if err != nil {
		return err
	}
	phase = "确认上传回调"
	var result struct{ State bool }
	if json.Unmarshal(callback, &result) != nil || !result.State {
		return &storageRequestError{message: "115 未确认上传回调成功，请检查网盘中的目标文件"}
	}
	return nil
}

func upload115Failure(phase string, err error) error {
	if err == nil {
		return nil
	}
	var diagnosed *fileTransferError
	if errors.As(err, &diagnosed) {
		return err
	}
	reason := transferFailureReason(err)
	var service oss.ServiceError
	var servicePointer *oss.ServiceError
	if errors.As(err, &servicePointer) && servicePointer != nil {
		service = *servicePointer
	}
	if errors.As(err, &service) || servicePointer != nil {
		code := map[string]string{
			"AccessDenied":          "AccessDenied，上传授权或分片请求被拒绝",
			"InvalidAccessKeyId":    "InvalidAccessKeyId，上传票据无效",
			"SignatureDoesNotMatch": "SignatureDoesNotMatch，上传签名不匹配",
			"SecurityTokenExpired":  "SecurityTokenExpired，上传票据已过期",
			"InvalidSecurityToken":  "InvalidSecurityToken，上传票据无效",
			"NoSuchUpload":          "NoSuchUpload，分片会话不存在或已过期",
			"InvalidPart":           "InvalidPart，分片校验失败",
			"InvalidPartOrder":      "InvalidPartOrder，分片顺序不正确",
			"EntityTooSmall":        "EntityTooSmall，分片大小不符合要求",
			"CallbackFailed":        "CallbackFailed，115 未确认上传回调",
			"RequestTimeTooSkewed":  "RequestTimeTooSkewed，请核对服务器时间",
			"InternalError":         "InternalError，上游内部错误",
			"ServiceUnavailable":    "ServiceUnavailable，上游暂不可用",
		}[service.Code]
		if code == "" {
			code = "未识别的 OSS 错误"
		}
		status := service.StatusCode
		if status < 100 || status > 599 {
			status = 0
		}
		reason = fmt.Sprintf("OSS HTTP %d（%s）", status, code)
	} else if errors.Is(err, driver.ErrNotLogin) || errors.Is(err, driver.ErrBadCookie) || errors.Is(err, driver.ErrCredentialInvalid) || errors.Is(err, driver.ErrDoesLoggedOut) || errors.Is(err, driver.ErrSessionExited) {
		reason = "115 登录或上传授权已失效，请重新扫码核对 CK"
	} else if errors.Is(err, driver.ErrUploadSigInvalid) {
		reason = "115 上传签名被拒绝，请核对当前账号凭据"
	} else if errors.Is(err, driver.ErrUploadSH1Invalid) {
		reason = "115 拒绝上传文件参数，请核对文件大小、摘要及目标目录"
	}
	return &fileTransferError{phase: "115 " + phase, reason: reason, cause: err}
}
