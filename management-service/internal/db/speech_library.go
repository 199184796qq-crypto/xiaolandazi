package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	lib "livecompanion/management/internal/speechlibrary"
)

// These tables are assets, not the transient anchorcycle State. No TTL applies
// to active text/audio. Retired owned objects are collected after references end.
func (s *Store) MigrateSpeechLibrary(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS mgmt_speech_library_rooms (room_key CHAR(64) CHARACTER SET ascii PRIMARY KEY, tenant_id BIGINT NOT NULL, plan_id BIGINT NOT NULL, room_id BIGINT NOT NULL, runtime_id BIGINT NOT NULL, context_sha CHAR(64) CHARACTER SET ascii NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mgmt_speech_library_slots (
 scope_key CHAR(64) CHARACTER SET ascii PRIMARY KEY, room_key CHAR(64) CHARACTER SET ascii NOT NULL, tenant_id BIGINT NOT NULL, plan_id BIGINT NOT NULL, room_id BIGINT NOT NULL,
 runtime_id BIGINT NOT NULL, text_sha CHAR(64) CHARACTER SET ascii NOT NULL, voice_sha CHAR(64) CHARACTER SET ascii NOT NULL
 ) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS mgmt_speech_library_entries (
 id CHAR(32) CHARACTER SET ascii PRIMARY KEY, scope_key CHAR(64) CHARACTER SET ascii NOT NULL,
 text_sha CHAR(64) CHARACTER SET ascii NOT NULL, output_sha CHAR(64) CHARACTER SET ascii NOT NULL,
 payload_json MEDIUMTEXT NOT NULL, active TINYINT NOT NULL DEFAULT 1, created_ms BIGINT NOT NULL, last_used_ms BIGINT NOT NULL DEFAULT 0, use_count BIGINT NOT NULL DEFAULT 0,
 INDEX(scope_key,active,created_ms)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS mgmt_speech_library_audio (
 id CHAR(32) CHARACTER SET ascii PRIMARY KEY, scope_key CHAR(64) CHARACTER SET ascii NOT NULL,
 entry_id CHAR(32) CHARACTER SET ascii NOT NULL, voice_sha CHAR(64) CHARACTER SET ascii NOT NULL,
 payload_json MEDIUMTEXT NOT NULL, ready TINYINT NOT NULL DEFAULT 0, retired TINYINT NOT NULL DEFAULT 0, delete_after_ms BIGINT NOT NULL DEFAULT 0,
 INDEX(scope_key,entry_id,ready,retired), INDEX(retired,delete_after_ms)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS mgmt_speech_library_leases (
 decision_key CHAR(64) CHARACTER SET ascii PRIMARY KEY, scope_key CHAR(64) CHARACTER SET ascii NOT NULL,
 tenant_id BIGINT NOT NULL, room_id BIGINT NOT NULL, runtime_id BIGINT NOT NULL,
 track_id VARCHAR(64) CHARACTER SET ascii NOT NULL, entry_id CHAR(32) CHARACTER SET ascii NOT NULL DEFAULT '', audio_id CHAR(32) CHARACTER SET ascii NOT NULL DEFAULT '',
 reused TINYINT NOT NULL DEFAULT 0, played TINYINT NOT NULL DEFAULT 0, released TINYINT NOT NULL DEFAULT 0,
 created_ms BIGINT NOT NULL, INDEX(tenant_id,room_id,runtime_id,track_id), INDEX(audio_id,released), INDEX(entry_id,released)
 ) ENGINE=InnoDB`,
	} {
		if _, e := s.db.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	// 复用命中率埋点：reason 列记录每次 Select 的命中/未命中原因（text_hit / fresh_empty /
	// fresh_cooldown / fresh_probability / retry_existing_decision），供统计真实复用命中率。
	if e := s.ensureSpeechLibraryReasonColumns(ctx); e != nil {
		return e
	}
	return nil
}

func (s *Store) ensureSpeechLibraryReasonColumns(ctx context.Context) error {
	if has, e := s.columnExists(ctx, "mgmt_speech_library_leases", "reason"); e != nil {
		return e
	} else if !has {
		if _, e := s.db.ExecContext(ctx, `ALTER TABLE mgmt_speech_library_leases ADD COLUMN reason VARCHAR(32) CHARACTER SET ascii NOT NULL DEFAULT ''`); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) SpeechLibraryPolicy(ctx context.Context) (lib.Policy, error) {
	p := lib.DefaultPolicy()
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, lib.SettingKey).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	if e = json.Unmarshal([]byte(raw), &p); e != nil {
		return p, e
	}
	return p, p.Validate()
}
func (s *Store) lockSpeechSlot(ctx context.Context, tx *sql.Tx, sc lib.Scope, activate bool) error {
	if e := sc.Validate(); e != nil {
		return e
	}
	key := sc.Key()
	roomKey := lib.Digest(struct {
		Realm        string
		Tenant, Room int64
	}{sc.Realm, sc.Tenant, sc.Room})
	var ownership int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_runtime_sessions WHERE id=? AND tenant_id=? AND room_id=? AND ended_at IS NULL AND status IN ('running','paused','starting','resuming')`, sc.Runtime, sc.Tenant, sc.Room).Scan(&ownership); e != nil {
		return e
	}
	if ownership != 1 {
		return lib.ErrStale
	}
	if activate {
		if _, e := tx.ExecContext(ctx, `INSERT INTO mgmt_speech_library_rooms VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE room_key=VALUES(room_key)`, roomKey, sc.Tenant, sc.Plan, sc.Room, sc.Runtime, sc.RoomContext); e != nil {
			return e
		}
	}
	var epoch int64
	var roomSHA string
	if e := tx.QueryRowContext(ctx, `SELECT runtime_id,context_sha FROM mgmt_speech_library_rooms WHERE room_key=? FOR UPDATE`, roomKey).Scan(&epoch, &roomSHA); e != nil {
		return e
	}
	if sc.Runtime < epoch || !activate && (sc.Runtime != epoch || roomSHA != sc.RoomContext) {
		return lib.ErrStale
	}
	if activate && roomSHA != sc.RoomContext {
		if _, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_entries e JOIN mgmt_speech_library_slots s ON s.scope_key=e.scope_key SET e.active=0 WHERE s.room_key=?`, roomKey); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_audio a JOIN mgmt_speech_library_slots s ON s.scope_key=a.scope_key SET a.retired=1,a.delete_after_ms=? WHERE s.room_key=? AND a.retired=0`, time.Now().Add(24*time.Hour).UnixMilli(), roomKey); e != nil {
			return e
		}
	}
	if _, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_rooms SET runtime_id=?,context_sha=?,plan_id=? WHERE room_key=?`, sc.Runtime, sc.RoomContext, sc.Plan, roomKey); e != nil {
		return e
	}
	if activate {
		if _, e := tx.ExecContext(ctx, `INSERT INTO mgmt_speech_library_slots VALUES(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE scope_key=VALUES(scope_key)`, key, roomKey, sc.Tenant, sc.Plan, sc.Room, sc.Runtime, sc.TextContext, sc.VoiceContext); e != nil {
			return e
		}
	}
	var runtime int64
	var text, voice string
	if e := tx.QueryRowContext(ctx, `SELECT runtime_id,text_sha,voice_sha FROM mgmt_speech_library_slots WHERE scope_key=? FOR UPDATE`, key).Scan(&runtime, &text, &voice); e != nil {
		return e
	}
	if runtime > sc.Runtime || !activate && (runtime != sc.Runtime || text != sc.TextContext || voice != sc.VoiceContext) {
		return lib.ErrStale
	}
	now := time.Now().UnixMilli() + int64((24*time.Hour)/time.Millisecond)
	if text != sc.TextContext {
		if _, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_entries SET active=0 WHERE scope_key=?`, key); e != nil {
			return e
		}
	}
	if text != sc.TextContext || voice != sc.VoiceContext {
		if _, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET retired=1,delete_after_ms=? WHERE scope_key=? AND retired=0`, now, key); e != nil {
			return e
		}
	}
	_, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_slots SET runtime_id=?,text_sha=?,voice_sha=? WHERE scope_key=?`, sc.Runtime, sc.TextContext, sc.VoiceContext, key)
	return e
}
func speechEntry(ctx context.Context, tx *sql.Tx, id string) (*lib.Entry, error) {
	var e lib.Entry
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT id,payload_json,last_used_ms,use_count FROM mgmt_speech_library_entries WHERE id=? AND active=1`, id).Scan(&e.ID, &raw, &e.LastUsedMS, &e.UseCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(raw), &e.Candidate)
	return &e, err
}
func (s *Store) Select(ctx context.Context, sc lib.Scope, p lib.Policy, decision, track string) (lib.Choice, error) {
	var out lib.Choice
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = s.lockSpeechSlot(ctx, tx, sc, true); e != nil {
		return out, e
	}
	if e = trimSpeechLibrary(ctx, tx, sc.Key(), p.Capacity); e != nil {
		return out, e
	}
	var prior string
	var released bool
	e = tx.QueryRowContext(ctx, `SELECT entry_id,track_id,released FROM mgmt_speech_library_leases WHERE decision_key=? AND scope_key=?`, decision, sc.Key()).Scan(&prior, &out.TrackID, &released)
	if e == nil {
		if prior != "" && !released {
			out.Entry, e = speechEntry(ctx, tx, prior)
			if e != nil {
				return out, e
			}
		}
		out.Reason = "retry_existing_decision"
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_speech_library_entries WHERE scope_key=? AND text_sha=? AND active=1`, sc.Key(), sc.TextContext).Scan(&count); e != nil {
		return out, e
	}
	out.Reason = "fresh_probability"
	chosen := ""
	if count == 0 {
		out.Reason = "fresh_empty"
	} else if lib.ReuseDecision(decision, count, p) {
		e = tx.QueryRowContext(ctx, `SELECT e.id FROM mgmt_speech_library_entries e WHERE e.scope_key=? AND e.text_sha=? AND e.active=1 AND e.last_used_ms<=?
   AND NOT EXISTS(SELECT 1 FROM mgmt_speech_library_leases l WHERE l.entry_id=e.id AND l.released=0)
   ORDER BY e.last_used_ms,e.created_ms,e.id LIMIT 1`, sc.Key(), sc.TextContext, time.Now().UnixMilli()-int64(p.CooldownSeconds)*1000).Scan(&chosen)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		out.Reason = "fresh_cooldown"
		if chosen != "" {
			out.Reason = "text_hit"
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO mgmt_speech_library_leases(decision_key,scope_key,tenant_id,room_id,runtime_id,track_id,entry_id,reused,reason,created_ms) VALUES(?,?,?,?,?,?,?,?,?,?)`, decision, sc.Key(), sc.Tenant, sc.Room, sc.Runtime, track, chosen, chosen != "", out.Reason, time.Now().UnixMilli()); e != nil {
		return out, e
	}
	if chosen != "" {
		out.Entry, e = speechEntry(ctx, tx, chosen)
		if e != nil {
			return out, e
		}
	}
	out.TrackID = track
	return out, tx.Commit()
}
func (s *Store) Cancel(ctx context.Context, key string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET released=1,entry_id='',audio_id='',reused=0 WHERE decision_key=?`, key)
	return e
}
func (s *Store) ReserveAudio(ctx context.Context, sc lib.Scope, decision, track string, a lib.Audio) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.lockSpeechSlot(ctx, tx, sc, false); e != nil {
		return e
	}
	raw, _ := json.Marshal(a)
	if _, e = tx.ExecContext(ctx, `INSERT INTO mgmt_speech_library_audio(id,scope_key,entry_id,voice_sha,payload_json,delete_after_ms) VALUES(?,?,?,?,?,?)`, a.ID, sc.Key(), a.EntryID, sc.VoiceContext, string(raw), time.Now().Add(5*time.Hour).UnixMilli()); e != nil {
		return e
	}
	// Pin before upload. A stopped/replaced run is released by the lifecycle sweep.
	_, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET audio_id=?,track_id=?,released=0 WHERE decision_key=? AND scope_key=? AND runtime_id=?`, a.ID, track, decision, sc.Key(), sc.Runtime)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Publish(ctx context.Context, sc lib.Scope, p lib.Policy, decision, track string, entry lib.Entry, a lib.Audio) error {
	if len(entry.ID) != 32 || len(a.ID) != 32 || a.EntryID != entry.ID || a.Prefix != lib.ObjectPrefix(sc, a.ID) || a.Bundle.AudioDurationMS <= 0 || a.Bundle.Text == "" || entry.Candidate.FinalContentAudit == nil || entry.Candidate.FinalContentAudit.Decision != "通过" {
		return errors.New("话术库条目或完整审核音频包无效")
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.lockSpeechSlot(ctx, tx, sc, false); e != nil {
		return e
	}
	var leaseCount int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_speech_library_leases WHERE decision_key=? AND scope_key=? AND runtime_id=? AND audio_id=? AND released=0`, decision, sc.Key(), sc.Runtime, a.ID).Scan(&leaseCount); e != nil {
		return e
	}
	if leaseCount != 1 {
		return lib.ErrStale
	}
	// Exact duplicate text is one entry; a missing or changed voice gets a new audio.
	output := lib.Digest(a.Bundle.Text)
	var duplicate string
	e = tx.QueryRowContext(ctx, `SELECT id FROM mgmt_speech_library_entries WHERE scope_key=? AND text_sha=? AND output_sha=? AND active=1 LIMIT 1`, sc.Key(), sc.TextContext, output).Scan(&duplicate)
	if e == nil {
		entry.ID = duplicate
		a.EntryID = duplicate
	} else if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	raw, _ := json.Marshal(entry.Candidate)
	if _, e = tx.ExecContext(ctx, `INSERT INTO mgmt_speech_library_entries(id,scope_key,text_sha,output_sha,payload_json,created_ms) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE payload_json=VALUES(payload_json),active=1`, entry.ID, sc.Key(), sc.TextContext, output, string(raw), time.Now().UnixMilli()); e != nil {
		return e
	}
	grace := time.Now().Add(24 * time.Hour).UnixMilli()
	if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET retired=1,delete_after_ms=? WHERE scope_key=? AND entry_id=? AND id<>? AND retired=0`, grace, sc.Key(), entry.ID, a.ID); e != nil {
		return e
	}
	audioRaw, _ := json.Marshal(a)
	if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET payload_json=?,entry_id=?,ready=1,retired=0,delete_after_ms=0 WHERE id=? AND scope_key=?`, string(audioRaw), entry.ID, a.ID, sc.Key()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET entry_id=?,track_id=? WHERE decision_key=?`, entry.ID, track, decision); e != nil {
		return e
	}
	if e = trimSpeechLibrary(ctx, tx, sc.Key(), p.Capacity); e != nil {
		return e
	}
	return tx.Commit()
}
func trimSpeechLibrary(ctx context.Context, tx *sql.Tx, key string, capacity int) error {
	rows, e := tx.QueryContext(ctx, `SELECT id FROM mgmt_speech_library_entries WHERE scope_key=? AND active=1 ORDER BY created_ms DESC,id DESC`, key)
	if e != nil {
		return e
	}
	var old []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		old = append(old, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	grace := time.Now().Add(24 * time.Hour).UnixMilli()
	for i := capacity; i < len(old); i++ {
		if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_entries SET active=0 WHERE id=?`, old[i]); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET retired=1,delete_after_ms=? WHERE entry_id=?`, grace, old[i]); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) ClaimAudio(ctx context.Context, sc lib.Scope, decision, entry string) (*lib.Audio, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = s.lockSpeechSlot(ctx, tx, sc, false); e != nil {
		return nil, e
	}
	var raw string
	e = tx.QueryRowContext(ctx, `SELECT payload_json FROM mgmt_speech_library_audio WHERE scope_key=? AND entry_id=? AND voice_sha=? AND ready=1 AND retired=0 LIMIT 1 FOR UPDATE`, sc.Key(), entry, sc.VoiceContext).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	var a lib.Audio
	if e = json.Unmarshal([]byte(raw), &a); e != nil {
		return nil, e
	}
	res, e := tx.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET audio_id=? WHERE decision_key=? AND runtime_id=? AND entry_id=? AND released=0`, a.ID, decision, sc.Runtime, entry)
	if e != nil {
		return nil, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		var exists int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_speech_library_leases WHERE decision_key=? AND runtime_id=? AND entry_id=? AND audio_id=? AND released=0`, decision, sc.Runtime, entry, a.ID).Scan(&exists); e != nil {
			return nil, e
		}
		if exists != 1 {
			return nil, lib.ErrStale
		}
	}
	return &a, tx.Commit()
}
func (s *Store) MarkPlayed(ctx context.Context, tenant, room, runtime int64, track string, at time.Time) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var decision, entry string
	var played bool
	e = tx.QueryRowContext(ctx, `SELECT decision_key,entry_id,played FROM mgmt_speech_library_leases WHERE tenant_id=? AND room_id=? AND runtime_id=? AND track_id=? LIMIT 1 FOR UPDATE`, tenant, room, runtime, track).Scan(&decision, &entry, &played)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if !played {
		if entry != "" {
			if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_entries SET last_used_ms=GREATEST(last_used_ms,?),use_count=use_count+1 WHERE id=?`, at.UnixMilli(), entry); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET played=1 WHERE decision_key=?`, decision); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) ReleaseRuntime(ctx context.Context, tenant, room, runtime int64) error {
	_, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET released=1 WHERE tenant_id=? AND room_id=? AND runtime_id=?`, tenant, room, runtime)
	return e
}

