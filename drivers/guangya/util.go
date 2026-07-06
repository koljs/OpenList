package guangya

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
)

const (
	apiBaseUrl     = "https://api.guangyapan.com"
	accountBaseUrl = "https://account.guangyapan.com"
	clientId       = "aMe_eFSlkrbQXpUV"
)

// Token 状态管理
type tokenManager struct {
	mu           sync.Mutex
	token        string
	refreshToken string
	expiresAt    time.Time
}

// 请求封装（带自动刷新）
func (d *GuangYa) request(method, baseUrl, path string, callback base.ReqCallback, out interface{}) error {
	// 确保 Token 有效
	if err := d.ensureValidToken(); err != nil {
		return err
	}

	u := baseUrl + path
	req := base.RestyClient.R()

	// 生成时间戳
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)

	// 获取当前 Token
	d.tokenMu.mu.Lock()
	token := d.tokenMu.token
	d.tokenMu.mu.Unlock()

	// 设置完整的请求头（模拟安卓客户端）
	req.SetHeaders(map[string]string{
		"app":             "com.guangshanyun.pan",
		"peerId":          "676777E27FF1B36V",
		"bd":              "Xiaomi",
		"os":              "34",
		"ch":              "10003",
		"X-Device-Id":     d.DeviceId,
		"nt":              "1",
		"sign":            "",
		"User-Agent":      d.getUserAgent(),
		"vc":              "1040",
		"client_id":       clientId,
		"dt":              "1",
		"Authorization":   "Bearer " + token,
		"X-Captcha-Token": "",
		"x-client-id":     clientId,
		"av":              "1.1.0",
		"vpn":             "0",
		"md":              "Xiaomi M2102K1AC mars Xiaomi",
		"guid":            d.DeviceId,
		"Accept-Language": "zh-CN",
		"did":             d.DeviceId,
		"ts":              ts,
		"Content-Type":    "application/json",
		"Accept":          "application/json",
	})

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

	u := accountBaseUrl + "/v1/auth/token?client_id=" + clientId
	req := base.RestyClient.R()

	req.SetHeaders(map[string]string{
		"X-Device-Id":     d.DeviceId,
		"User-Agent":      d.getUserAgent(),
		"Accept-Language": "zh-CN",
		"Content-Type":    "application/json",
		"Accept":          "application/json",
	})

	req.SetBody(map[string]interface{}{
		"client_id":     clientId,
		"grant_type":    "refresh_token",
		"refresh_token": d.RefreshToken,
		"device_id":     d.DeviceId,
	})

	var tokenResp TokenResp
	req.SetResult(&tokenResp)

	resp, err := req.Post(u)
	if err != nil {
		return err
	}

	if !resp.IsSuccess() {
		body := string(resp.Body())
		return errors.New("Token refresh failed: " + resp.Status() + " - " + body)
	}

	// 更新 Token 状态
	d.tokenMu.token = tokenResp.AccessToken
	d.tokenMu.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// 如果返回了新的 RefreshToken，更新它（同时更新内存中的 tokenMu）
	if tokenResp.RefreshToken != "" && tokenResp.RefreshToken != d.RefreshToken {
		d.RefreshToken = tokenResp.RefreshToken
		d.tokenMu.refreshToken = tokenResp.RefreshToken
	}

	return nil
}

// 生成 User-Agent
func (d *GuangYa) getUserAgent() string {
	return "ANDROID-com.guangshanyun.pan/1.1.0 protocolversion/200 accesstype/ clientid/" + clientId +
		" clientversion/1.1.0 action_type/ networktype/WIFI sessionid/ deviceid/" + d.DeviceId +
		" providername/NONE devicesign/div101." + d.DeviceId +
		"500fb2df465d3545f22ac4f1b962fd3e refresh_token/ sdkversion/2.0.7 datetime/" +
		strconv.FormatInt(time.Now().UnixMilli(), 10) +
		" usrno/ appname/android-com.guangshanyun.pan session_origin/ grant_type/ appid/ clientip/" +
		" devicename/Xiaomi_M2102k1ac osversion/14 platformversion/10 accessmode/ devicemodel/M2102K1AC" +
		" channel/10003 callApp/com.miui.home"
}

// API 请求 (api.guangyapan.com)
func (d *GuangYa) apiRequest(method, path string, callback base.ReqCallback, out interface{}) error {
	return d.request(method, apiBaseUrl, path, callback, out)
}

// Account 请求 (account.guangyapan.com) - 不带自动刷新
func (d *GuangYa) accountRequestNoRefresh(method, path string, callback base.ReqCallback, out interface{}) error {
	u := accountBaseUrl + path
	req := base.RestyClient.R()

	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)

	req.SetHeaders(map[string]string{
		"X-Device-Id":     d.DeviceId,
		"User-Agent":      d.getUserAgent(),
		"Accept-Language": "zh-CN",
		"Content-Type":    "application/json",
		"Accept":          "application/json",
		"Authorization":   "Bearer " + d.Token,
		"ts":              ts,
	})

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
	err := d.apiRequest(http.MethodPost, "/userres/v1/file/get_file_list", func(req *resty.Request) {
		req.SetBody(FileListReq{
			SortType: 1,
			ResType:  0,
			OrderBy:  3,
			PageSize: 50,
			Page:     page,
			DirType:  1,
			ParentId: parentId,
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
	err := d.apiRequest(http.MethodPost, "/userres/v1/get_res_download_url", func(req *resty.Request) {
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
	err := d.apiRequest(http.MethodPost, "/assets/v1/get_assets", func(req *resty.Request) {
		req.SetBody(AssetsReq{
			NeedTrafficData: true,
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
	err := d.apiRequest(http.MethodPost, "/userres/v1/file/create_dir", func(req *resty.Request) {
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
	return d.apiRequest(http.MethodPost, "/userres/v1/file/rename", func(req *resty.Request) {
		req.SetBody(RenameReq{
			NewName: newName,
			FileId:  fileId,
		})
	}, nil)
}

// 移动文件
func (d *GuangYa) moveFile(fileIds []string, parentId string) (string, error) {
	var resp MoveFileResp
	err := d.apiRequest(http.MethodPost, "/userres/v1/file/move_file", func(req *resty.Request) {
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
	err := d.apiRequest(http.MethodPost, "/userres/v1/file/copy_file", func(req *resty.Request) {
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
	err := d.apiRequest(http.MethodPost, "/userres/v1/file/delete_file", func(req *resty.Request) {
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
	err := d.apiRequest(http.MethodPost, "/userres/v1/get_task_status", func(req *resty.Request) {
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

// 解析 JWT Token 获取过期时间
func parseJWTExp(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("invalid JWT token format")
	}

	// 解码 payload (第二部分)
	payload := parts[1]
	// JWT 使用 base64url 编码，需要补齐 padding
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	// 这里简化处理，直接返回一个默认的过期时间
	// 实际应该解析 payload 中的 exp 字段
	return time.Now().Add(2 * time.Hour), nil
}

// 获取上传凭证
func (d *GuangYa) getUploadCredential(fileSize int64, fileName, parentId, gcid string) (*UploadCredentialResp, error) {
	var resp UploadCredentialResp
	err := d.apiRequest(http.MethodPost, "/userres/v1/get_res_center_token", func(req *resty.Request) {
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
	// 对于大文件，使用分片哈希的方式
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
