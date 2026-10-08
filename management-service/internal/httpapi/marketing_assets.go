package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"livecompanion/management/internal/model"
)

const maxMarketingSkinAssetBytes int64 = 8 << 20

var allowedMarketingSkinMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

func marketingPresentationAssetContentURL(assetID int64) string {
	return fmt.Sprintf("/api/v1/marketing-assets/%d/content", assetID)
}

func (s *Server) commercialUploadMarketingAsset(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "活动图片存储尚未初始化")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMarketingSkinAssetBytes+1024*1024)
	if err := r.ParseMultipartForm(maxMarketingSkinAssetBytes); err != nil {
		writeError(w, http.StatusBadRequest, "活动图片过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择活动图片")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxMarketingSkinAssetBytes {
		writeError(w, http.StatusBadRequest, "活动图片须小于 8MB")
		return
	}
	buffer := make([]byte, 512)
	n, readErr := file.Read(buffer)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		writeError(w, http.StatusBadRequest, "无法读取活动图片")
		return
	}
	mimeType := http.DetectContentType(buffer[:n])
	if !allowedMarketingSkinMIMETypes[mimeType] {
		writeError(w, http.StatusBadRequest, "活动图片仅支持 JPG、PNG、WebP 或 GIF")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "无法重置活动图片")
		return
	}
	objectKey, err := newMediaObjectKey(0, "marketing-skin", header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成活动图片编号失败")
		return
	}
	assetStore := s.assetStorage.Current()
	if err := assetStore.Put(r.Context(), objectKey, file, mimeType); err != nil {
		writeError(w, http.StatusBadGateway, "保存活动图片失败")
		return
	}
	item, err := s.store.CreateMarketingPresentationAsset(r.Context(), model.MarketingPresentationAsset{
		StorageDriver: assetStore.Driver(), StorageBucket: assetStore.Bucket(),
		ObjectKey: objectKey, OriginalName: filepath.Base(header.Filename),
		MIMEType: mimeType, SizeBytes: uint64(header.Size), CreatedByUserID: actor.UserID,
	})
	if err != nil {
		_ = assetStore.Delete(r.Context(), objectKey)
		writeError(w, http.StatusInternalServerError, "登记活动图片失败")
		return
	}
	item.ContentURL = marketingPresentationAssetContentURL(item.ID)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) marketingAssetContent(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "活动图片存储尚未初始化")
		return
	}
	assetID, ok := namedPathID(w, r, "assetID", "活动图片")
	if !ok {
		return
	}
	item, err := s.store.GetMarketingPresentationAsset(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "活动图片不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取活动图片失败")
		return
	}
	assetStore, err := s.assetStorage.For(item.StorageDriver)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "活动图片存储暂不可用")
		return
	}
	reader, err := assetStore.Open(r.Context(), item.ObjectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "活动图片文件不存在")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", item.MIMEType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", safeHeaderFilename(item.OriginalName)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}
