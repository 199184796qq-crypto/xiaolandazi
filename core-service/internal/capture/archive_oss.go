package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type OSSArchive struct{ internal, public *oss.Bucket }

func NewOSSArchive(endpoint, publicEndpoint, bucket, keyID, secret string) (*OSSArchive, error) {
	if endpoint == "" || bucket == "" || keyID == "" || secret == "" {
		return nil, ErrArchiveUnavailable
	}
	client, err := oss.New(endpoint, keyID, secret, oss.EnableCRC(true), oss.Timeout(10, 120))
	if err != nil {
		return nil, err
	}
	internal, err := client.Bucket(bucket)
	if err != nil {
		return nil, err
	}
	publicEndpoint = strings.TrimSpace(publicEndpoint)
	if publicEndpoint == "" {
		publicEndpoint = endpoint
	}
	if !strings.Contains(publicEndpoint, "://") {
		publicEndpoint = "https://" + publicEndpoint
	}
	parsed, err := url.Parse(publicEndpoint)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("invalid OSS public endpoint")
	}
	// Internal OSS endpoints are for server traffic, never for browser links.
	if strings.HasSuffix(parsed.Hostname(), ".aliyuncs.com") {
		parsed.Host = strings.Replace(parsed.Host, "-internal.", ".", 1)
		parsed.Scheme = "https"
	}
	if parsed.Scheme != "https" {
		return nil, errors.New("OSS public endpoint must use HTTPS")
	}
	publicClient, err := oss.New(parsed.String(), keyID, secret)
	if err != nil {
		return nil, err
	}
	public, err := publicClient.Bucket(bucket)
	if err != nil {
		return nil, err
	}
	return &OSSArchive{internal: internal, public: public}, nil
}

func (s *OSSArchive) Upload(ctx context.Context, key string, reader io.Reader, size int64, expires time.Time) error {
	if err := s.internal.PutObject(key, reader, oss.WithContext(ctx), oss.ContentType("audio/wav"), oss.ContentLength(size), oss.ObjectACL(oss.ACLPrivate), oss.CacheControl("private, no-store"), oss.Meta("retention-until", expires.UTC().Format(time.RFC3339))); err != nil {
		return err
	}
	metadata, err := s.internal.GetObjectDetailedMeta(key, oss.WithContext(ctx))
	if err != nil {
		return err
	}
	actual, err := strconv.ParseInt(metadata.Get("Content-Length"), 10, 64)
	if err != nil || actual != size {
		return errors.New("uploaded recording size does not match")
	}
	return nil
}

func (s *OSSArchive) Delete(ctx context.Context, key string) error {
	return s.internal.DeleteObject(key, oss.WithContext(ctx))
}

func (s *OSSArchive) DownloadURL(_ context.Context, key, name string, ttl time.Duration) (string, error) {
	seconds := int64(ttl / time.Second)
	if seconds < 1 {
		return "", ErrRecordingExpired
	}
	disposition := fmt.Sprintf("attachment; filename=\"recording.wav\"; filename*=UTF-8''%s", url.PathEscape(name))
	// OSS rejects response-content-type overrides (0017-00000902). The object
	// already carries audio/wav from Upload; only the attachment name is signed.
	return s.public.SignURL(key, oss.HTTPGet, seconds, oss.ResponseContentDisposition(disposition))
}
