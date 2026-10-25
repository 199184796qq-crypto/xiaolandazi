package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

type recordingCacheOwnerChanged struct{}

func (*recordingCacheOwnerChanged) Error() string {
	return "本场话术缓存已由新的供料任务接管"
}
func (*recordingCacheOwnerChanged) Unwrap() error { return anchorcycle.ErrStale }

var errRecordingCacheSuperseded = &recordingCacheOwnerChanged{}

// Content revisions and reusable dependency identity are different things.
// Only these known source/presentation paths are excluded. Unknown fields,
// including any facts or constraints named "text", remain in the fingerprint.
func recordingCacheCompatibility(version model.LiveAgentPlanVersion, policy *anchorcycle.State) (string, error) {
	if policy == nil || strings.TrimSpace(policy.P0PolicyMD) == "" {
		return "", errors.New("话术缓存缺少当前审核依据")
	}
	rewrite, _, err := loadFactSpeechRewritePrompt()
	if err != nil {
		return "", err
	}
	contextCopy := make(map[string]any, len(version.GenerationContext))
	for key, value := range version.GenerationContext {
		switch key {
		case "recording_script", "recording_original_sources", "duration_minutes", "round_minutes", "round_count", "variant_count":
			continue
		}
		contextCopy[key] = value
	}
	contextJSON, err := json.Marshal(contextCopy)
	if err != nil {
		return "", fmt.Errorf("话术缓存依据无法序列化: %w", err)
	}
	reviewJSON, err := json.Marshal(struct {
		Policy     string
		Surface    string
		Scope      string
		SkipReview bool
		Allowances []anchorcycle.P0Allowance
	}{policy.P0PolicyMD, policy.Surface, policy.Config.ComplianceScope, policy.Config.SkipReview, policy.P0Allowances})
	if err != nil {
		return "", err
	}
	// Bump the render contract when chunking, punctuation, timeline or
	// expression generation changes. Actual instruction outputs are included.
	parts := []string{"recording-incremental-dependencies-v1", "sentence-110-chunks-timeline-v1", string(contextJSON), string(reviewJSON), rewrite}
	for _, probe := range []string{"", "问题？", "提醒！", "价格337元", "欢迎朋友", "自然聊天"} {
		parts = append(parts, recordingInstructionForVersion(version, probe))
	}
	return recordingcache.VoiceKey(parts...), nil
}

// Explicit references cannot be assumed independent merely because the literal
// sentence did not change. Bind them to all source text, also across tabs. This
// is a conservative rule, not a claim of complete natural-language dependency
// analysis. Rewritten variants additionally retain their paragraph fingerprint.
func recordingSourceRefersToContext(source string) bool {
	return containsAnyText(source, "这个", "那个", "这些", "那些", "这款", "那款", "这份", "那份", "这套", "那套", "这家", "那家",
		"这样", "那样", "刚才", "刚刚", "刚说", "前面", "后面", "前边", "后边", "前文", "上面", "下面", "上述", "以上", "之前", "同样", "同款", "它", "其余")
}

