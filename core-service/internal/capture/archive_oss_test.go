package capture

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func TestOSSArchivePrivateUploadVerifiesSize(t *testing.T) {
	data := []byte("local test recording")
	var uploaded []byte
	headSize := len(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			if r.Header.Get("x-oss-object-acl") != "private" || r.Header.Get("x-oss-meta-retention-until") == "" {
				t.Error("private retention metadata missing")
			}
			var err error
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(headSize))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client, err := oss.New(server.URL, "test-key", "test-secret", oss.ForcePathStyle(true), oss.EnableCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket("test-bucket")
	if err != nil {
		t.Fatal(err)
	}
	archive := &OSSArchive{internal: bucket, public: bucket}
	if err := archive.Upload(context.Background(), "live-recordings/test.wav", bytes.NewReader(data), int64(len(data)), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(uploaded, data) {
		t.Fatal("upload altered audio")
	}
	headSize--
	if err := archive.Upload(context.Background(), "live-recordings/test.wav", bytes.NewReader(data), int64(len(data)), time.Now().Add(24*time.Hour)); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func TestOSSArchiveSignsPublicEndpointAndFilename(t *testing.T) {
	archive, err := NewOSSArchive("http://oss-cn-hangzhou-internal.aliyuncs.com", "", "test-bucket", "test-key", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Unix()
	address, err := archive.DownloadURL(context.Background(), "live-recordings/test.wav", "测试录音.wav", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || strings.Contains(u.Host, "-internal") || !strings.HasPrefix(u.Host, "test-bucket.") {
		t.Fatal("browser endpoint is not public HTTPS")
	}
	if u.Query().Get("Signature") == "" || u.Query().Get("response-content-disposition") != "attachment; filename=\"recording.wav\"; filename*=UTF-8''"+url.PathEscape("测试录音.wav") {
		t.Fatal("signed filename missing")
	}
	if expiry := u.Query().Get("Expires"); expiry != fmt.Sprint(before+3) && expiry != fmt.Sprint(before+4) {
		t.Fatalf("unexpected signed expiry %s", expiry)
	}
}
