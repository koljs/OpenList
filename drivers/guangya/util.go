package guangya

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
	"github.com/skip2/go-qrcode"
)

const (
	apiBaseUrl     = "https://api.guangyapan.com"
	accountBaseUrl = "https://account.guangyapan.com"
	// Web 端 client_id (与安卓端 aMe_eFSlkrbQXpUV 不同, Web 端允许本地生成设备ID)
	clientId = "aMe-8VSlkrbQXpUR"
	webUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
)

// Token 状态管理
type tokenManager struct {
	mu           sync.Mutex
	token        string
	refreshToken string
	expiresAt    time.Time
}

// generateDeviceId 本地生成 32 位 hex 设备ID (与官方 Web 端行为一致, 授权后即为独立设备)
func generateDeviceId() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback: 时间戳
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// commonHeaders Web 端公共请求头
func commonHeaders() map[string]string {
	return map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Content-Type":       "application/json",
		"Referer":            "https://www.guangyupan.com/",
		"User-Agent":         webUA,
		"Accept-Language":    "zh-CN",
		"X-Client-Id":        clientId,
		"X-Client-Version":   "0.0.1",
		"X-Device-Model":     "chrome%2F147.0.0.0",
		"X-Device-Name":      "PC-Chrome",
		"X-Net-Work-Type":    "NONE",
		"X-Os-Version":       "Win32",
		"X-Platform-Version": "1",
		"X-Protocol-Version": "301",
		"X-Provider-Name":    "NONE",
		"X-Sdk-Version":      "9.0.2",
	}
}

// deviceSign 生成 Web 端设备签名 (固定填充, 与官方 Web 客户端行为一致)
func deviceSign(deviceId string) string {
	return "wdi10." + deviceId + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
}

// 请求封装 (带自动刷新)
func (d *GuangYa) request(method, baseUrl, path string, callback base.ReqCallback, out interface{}) error {
	// 确保 Token 有效
	if err := d.ensureValidToken(); err != nil {
		return err
	}

	u := baseUrl + path
	req := base.RestyClient.R()

	d.tokenMu.mu.Lock()
	token := d.tokenMu.token
	d.tokenMu.mu.Unlock()

	// Web 端公共头 + 设备头 + 认证头
	headers := commonHeaders()
	headers["X-Device-Id"] = d.DeviceId
	headers["X-Device-Sign"] = deviceSign(d.DeviceId)
	headers["Authorization"] = "Bearer " + token
	headers["Did"] = d.DeviceId
	headers["Dt"] = "4"
	headers["did"] = d.DeviceId
	headers["dt"] = "4"
	headers["accessToken"] = token
	req.SetHeaders(headers)

	var r Resp
	req.SetResult(&r)

	if callback != nil {
		callback(req)
	}

	resp, err := req.Execute(method, u)
	if err != nil {
		return err
	}

	// 如果返回 401，尝试刷新 Token 并重试
	if resp.StatusCode() == 401 {
		if err := d.refreshToken(); err != nil {
			return errors.New("Token expired and refresh failed: " + err.Error())
		}
		// 重试请求
		return d.request(method, baseUrl, path, callback, out)
	}

	if !resp.IsSuccess() {
		return errors.New("HTTP error: " + resp.Status())
	}

	if r.Msg != "success" && r.Msg != "" {
		return errors.New(r.Msg)
	}

	if out != nil && r.Data != nil {
		marshal, err := jsoniter.Marshal(r.Data)
		if err != nil {
			return err
		}
		err = jsoniter.Unmarshal(marshal, out)
		if err != nil {
			return err
		}
	}

	return nil
}

// 确保 Token 有效
func (d *GuangYa) ensureValidToken() error {
	d.tokenMu.mu.Lock()
	defer d.tokenMu.mu.Unlock()

	// 如果 Token 为空或即将过期（提前 5 分钟刷新），则刷新
	if d.tokenMu.token == "" || time.Now().Add(5*time.Minute).After(d.tokenMu.expiresAt) {
		return d.doRefreshToken()
	}
	return nil
}

// 刷新 Token
func (d *GuangYa) refreshToken() error {
	d.tokenMu.mu.Lock()
	defer d.tokenMu.mu.Unlock()
	return d.doRefreshToken()
}