func recordingCacheSnapshot(version model.LiveAgentPlanVersion, texts []string, policy *anchorcycle.State, sessionID int64) (*recordingcache.Manifest, error) {
	compatibility, err := recordingCacheCompatibility(version, policy)
	if err != nil {
		return nil, err
	}
	voice, legacyKey := recordingCurrentCacheIdentity(version, texts, policy)
	ids := recordingStableTabIDs(version, texts)
	// A map has canonical key order in encoding/json; no text order dependency.
	sourceMap := make(map[string]string, len(texts))
	for i, text := range texts {
		if _, duplicate := sourceMap[ids[i]]; duplicate || strings.TrimSpace(ids[i]) == "" {
			return nil, errors.New("话术缓存标签身份重复或为空")
		}
		sourceMap[ids[i]] = text
	}
	sourceJSON, _ := json.Marshal(sourceMap)
	globalContext := recordingcache.VoiceKey("recording-source-context-v1", string(sourceJSON))
	next := &recordingcache.Manifest{Version: 3, VoiceKey: voice, ScriptHash: legacyKey,
		CompatibilityKey: compatibility, LegacyBootstrapKey: legacyKey, SessionID: sessionID,
		OwnerToken: anchorcycle.ID(), PlanVersionID: version.ID}
	for i, text := range texts {
		tab := recordingcache.SourceTab{ID: ids[i]}
		for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			paragraph = strings.TrimSpace(paragraph)
			contextKey := recordingcache.VoiceKey("recording-paragraph-v1", paragraph)
			for _, source := range splitRecordingSentences(paragraph) {
				dependency := ""
				if recordingSourceRefersToContext(source) {
					dependency = globalContext
				}
				tab.Sources = append(tab.Sources, source)
				tab.UnitKeys = append(tab.UnitKeys, recordingcache.VoiceKey("recording-source-unit-v1", ids[i], source, dependency))
				tab.ContextKeys = append(tab.ContextKeys, contextKey)
			}
		}
		next.Tabs = append(next.Tabs, tab)
	}
	return next, nil
}

type recordingCacheInheritance struct {
	InheritedVersions int
	InvalidVersions   int
	LegacyExact       bool
}

// Carry only entries proven compatible with the new immutable source snapshot.
// The old snapshot/media are never edited. Deleted/changed keys are absent from
// the new active index, including every previous expression/audio version.
func inheritRecordingCache(previous, next *recordingcache.Manifest, legacyExact bool) (recordingUpdatePlanner, recordingCacheInheritance) {
	planner := recordingUpdatePlanner{}
	stats := recordingCacheInheritance{}
	if previous == nil {
		return planner, stats
	}
	stats.InvalidVersions = len(previous.Entries)
	compatible := previous.Version == 3 && previous.VoiceKey == next.VoiceKey && previous.CompatibilityKey == next.CompatibilityKey
	legacyExact = legacyExact && previous.Version == 2 && previous.VoiceKey == next.VoiceKey && previous.ScriptHash == next.LegacyBootstrapKey
	if !compatible && !legacyExact {
		return planner, stats
	}
	type sourceIdentity struct {
		source   string
		contexts map[string]bool
	}
	current := map[string]sourceIdentity{}
	legacyKeys := map[string][]string{}
	for _, tab := range next.Tabs {
		for i, source := range tab.Sources {
			key := tab.UnitKeys[i]
			identity, ok := current[key]
			if !ok {
				identity = sourceIdentity{source: source, contexts: map[string]bool{}}
			}
			identity.contexts[tab.ContextKeys[i]] = true
			current[key] = identity
			legacyKeys[source] = append(legacyKeys[source], key)
		}
	}
	previousSources := map[string]string{}
	for _, tab := range previous.Tabs {
		for i, source := range tab.Sources {
			if i < len(tab.UnitKeys) {
				previousSources[tab.UnitKeys[i]] = source
			}
		}
	}
	for _, entry := range previous.Entries {
		// A historical boolean alone cannot authenticate literal audio. Keep
		// every spoken character (including non-basic Han/symbols) in this check.
		entry.Polished = entry.Polished || !recordingCacheEntryIsLiteral(entry)
		if legacyExact {
			// V2 did not record tab ownership. Only an exact full-script identity
			// and a unique source location can authenticate an upgrade.
			keys := legacyKeys[entry.Sentence]
			if len(keys) != 1 {
				continue
			}
			entry.UnitKey = keys[0]
			for contextKey := range current[entry.UnitKey].contexts {
				entry.ContextKey = contextKey
			}
		} else if previousSources[entry.UnitKey] != entry.Sentence {
			continue
		}
		identity, ok := current[entry.UnitKey]
		if !ok || identity.source != entry.Sentence || (entry.Polished && !identity.contexts[entry.ContextKey]) {
			continue
		}
		next.Entries = append(next.Entries, entry)
		stats.InheritedVersions++
		stats.InvalidVersions--
	}
	next.Entries = pruneRecordingCacheEntries(next.Entries)
	stats.LegacyExact = legacyExact
	if len(previous.UpdateState) > 0 && json.Unmarshal(previous.UpdateState, &planner) == nil && planner.Version == 1 {
		oldTabs := map[string][]string{}
		for _, tab := range previous.Tabs {
			oldTabs[tab.ID] = tab.Sources
		}
		retained := map[string]recordingUpdateCredit{}
		for _, tab := range next.Tabs {
			planner.MigrateTab(tab.ID, oldTabs[tab.ID], tab.Sources)
			if credit, ok := planner.Tabs[tab.ID]; ok {
				retained[tab.ID] = credit
			}
		}
		planner.Tabs = retained
	} else {
		planner = recordingUpdatePlanner{}
	}
	return planner, stats
}

