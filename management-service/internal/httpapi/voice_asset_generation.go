package httpapi

import (
	"context"
	"path/filepath"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

const voiceCacheEpochContextKey = "_runtime_voice_cache_stamp"

type voiceParagraphKey struct{}

// This value is attached only to a copied runtime version. It is never saved to
// the user's plan or included in model prompts.
func scriptVersionStamp(v model.LiveAgentPlanVersion) (voiceasset.Stamp, bool) {
	st, ok := v.GenerationContext[voiceCacheEpochContextKey].(voiceasset.Stamp)
	if ok && st.Scope.Voice != "" && st.Scope.Voice != voiceasset.Digest(v.VoiceIdentity) {
		return voiceasset.Stamp{}, false
	}
	return st, ok
}
func (s *Server) voiceAssetGeneration(ctx context.Context, tenant int64, v model.LiveAgentPlanVersion) (context.Context, model.LiveAgentPlanVersion, error) {
	if s.store == nil {
		return ctx, v, nil
	}
	scope := voiceasset.Scope{Tenant: tenant, Plan: v.PlanID, Category: "mainline", Voice: voiceasset.Digest(v.VoiceIdentity)}
	st, e := s.store.VoiceCacheStamp(ctx, scope)
	if e != nil {
		return ctx, v, e
	}
	copyMap := map[string]any{}
	for k, x := range v.GenerationContext {
		copyMap[k] = x
	}
	copyMap[voiceCacheEpochContextKey] = st
	v.GenerationContext = copyMap
	return voiceasset.WithStamp(ctx, st, s.store.CheckVoiceCacheStamp), v, nil
}
func (s *Server) voiceAssetParagraph(ctx context.Context, tenant int64, v model.LiveAgentPlanVersion, u volcengineScriptUnit) (context.Context, model.LiveAgentPlanVersion, volcengineScriptUnit, error) {
	// Never silently upgrade an in-flight old task to the new clear generation.
	if st, ok := voiceasset.FromContext(ctx); ok {
		if st.Scope != (voiceasset.Scope{Tenant: tenant, Plan: v.PlanID, Category: "mainline", Voice: voiceasset.Digest(v.VoiceIdentity)}) {
			return ctx, v, u, voiceasset.ErrCleared
		}
		if s.store == nil {
			return ctx, v, u, voiceasset.ErrCleared
		}
		if e := s.store.CheckVoiceCacheStamp(ctx, st); e != nil {
			return ctx, v, u, e
		}
	} else {
		var e error
		ctx, v, e = s.voiceAssetGeneration(ctx, tenant, v)
		if e != nil {
			return ctx, v, u, e
		}
	}
	if st, ok := voiceasset.FromContext(ctx); ok {
		m := map[string]any{}
		for k, x := range v.GenerationContext {
			m[k] = x
		}
		m[voiceCacheEpochContextKey] = st
		v.GenerationContext = m
	}
	u = scriptUnitForVoice(v, u)
	ctx = context.WithValue(ctx, voiceParagraphKey{}, u.Key)
	if s.store != nil {
		if e := s.store.CheckVoiceParagraph(ctx, tenant, v.PlanID, u.Key); e != nil {
			return ctx, v, u, e
		}
	}
	if s.store != nil {
		if st, ok := voiceasset.FromContext(ctx); ok {
			if e := s.store.RegisterVoiceLocalSource(ctx, st, filepath.Clean(u.Dir)); e != nil {
				return ctx, v, u, e
			}
		}
	}
	return ctx, v, u, nil
}
