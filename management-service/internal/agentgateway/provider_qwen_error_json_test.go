package agentgateway

import (
	"strings"
	"testing"
)

func TestQwenProviderErrorRetainsCompatibleNestedInspectionFailure(t *testing.T) {
	// Same envelope shape as session311's captured upstream response.
	body := []byte(`{"error":{"message":"<400> InternalError.Algo.DataInspectionFailed: Input text data may contain inappropriate content.","type":"data_inspection_failed","param":null,"code":"data_inspection_failed"},"request_id":"captured-request","request":{"api_key":"never-echo-this","prompt":"private-prompt"}}`)
	message := qwenProviderError("qwen", 400, body).Error()
	for _, wanted := range []string{"http 400", "code=data_inspection_failed", "Input text data", "request_id=captured-request"} {
		if !strings.Contains(message, wanted) {
			t.Fatal("lost upstream diagnostic", wanted, message)
		}
	}
	for _, unwanted := range []string{"never-echo-this", "private-prompt"} {
		if strings.Contains(message, unwanted) {
			t.Fatal("arbitrary response data escaped diagnostic allowlist", message)
		}
	}
}

func TestQwenProviderErrorBoundsNestedMessage(t *testing.T) {
	message := qwenProviderError("qwen", 400, []byte(`{"error":{"message":"`+strings.Repeat("x", 9000)+`","code":"bad_request"}}`)).Error()
	if len(message) > 2500 || !strings.Contains(message, "code=bad_request") {
		t.Fatal("unbounded upstream diagnostic", len(message))
	}
}
