package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voicebatch"
)

func (s *Store) RuntimeVoiceBatch(ctx context.Context, tenant, room, session, version int64) (voicebatch.State, error) {
	var raw string
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT state_json,revision FROM live_runtime_voice_batches WHERE tenant_id=? AND room_id=? AND session_id=? AND version_id=?`, tenant, room, session, version).Scan(&raw, &revision)
	if err != nil {
		return voicebatch.State{}, err
	}
	var state voicebatch.State
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return state, err
	}
	state.Tenant = tenant
	state.Room = room
	state.Session = session
	state.Version = version
	state.Revision = revision
	return state, nil
}

// CAS and the active-session predicate keep a late producer/request from
// overwriting a manual command or crossing a stopped/replaced live session.
func (s *Store) SaveRuntimeVoiceBatch(ctx context.Context, state voicebatch.State) (bool, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	var result sql.Result
	if state.Revision == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO live_runtime_voice_batches(tenant_id,room_id,session_id,version_id,revision,state_json)
		 SELECT ?,?,?,?,1,? FROM live_runtime_sessions WHERE id=? AND tenant_id=? AND room_id=? AND ended_at IS NULL AND status IN ('running','paused')`, state.Tenant, state.Room, state.Session, state.Version, string(raw), state.Session, state.Tenant, state.Room)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE live_runtime_voice_batches b SET state_json=?,revision=revision+1 WHERE tenant_id=? AND room_id=? AND session_id=? AND version_id=? AND revision=?
		 AND EXISTS(SELECT 1 FROM live_runtime_sessions r WHERE r.id=b.session_id AND r.tenant_id=b.tenant_id AND r.room_id=b.room_id AND r.ended_at IS NULL AND r.status IN ('running','paused'))`, string(raw), state.Tenant, state.Room, state.Session, state.Version, state.Revision)
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) RuntimeVoiceIdentity(ctx context.Context, tenant, room, version int64) (model.LiveAgentVoiceIdentity, error) {
	session, err := s.GetLiveRuntimeByRoom(ctx, tenant, room)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentVoiceIdentity{}, nil
	}
	if err != nil {
		return model.LiveAgentVoiceIdentity{}, err
	}
	if session.Status != "running" && session.Status != "paused" {
		return model.LiveAgentVoiceIdentity{}, nil
	}
	state, err := s.RuntimeVoiceBatch(ctx, tenant, room, session.ID, version)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentVoiceIdentity{}, nil
	}
	if err != nil {
		return model.LiveAgentVoiceIdentity{}, err
	}
	if state.Pending != nil {
		return model.LiveAgentVoiceIdentity{}, voicebatch.ErrSwitching
	}
	return state.Active, nil
}
