package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	appdb "livecompanion/management/internal/db"
)

func writeLiveDeviceControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appdb.ErrLiveDeviceNotBound):
		writeError(w, http.StatusConflict, "设备没有绑定到该直播间")
	case errors.Is(err, appdb.ErrLiveDeviceOffline):
		writeError(w, http.StatusConflict, "尚未收到盒子的有效连接，请确认盒子已联网并等待自动重连，再刷新状态")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "设备不存在或不属于当前客户")
	default:
		writeError(w, http.StatusInternalServerError, "设备控制失败，请稍后重试")
	}
}
