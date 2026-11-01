package ttsgateway

import (
	"encoding/json"
	"errors"
	"strings"
)

// ConcurrencyLimitError means an explicit supplier rejection, not a timeout
// after possible acceptance. Only the former permits a fresh submit attempt.
type ConcurrencyLimitError struct{}

func (*ConcurrencyLimitError) Error() string {
	return "火山合成并发已满，等待本地账号池后重试"
}
func IsConcurrencyLimit(err error) bool {
	var target *ConcurrencyLimitError
	return errors.As(err, &target)
}

func volcengineConcurrencyRejection(raw []byte) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	for _, key := range []string{"message", "msg", "error"} {
		var message string
		if json.Unmarshal(envelope[key], &message) == nil {
			message = strings.ToLower(message)
			if strings.Contains(message, "quota exceeded for types: concurrency") || strings.Contains(message, "concurrency limit exceeded") {
				return true
			}
		}
	}
	return false
}
