package recordingcache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func activeTestManifest(session int64, owner string) *Manifest {
	return &Manifest{Version: 3, VoiceKey: "voice", ScriptHash: "script", CompatibilityKey: "compatible",
		SessionID: session, OwnerToken: owner, PlanVersionID: 211, LegacyBootstrapKey: "legacy-proof",
		Tabs:    []SourceTab{{ID: "stable-tab", Sources: []string{"原句。"}, UnitKeys: []string{"unit"}, ContextKeys: []string{"paragraph"}}},
		Entries: []Entry{{UnitKey: "unit", ContextKey: "paragraph", Sentence: "原句。", Spoken: "原句。", AssetID: 123}},
	}
}

func activeTestScope(t *testing.T) Scope {
	t.Helper()
	scope, err := ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestActiveManifestOwnershipFencesAndDurableSourceSnapshots(t *testing.T) {
	scope := activeTestScope(t)
	if manifest, err := scope.LoadActive(); err != nil || manifest != nil {
		t.Fatal(manifest, err)
	}
	first := activeTestManifest(479, "owner-old")
	if saved, err := scope.SaveActive(first); err != nil || saved {
		t.Fatal("save bootstrapped without a claim", saved, err)
	}
	if claimed, err := scope.ClaimActive("missing-owner", first); err != nil || claimed {
		t.Fatal(claimed, err)
	}
	if claimed, err := scope.ClaimActive("", first); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	loaded, err := scope.LoadActive()
	if err != nil || !reflect.DeepEqual(loaded, first) {
		t.Fatal("source/paragraph/legacy evidence lost", loaded, err)
	}
	next := activeTestManifest(480, "owner-new")
	if claimed, err := scope.ClaimActive("wrong-owner", next); err != nil || claimed {
		t.Fatal("claim ignored its observed owner", claimed, err)
	}
	if claimed, err := scope.ClaimActive(first.OwnerToken, next); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if saved, err := scope.SaveActive(first); err != nil || saved {
		t.Fatal("old session revived its manifest", saved, err)
	}
	// A new read of the real owner must still not let an older session steal it.
	if claimed, err := scope.ClaimActive(next.OwnerToken, first); err != nil || claimed {
		t.Fatal("smaller session replaced a newer session", claimed, err)
	}
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.SessionID-- },
		func(m *Manifest) { m.OwnerToken = "other-owner" },
		func(m *Manifest) { m.VoiceKey = "changed-voice" },
		func(m *Manifest) { m.ScriptHash = "changed-script" },
		func(m *Manifest) { m.CompatibilityKey = "changed-facts" },
	} {
		changed := *next
		mutate(&changed)
		if saved, err := scope.SaveActive(&changed); err != nil || saved {
			t.Fatal("save changed an ownership/content identity", saved, err)
		}
	}
	next.Entries[0].Hits = 7
	if saved, err := scope.SaveActive(next); err != nil || !saved {
		t.Fatal("current owner cannot persist its result", saved, err)
	}
}

