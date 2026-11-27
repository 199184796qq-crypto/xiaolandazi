package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
	"path/filepath"
	"strconv"
)

// Read/modify/write operations share a short OS lock. Holding only the Go
// mutex loses updates when two Management processes share the media directory.
func lockScriptPoolStore(root string) (func(), error) {
	scriptVoicePoolMu.Lock()
	release, err := sharedfilelock.Lock(context.Background(), filepath.Join(root, "script-voices", ".store.lock"))
	if err != nil {
		scriptVoicePoolMu.Unlock()
		return nil, err
	}
	return func() { release(); scriptVoicePoolMu.Unlock() }, nil
}

// Busy work is skipped, not queued as another paid rewrite. A later PCM
// receipt can request more versions after the current shared task completes.
func tryScriptGeneration(root string, tenant, plan int64, piece string) (func(), bool, error) {
	return sharedfilelock.TryLock(filepath.Join(root, "script-generation-locks", strconv.FormatInt(tenant, 10), strconv.FormatInt(plan, 10), shortScriptIdentity(piece)+".lock"))
}

var errRoomScriptGenerationBusy = errors.New("mainline script generation is busy")

func tryRoomScriptGeneration(root string, tenant, room int64) (func(), bool, error) {
	return sharedfilelock.TryLock(filepath.Join(root, "script-generation-locks", strconv.FormatInt(tenant, 10), "rooms", strconv.FormatInt(room, 10)+".lock"))
}

func tryManualScriptGeneration(root string, tenant int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) (func(), error) {
	releasePiece, acquired, err := tryScriptGeneration(root, tenant, v.PlanID, unit.poolID())
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, errRoomScriptGenerationBusy
	}
	releasePlan, acquired, err := tryScriptGeneration(root, tenant, v.PlanID, "policy-"+scriptFirstParagraphKey(v, "policy"))
	if err != nil || !acquired {
		releasePiece()
		if err != nil {
			return nil, err
		}
		return nil, errRoomScriptGenerationBusy
	}
	return func() {
		releasePlan()
		releasePiece()
	}, nil
}
