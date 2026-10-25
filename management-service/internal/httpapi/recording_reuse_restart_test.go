package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

const recordingRestartFixtureSource = "欢迎进来看看，有问题可以直接问。"

type recordingRestartEvidence struct {
	PID            int                                        `json:"pid"`
	ModelCalls     int                                        `json:"model_calls"`
	TTSCalls       int                                        `json:"tts_calls"`
	Generated      bool                                       `json:"generated"`
	CacheHit       bool                                       `json:"cache_hit"`
	AssetID        int64                                      `json:"asset_id"`
	Text           string                                     `json:"text"`
	DurationMS     int                                        `json:"duration_ms"`
	Timeline       []coreclient.ProgramRefreshTimelineSegment `json:"timeline"`
	AudioSHA       string                                     `json:"audio_sha256"`
	UpdateSelected bool                                       `json:"short_tab_update_selected"`
	UpdateCredit   int64                                      `json:"short_tab_credit_micros"`
}

func recordingRestartVersion(facts string) model.LiveAgentPlanVersion {
	return model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Source: "official", VoiceID: "fixture-voice", Model: "fixture-tts", Rate: 1}, GenerationContext: map[string]any{
		"derived_safety": map[string]any{"source_sha256": facts},
	}}
}

// This uses two OS child processes, not a second call retaining the producer's
// memory. The paid providers and asset URL signer are explicit local stubs;
// resolveRecordingReuse, the original P0 check, cache entry conversion, and
// Scope.Save/Load are the real production functions. It does not claim a real
// provider bill, database signer or physical playback acceptance.
func TestRecordingReuseAcrossProcessRestartMakesZeroPaidCalls(t *testing.T) {
	root := t.TempDir()
	for _, phase := range []string{"write", "read"} {
		command := exec.Command(os.Args[0], "-test.run=^TestRecordingReuseRestartChild$", "-test.count=1", "-test.v")
		command.Env = append(os.Environ(), "RECORDING_REUSE_ACCEPTANCE_PHASE="+phase, "RECORDING_REUSE_ACCEPTANCE_ROOT="+root)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s child failed: %v\n%s", phase, err, output)
		}
	}
	var written, reloaded recordingRestartEvidence
	for phase, target := range map[string]*recordingRestartEvidence{"write": &written, "read": &reloaded} {
		raw, err := os.ReadFile(filepath.Join(root, phase+".json"))
		if err != nil || json.Unmarshal(raw, target) != nil {
			t.Fatalf("missing %s evidence: %v", phase, err)
		}
	}
	if written.PID == reloaded.PID || written.PID == os.Getpid() || reloaded.PID == os.Getpid() {
		t.Fatalf("not independent fresh OS processes: parent=%d write=%d read=%d", os.Getpid(), written.PID, reloaded.PID)
	}
	if written.ModelCalls != 0 || written.TTSCalls != 1 || !written.Generated || written.CacheHit ||
		reloaded.ModelCalls != 0 || reloaded.TTSCalls != 0 || reloaded.Generated || !reloaded.CacheHit {
		t.Fatalf("paid work was repeated across restart: written=%+v reloaded=%+v", written, reloaded)
	}
	if written.AssetID != reloaded.AssetID || written.Text != reloaded.Text || written.DurationMS != reloaded.DurationMS ||
		written.AudioSHA != reloaded.AudioSHA || !reflect.DeepEqual(written.Timeline, reloaded.Timeline) {
		t.Fatalf("cached audio/text/timeline did not preserve the generated bundle: written=%+v reloaded=%+v", written, reloaded)
	}
	if written.UpdateSelected || !reloaded.UpdateSelected || written.UpdateCredit <= 0 ||
		reloaded.UpdateCredit != int64(len([]rune(recordingRestartFixtureSource)))*recordingUpdateCreditScale {
		t.Fatalf("fractional short-tab update credit was lost across process/JSON/reordering: written=%+v reloaded=%+v", written, reloaded)
	}
	t.Logf("independent processes writer=%d(model=0,TTS=1), reader=%d(model=0,TTS=0); original asset=%d, text/timeline/duration/audioSHA unchanged", written.PID, reloaded.PID, reloaded.AssetID)
}