// A later actual mainline start proves earlier sequential tracks no longer
// need their source object, even on Core versions lacking completion counters.
func (s *Store) ReleaseLibraryBefore(ctx context.Context, tenant, room, runtime int64, current string, at time.Time) error {
	_, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET released=1 WHERE tenant_id=? AND room_id=? AND runtime_id=? AND played=1 AND track_id<>? AND created_ms<=?`, tenant, room, runtime, current, at.UnixMilli())
	return e
}
func (s *Store) FindLibraryAudio(ctx context.Context, tenant, room, runtime int64, track string) (*lib.Audio, error) {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT a.payload_json FROM mgmt_speech_library_leases l JOIN mgmt_speech_library_audio a ON a.id=l.audio_id WHERE l.tenant_id=? AND l.room_id=? AND l.runtime_id=? AND l.track_id=? AND a.ready=1 LIMIT 1`, tenant, room, runtime, track).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var a lib.Audio
	e = json.Unmarshal([]byte(raw), &a)
	return &a, e
}
func (s *Store) ReleaseLibraryTrack(ctx context.Context, tenant, room, runtime int64, track string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_leases SET released=1 WHERE tenant_id=? AND room_id=? AND runtime_id=? AND track_id=? AND played=1`, tenant, room, runtime, track)
	return e
}
func (s *Store) Due(ctx context.Context, limit int) ([]lib.Audio, error) {
	// Deleted plans/rooms cannot accumulate immortal assets. Never retire from
	// an active/paused AI snapshot; its saved content still owns those references.
	orphan := ` FROM mgmt_speech_library_slots s
 LEFT JOIN live_agent_plans p ON p.id=s.plan_id AND p.tenant_id=s.tenant_id
 LEFT JOIN core_rooms r ON r.id=s.room_id AND r.tenant_id=s.tenant_id
 WHERE (p.id IS NULL OR r.id IS NULL) AND NOT EXISTS
 (SELECT 1 FROM live_runtime_sessions x WHERE x.tenant_id=s.tenant_id AND x.room_id=s.room_id AND x.ended_at IS NULL AND x.status IN ('running','paused','starting','resuming'))`
	if _, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_entries SET active=0 WHERE active=1 AND scope_key IN (SELECT s.scope_key`+orphan+`)`); e != nil {
		return nil, e
	}
	if _, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET retired=1,delete_after_ms=? WHERE retired=0 AND scope_key IN (SELECT s.scope_key`+orphan+`)`, time.Now().Add(24*time.Hour).UnixMilli()); e != nil {
		return nil, e
	}
	// A pause, room collection stop, timeout or process restart cannot release a
	// live AI's references. Only its real terminal DB lifecycle does so.
	if _, e := s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_leases l JOIN live_runtime_sessions r ON r.id=l.runtime_id AND r.tenant_id=l.tenant_id AND r.room_id=l.room_id SET l.released=1 WHERE l.released=0 AND (r.ended_at IS NOT NULL OR r.status IN ('stopped','failed','completed','cancelled'))`); e != nil {
		return nil, e
	}
	if _, e := s.db.ExecContext(ctx, `DELETE FROM mgmt_speech_library_leases WHERE released=1 AND created_ms<UNIX_TIMESTAMP(UTC_TIMESTAMP()-INTERVAL 24 HOUR)*1000`); e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT a.payload_json FROM mgmt_speech_library_audio a WHERE (a.retired=1 OR a.ready=0) AND a.delete_after_ms>0 AND a.delete_after_ms<=?
 AND NOT EXISTS(SELECT 1 FROM mgmt_speech_library_leases l WHERE l.audio_id=a.id AND l.released=0) LIMIT ?`, time.Now().UnixMilli(), limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []lib.Audio{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var a lib.Audio
		if e = json.Unmarshal([]byte(raw), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ForgetAudio(ctx context.Context, id string) error {
	if _, e := s.db.ExecContext(ctx, `DELETE a FROM mgmt_speech_library_audio a WHERE id=? AND (retired=1 OR ready=0) AND NOT EXISTS(SELECT 1 FROM mgmt_speech_library_leases l WHERE l.audio_id=a.id AND l.released=0)`, id); e != nil {
		return e
	}
	_, e := s.db.ExecContext(ctx, `DELETE e FROM mgmt_speech_library_entries e WHERE active=0 AND NOT EXISTS(SELECT 1 FROM mgmt_speech_library_audio a WHERE a.entry_id=e.id) AND NOT EXISTS(SELECT 1 FROM mgmt_speech_library_leases l WHERE l.entry_id=e.id AND l.released=0)`)
	return e
}
func (s *Store) Statistics(ctx context.Context) (lib.Stats, error) {
	var out lib.Stats
	for _, item := range []struct {
		q string
		n *int64
	}{{`SELECT COUNT(*) FROM mgmt_speech_library_entries WHERE active=1`, &out.Texts}, {`SELECT COUNT(*) FROM mgmt_speech_library_audio WHERE ready=1 AND retired=0`, &out.Audio}, {`SELECT COUNT(*) FROM mgmt_speech_library_leases WHERE reused=1 AND played=1 AND created_ms>=UNIX_TIMESTAMP(UTC_TIMESTAMP()-INTERVAL 24 HOUR)*1000`, &out.PlayedReuses}} {
		if e := s.db.QueryRowContext(ctx, item.q).Scan(item.n); e != nil {
			return out, e
		}
	}
	return out, nil
}

// Policy updates use a current-read CAS. HTTP management auditing remains in
// the existing adminAuditMiddleware; no user's cache gate is written here.
func (s *Store) SaveSpeechLibraryPolicy(ctx context.Context, p lib.Policy, user int64, expected string) error {
	if e := p.Validate(); e != nil {
		return e
	}
	if expected == "" {
		return lib.ErrStale
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw string
	if e = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? FOR UPDATE`, lib.SettingKey).Scan(&raw); e != nil {
		return e
	}
	var old lib.Policy
	if e = json.Unmarshal([]byte(raw), &old); e != nil {
		return e
	}
	if lib.Digest(old) != expected {
		return lib.ErrStale
	}
	b, _ := json.Marshal(p)
	if _, e = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=?`, string(b), user, lib.SettingKey); e != nil {
		return e
	}
	return tx.Commit()
}
