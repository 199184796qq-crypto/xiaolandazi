package ttsscheduler

import "testing"

func TestVoicePoolIsolation(t *testing.T) {
	for _, tc := range []struct {
		pool, provider, model string
		valid                 bool
	}{
		{"standard", "aliyun_qwen", "qwen-audio-3.1-tts-flash", true},
		{"standard", "cosyvoice", "cosyvoice-v3.5-flash", true},
		{"high_fidelity", "volcengine", "seed-icl-2.0", true},
		{"high_fidelity", "aliyun_qwen", "seed-icl-2.0", false},
		{"standard", "volcengine", "qwen-audio-3.1-tts-flash", false},
		{"high_fidelity", "volcengine", "cosyvoice-v3.5-flash", false},
		{"standard", "tencent_tts", "tencent-premium", false},
		{"standard", "aliyun", "tencent-premium", false},
		{"standard", "", "qwen-audio-3.1-tts-flash", false},
		{"unknown", "aliyun", "qwen-audio-3.1-tts-flash", false},
		{"", "tencent_tts", "tencent-premium", true},
	} {
		t.Run(tc.pool+"/"+tc.provider+"/"+tc.model, func(t *testing.T) {
			if err := ValidateVoicePool(tc.pool, tc.provider, tc.model); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	if PoolForProvider("tencent_tts") != "" || PoolForProvider("aliyun_qwen_clone") != PoolStandard || PoolForProvider("doubao") != PoolHighFidelity {
		t.Fatal("historical provider or aliases misrouted")
	}
}