func TestActiveManifestCorruptionIsNeverBootstrapPermission(t *testing.T) {
	for _, content := range []string{"{", "null", `{"version":2}`, `{"version":3}`} {
		scope := activeTestScope(t)
		if err := os.MkdirAll(filepath.Dir(scope.activePath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scope.activePath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := scope.LoadActive(); err == nil {
			t.Fatal("corrupt active file was interpreted as absent", content)
		}
		if claimed, err := scope.ClaimActive("", activeTestManifest(480, "owner")); err == nil || claimed {
			t.Fatal("corrupt active file was overwritten by bootstrap", content, claimed, err)
		}
		if saved, err := scope.SaveActive(activeTestManifest(480, "owner")); err == nil || saved {
			t.Fatal("corrupt active file was overwritten by save", content, saved, err)
		}
	}
}

func TestActiveManifestExplicitClearInvalidatesOwnerWithoutDeletingMedia(t *testing.T) {
	for _, which := range []string{"full", "voice", "script", "unrelated-voice", "unrelated-script"} {
		t.Run(which, func(t *testing.T) {
			scope := activeTestScope(t)
			manifest := activeTestManifest(479, "owner")
			if claimed, err := scope.ClaimActive("", manifest); err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			media := filepath.Join(scope.Root, "generated_voice", "in-use.wav")
			if err := os.MkdirAll(filepath.Dir(media), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(media, []byte("currently referenced audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			voice, script := "", ""
			switch which {
			case "voice":
				voice = manifest.VoiceKey
			case "script":
				voice, script = manifest.VoiceKey, manifest.ScriptHash
			case "unrelated-voice":
				voice = "other"
			case "unrelated-script":
				voice, script = manifest.VoiceKey, "other"
			}
			if err := scope.RemoveCache(voice, script); err != nil {
				t.Fatal(err)
			}
			saved, err := scope.SaveActive(manifest)
			unrelated := strings.HasPrefix(which, "unrelated-")
			if err != nil || saved != unrelated {
				t.Fatal("explicit clear was undone or affected an unrelated active scope", saved, err)
			}
			if body, err := os.ReadFile(media); err != nil || string(body) != "currently referenced audio" {
				t.Fatal("cache clear removed in-use generated media", string(body), err)
			}
			if !unrelated {
				if claimed, err := scope.ClaimActive(manifest.OwnerToken, manifest); err != nil || claimed {
					t.Fatal("old expected owner reclaimed cleared cache", claimed, err)
				}
			}
		})
	}
}

func TestActiveManifestAtomicReadWriteAndFailedEncodingKeepPriorFile(t *testing.T) {
	scope := activeTestScope(t)
	manifest := activeTestManifest(479, "owner")
	if claimed, err := scope.ClaimActive("", manifest); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	broken := *manifest
	broken.UpdateState = json.RawMessage("{")
	if saved, err := scope.SaveActive(&broken); err == nil || saved {
		t.Fatal("invalid payload replaced valid active file", saved, err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for reader := 0; reader < 2; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 40; n++ {
				loaded, err := scope.LoadActive()
				if err != nil || loaded == nil || loaded.OwnerToken != "owner" || loaded.Entries[0].Sentence != "原句。" {
					failures <- fmt.Errorf("partial active file: manifest=%+v error=%v", loaded, err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 40; n++ {
			manifest.Entries[0].Hits = n
			if saved, err := scope.SaveActive(manifest); err != nil || !saved {
				failures <- fmt.Errorf("save: saved=%v error=%v", saved, err)
				return
			}
		}
	}()
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	temporaries, err := filepath.Glob(filepath.Join(filepath.Dir(scope.activePath()), ".active-*.tmp"))
	if err != nil || len(temporaries) != 0 {
		t.Fatal("atomic write leaked temporary files", temporaries, err)
	}
}

func TestActiveManifestScopesRemainIsolated(t *testing.T) {
	root := t.TempDir()
	var scopes []Scope
	for _, ids := range [][3]int64{{14, 29, 5}, {15, 29, 5}, {14, 30, 5}, {14, 29, 6}} {
		scope, err := ScopeFor(root, ids[0], ids[1], ids[2])
		if err != nil {
			t.Fatal(err)
		}
		if claimed, err := scope.ClaimActive("", activeTestManifest(479, "same-owner")); err != nil || !claimed {
			t.Fatal(claimed, err)
		}
		scopes = append(scopes, scope)
	}
	if err := scopes[0].RemoveCache("", ""); err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes[1:] {
		if loaded, err := scope.LoadActive(); err != nil || loaded == nil || loaded.OwnerToken != "same-owner" {
			t.Fatal("clear crossed tenant/room/plan scope", loaded, err)
		}
	}
}

// Separate test processes exercise OS ownership, not an in-memory mutex.
func TestActiveManifestProcessHelper(t *testing.T) {
	action := os.Getenv("RECORDING_ACTIVE_HELPER_ACTION")
	if action == "" {
		t.Skip("subprocess helper")
	}
	scope := Scope{Root: os.Getenv("RECORDING_ACTIVE_HELPER_ROOT")}
	switch action {
	case "claim":
		for {
			if _, err := os.Stat(filepath.Join(scope.Root, "start")); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		owner := os.Getenv("RECORDING_ACTIVE_HELPER_OWNER")
		ok, err := scope.ClaimActive("base-owner", activeTestManifest(480, owner))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scope.Root, owner+".result"), []byte(fmt.Sprint(ok)), 0o600); err != nil {
			t.Fatal(err)
		}
	case "hold":
		unlock, err := scope.lockActiveIndex()
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if err := os.WriteFile(filepath.Join(scope.Root, "held"), []byte("locked"), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		t.Fatal("unknown subprocess action")
	}
}

func activeHelperCommand(ctx context.Context, scope Scope, action, owner string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestActiveManifestProcessHelper$")
	command.Env = append(os.Environ(), "RECORDING_ACTIVE_HELPER_ACTION="+action, "RECORDING_ACTIVE_HELPER_ROOT="+scope.Root, "RECORDING_ACTIVE_HELPER_OWNER="+owner)
	return command
}

func TestActiveManifestCrossProcessCASHasOneWinner(t *testing.T) {
	scope := activeTestScope(t)
	if claimed, err := scope.ClaimActive("", activeTestManifest(479, "base-owner")); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	commands := make([]*exec.Cmd, 6)
	for index := range commands {
		commands[index] = activeHelperCommand(ctx, scope, "claim", fmt.Sprintf("claim-%d", index))
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(scope.Root, "start"), []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	winners := 0
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal("child CAS failed", err)
		}
		result, err := os.ReadFile(filepath.Join(scope.Root, fmt.Sprintf("claim-%d.result", index)))
		if err != nil {
			t.Fatal(err)
		}
		if string(result) == "true" {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("concurrent claims did not preserve a single owner", winners)
	}
}

func TestActiveManifestOSLockReleasesAfterProcessExit(t *testing.T) {
	scope := activeTestScope(t)
	if claimed, err := scope.ClaimActive("", activeTestManifest(479, "owner")); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := activeHelperCommand(ctx, scope, "hold", "")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(scope.Root, "held")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not acquire OS lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	finished := make(chan error, 1)
	go func() {
		_, err := scope.SaveActive(activeTestManifest(479, "owner"))
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dead process left a permanent index lock")
	}
}

func TestActiveManifestLegacyBarrierPreventsReimportAfterTargetedClear(t *testing.T) {
	scope := activeTestScope(t)
	first := activeTestManifest(479, "owner-a")
	legacy := *first
	legacy.Version = 2
	if err := scope.Save(&legacy); err != nil {
		t.Fatal(err)
	}
	if allowed, err := scope.LegacyBootstrapAllowed(); err != nil || !allowed {
		t.Fatal("new scope could not perform its initial exact V2 import", allowed, err)
	}
	if claimed, err := scope.ClaimActive("", first); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	next := activeTestManifest(480, "owner-b")
	next.ScriptHash = "edited-script-b"
	next.Entries = nil
	if claimed, err := scope.ClaimActive(first.OwnerToken, next); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := scope.RemoveCache(next.VoiceKey, next.ScriptHash); err != nil {
		t.Fatal(err)
	}
	if active, err := scope.LoadActive(); err != nil || active != nil {
		t.Fatal("fixture must remove B's active pointer", active, err)
	}
	if old, err := scope.Load(first.VoiceKey, first.ScriptHash); err != nil || old == nil || old.Version != 2 || old.Entries[0].AssetID != 123 {
		t.Fatal("fixture must retain historical V2 A and its old audio reference", old, err)
	}
	// A reconstructed Scope represents another process after editing B back
	// to A. The exact V2 namespace still exists but is no longer admissible.
	restarted := Scope{Root: scope.Root, TenantID: 14, RoomID: 29, PlanID: 5}
	if allowed, err := restarted.LegacyBootstrapAllowed(); err != nil || allowed {
		t.Fatal("A->B->targeted clear->A reopened invalidated historical import", allowed, err)
	}
	if err := scope.RemoveCache("", ""); err != nil {
		t.Fatal(err)
	}
	if allowed, err := restarted.LegacyBootstrapAllowed(); err != nil || allowed {
		t.Fatal("complete cache clear removed the persistent history barrier", allowed, err)
	}
	if err := scope.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(scope.activeHistoryPath()); !os.IsNotExist(err) {
		t.Fatal("explicit whole-plan deletion did not remove its history barrier", err)
	}
}

func TestActiveManifestLegacyBarrierErrorsFailClosed(t *testing.T) {
	for _, content := range []string{"", "wrong-history", "directory"} {
		t.Run(content, func(t *testing.T) {
			scope := activeTestScope(t)
			if err := os.MkdirAll(scope.Root, 0o755); err != nil {
				t.Fatal(err)
			}
			var err error
			if content == "directory" {
				err = os.Mkdir(scope.activeHistoryPath(), 0o755)
			} else {
				err = os.WriteFile(scope.activeHistoryPath(), []byte(content), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if allowed, err := scope.LegacyBootstrapAllowed(); err == nil || allowed {
				t.Fatal("damaged/unreadable marker became permission to import V2", allowed, err)
			}
			if claimed, err := scope.ClaimActive("", activeTestManifest(479, "owner")); err == nil || claimed {
				t.Fatal("claim silently replaced invalid history evidence", claimed, err)
			}
		})
	}
}

func TestActiveManifestLegacyBarrierPrecedesFailedActiveWrite(t *testing.T) {
	scope := activeTestScope(t)
	manifest := activeTestManifest(479, "owner")
	manifest.UpdateState = json.RawMessage("{")
	if claimed, err := scope.ClaimActive("", manifest); err == nil || claimed {
		t.Fatal("invalid active payload was accepted", claimed, err)
	}
	if active, err := scope.LoadActive(); err != nil || active != nil {
		t.Fatal("invalid active payload wrote a partial active file", active, err)
	}
	if allowed, err := scope.LegacyBootstrapAllowed(); err != nil || allowed {
		t.Fatal("failed active write reopened historical import after ownership was attempted", allowed, err)
	}
}

func TestActiveManifestLegacyReadBeforeConcurrentClaimAndClearCannotBeImported(t *testing.T) {
	scope := activeTestScope(t)
	oldAttempt := activeTestManifest(479, "old-reader")
	legacy := *oldAttempt
	legacy.Version = 2
	if err := scope.Save(&legacy); err != nil {
		t.Fatal(err)
	}
	// The old opener read A while the scope still had no V3 history.
	if allowed, err := scope.LegacyBootstrapAllowed(); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	readBeforeClaim, err := scope.Load(legacy.VoiceKey, legacy.ScriptHash)
	if err != nil || readBeforeClaim == nil || len(readBeforeClaim.Entries) != 1 {
		t.Fatal(readBeforeClaim, err)
	}
	// During that opener's inheritance work another producer claimed B,
	// invalidated A, and then an explicit scoped clear removed B's pointer.
	concurrent := activeTestManifest(480, "new-owner")
	concurrent.ScriptHash = "script-b"
	concurrent.Entries = nil
	if claimed, err := scope.ClaimActive("", concurrent); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := scope.RemoveCache(concurrent.VoiceKey, concurrent.ScriptHash); err != nil {
		t.Fatal(err)
	}
	if claimed, err := scope.ClaimActive("", oldAttempt); err != nil || claimed {
		t.Fatal("old V2 read slipped through an absent active pointer", claimed, err)
	}
	// A legitimate next producer remains able to rebuild the cleared scope;
	// the barrier forbids historical entries, not a new empty cold index.
	fresh := activeTestManifest(481, "fresh-owner")
	fresh.Entries = nil
	if claimed, err := scope.ClaimActive("", fresh); err != nil || !claimed {
		t.Fatal("history barrier permanently blocked a valid cold rebuild", claimed, err)
	}
	loaded, err := scope.LoadActive()
	if err != nil || loaded == nil || loaded.OwnerToken != fresh.OwnerToken || len(loaded.Entries) != 0 {
		t.Fatal(loaded, err)
	}
}
