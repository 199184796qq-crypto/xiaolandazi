package httpapi

import (
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestUnavailableVoiceReportsActualStateAndName(t *testing.T) {
	for _, tc := range []struct{ status, code, reason string }{
		{"disabled", "voice_disabled", "已停用"},
		{"pending", "voice_clone_pending", "仍在制作中"},
		{"failed", "voice_clone_failed", "制作失败"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			_, _, err := resolveSelectedCloneVoice(model.VoiceProfile{ID: 6, TenantID: 14, Name: "小芳", CloneStatus: tc.status}, fullShowVariantVoiceInput{}, nil)
			if err == nil {
				t.Fatal("unavailable voice accepted")
			}
			failure := publicLiveFailure(389, fmt.Errorf("准备开场: %w", err))
			if failure.Code != tc.code || !strings.Contains(failure.Message, "小芳") || !strings.Contains(failure.Message, tc.reason) || failure.Stage != "声音配置校验" || failure.Action == "" {
				t.Fatal(failure)
			}
			var snapshot model.LiveRuntimeSnapshot
			snapshot.Session = &model.LiveRuntimeSession{ID: 389, Status: "stopped", StopReason: "formal_mainline_failed"}
			applyLiveRuntimeFailure(&snapshot, &failure)
			if snapshot.Failure == nil || snapshot.Failure.Code != tc.code || snapshot.Failure.Stage != failure.Stage || snapshot.Failure.Action != failure.Action {
				t.Fatal(snapshot)
			}
		})
	}
}

func TestDisabledSelectedBindingReportsBindingProblem(t *testing.T) {
	p := model.VoiceProfile{ID: 6, TenantID: 14, Name: "小芳", CloneStatus: "ready", VoiceID: "old-voice"}
	b := model.VoiceModelBinding{ID: 11, ProfileID: 6, TenantID: 14, Status: "disabled", VoiceID: "selected-voice"}
	_, _, err := resolveSelectedCloneVoice(p, fullShowVariantVoiceInput{BindingID: 11, VoiceID: b.VoiceID}, []model.VoiceModelBinding{b})
	if err == nil || !strings.Contains(err.Error(), "模型绑定已停用") {
		t.Fatal(err)
	}
}
