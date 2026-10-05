package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevicePrivateOSSAvailableWithLocalDefault(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("x-oss-object-acl") != "private" {
			t.Errorf("device asset is not explicitly private")
		}
		if r.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("wrong MIME")
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "picture" {
			t.Errorf("wrong upload")
		}
		w.Header().Set("ETag", "test")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	registry, err := NewRegistry(Config{Driver: "local", LocalRoot: t.TempDir(), OSSEndpoint: server.URL, OSSBucket: "private-device-bucket", OSSAccessKeyID: "test-key-id", OSSAccessKeySecret: "test-key-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Current().Driver() != "local" {
		t.Fatal("default driver changed")
	}
	configured, err := registry.For("oss")
	if err != nil {
		t.Fatal(err)
	}
	private, ok := configured.(*OSS)
	if !ok {
		t.Fatal("private OSS missing")
	}
	if err := private.PutPrivate(context.Background(), "25/snapshot.jpg", strings.NewReader("picture"), "image/jpeg"); err != nil {
		t.Fatal("private upload failed", err)
	}
	if !called {
		t.Fatal("upload not sent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := private.PutPrivate(ctx, "25/canceled.jpg", strings.NewReader("picture"), "image/jpeg"); err == nil {
		t.Fatal("cancellation ignored")
	}
}