// One active lineage is the only inheritance source. Never search arbitrary
// historical manifests: editing A -> B -> A must not resurrect invalidated A.
func openRecordingIncrementalCache(scope recordingcache.Scope, blueprint *recordingcache.Manifest) (*recordingcache.Manifest, recordingUpdatePlanner, recordingCacheInheritance, error) {
	previous, err := scope.LoadActive()
	if err != nil {
		return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, err
	}
	expectedOwner, legacy := "", false
	if previous != nil {
		expectedOwner = previous.OwnerToken
		if previous.SessionID > blueprint.SessionID {
			return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, errRecordingCacheSuperseded
		}
	} else {
		allowed, err := scope.LegacyBootstrapAllowed()
		if err != nil {
			return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, err
		}
		if allowed {
			previous, err = scope.Load(blueprint.VoiceKey, blueprint.LegacyBootstrapKey)
			if err != nil {
				return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, err
			}
			if previous != nil && previous.Version != 2 {
				previous = nil
			}
			legacy = previous != nil
		}
	}
	next := *blueprint
	next.Entries = nil
	planner, stats := inheritRecordingCache(previous, &next, legacy)
	next.UpdateState, _ = json.Marshal(planner)
	claimed, err := scope.ClaimActive(expectedOwner, &next)
	if err != nil {
		return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, err
	}
	if !claimed {
		// A failed CAS means another producer/clear operation won. Do not read
		// its new token and immediately reclaim ownership in a retry loop.
		return nil, recordingUpdatePlanner{}, recordingCacheInheritance{}, errRecordingCacheSuperseded
	}
	return &next, planner, stats, nil
}

func recordingScopedCacheLookup(manifest *recordingcache.Manifest) map[string][]recordingcache.Entry {
	hit := map[string][]recordingcache.Entry{}
	if manifest != nil {
		for _, entry := range manifest.Entries {
			if entry.UnitKey != "" && entry.AssetID > 0 && len(entry.Segments) > 0 {
				hit[entry.UnitKey] = append(hit[entry.UnitKey], entry)
			}
		}
	}
	return hit
}

func recordingCacheCandidates(hit map[string][]recordingcache.Entry, unitKey, contextKey string) []recordingcache.Entry {
	var candidates []recordingcache.Entry
	for _, entry := range hit[unitKey] {
		if (!entry.Polished && recordingCacheEntryIsLiteral(entry)) || entry.ContextKey == contextKey {
			candidates = append(candidates, entry)
		}
	}
	return candidates
}

func recordingCacheEntryIsLiteral(entry recordingcache.Entry) bool {
	spoken := entry.Spoken
	if len(entry.Parts) > 0 {
		var joined strings.Builder
		for _, part := range entry.Parts {
			joined.WriteString(part.Spoken)
		}
		spoken = joined.String()
	}
	normalize := func(text string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				return -1
			}
			return r
		}, text)
	}
	return strings.TrimSpace(spoken) != "" && normalize(entry.Sentence) == normalize(spoken)
}
