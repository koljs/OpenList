package guangya

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootID
	Token        string `json:"token" required:"true" help:"Bearer Token (从抓包获取，可留空让系统自动刷新)"`
	RefreshToken string `json:"refresh_token" required:"true" help:"Refresh Token (从抓包获取，长期有效)"`
	DeviceId     string `json:"device_id" required:"true" help:"设备ID (从抓包获取)"`
}

var config = driver.Config{
	Name:        "GuangYa",
	DefaultRoot: "",
	LocalSort:   false,
	NoUpload:    false,
	Alert:       "请通过抓包获取 RefreshToken 和 DeviceId。Token 可留空，系统会自动刷新。",
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &GuangYa{}
	})
}
