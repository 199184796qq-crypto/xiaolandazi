package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// recordingSupplyCheckpoint is the minimum durable cursor needed to resume a
// recording-stream producer without replaying the first round or reshuffling
// the current round. It is deliberately scoped to one frozen version/session;
// a new session or a different published version must not inherit it.
type recordingSupplyCheckpoint struct {
	TenantID     int64     `json:"tenant_id"`
	RoomID       int64     `json:"room_id"`
	SessionID    int64     `json:"session_id"`
	VersionID    int64     `json:"version_id"`
	ProgramID    string    `json:"program_id,omitempty"`
	Generation   uint64    `json:"generation,omitempty"`
	Round        int       `json:"round"`
	TabOrder     []int     `json:"tab_order"`
	TabPos       int       `json:"tab_pos"`
	Index        int       `json:"index"`
	Sequence     int       `json:"sequence"`
	BlockSeq     int       `json:"block_seq"`
	PreviousLast int       `json:"previous_last"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func recordingSupplyCheckpointPath(tenant, room, session int64) string {
	return filepath.Join(recordingCacheMediaRoot(), "supply-checkpoints",
		fmt.Sprintf("%d-%d-%d.json", tenant, room, session))
}

func loadRecordingSupplyCheckpoint(tenant, room, session int64) (recordingSupplyCheckpoint, bool) {
	var checkpoint recordingSupplyCheckpoint
	raw, err := os.ReadFile(recordingSupplyCheckpointPath(tenant, room, session))
	if err != nil || len(raw) == 0 || len(raw) > 64*1024 {
		return checkpoint, false
	}
	if json.Unmarshal(raw, &checkpoint) != nil {
		return checkpoint, false
	}
	if checkpoint.TenantID != tenant || checkpoint.RoomID != room || checkpoint.SessionID != session || checkpoint.VersionID <= 0 {
		return checkpoint, false
	}
	return checkpoint, true
}

func saveRecordingSupplyCheckpoint(checkpoint recordingSupplyCheckpoint) error {
	if checkpoint.TenantID <= 0 || checkpoint.RoomID <= 0 || checkpoint.SessionID <= 0 || checkpoint.VersionID <= 0 {
		return errors.New("供料检查点身份无效")
	}
	checkpoint.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	path := recordingSupplyCheckpointPath(checkpoint.TenantID, checkpoint.RoomID, checkpoint.SessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func recordingSupplyCheckpointMatches(checkpoint recordingSupplyCheckpoint, versionID int64, tabs int) bool {
	if checkpoint.VersionID != versionID || len(checkpoint.TabOrder) != tabs || checkpoint.TabPos < 0 || checkpoint.TabPos > tabs || checkpoint.Index < 0 || checkpoint.Round < 0 {
		return false
	}
	seen := make([]bool, tabs)
	for _, index := range checkpoint.TabOrder {
		if index < 0 || index >= tabs || seen[index] {
			return false
		}
		seen[index] = true
	}
	return true
}
