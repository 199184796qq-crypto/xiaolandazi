package mainlineasset

import "testing"

func TestAssetRequiresSemanticMap(t *testing.T) {
	asset := Asset{ID: "a", Role: RoleMainline, AudioURI: "file.wav", DurationMS: 10000}
	if err := asset.ValidateSchedulable(); err == nil {
		t.Fatal("mainline asset without semantic units must be rejected")
	}
	asset.Units = []SemanticUnit{{
		ID: "u1", Text: "hello", StartMS: 0, EndMS: 10000,
		SafeExitPoints: []SafePoint{{ID: "x", AtMS: 9000, Score: 90, Grade: GradeA, CanExit: true}},
	}}
	if err := asset.ValidateSchedulable(); err != nil {
		t.Fatal(err)
	}
	point, ok := asset.BestExitAfter(1000, GradeA)
	if !ok || point.AtMS != 9000 {
		t.Fatalf("point=%#v ok=%v", point, ok)
	}
}

func TestCacheIdentityStable(t *testing.T) {
	a := CacheIdentity{Text: "  hello   world ", VoiceID: "v", TTSProvider: "q", TTSModel: "m"}.Key()
	b := CacheIdentity{Text: "hello world", VoiceID: "v", TTSProvider: "q", TTSModel: "m"}.Key()
	if a != b {
		t.Fatal("normalized text should share cache key")
	}
}
