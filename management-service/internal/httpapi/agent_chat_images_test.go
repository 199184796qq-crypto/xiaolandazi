package httpapi

import "testing"

func TestSanitizeAgentChatImageURLs(t *testing.T) {
	valid := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z9uQAAAAASUVORK5CYII="
	got, err := sanitizeAgentChatImageURLs([]string{valid})
	if err != nil {
		t.Fatalf("valid image rejected: %v", err)
	}
	if len(got) != 1 || got[0] != valid {
		t.Fatalf("unexpected sanitized images: %#v", got)
	}

	if _, err := sanitizeAgentChatImageURLs([]string{"https://example.com/image.png"}); err == nil {
		t.Fatal("remote URL must not be accepted by chat image sanitizer")
	}

	tooMany := []string{valid, valid, valid, valid, valid}
	if _, err := sanitizeAgentChatImageURLs(tooMany); err == nil {
		t.Fatal("more than four images must be rejected")
	}
}
