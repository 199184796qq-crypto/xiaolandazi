package storage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVoiceInventoryFlatPaginationWithoutNextMarkerAndScope(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("inventory used a mutation or audio fetch")
		}
		if r.URL.Query().Get("prefix") != "tenants/14/generated_voice/" {
			t.Error("inventory escaped tenant")
		}
		key := "tenants/14/generated_voice/a.wav"
		truncated := "true"
		if marker := r.URL.Query().Get("marker"); marker != "" {
			if marker != key {
				t.Error("wrong continuation")
			}
			key = "tenants/14/generated_voice/b.wav"
			truncated = "false"
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListBucketResult><IsTruncated>%s</IsTruncated><Contents><Key>%s</Key><Size>88</Size><LastModified>2026-11-04T00:00:00.000Z</LastModified></Contents></ListBucketResult>`, truncated, key)
	}))
	defer server.Close()
	store, e := NewOSS(Config{OSSEndpoint: server.URL, OSSBucket: "fixture-voice", OSSAccessKeyID: "fixture", OSSAccessKeySecret: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	first, e := store.ListGeneratedVoices(t.Context(), 14, "")
	if e != nil || !first.Truncated || first.Next != "tenants/14/generated_voice/a.wav" {
		t.Fatal(first, e)
	}
	second, e := store.ListGeneratedVoices(t.Context(), 14, first.Next)
	if e != nil || second.Truncated || len(second.Objects) != 1 || second.Objects[0].Bytes != 88 {
		t.Fatal(second, e)
	}
	if _, e = store.ListGeneratedVoices(t.Context(), 14, "tenants/15/generated_voice/other"); e == nil {
		t.Fatal("foreign marker accepted")
	}
	if calls != 2 {
		t.Fatal("unexpected requests", calls)
	}
}
