package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type OSS struct {
	bucket       *oss.Bucket
	publicBucket *oss.Bucket
	bucketName   string
}

func NewOSS(cfg Config) (*OSS, error) {
	endpoint := strings.TrimSpace(cfg.OSSEndpoint)
	bucketName := strings.TrimSpace(cfg.OSSBucket)
	accessKeyID := strings.TrimSpace(cfg.OSSAccessKeyID)
	accessKeySecret := cfg.OSSAccessKeySecret
	if endpoint == "" || bucketName == "" || accessKeyID == "" || accessKeySecret == "" {
		return nil, fmt.Errorf("OSS storage requires endpoint, bucket, access key id and access key secret")
	}

	client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create OSS client: %w", err)
	}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("open OSS bucket: %w", err)
	}

	publicBucket := bucket
	if publicEndpoint := strings.TrimSpace(cfg.OSSPublicEndpoint); publicEndpoint != "" && publicEndpoint != endpoint {
		publicClient, err := oss.New(publicEndpoint, accessKeyID, accessKeySecret)
		if err != nil {
			return nil, fmt.Errorf("create public OSS client: %w", err)
		}
		publicBucket, err = publicClient.Bucket(bucketName)
		if err != nil {
			return nil, fmt.Errorf("open public OSS bucket: %w", err)
		}
	}

	return &OSS{
		bucket:       bucket,
		publicBucket: publicBucket,
		bucketName:   bucketName,
	}, nil
}

func (s *OSS) Driver() string { return "oss" }
func (s *OSS) Bucket() string { return s.bucketName }

func (s *OSS) Put(_ context.Context, objectKey string, source io.Reader, contentType string) error {
	options := make([]oss.Option, 0, 1)
	if strings.TrimSpace(contentType) != "" {
		options = append(options, oss.ContentType(contentType))
	}
	return s.bucket.PutObject(objectKey, source, options...)
}

// Device recordings and snapshots are private even if a bucket policy changes.
func (s *OSS) PutPrivate(ctx context.Context, objectKey string, source io.Reader, contentType string) error {
	return s.bucket.PutObject(objectKey, source, oss.ContentType(contentType), oss.ObjectACL(oss.ACLPrivate), oss.WithContext(ctx))
}

func (s *OSS) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return s.bucket.GetObject(objectKey, oss.WithContext(ctx))
}

func (s *OSS) Delete(ctx context.Context, objectKey string) error {
	return s.bucket.DeleteObject(objectKey, oss.WithContext(ctx))
}

func (s *OSS) InternalSignedURL(_ context.Context, objectKey string, expiry time.Duration) (string, error) {
	seconds := int64(expiry.Seconds())
	if seconds <= 0 {
		seconds = 900
	}
	return s.bucket.SignURL(objectKey, oss.HTTPGet, seconds)
}

func (s *OSS) SignedURL(_ context.Context, objectKey string, expiry time.Duration) (string, error) {
	seconds := int64(expiry.Seconds())
	if seconds <= 0 {
		seconds = 900
	}
	return s.publicBucket.SignURL(objectKey, oss.HTTPGet, seconds)
}