// 执行刷新 Token
func (d *GuangYa) doRefreshToken() error {
	if d.RefreshToken == "" {
		return errors.New("RefreshToken not configured")
	}

	req := base.RestyClient.R()
	req.SetHeaders(commonHeaders())
	req.SetHeader("X-Device-Id", d.DeviceId)
	req.SetHeader("X-Device-Sign", deviceSign(d.DeviceId))

	req.SetBody(map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": d.RefreshToken,
		"client_id":     clientId,
	})

	var tokenResp TokenResp
	req.SetResult(&tokenResp)

	resp, err := req.Post(accountBaseUrl + "/v1/auth/token")
	if err != nil {
		return err
	}

	if !resp.IsSuccess() || tokenResp.AccessToken == "" {
		body := string(resp.Body())
		// 检测 refresh_token 失效，给出明确提示
		if strings.Contains(body, "invalid_grant") || tokenResp.Error == "invalid_grant" {
			return errors.New("RefreshToken 已失效, 请编辑存储并重新扫码授权获取新的 RefreshToken。详情: " + body)
		}
		return errors.New("Token refresh failed: " + resp.Status() + " - " + body)
	}

	// 更新 Token 状态
	d.tokenMu.token = tokenResp.AccessToken
	d.tokenMu.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// 如果返回了新的 RefreshToken (轮换机制), 更新并持久化到存储配置
	tokenChanged := false
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != d.RefreshToken {
		d.RefreshToken = tokenResp.RefreshToken
		d.tokenMu.refreshToken = tokenResp.RefreshToken
		tokenChanged = true
	}
	if d.Token != tokenResp.AccessToken {
		d.Token = tokenResp.AccessToken
		tokenChanged = true
	}
	if tokenChanged {
		op.MustSaveDriverStorage(d)
	}

	return nil
}

// ===================== 设备码扫码授权 (OAuth2 Device Authorization Grant) =====================

// getDeviceCode 获取设备码与二维码链接
func (d *GuangYa) getDeviceCode() (*DeviceCodeResp, error) {
	req := base.RestyClient.R()
	req.SetHeaders(commonHeaders())
	req.SetHeader("X-Device-Id", d.DeviceId)
	req.SetHeader("X-Device-Sign", deviceSign(d.DeviceId))

	req.SetBody(map[string]string{
		"scope":     "user",
		"client_id": clientId,
	})

	var resp DeviceCodeResp
	req.SetResult(&resp)

	r, err := req.Post(accountBaseUrl + "/v1/auth/device/code")
	if err != nil {
		return nil, err
	}
	if !r.IsSuccess() {
		return nil, errors.New("获取设备码失败: " + r.Status() + " - " + string(r.Body()))
	}
	if resp.DeviceCode == "" || resp.VerificationURIComplete == "" {
		return nil, errors.New("获取设备码失败: " + string(r.Body()))
	}
	return &resp, nil
}

// pollDeviceCode 轮询设备码授权状态
func (d *GuangYa) pollDeviceCode(deviceCode string) (*TokenResp, error) {
	req := base.RestyClient.R()
	req.SetHeaders(commonHeaders())
	req.SetHeader("X-Device-Id", d.DeviceId)
	req.SetHeader("X-Device-Sign", deviceSign(d.DeviceId))

	req.SetBody(map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": deviceCode,
		"client_id":   clientId,
	})

	var tokenResp TokenResp
	req.SetResult(&tokenResp)

	r, err := req.Post(accountBaseUrl + "/v1/auth/token")
	if err != nil {
		return nil, err
	}
	// 无论 HTTP 状态码如何都解析 body (authorization_pending 时返回 4xx)
	if jsonErr := jsoniter.Unmarshal(r.Body(), &tokenResp); jsonErr != nil {
		return nil, errors.New("轮询扫码状态失败: " + r.Status() + " - " + string(r.Body()))
	}
	return &tokenResp, nil
}

// loginByQRCode 扫码授权登录 (189pc 模式: 二维码通过错误信息返回, 用户扫码后再次保存触发轮询)
func (d *GuangYa) loginByQRCode() error {
	// 二维码不存在或已过期, 重新生成
	if d.qrParam == nil || time.Now().After(d.qrExpireAt) {
		deviceCode, err := d.getDeviceCode()
		if err != nil {
			d.qrParam = nil
			return err
		}
		d.qrParam = deviceCode
		expiresIn := deviceCode.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 300
		}
		d.qrExpireAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		return d.genQRCode("请使用光鸭云盘App扫描二维码并确认授权, 然后再次点击保存完成登录")
	}

	// 轮询扫码状态
	tokenResp, err := d.pollDeviceCode(d.qrParam.DeviceCode)
	if err != nil {
		d.qrParam = nil
		return err
	}

	switch {
	case tokenResp.AccessToken != "":
		// 授权成功, 保存 token 并持久化
		d.tokenMu.token = tokenResp.AccessToken
		d.tokenMu.refreshToken = tokenResp.RefreshToken
		d.tokenMu.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		d.Token = tokenResp.AccessToken
		d.RefreshToken = tokenResp.RefreshToken
		d.qrParam = nil
		op.MustSaveDriverStorage(d)
		return nil
	case tokenResp.Error == "authorization_pending":
		// 等待扫码
		return d.genQRCode("二维码尚未扫描, 请使用光鸭云盘App扫描后再次点击保存")
	case tokenResp.Error == "slow_down":
		return d.genQRCode("操作过于频繁, 请稍后再次点击保存")
	case tokenResp.Error == "expired_token":
		// 二维码过期, 重新生成
		d.qrParam = nil
		return d.loginByQRCode()
	default:
		d.qrParam = nil
		if tokenResp.ErrorDescription != "" {
			return errors.New("扫码授权失败: " + tokenResp.Error + " - " + tokenResp.ErrorDescription)
		}
		return errors.New("扫码授权失败: " + tokenResp.Error)
	}
}

