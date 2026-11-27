package storage

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

const MaxScriptHumanRecordingBytes = 100 << 20
const scriptHumanUploadPrefix = "script-human-recording-uploads/"

var scriptHumanUploadID = regexp.MustCompile(`^[0-9]{10}-[a-f0-9]{32}$`)

func ScriptHumanUploadExpiry(id string) time.Time {
	if !scriptHumanUploadID.MatchString(id) {
		return time.Time{}
	}
	seconds, _ := strconv.ParseInt(id[:10], 10, 64)
	return time.Unix(seconds, 0).UTC()
}

func (s *OSS) ScriptHumanUploadForm(id string, size int64) (DirectUploadForm, error) {
	expires := ScriptHumanUploadExpiry(id)
	if size <= 0 || size > MaxScriptHumanRecordingBytes || time.Until(expires) <= 0 || time.Until(expires) > time.Hour {
		return DirectUploadForm{}, fmt.Errorf("invalid recording upload")
	}
	return s.directUploadForm(scriptHumanUploadPrefix+id, "application/octet-stream", size, expires)
}

func (s *OSS) OpenScriptHumanUpload(ctx context.Context, id string, size int64) (io.ReadCloser, error) {
	if ScriptHumanUploadExpiry(id).IsZero() || size <= 0 || size > MaxScriptHumanRecordingBytes {
		return nil, fmt.Errorf("invalid recording upload")
	}
	key := scriptHumanUploadPrefix + id
	meta, err := s.bucket.GetObjectDetailedMeta(key, oss.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	actual, err := strconv.ParseInt(meta.Get("Content-Length"), 10, 64)
	etag := meta.Get("ETag")
	if err != nil || actual != size || etag == "" {
		return nil, fmt.Errorf("recording upload size mismatch")
	}
	return s.bucket.GetObject(key, oss.IfMatch(etag), oss.WithContext(ctx))
}

func (s *OSS) DeleteScriptHumanUpload(ctx context.Context, id string) error {
	if ScriptHumanUploadExpiry(id).IsZero() {
		return fmt.Errorf("invalid recording upload")
	}
	return s.Delete(ctx, scriptHumanUploadPrefix+id)
}

func (s *OSS) DeleteExpiredScriptHumanUploads(ctx context.Context, now time.Time) error {
	// The expiration is the first, fixed-width part of every key. Only this
	// temporary prefix is scanned, stopping at the first live upload.
	marker := ""
	for {
		page, err := s.bucket.ListObjects(oss.Prefix(scriptHumanUploadPrefix), oss.Marker(marker), oss.MaxKeys(500), oss.WithContext(ctx))
		if err != nil {
			return err
		}
		keys := make([]string, 0, len(page.Objects))
		for _, object := range page.Objects {
			if object.Key <= marker || len(object.Key) <= len(scriptHumanUploadPrefix) || object.Key[:len(scriptHumanUploadPrefix)] != scriptHumanUploadPrefix {
				return fmt.Errorf("invalid recording upload listing")
			}
			marker = object.Key
			expires := ScriptHumanUploadExpiry(object.Key[len(scriptHumanUploadPrefix):])
			if expires.IsZero() {
				continue
			}
			if expires.After(now) {
				page.IsTruncated = false
				break
			}
			keys = append(keys, object.Key)
		}
		if len(keys) > 0 {
			if _, err := s.bucket.DeleteObjects(keys, oss.DeleteObjectsQuiet(true), oss.WithContext(ctx)); err != nil {
				return err
			}
		}
		if !page.IsTruncated || len(page.Objects) == 0 {
			return nil
		}
	}
}
