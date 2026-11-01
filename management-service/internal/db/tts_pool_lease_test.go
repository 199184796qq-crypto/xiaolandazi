package db

import (
	"testing"
	"time"
)

func TestTTSPoolMySQLLongLeaseDoesNotResurrect(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx := t.Context()
	ttsEnqueue(t, s, "long-lease", "voice-a")
	claim, err := s.ClaimTTS(ctx, "long-lease")
	if err != nil || !claim.Granted {
		t.Fatal(claim.Granted, err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET expires_at=UTC_TIMESTAMP(6)+INTERVAL 1 SECOND WHERE id='long-lease'`); err != nil {
		t.Fatal(err)
	}
	if err = s.RenewTTS(ctx, "long-lease"); err != nil {
		t.Fatal(err)
	}
	var expires time.Time
	if err = s.db.QueryRowContext(ctx, `SELECT expires_at FROM mgmt_tts_pool_jobs WHERE id='long-lease'`).Scan(&expires); err != nil || time.Until(expires) < 110*time.Second {
		t.Fatal("lease not extended", err)
	}
	if err = s.FinishTTS(ctx, "long-lease", "completed"); err != nil {
		t.Fatal(err)
	}
	if err = s.RenewTTS(ctx, "long-lease"); err == nil {
		t.Fatal("completed permit resurrected")
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET status='running',expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id='long-lease'`); err != nil {
		t.Fatal(err)
	}
	if err = s.RenewTTS(ctx, "long-lease"); err == nil {
		t.Fatal("expired permit resurrected")
	}
}
