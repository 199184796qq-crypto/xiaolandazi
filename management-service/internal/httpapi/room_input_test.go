package httpapi

import (
	"net/url"
	"testing"
)

func TestNormalizeDouyinRoomInputRoomID(t *testing.T) {
	roomID, sourceURL, err := normalizeDouyinRoomInput("698128112856")
	if err != nil {
		t.Fatalf("normalize error: %v", err)
	}
	if roomID != "698128112856" {
		t.Fatalf("room id = %q", roomID)
	}
	if sourceURL != "https://live.douyin.com/698128112856?from=web_code_link" {
		t.Fatalf("source url = %q", sourceURL)
	}
}

func TestNormalizeDouyinRoomInputShareURL(t *testing.T) {
	input := "https://live.douyin.com/698128112856?enter_from_merge=link_share&enter_method=copy_link_share&action_type=click&from=web_code_link"

	roomID, sourceURL, err := normalizeDouyinRoomInput(input)
	if err != nil {
		t.Fatalf("normalize error: %v", err)
	}
	if roomID != "698128112856" {
		t.Fatalf("room id = %q", roomID)
	}

	parsed, err := url.Parse(sourceURL)
	if err != nil {
		t.Fatalf("parse normalized url: %v", err)
	}
	if parsed.Hostname() != "live.douyin.com" {
		t.Fatalf("host = %q", parsed.Hostname())
	}
	if parsed.Path != "/698128112856" {
		t.Fatalf("path = %q", parsed.Path)
	}
	if parsed.Query().Get("from") != "web_code_link" {
		t.Fatalf("from = %q", parsed.Query().Get("from"))
	}
	if parsed.Query().Get("enter_from_merge") != "link_share" {
		t.Fatalf("enter_from_merge = %q", parsed.Query().Get("enter_from_merge"))
	}
}
