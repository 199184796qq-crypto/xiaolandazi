package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
)

// Display data is platform catalogue data. It never changes the source/target
// mapping, synthesis parameters, customer profiles or a running plan snapshot.
type TTSVoiceDisplay struct {
	VoiceID  string `json:"voice_id"`
	Name     string `json:"name"`
	Gender   string `json:"gender"`
	Revision uint64 `json:"revision"`
}

const ttsVoiceDisplayGroup = "tts_voice_display"

func validTTSVoiceDisplay(v TTSVoiceDisplay) bool {
	return ttsgateway.ValidVolcengineVoiceID(v.VoiceID) &&
		utf8.RuneCountInString(v.Name) >= 1 && utf8.RuneCountInString(v.Name) <= 80 &&
		!strings.ContainsFunc(v.Name, unicode.IsControl) &&
		(v.Gender == "" || v.Gender == "女声" || v.Gender == "男声")
}

func ttsVoiceDisplayKey(voiceID string) string {
	return fmt.Sprintf("tts_voice_display_%x", sha256.Sum256([]byte(ttsgateway.ProviderVolcengine+"/"+ttsgateway.VolcengineICL2+"/"+voiceID)))
}

func (s *Store) TTSVoiceDisplays(ctx context.Context) (map[string]TTSVoiceDisplay, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key,value_text FROM mgmt_system_settings WHERE setting_group=?`, ttsVoiceDisplayGroup)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TTSVoiceDisplay{}
	for rows.Next() {
		var key, raw string
		var v TTSVoiceDisplay
		if err = rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		if !validTTSVoiceDisplay(v) || v.Revision == 0 || key != ttsVoiceDisplayKey(v.VoiceID) {
			return nil, ttsscheduler.ErrConfig
		}
		out[v.VoiceID] = v
	}
	return out, rows.Err()
}

func (s *Store) SaveTTSVoiceDisplay(ctx context.Context, v TTSVoiceDisplay, actorID int64) (TTSVoiceDisplay, error) {
	v.Name = strings.TrimSpace(v.Name)
	if !validTTSVoiceDisplay(v) {
		return v, fmt.Errorf("%w：请填写1—80字的声音名称并选择男声、女声或未知", ttsscheduler.ErrConfig)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	key := ttsVoiceDisplayKey(v.VoiceID)
	// Insert and lock even on first save, so concurrent revision-0 writers cannot
	// overwrite one another. The placeholder rolls back on any failed save.
	_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO mgmt_system_settings(setting_key,setting_group,label,value_text,input_type,sort_order,updated_by_user_id) VALUES(?,?,?,'','json',0,?)`, key, ttsVoiceDisplayGroup, "高仿真声音展示资料", actorID)
	if err != nil {
		return v, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? AND setting_group=? FOR UPDATE`, key, ttsVoiceDisplayGroup).Scan(&raw); err != nil {
		return v, err
	}
	var previous TTSVoiceDisplay
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &previous); err != nil {
			return v, err
		}
		if !validTTSVoiceDisplay(previous) || previous.VoiceID != v.VoiceID || previous.Revision == 0 {
			return v, ttsscheduler.ErrConfig
		}
	}
	if previous.Revision != v.Revision {
		return v, ttsscheduler.ErrRevision
	}
	v.Revision++
	encoded, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=? AND setting_group=?`, string(encoded), actorID, key, ttsVoiceDisplayGroup); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