// genQRCode 生成二维码错误信息 (复用 OpenList 189pc 驱动模式)
func (d *GuangYa) genQRCode(stateText string) error {
	png, err := qrcode.Encode(d.qrParam.VerificationURIComplete, qrcode.Medium, 256)
	if err != nil {
		return fmt.Errorf("生成二维码失败: %v, 请手动打开链接: %s", err, d.qrParam.VerificationURIComplete)
	}
	qrBase64 := base64.StdEncoding.EncodeToString(png)
	qrPage := fmt.Sprintf(`<body>
	state: %s
	<br><img src="data:image/png;base64,%s"/>
	<br>Or open this URL: <a href="%s">%s</a>
</body>`, stateText, qrBase64, d.qrParam.VerificationURIComplete, d.qrParam.VerificationURIComplete)
	return fmt.Errorf("need scan: \n%s", qrPage)
}

// ===================== 业务 API (Web 端协议) =====================

// API 请求 (api.guangyapan.com)
func (d *GuangYa) apiRequest(method, path string, callback base.ReqCallback, out interface{}) error {
	return d.request(method, apiBaseUrl, path, callback, out)
}

// Account 请求 (account.guangyapan.com) - 使用 token manager 中的最新 token
func (d *GuangYa) accountRequestNoRefresh(method, path string, callback base.ReqCallback, out interface{}) error {
	u := accountBaseUrl + path
	req := base.RestyClient.R()

	d.tokenMu.mu.Lock()
	currentToken := d.tokenMu.token
	d.tokenMu.mu.Unlock()

	headers := commonHeaders()
	headers["X-Device-Id"] = d.DeviceId
	headers["X-Device-Sign"] = deviceSign(d.DeviceId)
	headers["Authorization"] = "Bearer " + currentToken
	headers["Did"] = d.DeviceId
	headers["Dt"] = "4"
	headers["did"] = d.DeviceId
	headers["dt"] = "4"
	headers["accessToken"] = currentToken
	req.SetHeaders(headers)

	var r Resp
	req.SetResult(&r)

	if callback != nil {
		callback(req)
	}

	resp, err := req.Execute(method, u)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		return errors.New("HTTP error: " + resp.Status())
	}

	if r.Msg != "success" && r.Msg != "" {
		return errors.New(r.Msg)
	}

	if out != nil && r.Data != nil {
		marshal, err := jsoniter.Marshal(r.Data)
		if err != nil {
			return err
		}
		err = jsoniter.Unmarshal(marshal, out)
		if err != nil {
			return err
		}
	}

	return nil
}

// 获取文件列表
func (d *GuangYa) getFileList(parentId string, page int) ([]FileInfo, error) {
	var resp FileListResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/get_file_list", func(req *resty.Request) {
		req.SetBody(FileListReq{
			ParentId:  parentId,
			Page:      page,
			PageSize:  50,
			OrderBy:   3,
			SortType:  1,
			FileTypes: []int{},
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.List, nil
}

// 获取下载链接
func (d *GuangYa) getDownloadUrl(fileId string) (string, error) {
	var resp DownloadResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/get_res_download_url", func(req *resty.Request) {
		req.SetBody(DownloadReq{
			RequestId: "",
			FileId:    fileId,
		})
	}, &resp)
	if err != nil {
		return "", err
	}
	return resp.SignedURL, nil
}

