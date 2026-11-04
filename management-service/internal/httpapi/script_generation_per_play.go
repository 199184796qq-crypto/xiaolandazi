package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/voiceasset"
)

type scriptGenerationPlay struct {
	Instance  string `json:"instance"`
	Piece     string `json:"piece"`
	Identity  string `json:"identity"`
	Inventory string `json:"inventory"`
}

type scriptGenerationPlays struct {
	Session      int64                 `json:"session"`
	Version      int64                 `json:"version"`
	LastSequence int64                 `json:"last_sequence"`
	Pending      *scriptGenerationPlay `json:"pending,omitempty"`
}

type scriptGenerationGuardKey struct{}

// The same guard accompanies all existing rewrite/repair calls. It changes
// admission only; it does not add model requests, prompts or automatic retries.
func checkScriptGenerationGuard(ctx context.Context) error {
	if err := voiceasset.Check(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard, ok := ctx.Value(scriptGenerationGuardKey{}).(func(context.Context) error); ok {
		return guard(ctx)
	}
	return nil
}

func (s *Server) completePlayedScriptModel(ctx context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	if err := checkScriptGenerationGuard(ctx); err != nil {
		return agentgateway.Response{}, err
	}
	return s.completePaidScriptModel(ctx, request)
}

func scriptGenerationPlayPath(root string, tenant, room, plan int64) string {
	return filepath.Join(root, "script-generation-plays", strconv.FormatInt(tenant, 10), strconv.FormatInt(room, 10), strconv.FormatInt(plan, 10)+".json")
}

func scriptGenerationIdentity(v model.LiveAgentPlanVersion) string {
	// Audio keys deliberately share some provider settings. A purchase permit
	// is stricter: switching account/binding/style must not inherit old work.
	raw, err := json.Marshal([]any{scriptRewriteFactsKey(v), scriptFirstParagraphKey(v, "policy"), v.VoiceIdentity})
	if err != nil {
		return ""
	}
	return shortScriptIdentity(string(raw))
}

func scriptGenerationInventory(root string, v model.LiveAgentPlanVersion, u volcengineScriptUnit) (string, error) {
	// These files are atomically replaced. A read-only snapshot must not wait
	// for the global pool writer on the foreground PCM acknowledgement path.
	pool, err := loadScriptVoicePoolLocked(root, v.PlanID, u.poolID())
	if err != nil {
		return "", err
	}
	var refs []string
	for _, x := range pool.Versions {
		if _, ok := scriptVersionAudio(root, u, v, x); ok {
			refs = append(refs, x.AudioRef)
		}
	}
	sort.Strings(refs)
	return shortScriptIdentity(strings.Join(refs, ";")), nil
}

func validScriptGenerationScope(tenant, room, session int64, v model.LiveAgentPlanVersion) bool {
	return tenant > 0 && tenant == v.TenantID && room > 0 && session > 0 && v.ID > 0 && v.PlanID > 0 &&
		v.VoiceIdentity.VoiceID != "" && v.VoiceIdentity.Model != "" && scriptGenerationIdentity(v) != ""
}

func scriptGenerationSequence(session int64, instance string) (int64, bool) {
	prefix := "long-" + strconv.FormatInt(session, 10) + "-"
	seq, err := strconv.ParseInt(strings.TrimPrefix(instance, prefix), 10, 64)
	return seq, err == nil && seq > 0 && instance == prefix+strconv.FormatInt(seq, 10)
}

func readScriptGenerationPlays(path string) (scriptGenerationPlays, error) {
	var st scriptGenerationPlays
	err := readVolcengineJSON(path, &st)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err == nil && (st.Session <= 0 || st.Version <= 0 || st.LastSequence < 0) {
		err = errors.New("invalid script generation scope checkpoint")
	}
	if err == nil && st.Pending != nil {
		seq, valid := scriptGenerationSequence(st.Session, st.Pending.Instance)
		if !valid || seq != st.LastSequence || st.Pending.Piece == "" || st.Pending.Identity == "" || st.Pending.Inventory == "" {
			err = errors.New("invalid script generation playback checkpoint")
		}
	}
	return st, err
}

// Only a positive PCM receipt calls this function. A room keeps its latest
// playback opportunity, never a queue of purchases to catch up after a pause.
// Sequence high-water survives cursor replay and has no bounded dedupe horizon.
func recordScriptGenerationPlay(root string, tenant, room, session int64, v model.LiveAgentPlanVersion, u volcengineScriptUnit, instance string) error {
	seq, valid := scriptGenerationSequence(session, instance)
	if !validScriptGenerationScope(tenant, room, session, v) || !valid || strings.TrimSpace(u.Text) == "" || u.poolID() == "" {
		return nil
	}
	path := scriptGenerationPlayPath(root, tenant, room, v.PlanID)
	release, acquired, err := sharedfilelock.TryLock(path + ".lock")
	if err != nil || !acquired {
		return err
	}
	defer release()
	st, err := readScriptGenerationPlays(path)
	if err != nil || st.Session > session || (st.Session == session && st.Version > v.ID) {
		return err
	}
	if st.Session != session || st.Version != v.ID {
		st = scriptGenerationPlays{Session: session, Version: v.ID}
	}
	if seq <= st.LastSequence {
		return nil
	}
	st.LastSequence = seq
	st.Pending = nil
	// This nonblocking lease is held through model+TTS by the consumer. Plays
	// heard while it is running do not accumulate additional future purchases.
	activeRelease, idle, err := sharedfilelock.TryLock(path + ".active.lock")
	if err != nil {
		return err
	}
	if idle {
		defer activeRelease()
		inventory, err := scriptGenerationInventory(root, v, u)
		if err != nil {
			return err
		}
		st.Pending = &scriptGenerationPlay{Instance: instance, Piece: u.poolID(), Identity: scriptGenerationIdentity(v), Inventory: inventory}
	}
	return writeVolcengineJSON(path, st)
}

func pendingScriptGenerationPlay(root string, tenant, room, session int64, v model.LiveAgentPlanVersion) (*scriptGenerationPlay, error) {
	if !validScriptGenerationScope(tenant, room, session, v) {
		return nil, nil
	}
	st, err := readScriptGenerationPlays(scriptGenerationPlayPath(root, tenant, room, v.PlanID))
	if err != nil || st.Session != session || st.Version != v.ID || st.Pending == nil {
		return nil, err
	}
	if st.Pending.Identity != scriptGenerationIdentity(v) {
		// Do not let A -> B -> A (or an older implementation's permit) revive
		// pre-switch work without another real playback instance.
		discardScriptGenerationPlay(root, tenant, room, session, v, st.Pending)
		return nil, nil
	}
	return st.Pending, nil
}

// A claim is durably consumed before any paid work, not after a successful
// response. Failure/restart must wait for another actual play, not repurchase.
func claimScriptGenerationPlay(ctx context.Context, root string, tenant, room, session int64, v model.LiveAgentPlanVersion, u volcengineScriptUnit, expected *scriptGenerationPlay) (func(), bool, error) {
	if !validScriptGenerationScope(tenant, room, session, v) || expected == nil {
		return nil, false, nil
	}
	if expected.Identity != scriptGenerationIdentity(v) {
		discardScriptGenerationPlay(root, tenant, room, session, v, expected)
		return nil, false, nil
	}
	if ctx.Err() != nil {
		discardScriptGenerationPlay(root, tenant, room, session, v, expected)
		return nil, false, ctx.Err()
	}
	path := scriptGenerationPlayPath(root, tenant, room, v.PlanID)
	activeRelease, idle, err := sharedfilelock.TryLock(path + ".active.lock")
	if err != nil || !idle {
		return nil, false, err
	}
	claimed := false
	defer func() {
		if !claimed {
			activeRelease()
		}
	}()
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return nil, false, err
	}
	defer release()
	st, err := readScriptGenerationPlays(path)
	if err != nil || st.Session != session || st.Version != v.ID || st.Pending == nil || *st.Pending != *expected || expected.Piece != u.poolID() || expected.Identity != scriptGenerationIdentity(v) {
		return nil, false, err
	}
	st.Pending = nil
	if err = writeVolcengineJSON(path, st); err != nil {
		return nil, false, err
	}
	// A different room may already have supplied this shared piece since play.
	// Reuse its result instead of buying one for every observing room.
	inventory, err := scriptGenerationInventory(root, v, u)
	if err != nil {
		return nil, false, err
	}
	if expected.Inventory != inventory || ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	claimed = true
	return activeRelease, true, nil
}

func discardScriptGenerationPlay(root string, tenant, room, session int64, v model.LiveAgentPlanVersion, expected *scriptGenerationPlay) {
	if expected == nil {
		return
	}
	path := scriptGenerationPlayPath(root, tenant, room, v.PlanID)
	release, acquired, err := sharedfilelock.TryLock(path + ".lock")
	if err != nil || !acquired {
		return
	}
	defer release()
	st, err := readScriptGenerationPlays(path)
	if err == nil && st.Session == session && st.Version == v.ID && st.Pending != nil && *st.Pending == *expected {
		st.Pending = nil
		_ = writeVolcengineJSON(path, st)
	}
}
