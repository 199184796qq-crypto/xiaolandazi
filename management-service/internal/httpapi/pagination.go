package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func queryInt64(r *http.Request, key string, fallback int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func normalizedPageQuery(r *http.Request, defaultSize int, maxSize int) (int, int) {
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "page_size", defaultSize)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultSize
	}
	if pageSize > maxSize {
		pageSize = maxSize
	}
	return page, pageSize
}

func totalPages(total int64, pageSize int) int64 {
	if pageSize <= 0 {
		return 1
	}
	if total <= 0 {
		return 1
	}
	return (total + int64(pageSize) - 1) / int64(pageSize)
}
