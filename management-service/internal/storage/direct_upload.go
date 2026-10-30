package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const MaxDirectAudioBytes int64 = 2 << 30

type DirectUploadForm struct {
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields"`
}

// The client receives a one-key, size-bound POST policy, never the signing secret.
func (s *OSS) DirectUploadForm(key, contentType string, size int64, expires time.Time) (DirectUploadForm, error) {
	if size <= 0 || size > MaxDirectAudioBytes || !strings.HasPrefix(key, "custom-mainline-uploads/") || strings.Contains(key, "..") {
		return DirectUploadForm{}, fmt.Errorf("invalid upload object")
	}
	if time.Until(expires) <= 0 || time.Until(expires) > 2*time.Hour {
		return DirectUploadForm{}, fmt.Errorf("invalid upload expiration")
	}
	signed, err := s.publicBucket.SignURL(key, oss.HTTPGet, 60)
	if err != nil {
		return DirectUploadForm{}, err
	}
	u, err := url.Parse(signed)
	if err != nil {
		return DirectUploadForm{}, err
	}
	if u.Scheme != "https" || strings.Contains(u.Host, "-internal.") {
		return DirectUploadForm{}, fmt.Errorf("direct upload requires public HTTPS OSS endpoint")
	}
	credentials := s.bucket.Client.Config.GetCredentials()
	fields := map[string]string{"key": key, "success_action_status": "204", "x-oss-object-acl": "private", "Content-Type": contentType}
	conditions := []any{map[string]string{"bucket": s.bucketName}, []any{"content-length-range", size, size}}
	for name, value := range fields {
		conditions = append(conditions, map[string]string{name: value})
	}
	if token := credentials.GetSecurityToken(); token != "" {
		fields["x-oss-security-token"] = token
		conditions = append(conditions, map[string]string{"x-oss-security-token": token})
	}
	policy, err := json.Marshal(map[string]any{"expiration": expires.UTC().Format(time.RFC3339), "conditions": conditions})
	if err != nil {
		return DirectUploadForm{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(policy)
	mac := hmac.New(sha1.New, []byte(credentials.GetAccessKeySecret()))
	_, _ = mac.Write([]byte(encoded))
	fields["policy"] = encoded
	fields["OSSAccessKeyId"] = credentials.GetAccessKeyID()
	fields["Signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return DirectUploadForm{URL: u.Scheme + "://" + u.Host + "/", Fields: fields}, nil
}

// Pin the exact HEAD-verified object before reading it. A still-valid client
// policy cannot overwrite this server-only snapshot key.
func (s *OSS) PinDirectUpload(ctx context.Context, key, snapshot string, size int64) error {
	if size <= 0 || size > MaxDirectAudioBytes {
		return fmt.Errorf("invalid upload size")
	}
	meta, err := s.bucket.GetObjectDetailedMeta(key, oss.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("uploaded object unavailable: %w", err)
	}
	actual, err := strconv.ParseInt(meta.Get("Content-Length"), 10, 64)
	if err != nil || actual != size || meta.Get("ETag") == "" {
		return fmt.Errorf("uploaded object size mismatch")
	}
	if size <= 1<<30 {
		_, err = s.bucket.CopyObject(key, snapshot, oss.CopySourceIfMatch(meta.Get("ETag")), oss.ObjectACL(oss.ACLPrivate), oss.WithContext(ctx))
		return err
	}
	// CopyObject has a 1GB service limit; large uploads need server-side part
	// copying. File bytes still never pass through the management HTTP server.
	upload, err := s.bucket.InitiateMultipartUpload(snapshot, oss.ObjectACL(oss.ACLPrivate), oss.ContentType(meta.Get("Content-Type")), oss.WithContext(ctx))
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = s.bucket.AbortMultipartUpload(upload, oss.WithContext(cleanup))
		}
	}()
	parts := []oss.UploadPart{}
	const partSize int64 = 64 << 20
	for offset := int64(0); offset < size; offset += partSize {
		part, copyErr := s.bucket.UploadPartCopy(upload, s.bucketName, key, offset, min(partSize, size-offset), len(parts)+1, oss.CopySourceIfMatch(meta.Get("ETag")), oss.WithContext(ctx))
		if copyErr != nil {
			return copyErr
		}
		parts = append(parts, part)
	}
	_, err = s.bucket.CompleteMultipartUpload(upload, parts, oss.WithContext(ctx))
	completed = err == nil
	return err
}
