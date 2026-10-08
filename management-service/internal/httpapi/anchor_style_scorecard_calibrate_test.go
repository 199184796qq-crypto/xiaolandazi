package httpapi

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestLegacyScorecardRecoversOriginalSampleAndChecksHash(t *testing.T) {
	sample := "原始主播全文啊！我们家哟。"
	asset := model.LiveAnchorStyleScorecardAsset{
		SampleSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(sample))),
		RequestUser:  renderAnchorStyleScorecardRequest("旧MD", "old.md", sample),
	}
	found, err := originalScorecardSample(asset, "")
	if err != nil || found != sample {
		t.Fatalf("recover=%q error=%v", found, err)
	}
	if _, err := originalScorecardSample(asset, "另一份样本"); err == nil {
		t.Fatal("wrong sample accepted")
	}
	asset.SampleText = sample
	found, err = originalScorecardSample(asset, "另一份样本")
	if err != nil || found != sample {
		t.Fatal("immutable saved source was not preferred")
	}
}

func TestQualitativeRequestDoesNotDelegateCountingToModel(t *testing.T) {
	prompt, source, err := loadAnchorStyleScorecardPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "qualitative-extraction.md") {
		t.Fatalf("old numeric prompt selected: %s", source)
	}
	rendered := renderAnchorStyleScorecardRequest(prompt, source, "这是真实样本")
	for _, expected := range []string{"禁止输出次数", "候选语气词", "原句数组", "这是真实样本"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("missing %q", expected)
		}
	}
}
