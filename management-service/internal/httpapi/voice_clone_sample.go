package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/storage"
)

// Browser uploads may use local storage. Qwen must receive a server-created
// public OSS URL, so the normalized temporary sample always uses OSS.
func uploadCloneSample(ctx context.Context, registry *storage.Registry, tenantID, userID int64, raw []byte, mimeType string) (string, func(), error) {
	if registry == nil {
		return "", nil, errors.New("声音复刻 OSS 存储暂不可用")
	}
	ossStore, err := registry.For("oss")
	if err != nil {
		return "", nil, errors.New("声音复刻 OSS 存储暂不可用")
	}
	key := fmt.Sprintf("tenants/%d/voice-samples/tmp/%d-%d.wav", tenantID, userID, time.Now().UnixNano())
	if err := ossStore.Put(ctx, key, bytes.NewReader(raw), mimeType); err != nil {
		return "", nil, errors.New("上传声音复刻临时样本到 OSS 失败")
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = ossStore.Delete(cleanupCtx, key)
	}
	url, err := ossStore.SignedURL(ctx, key, 15*time.Minute)
	if err != nil || strings.TrimSpace(url) == "" {
		cleanup()
		return "", nil, errors.New("生成声音复刻公网样本地址失败")
	}
	return url, cleanup, nil
}