// 获取资产信息
func (d *GuangYa) getAssets() (*AssetsResp, error) {
	var resp AssetsResp
	err := d.apiRequest(http.MethodPost, "/nd.bizassets.s/v1/get_assets", func(req *resty.Request) {
		req.SetBody(map[string]interface{}{
			"needTrafficData": true,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// 获取用户信息
func (d *GuangYa) getUserInfo() (*UserInfo, error) {
	var resp UserInfo
	err := d.accountRequestNoRefresh(http.MethodGet, "/v1/user/me", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// 新建文件夹
func (d *GuangYa) createDir(parentId, dirName string) (*CreateDirResp, error) {
	var resp CreateDirResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/create_dir", func(req *resty.Request) {
		req.SetBody(CreateDirReq{
			FailIfNameExist: false,
			ParentId:        parentId,
			DirName:         dirName,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// 重命名文件
func (d *GuangYa) renameFile(fileId, newName string) error {
	return d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/rename", func(req *resty.Request) {
		req.SetBody(RenameReq{
			NewName: newName,
			FileId:  fileId,
		})
	}, nil)
}

// 移动文件
func (d *GuangYa) moveFile(fileIds []string, parentId string) (string, error) {
	var resp MoveFileResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/move_file", func(req *resty.Request) {
		req.SetBody(MoveFileReq{
			FileIds:  fileIds,
			ParentId: parentId,
		})
	}, &resp)
	if err != nil {
		return "", err
	}
	return resp.TaskId, nil
}

// 复制文件
func (d *GuangYa) copyFile(fileIds []string, parentId string) (string, error) {
	var resp CopyFileResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/copy_file", func(req *resty.Request) {
		req.SetBody(CopyFileReq{
			FileIds:  fileIds,
			ParentId: parentId,
		})
	}, &resp)
	if err != nil {
		return "", err
	}
	return resp.TaskId, nil
}

// 删除文件
func (d *GuangYa) deleteFile(fileIds []string) (string, error) {
	var resp DeleteFileResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/file/delete_file", func(req *resty.Request) {
		req.SetBody(DeleteFileReq{
			FileIds: fileIds,
		})
	}, &resp)
	if err != nil {
		return "", err
	}
	return resp.TaskId, nil
}

// 获取任务状态
func (d *GuangYa) getTaskStatus(taskId string) (*TaskStatusResp, error) {
	var resp TaskStatusResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/get_task_status", func(req *resty.Request) {
		req.SetBody(TaskStatusReq{
			TaskId: taskId,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// 等待任务完成
func (d *GuangYa) waitForTask(taskId string, timeout time.Duration) error {
	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return errors.New("task timeout")
		}

		status, err := d.getTaskStatus(taskId)
		if err != nil {
			return err
		}

		// taskStatus: 1 进行中, 2 完成, 3 失败
		if status.TaskStatus == 2 {
			return nil
		}
		if status.TaskStatus == 3 {
			return errors.New("task failed")
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// 获取上传凭证
func (d *GuangYa) getUploadCredential(fileSize int64, fileName, parentId, gcid string) (*UploadCredentialResp, error) {
	var resp UploadCredentialResp
	err := d.apiRequest(http.MethodPost, "/nd.bizuserres.s/v1/get_res_center_token", func(req *resty.Request) {
		req.SetBody(UploadCredentialReq{
			Res: UploadCredentialRes{
				FileSize: fileSize,
				Gcid:     gcid,
			},
			Name:     fileName,
			ParentId: parentId,
			Capacity: 2,
		})
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// 计算 Gcid (基于内容的哈希)
func computeGcid(data []byte) string {
	// Gcid 是基于文件内容的 SHA1 哈希
	h := sha1.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// OSS 分片上传（使用阿里云 OSS SDK）
func (d *GuangYa) uploadToOSS(creds *UploadCredentialResp, data []byte) error {
	// 使用 STS Token 创建 OSS Client
	// endpoint 格式: https://oss-cn-qingdao.aliyuncs.com
	endpoint := "https://oss-" + creds.Region + ".aliyuncs.com"

	client, err := oss.New(endpoint, creds.Creds.AccessKeyID, creds.Creds.SecretAccessKey, oss.SecurityToken(creds.Creds.SessionToken))
	if err != nil {
		return errors.New("create OSS client failed: " + err.Error())
	}

	bucket, err := client.Bucket(creds.BucketName)
	if err != nil {
		return errors.New("get bucket failed: " + err.Error())
	}

	// 使用分片上传
	// 初始化分片上传
	imur, err := bucket.InitiateMultipartUpload(creds.ObjectPath)
	if err != nil {
		return errors.New("initiate multipart upload failed: " + err.Error())
	}

	// 分片大小 (4MB)
	partSize := int64(4 * 1024 * 1024)
	fileSize := int64(len(data))
	partCount := int((fileSize + partSize - 1) / partSize)

	// 上传分片
	parts := make([]oss.UploadPart, partCount)
	for i := 0; i < partCount; i++ {
		start := i * int(partSize)
		end := start + int(partSize)
		if end > len(data) {
			end = len(data)
		}
		partData := data[start:end]

		// 使用 reader 上传分片
		parts[i], err = bucket.UploadPart(imur, strings.NewReader(string(partData)), int64(len(partData)), i+1)
		if err != nil {
			// 取消上传
			bucket.AbortMultipartUpload(imur)
			return errors.New("upload part failed: " + err.Error())
		}
	}

	// 完成分片上传
	_, err = bucket.CompleteMultipartUpload(imur, parts)
	if err != nil {
		return errors.New("complete multipart upload failed: " + err.Error())
	}

	return nil
}