func TestRecordingReuseRestartChild(t *testing.T) {
	phase := os.Getenv("RECORDING_REUSE_ACCEPTANCE_PHASE")
	if phase == "" {
		t.Skip("only run by the parent cross-process acceptance test")
	}
	root := os.Getenv("RECORDING_REUSE_ACCEPTANCE_ROOT")
	if root == "" || (phase != "write" && phase != "read") {
		t.Fatal("invalid child fixture scope")
	}
	scope, err := recordingcache.ScopeFor(root, 900014, 900029, 900005)
	if err != nil {
		t.Fatal(err)
	}
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy := &anchorcycle.State{Surface: "formal_live"}
	if err := policy.InstallP0(md, "restart-acceptance-fixture"); err != nil {
		t.Fatal(err)
	}
	voice, key := recordingCurrentCacheIdentity(recordingRestartVersion("facts-a"), []string{recordingRestartFixtureSource}, policy)
	audioPath := filepath.Join(scope.CacheDir(voice, key), recordingcache.SentenceKey(recordingRestartFixtureSource)+".wav")
	evidence := recordingRestartEvidence{PID: os.Getpid()}
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: recordingRestartFixtureSource, Refreshed: -1}}
	request := recordingReuseRequest{}
	if phase == "write" {
		for round := 1; round <= 3; round++ {
			if plan := planner.Plan("stable-tab-first", fmt.Sprintf("session-a:%d", round), states, 25, nil); plan.Selected[0] {
				t.Fatal("one-sentence tab rounded fractional credit up before fourth round")
			}
		}
		planner.Plan("stable-tab-second", "session-a:3", states, 0, nil)
		evidence.UpdateCredit = planner.Tabs["stable-tab-first"].CreditMicros
		request.Generate = func(context.Context) ([]recordingReadyUnit, error) {
			// This represents the approved original branch: local P0 check,
			// no model call, exactly one stub TTS call.
			candidate := anchorcycle.Candidate{ID: "original-fixture", Text: recordingRestartFixtureSource, OutputText: recordingRestartFixtureSource}
			if err := policy.CheckP0Candidate(&candidate); err != nil {
				return nil, err
			}
			if candidate.P0Report == nil || !candidate.P0Report.Passed {
				return nil, fmt.Errorf("原句P0未通过: %+v", candidate.P0Report)
			}
			if err := policy.CheckApprovedText(candidate, candidate.OutputText); err != nil {
				return nil, err
			}
			evidence.TTSCalls++
			// A local deterministic byte fixture, not a real TTS request.
			if _, err := scope.WriteAudio(voice, key, recordingcache.SentenceKey(recordingRestartFixtureSource), []byte("RIFF-local-synthesized-original-fixture")); err != nil {
				return nil, err
			}
			unit := recordingReuseFixtureUnit("recording-001-block-701", recordingRestartFixtureSource)
			unit.Track.AudioURL = "fixture-local://" + audioPath
			return []recordingReadyUnit{unit}, nil
		}
	} else {
		manifest, err := scope.Load(voice, key)
		if err != nil || manifest == nil || len(manifest.Entries) != 1 || manifest.Entries[0].Polished {
			t.Fatalf("original was not persisted and reloaded: %+v %v", manifest, err)
		}
		if err := json.Unmarshal(manifest.UpdateState, &planner); err != nil {
			t.Fatal(err)
		}
		// Visit the second tab first after restart. Its balance must not
		// inherit the first tab's three rounds of fractional credit.
		if plan := planner.Plan("stable-tab-second", "session-b:1", states, 25, nil); plan.Selected[0] {
			t.Fatal("reordered second tab borrowed the first tab's credit")
		}
		plan := planner.Plan("stable-tab-first", "session-b:1", states, 25, nil)
		evidence.UpdateSelected = plan.Selected[0]
		// This reader only reuses audio. Planning does not consume credit;
		// no fresh result exists here, so Commit must not be called.
		evidence.UpdateCredit = planner.Tabs["stable-tab-first"].CreditMicros
		request.Cached = recordingCacheLookup(manifest)[recordingRestartFixtureSource]
		request.BuildCache = func(_ context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
			if entry.AssetID != 701 {
				return nil, false
			}
			if _, err := os.ReadFile(audioPath); err != nil {
				return nil, false
			}
			unit := recordingReadyUnit{Text: entry.Spoken, Track: coreclient.ProgramRefreshTrack{
				ID: fmt.Sprintf("recording-cache-%d", entry.AssetID), Text: entry.Spoken,
				AudioURL: "fixture-local://" + audioPath, DurationMS: int(entry.Duration),
			}}
			for index, segment := range entry.Segments {
				unit.Track.Timeline = append(unit.Track.Timeline, coreclient.ProgramRefreshTimelineSegment{
					SegmentID: fmt.Sprintf("segment-%d", index+1), Index: index + 1, Text: segment.Text,
					StartMS: int(segment.StartMS), EndMS: int(segment.EndMS), SafeCut: segment.SafeCut,
				})
			}
			return []recordingReadyUnit{unit}, true
		}
		request.Generate = func(context.Context) ([]recordingReadyUnit, error) {
			evidence.ModelCalls++
			evidence.TTSCalls++
			return nil, errors.New("cache hit must bypass both paid stubs")
		}
	}
	result, err := resolveRecordingReuse(context.Background(), request)
	if err != nil || len(result.Units) != 1 || len(result.Versions) != 1 {
		t.Fatalf("selection failed: %+v %v", result, err)
	}
	if phase == "write" {
		entry, ok := recordingCacheEntryFromUnits(recordingRestartFixtureSource, result.Units)
		if !ok {
			t.Fatal("generated original cannot be cached")
		}
		entry.Polished = false
		updateState, err := json.Marshal(planner)
		if err != nil {
			t.Fatal(err)
		}
		if err := scope.Save(&recordingcache.Manifest{Version: 2, VoiceKey: voice, ScriptHash: key, Entries: []recordingcache.Entry{entry}, UpdateState: updateState}); err != nil {
			t.Fatal(err)
		}
	}
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(audio)
	evidence.AudioSHA = hex.EncodeToString(hash[:])
	evidence.AssetID = recordingAssetIDFromTrack(result.Units[0].Track.ID)
	evidence.Generated, evidence.CacheHit = result.Generated, result.CacheHit
	evidence.Text, evidence.DurationMS = result.Units[0].Text, result.Units[0].Track.DurationMS
	evidence.Timeline = result.Units[0].Track.Timeline
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, phase+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingReuseChangedDependenciesMissWithoutLegacyFallback(t *testing.T) {
	root := t.TempDir()
	scope, _ := recordingcache.ScopeFor(root, 900014, 900029, 900005)
	policy := &anchorcycle.State{P0PolicyMD: "fixture-policy"}
	voice, key := recordingCurrentCacheIdentity(recordingRestartVersion("facts-a"), []string{recordingRestartFixtureSource}, policy)
	legacy := recordingcache.ScriptHash([]string{recordingRestartFixtureSource})
	entry := recordingcache.Entry{Sentence: recordingRestartFixtureSource, AssetID: 701, Spoken: recordingRestartFixtureSource,
		Duration: 1000, Segments: []recordingcache.Segment{{Text: recordingRestartFixtureSource, EndMS: 1000, SafeCut: true}}}
	for _, storedKey := range []string{key, legacy} {
		if err := scope.Save(&recordingcache.Manifest{Version: 2, VoiceKey: voice, ScriptHash: storedKey, Entries: []recordingcache.Entry{entry}}); err != nil {
			t.Fatal(err)
		}
	}
	otherScope, _ := recordingcache.ScopeFor(root, 900014, 900029, 900006)
	_, changedFacts := recordingCurrentCacheIdentity(recordingRestartVersion("facts-b"), []string{recordingRestartFixtureSource}, policy)
	changedVoiceVersion := recordingRestartVersion("facts-a")
	changedVoiceVersion.VoiceIdentity.VoiceID = "different-voice"
	changedVoice, _ := recordingCurrentCacheIdentity(changedVoiceVersion, []string{recordingRestartFixtureSource}, policy)
	for _, check := range []struct {
		name  string
		scope recordingcache.Scope
		voice string
		key   string
	}{
		{"facts", scope, voice, changedFacts},
		{"voice", scope, changedVoice, key},
		{"ownership", otherScope, voice, key},
	} {
		t.Run(check.name, func(t *testing.T) {
			loaded, err := check.scope.Load(check.voice, check.key)
			if err != nil || loaded != nil {
				t.Fatalf("changed dependency loaded old audio: %+v %v", loaded, err)
			}
			generated := 0
			wantErr := errors.New("new namespace has no funded audio yet")
			result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
				Generate: func(context.Context) ([]recordingReadyUnit, error) { generated++; return nil, wantErr },
			})
			if !errors.Is(err, wantErr) || result.CacheHit || len(result.Units) != 0 || generated != 1 {
				t.Fatal("legacy assets escaped their namespace", result, generated, err)
			}
		})
	}
}
