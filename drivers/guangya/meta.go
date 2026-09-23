package guangya

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootID
	LoginType    string `json:"login_type" type:"select" options:"qrcode,manual" default:"qrcode" required:"true" help:"扫码授权: 保存后用光鸭云盘App扫描二维码, 再次保存完成登录(推荐); 手动填入: 自行填入RefreshToken和DeviceId"`
	Token        string `json:"token" required:"false" type:"text" default:"" help:"Access Token (可留空, 系统自动刷新)"`
	RefreshToken string `json:"refresh_token" required:"false" type:"text" default:"" help:"Refresh Token (扫码模式下留空, 自动获取)"`
	DeviceId     string `json:"device_id" required:"false" type:"text" default:"" help:"设备ID (留空自动生成, 与手机App互不干扰)"`
}

var config = driver.Config{
	Name:        "GuangYa",
	DefaultRoot: "",
	LocalSort:   false,
	NoUpload:    false,
	Alert:       "推荐扫码授权: 选择「扫码授权」后直接保存, 用光鸭云盘App扫描提示中的二维码并确认, 然后再次点击保存即可完成登录, 无需抓包。",
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &GuangYa{}
	})
}
