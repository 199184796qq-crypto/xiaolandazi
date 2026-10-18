package agentgateway

import (
	"encoding/json"
	"strings"
)

// DashScope's compatible endpoint nests code/message under error, while its
// native endpoint uses the top level. Decode only diagnostic fields in either
// shape, never arbitrary response data or an echoed request.
func (e *qwenErrorResponse) UnmarshalJSON(data []byte) error {
	type detail struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Type      string `json:"type"`
	}
	var wire struct {
		detail
		Error *detail `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	e.Code, e.Message, e.RequestID = wire.Code, wire.Message, wire.RequestID
	if wire.Error != nil {
		if e.Code == "" {
			e.Code = wire.Error.Code
			if e.Code == "" {
				e.Code = wire.Error.Type
			}
		}
		if e.Message == "" {
			e.Message = wire.Error.Message
		}
		if e.RequestID == "" {
			e.RequestID = wire.Error.RequestID
		}
	}
	e.Code = boundedProviderDiagnostic(e.Code, 160)
	e.Message = boundedProviderDiagnostic(e.Message, 2048)
	e.RequestID = boundedProviderDiagnostic(e.RequestID, 160)
	return nil
}

func boundedProviderDiagnostic(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return value
}
