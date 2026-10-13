package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"testing"
)

type scopedVoiceCacheFixture struct {
	tenant int64
	fail   bool
}

func (f *scopedVoiceCacheFixture) GenerationCacheReuseEnabled(context.Context, string) (bool, error) {
	return false, errors.New("runtime must use tenant policy")
}
func (f *scopedVoiceCacheFixture) GenerationCacheReuseEnabledForTenant(_ context.Context, tenant int64, key string) (bool, error) {
	f.tenant = tenant
	if f.fail {
		return false, errors.New("unavailable")
	}
	return tenant == 14 && key == model.MainlineGenerationCacheReuseSetting, nil
}
func (f *scopedVoiceCacheFixture) AnchorVoiceCacheRetentionHoursForTenant(context.Context, int64) (int, error) {
	return 12, nil
}

func TestMainlineCacheUsesCurrentTenantAndFailsToFreshGeneration(t *testing.T) {
	f := &scopedVoiceCacheFixture{}
	s := &Server{generationCachePolicy: f}
	ctx := context.Background()
	if !s.mainlineCacheReuseEnabled(ctx, 14) || f.tenant != 14 {
		t.Fatal("tenant override not applied")
	}
	if s.mainlineCacheReuseEnabled(ctx, 15) || f.tenant != 15 {
		t.Fatal("other user inherited override")
	}
	f.fail = true
	if s.mainlineCacheReuseEnabled(ctx, 14) {
		t.Fatal("failed lookup authorized reuse")
	}
}

func TestUserCacheWritesAreAudited(t *testing.T) {
	path := "/api/v1/liveops/user-voice-cache-policies/14"
	if !shouldAuditRequest("PUT", path) || auditAction("PUT", path) != "liveops.user_voice_cache.update" {
		t.Fatal("user cache changes escaped audit")
	}
}
