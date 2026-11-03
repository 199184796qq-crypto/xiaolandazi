package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"livecompanion/management/internal/sharedfilelock"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type scriptRoomPlayState struct {
	PlayCount     int             `json:"play_count"`
	CompleteCount int             `json:"complete_count"`
	LastPlayedAt  string          `json:"last_played_at,omitempty"`
	Instances     map[string]bool `json:"instances,omitempty"` // false=started; true=complete
	Order         []string        `json:"order,omitempty"`
}
type scriptRoomPlayback struct {
	Tenant int64                          `json:"tenant"`
	Room   int64                          `json:"room"`
	Plan   int64                          `json:"plan"`
	Assets map[string]scriptRoomPlayState `json:"assets"`
}

func shortScriptIdentity(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func scriptRoomPlaybackPath(root string, tenant, room, plan int64) (string, error) {
	if tenant <= 0 || room <= 0 || plan <= 0 {
		return "", errors.New("房间播放记录身份无效")
	}
	return filepath.Join(root, "script-room-playback", strconv.FormatInt(tenant, 10), strconv.FormatInt(room, 10), strconv.FormatInt(plan, 10)+".json"), nil
}
func readScriptRoomPlayback(path string, tenant, room, plan int64) (scriptRoomPlayback, error) {
	h := scriptRoomPlayback{Tenant: tenant, Room: room, Plan: plan, Assets: map[string]scriptRoomPlayState{}}
	if err := readVolcengineJSON(path, &h); err != nil && !errors.Is(err, os.ErrNotExist) {
		return h, err
	}
	if h.Tenant != tenant || h.Room != room || h.Plan != plan {
		return h, errors.New("房间播放记录归属不一致")
	}
	if h.Assets == nil {
		h.Assets = map[string]scriptRoomPlayState{}
	}
	return h, nil
}
func loadScriptRoomPlayback(root string, tenant, room, plan int64) (map[string]scriptRoomPlayState, error) {
	path, err := scriptRoomPlaybackPath(root, tenant, room, plan)
	if err != nil {
		return nil, err
	}
	h, err := readScriptRoomPlayback(path, tenant, room, plan)
	return h.Assets, err
}
func scriptRoomAssetKey(piece, asset string) string { return shortScriptIdentity(piece) + ":" + asset }
func markScriptRoomPlayback(root string, tenant, room, plan int64, piece, asset, instance string, complete bool) error {
	if piece == "" || asset == "" || instance == "" {
		return fmt.Errorf("房间播放回执身份缺失")
	}
	path, err := scriptRoomPlaybackPath(root, tenant, room, plan)
	if err != nil {
		return err
	}
	release, err := sharedfilelock.Lock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer release()
	h, err := readScriptRoomPlayback(path, tenant, room, plan)
	if err != nil {
		return err
	}
	key := scriptRoomAssetKey(piece, asset)
	entry := h.Assets[key]
	if entry.Instances == nil {
		entry.Instances = map[string]bool{}
	}
	previous, seen := entry.Instances[instance]
	if !seen {
		entry.PlayCount++
		entry.LastPlayedAt = time.Now().UTC().Format(time.RFC3339Nano)
		entry.Order = append(entry.Order, instance)
	}
	if complete && !previous {
		entry.CompleteCount++
	}
	entry.Instances[instance] = previous || complete
	for len(entry.Order) > 256 {
		delete(entry.Instances, entry.Order[0])
		entry.Order = entry.Order[1:]
	}
	h.Assets[key] = entry
	return writeVolcengineJSON(path, h)
}
