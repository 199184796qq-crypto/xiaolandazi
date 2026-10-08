package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"livecompanion/management/internal/model"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

type factCategoryDefinition = model.LiveFactCategory

type factCategoryCatalog struct {
	SchemaVersion int                      `json:"schema_version"`
	Version       string                   `json:"version"`
	Categories    []factCategoryDefinition `json:"categories"`
}

func (c factCategoryCatalog) contains(value string) bool {
	for _, category := range c.Categories {
		if category.Value == value {
			return true
		}
	}
	return false
}

func parseFactCategoryCatalog(raw []byte) (factCategoryCatalog, error) {
	var catalog factCategoryCatalog
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	if len(raw) == 0 || len(raw) > 128*1024 || !utf8.Valid(raw) {
		return catalog, errors.New("事实分类JSON必须是有效UTF-8文件，且不超过128KB")
	}
	if err := decoder.Decode(&catalog); err != nil {
		return catalog, fmt.Errorf("事实分类JSON格式错误: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return catalog, errors.New("事实分类JSON不能包含多个对象")
	}
	if catalog.SchemaVersion != 1 || strings.TrimSpace(catalog.Version) == "" || len(catalog.Categories) < 6 || len(catalog.Categories) > 200 {
		return catalog, errors.New("事实分类配置版本或分类数量无效")
	}
	families := map[string]bool{"product": true, "link": true, "trade": true, "fulfillment": true, "identity_location": true, "other": true}
	identifier := regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	seen := map[string]bool{}
	for _, category := range catalog.Categories {
		if !identifier.MatchString(category.Value) || seen[category.Value] || !families[category.Family] || strings.TrimSpace(category.Label) == "" || strings.TrimSpace(category.Group) == "" || strings.TrimSpace(category.Description) == "" || utf8.RuneCountInString(category.Label) > 40 || utf8.RuneCountInString(category.Description) > 200 || utf8.RuneCountInString(category.Group) > 40 {
			return catalog, fmt.Errorf("事实分类配置含无效或重复条目: %s", category.Value)
		}
		seen[category.Value] = true
		if families[category.Value] && category.Family != category.Value {
			return catalog, fmt.Errorf("历史分类的family不能修改: %s", category.Value)
		}
	}
	for legacy := range families {
		if !seen[legacy] {
			return catalog, fmt.Errorf("请保留历史事实分类标识: %s", legacy)
		}
	}
	return catalog, nil
}

// Read on each request so replacing the JSON does not need a rebuild or restart.
func managedFactCategoryConfigPath(directory string) string {
	for range 7 {
		if filepath.Base(filepath.Dir(directory)) == "releases" {
			return filepath.Join(filepath.Dir(filepath.Dir(directory)), "current", "configs", "fact-categories.json")
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return ""
}

func loadFactCategoryCatalog() (factCategoryCatalog, string, error) {
	paths := []string{}
	if override := strings.TrimSpace(os.Getenv("FACT_CATEGORY_CONFIG_PATH")); override != "" {
		paths = append(paths, override)
	} else {
		executable, _ := os.Executable()
		// Frontend-only releases can move current without restarting management.
		// Never pin the catalog to the older executable's immutable release.
		if managed := managedFactCategoryConfigPath(filepath.Dir(executable)); managed != "" {
			paths = append(paths, managed)
		} else {
			if cwd, err := os.Getwd(); err == nil {
				for range 7 {
					paths = append(paths, filepath.Join(cwd, "configs", "fact-categories.json"))
					parent := filepath.Dir(cwd)
					if parent == cwd {
						break
					}
					cwd = parent
				}
			}
			if executable != "" {
				paths = append(paths, filepath.Join(filepath.Dir(executable), "..", "configs", "fact-categories.json"))
			}
		}
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.IsDir() || info.Size() > 128*1024 {
			return factCategoryCatalog{}, "", fmt.Errorf("无法读取事实分类配置: %s", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return factCategoryCatalog{}, "", fmt.Errorf("读取事实分类配置失败: %w", err)
		}
		catalog, err := parseFactCategoryCatalog(raw)
		return catalog, fmt.Sprintf("%x", sha256.Sum256(raw)), err
	}
	return factCategoryCatalog{}, "", errors.New("找不到服务器事实分类配置 configs/fact-categories.json")
}

func (s *Server) liveFactCategoryCatalog(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	catalog, hash, err := loadFactCategoryCatalog()
	if err == nil {
		if actor.Role == "customer" && actor.TenantID != nil {
			catalog, hash, err = s.tenantFactCategoryCatalog(r.Context(), *actor.TenantID)
		} else if room, valid := liveSupportScopeRoomID(r); valid && room > 0 {
			tenant, valid := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
			if !valid {
				return
			}
			catalog, hash, err = s.tenantFactCategoryCatalog(r.Context(), tenant)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Fact-Category-SHA256", hash)
	writeJSON(w, http.StatusOK, catalog)
}

func (s *Server) tenantFactCategoryCatalog(ctx context.Context, tenant int64) (factCategoryCatalog, string, error) {
	catalog, hash, err := loadFactCategoryCatalog()
	if err != nil {
		return catalog, hash, err
	}
	items, err := s.store.ListLiveFactCategories(ctx, tenant)
	if err != nil {
		return catalog, "", err
	}
	for _, item := range items {
		if catalog.contains(item.Value) {
			return catalog, "", errors.New("自定义分类与基础配置标识冲突")
		}
		catalog.Categories = append(catalog.Categories, item)
	}
	raw, _ := json.Marshal(catalog.Categories)
	hash = fmt.Sprintf("%x", sha256.Sum256(raw))
	catalog.Version += ":" + hash[:12]
	return catalog, hash, nil
}

func (s *Server) validateTenantFactCandidate(ctx context.Context, tenant int64, candidate model.LiveAgentPlanFactCandidate) error {
	catalog, _, err := s.tenantFactCategoryCatalog(ctx, tenant)
	if err != nil {
		return err
	}
	return validateLiveAgentPlanFactCandidate(candidate, catalog)
}
