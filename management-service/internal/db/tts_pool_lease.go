package db

import (
	"context"
	"errors"
)

// Renew only an existing unexpired running permit. A cancelled/finished job
// cannot be resurrected by a delayed heartbeat, nor can its account change.
func (s *Store) RenewTTS(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET heartbeat_at=UTC_TIMESTAMP(6),expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 2 MINUTE) WHERE id=? AND status='running' AND expires_at>UTC_TIMESTAMP(6)`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("TTS运行租约已结束或过期")
	}
	return nil
}
