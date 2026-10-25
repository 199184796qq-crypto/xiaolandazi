package model

import "time"

// LiveRoomCost is the latest rolling-hour cost snapshot written by cost_report.py.
type LiveRoomCost struct {
	TenantID        int64     `json:"tenant_id"`
	RoomID          int64     `json:"room_id"`
	WindowStart     time.Time `json:"window_start"`
	WindowEnd       time.Time `json:"window_end"`
	ModelCostMicros int64     `json:"model_cost_micros"`
	TTSCostMicros   int64     `json:"tts_cost_micros"`
	TotalCostMicros int64     `json:"total_cost_micros"`
}
