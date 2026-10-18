package agentgateway

import (
	"strings"
	"testing"
)

func TestQwenProviderErrorPreservesCodeAndRequestID(t *testing.T) {
	body := []byte(`{"code":"data_inspection_failed","message":"Input text data may contain inappropriate content.","request_id":"req-abc123"}`)
	err := qwenProviderError(ProviderQwen, 400, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"http 400",
		"data_inspection_failed",
		"Input text data may contain inappropriate content",
		"req-abc123",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}

func TestQwenProviderErrorDoesNotEchoNonJSONBody(t *testing.T) {
	// 非 JSON 响应体（可能是任意/敏感内容）不得回显，只保留状态码。
	body := []byte("fixture-only-secret-raw-body")
	err := qwenProviderError(ProviderQwen, 401, body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "fixture-only-secret-raw-body") {
		t.Errorf("non-JSON body leaked into error: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "http 401") {
		t.Errorf("error missing status code: %s", err.Error())
	}
}

func TestQwenProviderErrorMissingOptionalFields(t *testing.T) {
	// 只有 code，没有 message/request_id 时不应 panic，也不带多余空字段。
	err := qwenProviderError(ProviderQwen, 429, []byte(`{"code":"Throttling.RateQuota"}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "http 429") || !strings.Contains(msg, "Throttling.RateQuota") {
		t.Errorf("unexpected error: %s", msg)
	}
	if strings.Contains(msg, "request_id=") {
		t.Errorf("should not append request_id when absent: %s", msg)
	}
}
