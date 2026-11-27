package storage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomerAssetsDeleteExactLocalPaths(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tenants/14/sample.wav", "tenants/140/safe.wav", "14/plans/4/cache.wav"} {
		if err := store.Put(t.Context(), key, strings.NewReader("fixture"), "audio/wav"); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := store.DeletePrivatePaths(t.Context(), []string{"tenants/14", "14"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "tenants", "140", "safe.wav")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tenants", "14", "sample.wav")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := store.DeletePrivatePaths(t.Context(), []string{"../neighbor"}); err == nil {
		t.Fatal("storage escape accepted")
	}
}

func TestCustomerAssetsOSSUploadsVersionsAndMarker(t *testing.T) {
	upload, versioned, marker, current := true, true, true, true
	foreign := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			if q.Get("prefix") != "tenants/14/" {
				t.Errorf("unexpected listing prefix %q", q.Get("prefix"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if q.Has("uploads") {
				io := ""
				if upload {
					io = "<Upload><Key>tenants/14/partial.wav</Key><UploadId>up-1</UploadId></Upload>"
				}
				fmt.Fprintf(w, "<ListMultipartUploadsResult>%s</ListMultipartUploadsResult>", io)
				return
			}
			if !q.Has("versions") {
				t.Error("expected version listing")
			}
			fmt.Fprint(w, "<ListVersionsResult>")
			if versioned {
				fmt.Fprint(w, "<Version><Key>tenants/14/v.wav</Key><VersionId>v1</VersionId></Version>")
			}
			if marker {
				fmt.Fprint(w, "<DeleteMarker><Key>tenants/14/v.wav</Key><VersionId>m1</VersionId></DeleteMarker>")
			}
			if current {
				fmt.Fprint(w, "<Version><Key>tenants/14/current.wav</Key><VersionId/></Version>")
			}
			if foreign {
				fmt.Fprint(w, "<Version><Key>tenants/140/foreign.wav</Key><VersionId/></Version>")
			}
			fmt.Fprint(w, "</ListVersionsResult>")
		case http.MethodDelete:
			switch {
			case strings.HasSuffix(r.URL.Path, "/tenants/14/partial.wav") && q.Get("uploadId") == "up-1":
				upload = false
			case strings.HasSuffix(r.URL.Path, "/tenants/14/v.wav") && q.Get("versionId") == "v1":
				versioned = false
			case strings.HasSuffix(r.URL.Path, "/tenants/14/v.wav") && q.Get("versionId") == "m1":
				marker = false
			case strings.HasSuffix(r.URL.Path, "/tenants/14/current.wav") && !q.Has("versionId"):
				current = false
			default:
				t.Errorf("unexpected delete %s", r.URL)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	store, err := NewOSS(Config{OSSEndpoint: server.URL, OSSBucket: "fixture-customer-assets", OSSAccessKeyID: "fixture", OSSAccessKeySecret: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.DeletePrivatePrefix(t.Context(), "tenants/14/"); err != nil {
			t.Fatal(err)
		}
	}
	foreign = true
	if err := store.DeletePrivatePrefix(t.Context(), "tenants/14/"); err == nil {
		t.Fatal("foreign result accepted")
	}
}
