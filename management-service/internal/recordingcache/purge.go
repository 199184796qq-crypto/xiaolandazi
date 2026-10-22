package recordingcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// MediaEntry 是索引表里的一行资产，删除时只需要这几个字段，
// 避免让缓存包依赖数据库模型。
type MediaEntry struct {
	AssetID   int64
	ObjectKey string
	SizeBytes int64
}

// PurgeResult 汇总一次清理的结果，便于写审计日志。
type PurgeResult struct {
	Removed int
	Missing int
	Bytes   int64
}

// PurgeFiles 按索引行删除磁盘文件。objectKey 是相对媒体根目录的路径，
// 解析后必须仍然落在根目录内，否则拒绝删除（防止越权删库外文件）。
func PurgeFiles(mediaRoot string, entries []MediaEntry) (PurgeResult, error) {
	var result PurgeResult
	root, err := filepath.Abs(mediaRoot)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		clean := filepath.Clean(strings.TrimPrefix(entry.ObjectKey, "/"))
		if clean == "." || clean == "" || strings.HasPrefix(clean, "..") {
			continue
		}
		path := filepath.Join(root, clean)
		abs, err := filepath.Abs(path)
		if err != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			continue
		}
		info, statErr := os.Stat(abs)
		if errors.Is(statErr, os.ErrNotExist) {
			result.Missing++
			continue
		}
		if statErr != nil {
			return result, statErr
		}
		if err := os.Remove(abs); err != nil {
			return result, err
		}
		result.Removed++
		if entry.SizeBytes > 0 {
			result.Bytes += entry.SizeBytes
		} else {
			result.Bytes += info.Size()
		}
	}
	return result, nil
}

// PurgeScopeDir 整目录删除一个方案或房间，先改名再删，避免读到半截目录。
func PurgeScopeDir(dir string) error {
	return removeDir(dir)
}
