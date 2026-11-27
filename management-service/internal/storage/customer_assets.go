package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// DeletePrivatePaths removes only paths resolved beneath the configured media
// root. The caller supplies paths derived from verified owner IDs.
func (s *Local) DeletePrivatePaths(ctx context.Context, keys []string) error {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := s.resolve(key)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, name)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			return fmt.Errorf("invalid private media path")
		}
		if err := root.RemoveAll(rel); err != nil {
			return err
		}
	}
	return nil
}

func (s *Local) DeletePrivateFilesWithPrefix(ctx context.Context, directory, prefix string) error {
	if directory == "" || prefix == "" {
		return fmt.Errorf("empty private file scope")
	}
	name, err := s.resolve(directory)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), prefix) {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	return s.DeletePrivatePaths(ctx, paths)
}

func (s *OSS) DeletePrivatePrefix(ctx context.Context, prefix string) error {
	if prefix == "" || strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("invalid private OSS prefix")
	}
	// Abort uploads first so unfinished parts cannot repopulate the prefix.
	options := []oss.Option{oss.Prefix(prefix), oss.WithContext(ctx)}
	for {
		page, err := s.bucket.ListMultipartUploads(options...)
		if err != nil {
			return err
		}
		if len(page.Uploads) == 0 && !page.IsTruncated {
			break
		}
		if len(page.Uploads) == 0 {
			return fmt.Errorf("incomplete multipart inventory")
		}
		for _, upload := range page.Uploads {
			if !strings.HasPrefix(upload.Key, prefix) || upload.UploadID == "" {
				return fmt.Errorf("unexpected multipart object")
			}
			if err := s.bucket.AbortMultipartUpload(oss.InitiateMultipartUploadResult{Key: upload.Key, UploadID: upload.UploadID}, oss.WithContext(ctx)); err != nil {
				if serviceErr, ok := err.(oss.ServiceError); !ok || serviceErr.Code != "NoSuchUpload" {
					return err
				}
			}
		}
	}
	for {
		page, err := s.bucket.ListObjectVersions(options...)
		if err != nil {
			return err
		}
		if len(page.ObjectVersions) == 0 && len(page.ObjectDeleteMarkers) == 0 {
			if page.IsTruncated {
				return fmt.Errorf("incomplete OSS version inventory")
			}
			return nil
		}
		remove := func(key, version string) error {
			if !strings.HasPrefix(key, prefix) {
				return fmt.Errorf("unexpected OSS object version")
			}
			if version == "" {
				return s.Delete(ctx, key)
			}
			return s.bucket.DeleteObject(key, oss.VersionId(version), oss.WithContext(ctx))
		}
		for _, object := range page.ObjectVersions {
			if err := remove(object.Key, object.VersionId); err != nil {
				return err
			}
		}
		for _, marker := range page.ObjectDeleteMarkers {
			if marker.VersionId == "" {
				return fmt.Errorf("OSS delete marker missing version ID")
			}
			if err := remove(marker.Key, marker.VersionId); err != nil {
				return err
			}
		}
	}
}
