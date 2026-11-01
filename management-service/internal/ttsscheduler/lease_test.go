package ttsscheduler

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type longLeaseRepo struct {
	mu                sync.Mutex
	renewed, finished int
}

func (*longLeaseRepo) EnqueueTTS(context.Context, Work) error { return nil }
func (*longLeaseRepo) ClaimTTS(context.Context, string) (Claim, error) {
	return Claim{Granted: true, Credential: Credential{AccountID: "owner"}}, nil
}
func (r *longLeaseRepo) FinishTTS(context.Context, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished++
	return nil
}
func (r *longLeaseRepo) RenewTTS(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renewed++
	return nil
}
func TestLongLeaseRenewsAfterLiveStopUntilCloudFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root, stop := context.WithCancel(context.Background())
		defer stop()
		parent, cancel := context.WithCancel(root)
		defer cancel()
		repo := &longLeaseRepo{}
		service := New(root, repo)
		permit, err := service.Acquire(parent, Work{Model: "seed-icl-2.0"})
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		time.Sleep(141 * time.Second)
		repo.mu.Lock()
		n, f := repo.renewed, repo.finished
		repo.mu.Unlock()
		if n < 6 || f != 0 {
			t.Fatal("cloud permit lost when caller stopped", n, f)
		}
		permit.Finish("completed")
		permit.Finish("completed")
		time.Sleep(41 * time.Second)
		repo.mu.Lock()
		defer repo.mu.Unlock()
		if repo.renewed != n || repo.finished != 1 {
			t.Fatal("lease renewed after finish or settled twice")
		}
	})
}
