package guangya

import (
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// 基础响应结构
type Resp struct {
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

// Token 刷新响应
type TokenResp struct {
	TokenType    string `json:"token_type"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Sub          string `json:"sub"`
}

// Token 刷新请求
type RefreshTokenReq struct {
	ClientId     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

// 文件列表请求
type FileListReq struct {
	SortType int    `json:"sortType"`
	ResType  int    `json:"resType"`
	OrderBy  int    `json:"orderBy"`
	PageSize int    `json:"pageSize"`
	Page     int    `json:"page"`
	DirType  int    `json:"dirType"`
	ParentId string `json:"parentId"`
}

// 文件列表响应
type FileListResp struct {
	Total int        `json:"total"`
	List  []FileInfo `json:"list"`
}

// 文件信息
type FileInfo struct {
	FileId        string `json:"fileId"`
	FileName      string `json:"fileName"`
	FileSize      int64  `json:"fileSize"`
	ParentId      string `json:"parentId"`
	Depth         int    `json:"depth"`
	DirType       int    `json:"dirType"`
	ResType       int    `json:"resType"`
	FullParentIds string `json:"fullParentIds"`
	Ctime         int64  `json:"ctime"`
	Utime         int64  `json:"utime"`
	MineType      string `json:"mineType"`
	FileType      int    `json:"fileType"`
	Ext           string `json:"ext"`
	Md5           string `json:"md5"`
	Gcid          string `json:"gcid"`
	AuditStatus   int    `json:"auditStatus"`
}

// 下载链接请求
type DownloadReq struct {
	RequestId string `json:"requestId"`
	FileId    string `json:"fileId"`
}

// 下载链接响应
type DownloadResp struct {
	SignedURL        string `json:"signedURL"`
	URLDuration      int    `json:"urlDuration"`
	SpeedupSignature string `json:"speedupSignature"`
	RequestId        string `json:"requestId"`
}

// 资产信息请求
type AssetsReq struct {
	NeedTrafficData bool `json:"needTrafficData"`
}

// 资产信息响应
type AssetsResp struct {
	TotalSpaceSize int64 `json:"totalSpaceSize"`
	UsedSpaceSize  int64 `json:"usedSpaceSize"`
	VipStatus      int   `json:"vipStatus"`
	VipLeftTime    int   `json:"vipLeftTime"`
	SvipStatus     int   `json:"svipStatus"`
	VipExpireTime  int64 `json:"vipExpireTime"`
	SystemTime     int64 `json:"systemTime"`
}

// 用户信息
type UserInfo struct {
	Sub         string `json:"sub"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
	CreatedAt   string `json:"created_at"`
}

// Token 状态管理
type TokenState struct {
	mu            sync.Mutex
	token         string
	refreshToken  string
	expiresAt     time.Time
	lastRefresh   time.Time
	refreshNeeded bool
}

// 新建文件夹请求
type CreateDirReq struct {
	FailIfNameExist bool   `json:"failIfNameExist"`
	ParentId        string `json:"parentId"`
	DirName         string `json:"dirName"`
}

// 新建文件夹响应
type CreateDirResp struct {
	FileId   string `json:"fileId"`
	FileName string `json:"fileName"`
	Depth    int    `json:"depth"`
	DirType  int    `json:"dirType"`
	ResType  int    `json:"resType"`
	Ctime    int64  `json:"ctime"`
	Utime    int64  `json:"utime"`
}

// 重命名请求
type RenameReq struct {
	NewName string `json:"newName"`
	FileId  string `json:"fileId"`
}

// 移动文件请求
type MoveFileReq struct {
	FileIds  []string `json:"fileIds"`
	ParentId string   `json:"parentId"`
}

// 移动文件响应
type MoveFileResp struct {
	TaskId string `json:"taskId"`
}

// 复制文件请求
type CopyFileReq struct {
	FileIds  []string `json:"fileIds"`
	ParentId string   `json:"parentId"`
}

// 复制文件响应
type CopyFileResp struct {
	TaskId string `json:"taskId"`
}

// 删除文件请求
type DeleteFileReq struct {
	FileIds []string `json:"fileIds"`
}

// 删除文件响应
type DeleteFileResp struct {
	TaskId string `json:"taskId"`
}

// 任务状态请求
type TaskStatusReq struct {
	TaskId string `json:"taskId"`
}

// 任务状态响应
type TaskStatusResp struct {
	TaskId     string `json:"taskId"`
	TaskType   int    `json:"taskType"`
	TaskStatus int    `json:"taskStatus"`
	Progress   int    `json:"progress"`
	CreateTime int64  `json:"createTime"`
	EndTime    int64  `json:"endTime"`
}

// 上传凭证请求
type UploadCredentialReq struct {
	Res      UploadCredentialRes `json:"res"`
	Name     string              `json:"name"`
	ParentId string              `json:"parentId"`
	Capacity int                 `json:"capacity"`
}

type UploadCredentialRes struct {
	FileSize int64  `json:"fileSize"`
	Gcid     string `json:"gcid"`
}

// 上传凭证响应
type UploadCredentialResp struct {
	Gcid         string         `json:"gcid"`
	Provider     int            `json:"provider"`
	Creds        UploadOSSCreds `json:"creds"`
	EndPoint     string         `json:"endPoint"`
	BucketName   string         `json:"bucketName"`
	ObjectPath   string         `json:"objectPath"`
	Region       string         `json:"region"`
	TaskId       string         `json:"taskId"`
	FullEndPoint string         `json:"fullEndPoint"`
	CallbackVar  string         `json:"callbackVar"`
}

type UploadOSSCreds struct {
	AccessKeyID     string `json:"accessKeyID"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
	Expiration      string `json:"expiration"`
}

// 将 FileInfo 转换为 model.Obj
func fileToObj(f FileInfo) model.Obj {
	// dirType: 1 表示目录，resType: 1 表示文件，2 表示文件夹
	isFolder := f.DirType == 1 && f.ResType == 2
	return &model.Object{
		ID:       f.FileId,
		Name:     f.FileName,
		Size:     f.FileSize,
		Modified: time.Unix(f.Utime, 0),
		IsFolder: isFolder,
	}
}
