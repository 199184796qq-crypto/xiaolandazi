package errordetail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDescribePreservesCauseAndProviderEvidence(t *testing.T) {
	err := errors.New("qwen provider http 400 code=data_inspection_failed: Input text data rejected (request_id=req-real-123)")
	d := Describe(err, "主线运行")
	if d.Code != "provider_content_rejected" || d.Stage != "模型调用" || d.HTTPStatus != 400 || d.RequestID != "req-real-123" || !strings.Contains(d.Message, "Input text data rejected") {
		t.Fatalf("lost actual provider reason: %+v", d)
	}
	d = Describe(fmt.Errorf("TTS合成: %w", context.DeadlineExceeded), "主线运行")
	if d.Code != "request_timeout" || d.Stage != "声音合成" {
		t.Fatal(d)
	}
	d = Describe(errors.New("unexpected worker exit: channel closed"), "主线运行")
	if !strings.Contains(d.Message, "channel closed") {
		t.Fatal(d)
	}
}

func TestTypedErrorSurvivesWrappingWithoutLosingCause(t *testing.T) {
	cause := errors.New("specific failure")
	err := New("voice_disabled", "声音配置校验", "小芳声音已停用", "重新选择声音", cause)
	err = fmt.Errorf("准备开场: %w", err)
	if !errors.Is(err, cause) {
		t.Fatal("cause lost")
	}
	d := Describe(err, "主线运行")
	if d.Code != "voice_disabled" || d.Stage != "声音配置校验" || !strings.Contains(d.Message, cause.Error()) || d.Action != "重新选择声音" {
		t.Fatal(d)
	}
}

func TestRecordedOperationWinsKeywordInferenceAndKeepsSpecificVoiceError(t *testing.T) {
	err := WithStage("主线文字生成", errors.New("模型提到声音事实时请求超时: deadline exceeded"))
	d := Describe(err, "主线运行")
	if d.Stage != "主线文字生成" || d.Code != "request_timeout" {
		t.Fatal(d)
	}
	voice := New("voice_disabled", "声音配置校验", "声音已停用", "重新选择声音", nil)
	if d = Describe(WithStage("方案初始化", voice), "主线运行"); d.Code != "voice_disabled" || d.Stage != "声音配置校验" {
		t.Fatal(d)
	}
}

func TestSafeDiagnosticRedactsSecretsButRetainsReason(t *testing.T) {
	for _, original := range []string{
		`provider http 403 code=denied api_key="secret value" request_id=req-1`,
		`Authorization: Bearer abcdefghijklmnopqrstuvwxyz token=secret-token permission denied`,
		`download https://example.test/a.wav?Signature=secret-signature failed: not found`,
		`database user:secret-password@tcp(127.0.0.1:3306)/db refused`,
		`provider denied {"error":"denied","request":{"api_key":"nested-secret","messages":[{"content":"private prompt"}]}}`,
		`api_key='two words secret' code=401`,
	} {
		got := Safe(original)
		for _, secret := range []string{"secret value", "abcdefghijklmnopqrstuvwxyz", "secret-token", "secret-signature", "example.test", "secret-password", "nested-secret", "private prompt", "two words secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("leaked %q in %q", secret, got)
			}
		}
		if got == "" {
			t.Fatal("empty diagnostic")
		}
	}
	if got := Safe(strings.Repeat("错", 2000)); len([]rune(got)) > 1300 || !strings.HasSuffix(got, "（详情见后台日志）") {
		t.Fatal("bad diagnostic bound")
	}
}
