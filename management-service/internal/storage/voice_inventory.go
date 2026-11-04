package storage

import (
	"context"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"strings"
	"time"
)

type VoiceInventoryObject struct {
	Key      string
	Bytes    uint64
	Modified time.Time
}
type VoiceInventoryPage struct {
	Objects   []VoiceInventoryObject
	Next      string
	Truncated bool
}

func (s *OSS) ListGeneratedVoices(ctx context.Context, tenant int64, marker string) (VoiceInventoryPage, error) {
	prefix := fmt.Sprintf("tenants/%d/generated_voice/", tenant)
	if tenant <= 0 || (marker != "" && !strings.HasPrefix(marker, prefix)) {
		return VoiceInventoryPage{}, fmt.Errorf("inventory scope invalid")
	}
	result, e := s.bucket.ListObjects(oss.Prefix(prefix), oss.Marker(marker), oss.MaxKeys(1000), oss.WithContext(ctx))
	if e != nil {
		return VoiceInventoryPage{}, e
	}
	page := VoiceInventoryPage{Next: result.NextMarker, Truncated: result.IsTruncated}
	for _, v := range result.Objects {
		if !strings.HasPrefix(v.Key, prefix) {
			return page, fmt.Errorf("inventory prefix mismatch")
		}
		page.Objects = append(page.Objects, VoiceInventoryObject{v.Key, uint64(max(0, v.Size)), v.LastModified})
	}
	// Flat ListObjects v1 responses may omit NextMarker. With no delimiter,
	// the last returned key is the exclusive continuation marker.
	if page.Truncated && page.Next == "" && len(page.Objects) > 0 {
		page.Next = page.Objects[len(page.Objects)-1].Key
	}
	if page.Truncated && (page.Next == "" || page.Next <= marker) {
		return page, fmt.Errorf("inventory cursor did not advance")
	}
	return page, nil
}
func (s *OSS) VoiceBucketVersionState(ctx context.Context) string {
	result, e := s.bucket.Client.GetBucketVersioning(s.bucketName, oss.WithContext(ctx))
	if e != nil {
		return "unknown"
	}
	if result.Status == "" {
		return "disabled"
	}
	return strings.ToLower(result.Status)
}
