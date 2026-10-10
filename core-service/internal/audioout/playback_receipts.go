package audioout

type MainlineCoverageReceipt struct {
	Sequence uint64 `json:"sequence"`
	TrackID  string `json:"track_id"`
	PlayedMS int64  `json:"played_ms"`
	Complete bool   `json:"complete"`
	Terminal bool   `json:"terminal"`
}
