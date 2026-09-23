package guangya

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type GuangYa struct {
	model.Storage
	Addition
	tokenMu    tokenManager    // Token 状态管理
	qrParam    *DeviceCodeResp // 设备码扫码授权暂存参数 (依赖 OpenList 更新存储时复用驱动实例)
	qrExpireAt time.Time       // 设备码有效期
}

func (d *GuangYa) Config() driver.Config {
	return config
}

func (d *GuangYa) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *GuangYa) Init(ctx context.Context) error {
	// DeviceId: 留空则本地自动生成并立即持久化,
	// 确保首次扫码与后续保存使用同一设备身份 (授权后即为独立设备, 与手机App互不干扰)
	if d.DeviceId == "" {
		d.DeviceId = generateDeviceId()
		op.MustSaveDriverStorage(d)
	}

	// 扫码授权模式且尚未拿到 RefreshToken: 走设备码扫码流程
	if d.LoginType != "manual" && d.RefreshToken == "" {
		if err := d.loginByQRCode(); err != nil {
			return err
		}
	} else {
		// 手动模式或已有 RefreshToken
		if d.RefreshToken == "" {
			return errors.New("手动模式必须填入 RefreshToken, 或改用扫码授权模式")
		}

		// 初始化 Token 状态
		d.tokenMu.refreshToken = d.RefreshToken
		if d.Token != "" {
			d.tokenMu.token = d.Token
			d.tokenMu.expiresAt = time.Now().Add(2 * time.Hour)
		}

		// 如果 Token 为空或即将过期，立即刷新
		if d.tokenMu.token == "" {
			if err := d.refreshToken(); err != nil {
				return err
			}
		}
	}

	// 测试连接 - 获取用户信息验证 Token 是否有效
	_, err := d.getUserInfo()
	if err != nil {
		// 如果失败，尝试刷新 Token 后再验证
		if err := d.refreshToken(); err != nil {
			return err
		}
		_, err = d.getUserInfo()
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *GuangYa) Drop(ctx context.Context) error {
	return nil
}

func (d *GuangYa) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	parentId := dir.GetID()
	// 根目录使用空字符串
	if parentId == "" {
		parentId = d.RootFolderID
		if parentId == "" {
			parentId = ""
		}
	}

	files, err := d.getFileList(parentId, 0)
	if err != nil {
		return nil, err
	}

	return utils.SliceConvert(files, func(src FileInfo) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *GuangYa) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	url, err := d.getDownloadUrl(file.GetID())
	if err != nil {
		return nil, err
	}

	return &model.Link{
		URL: url,
		Header: http.Header{
			"User-Agent": []string{webUA},
		},
	}, nil
}

func (d *GuangYa) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	parentId := parentDir.GetID()
	if parentId == "" {
		parentId = d.RootFolderID
	}

	_, err := d.createDir(parentId, dirName)
	return err
}

func (d *GuangYa) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcId := srcObj.GetID()
	dstId := dstDir.GetID()
	if dstId == "" {
		dstId = d.RootFolderID
	}

	_, err := d.moveFile([]string{srcId}, dstId)
	return err
}

func (d *GuangYa) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return d.renameFile(srcObj.GetID(), newName)
}

func (d *GuangYa) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcId := srcObj.GetID()
	dstId := dstDir.GetID()
	if dstId == "" {
		dstId = d.RootFolderID
	}

	_, err := d.copyFile([]string{srcId}, dstId)
	return err
}

func (d *GuangYa) Remove(ctx context.Context, obj model.Obj) error {
	_, err := d.deleteFile([]string{obj.GetID()})
	return err
}

func (d *GuangYa) GetDetails(ctx context.Context) (*model.StorageDetails, error) {
	assets, err := d.getAssets()
	if err != nil {
		return nil, err
	}

	return &model.StorageDetails{
		DiskUsage: model.DiskUsage{
			TotalSpace: assets.TotalSpaceSize,
			UsedSpace:  assets.UsedSpaceSize,
		},
	}, nil
}

func (d *GuangYa) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	parentId := dstDir.GetID()
	if parentId == "" {
		parentId = d.RootFolderID
	}

	// 读取文件内容
	data, err := io.ReadAll(stream)
	if err != nil {
		return err
	}

	// 计算 Gcid
	gcid := computeGcid(data)

	// 获取上传凭证
	creds, err := d.getUploadCredential(stream.GetSize(), stream.GetName(), parentId, gcid)
	if err != nil {
		return err
	}

	// 上传到 OSS
	err = d.uploadToOSS(creds, data)
	if err != nil {
		return err
	}

	return nil
}

var _ driver.Driver = (*GuangYa)(nil)
