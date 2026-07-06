package bootstrap

import (
	"context"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

const (
	maxRetries     = 3
	retryDelay     = 5 * time.Second
	dnsRetryDelay  = 10 * time.Second
)

func LoadStorages() {
	storages, err := db.GetEnabledStorages()
	if err != nil {
		utils.Log.Fatalf("failed get enabled storages: %+v", err)
	}
	go func(storages []model.Storage) {
		var failedStorages []model.Storage
		for i := range storages {
			err := op.LoadStorage(context.Background(), storages[i])
			if err != nil {
				utils.Log.Errorf("failed load storage: [%s], driver: [%s], err: %+v",
					storages[i].MountPath, storages[i].Driver, err)
				failedStorages = append(failedStorages, storages[i])
			} else {
				utils.Log.Infof("success load storage: [%s], driver: [%s], order: [%d]",
					storages[i].MountPath, storages[i].Driver, storages[i].Order)
			}
		}
		conf.SendStoragesLoadedSignal()

		// Retry failed storages (e.g. DNS not ready on Android startup)
		for attempt := 1; attempt <= maxRetries && len(failedStorages) > 0; attempt++ {
			delay := retryDelay
			if attempt == 1 {
				delay = dnsRetryDelay
			}
			utils.Log.Infof("retrying %d failed storages (attempt %d/%d) after %v...",
				len(failedStorages), attempt, maxRetries, delay)
			time.Sleep(delay)

			var stillFailed []model.Storage
			for i := range failedStorages {
				err := op.LoadStorage(context.Background(), failedStorages[i])
				if err != nil {
					utils.Log.Errorf("failed load storage: [%s], driver: [%s], err: %+v",
						failedStorages[i].MountPath, failedStorages[i].Driver, err)
					// Check if it's a DNS error - likely temporary
					if strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "lookup") {
						stillFailed = append(stillFailed, failedStorages[i])
					}
				} else {
					utils.Log.Infof("success load storage on retry: [%s], driver: [%s]",
						failedStorages[i].MountPath, failedStorages[i].Driver)
				}
			}
			failedStorages = stillFailed
		}
		if len(failedStorages) > 0 {
			utils.Log.Warnf("%d storages still failed after %d retries", len(failedStorages), maxRetries)
		}
	}(storages)
}
