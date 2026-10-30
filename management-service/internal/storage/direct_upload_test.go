package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDirectUploadPolicyBoundsAndSecrets(t *testing.T) {
	s, err := NewOSS(Config{OSSEndpoint: "https://oss-cn-hangzhou.aliyuncs.com", OSSBucket: "test-bucket", OSSAccessKeyID: "test-id", OSSAccessKeySecret: "never-return-secret"})
	if err != nil {
		t.Fatal(err)
	}
	key := "custom-mainline-uploads/tenant-14/plan-1/random/source-00.wav"
	form, err := s.DirectUploadForm(key, "audio/wav", 2<<30, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if form.URL != "https://test-bucket.oss-cn-hangzhou.aliyuncs.com/" || form.Fields["key"] != key || form.Fields["x-oss-object-acl"] != "private" {
		t.Fatal("incorrect upload destination or ACL")
	}
	raw, _ := json.Marshal(form)
	if strings.Contains(string(raw), "never-return-secret") {
		t.Fatal("signing secret returned")
	}
	policy, err := base64.StdEncoding.DecodeString(form.Fields["policy"])
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Expiration string            `json:"expiration"`
		Conditions []json.RawMessage `json:"conditions"`
	}
	if err = json.Unmarshal(policy, &parsed); err != nil {
		t.Fatal(err)
	}
	checks := map[string]bool{}
	for _, condition := range parsed.Conditions {
		var equality map[string]string
		if json.Unmarshal(condition, &equality) == nil {
			for k, v := range equality {
				checks[k+"="+v] = true
			}
			continue
		}
		if string(condition) == `["content-length-range",2147483648,2147483648]` {
			checks["size"] = true
		}
	}
	for _, required := range []string{"key=" + key, "x-oss-object-acl=private", "Content-Type=audio/wav", "success_action_status=204", "bucket=test-bucket", "size"} {
		if !checks[required] {
			t.Fatal("unbound policy condition", required)
		}
	}
	mac := hmac.New(sha1.New, []byte("never-return-secret"))
	_, _ = mac.Write([]byte(form.Fields["policy"]))
	if form.Fields["Signature"] != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("invalid policy signature")
	}
	for _, size := range []int64{0, -1, (2 << 30) + 1} {
		if _, err = s.DirectUploadForm(key, "audio/wav", size, time.Now().Add(time.Hour)); err == nil {
			t.Fatal("size accepted", size)
		}
	}
	if _, err = s.DirectUploadForm("tenants/14/formal.wav", "audio/wav", 1, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("formal asset key exposed")
	}
}

func TestPinDirectUploadLargeCopyAndAbort(t *testing.T) {
	for _, failPart := range []int{0, 3} {
		t.Run(fmt.Sprint(failPart), func(t *testing.T) {
			parts, completed, aborted := 0, false, false
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				switch {
				case r.Method == "HEAD":
					w.Header().Set("Content-Length", strconv.FormatInt(2<<30, 10))
					w.Header().Set("ETag", `"same-original"`)
				case r.Method == "POST" && q.Has("uploads"):
					if r.Header.Get("x-oss-object-acl") != "private" {
						t.Error("snapshot is not private")
					}
					fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>test-bucket</Bucket><Key>verified.wav</Key><UploadId>parts-id</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == "PUT" && q.Get("partNumber") != "":
					parts++
					if r.Header.Get("x-oss-copy-source-if-match") != `"same-original"` || r.Header.Get("x-oss-copy-source-range") == "" {
						t.Error("copy was not pinned")
					}
					if parts == failPart {
						http.Error(w, "changed source", 412)
						return
					}
					fmt.Fprint(w, `<CopyPartResult><ETag>part-etag</ETag></CopyPartResult>`)
				case r.Method == "POST" && q.Get("uploadId") != "":
					completed = true
					fmt.Fprint(w, `<CompleteMultipartUploadResult><ETag>complete-etag</ETag></CompleteMultipartUploadResult>`)
				case r.Method == "DELETE":
					aborted = true
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected OSS operation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer fake.Close()
			client, err := oss.New(fake.URL, "id", "secret", oss.ForcePathStyle(true))
			if err != nil {
				t.Fatal(err)
			}
			bucket, _ := client.Bucket("test-bucket")
			s := &OSS{bucket: bucket, bucketName: "test-bucket"}
			err = s.PinDirectUpload(context.Background(), "source.wav", "verified.wav", 2<<30)
			if failPart == 0 {
				if err != nil || parts != 32 || !completed || aborted {
					t.Fatal(parts, completed, aborted, err)
				}
			} else if err == nil || completed || !aborted {
				t.Fatal("failed multipart copy leaked", completed, aborted, err)
			}
		})
	}
}
