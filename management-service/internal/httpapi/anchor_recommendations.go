package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const anchorRecommendationPermission = "system.settings.liveops.manage"

func (s *Server) registerAnchorRecommendationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/live-anchor-styles/{styleID}/avatar-image", s.liveAnchorAvatarUpload)
	mux.HandleFunc("GET /api/v1/live-anchor-styles/current", s.currentLiveAnchor)
	mux.HandleFunc("GET /api/v1/live/anchor-recommendations", s.anchorRecommendationsList)
	mux.HandleFunc("POST /api/v1/live/anchor-recommendations/import", s.anchorRecommendationsImport)
	mux.HandleFunc("GET /api/v1/system/anchor-recommendations", s.systemAnchorRecommendationsList)
	mux.HandleFunc("GET /api/v1/system/anchor-recommendations/sources", s.systemAnchorRecommendationSources)
	mux.HandleFunc("POST /api/v1/system/anchor-recommendations/preview", s.systemAnchorRecommendationPreview)
	mux.HandleFunc("POST /api/v1/system/anchor-recommendations", s.systemAnchorRecommendationPublish)
	mux.HandleFunc("POST /api/v1/system/anchor-recommendations/{recommendationID}/withdraw", s.systemAnchorRecommendationWithdraw)
}
func normalizeAnchorAvatar(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 1<<20 {
		return "", errors.New("头像需小于1MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return "", errors.New("请选择有效的JPG或PNG图片")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		return "", errors.New("头像尺寸需在1024×1024以内")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err = jpeg.Encode(&output, dst, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(output.Bytes()), nil
}
func (s *Server) liveAnchorAvatarUpload(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	id, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAnchorStyle(r.Context(), tenant, id); err != nil {
		writeError(w, 404, "主播不存在")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (1<<20)+(64<<10))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, 400, "头像文件过大或上传格式错误")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, 400, "请选择头像图片")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		writeError(w, 400, "读取头像失败")
		return
	}
	avatar, err := normalizeAnchorAvatar(raw)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	item, err := s.store.SaveLiveAnchorAvatarData(r.Context(), tenant, id, actor.UserID, avatar)
	if err != nil {
		writeError(w, 500, "保存主播头像失败")
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) currentLiveAnchor(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.liveAnchorStyleTenant(w, r, false)
	if !ok {
		return
	}
	plan, err := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	if err != nil || plan <= 0 {
		writeError(w, 400, "请选择当前方案")
		return
	}
	if _, err = s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "方案不存在")
		return
	}
	style, err := s.store.GetLiveAnchorStyleForPlan(r.Context(), tenant, plan)
	if errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeJSON(w, 200, map[string]any{"style_id": 0})
		return
	}
	if err != nil {
		writeError(w, 500, "读取当前主播失败")
		return
	}
	writeJSON(w, 200, map[string]any{"style_id": style.ID})
}
func (s *Server) anchorRecommendationsList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	items, err := s.store.ListAnchorRecommendations(r.Context(), false)
	if err != nil {
		writeError(w, 500, "读取系统推荐主播失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) anchorRecommendationsImport(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	var input struct {
		IDs []int64 `json:"ids"`
	}
	if readJSON(w, r, &input) != nil || len(input.IDs) == 0 || len(input.IDs) > 50 {
		writeError(w, 400, "请选择1到50位推荐主播")
		return
	}
	for _, id := range input.IDs {
		if id <= 0 {
			writeError(w, 400, "推荐主播编号无效")
			return
		}
	}
	items, err := s.store.ImportAnchorRecommendations(r.Context(), tenant, actor.UserID, input.IDs)
	if errors.Is(err, appdb.ErrAnchorRecommendationUnavailable) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "加入我的主播列表失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) systemAnchorRecommendationsList(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, anchorRecommendationPermission); !ok {
		return
	}
	items, err := s.store.ListAnchorRecommendations(r.Context(), true)
	if err != nil {
		writeError(w, 500, "读取推荐管理失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) systemAnchorRecommendationSources(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, anchorRecommendationPermission); !ok {
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if utf8.RuneCountInString(search) > 80 {
		writeError(w, 400, "搜索词最多80字")
		return
	}
	items, err := s.store.ListAnchorRecommendationSources(r.Context(), search)
	if err != nil {
		writeError(w, 500, "读取用户主播失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

type anchorRecommendationDraftInput struct {
	TenantID int64 `json:"tenant_id"`
	StyleID  int64 `json:"style_id"`
}

func parseAnchorRecommendationSummary(raw string) (string, error) {
	var parsed struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &parsed); err != nil {
		return "", errors.New("模型未返回有效的简介JSON，请重新生成")
	}
	text := strings.TrimSpace(parsed.Summary)
	if utf8.RuneCountInString(text) != 50 || strings.ContainsAny(text, "\n\r") {
		return text, errors.New("模型简介不是50字，请校对或重新生成")
	}
	return text, nil
}
func (s *Server) systemAnchorRecommendationPreview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, anchorRecommendationPermission); !ok {
		return
	}
	var input anchorRecommendationDraftInput
	if readJSON(w, r, &input) != nil || input.TenantID <= 0 || input.StyleID <= 0 {
		writeError(w, 400, "请选择用户和主播")
		return
	}
	source, err := s.store.GetLiveAnchorStyle(r.Context(), input.TenantID, input.StyleID)
	if err != nil {
		writeError(w, 404, "用户主播不存在")
		return
	}
	if !model.HasReusableAnchorProfile(source.Profile) {
		writeError(w, 400, "请先完成该主播的风格分析，再设为系统推荐")
		return
	}
	mother, motherSource, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/anchor-recommendation-summary.md", "推荐主播简介母版")
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	data, _ := json.Marshal(map[string]any{"name": source.Name, "description": source.Description, "style": source.Profile})
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	result, _, err := s.completeAnchorStyleWorkbench(ctx, "", "anchor_recommendation_summary", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(data)}}, 400, false)
	if err != nil {
		writeError(w, 502, "生成主播特征简介失败："+err.Error())
		return
	}
	summary, validationErr := parseAnchorRecommendationSummary(result.Text)
	validation := ""
	if validationErr != nil {
		validation = validationErr.Error()
	}
	writeJSON(w, 200, map[string]any{"tenant_id": source.TenantID, "style_id": source.ID, "name": source.Name, "introduction": source.Description, "summary": summary, "avatar_type": source.AvatarType, "avatar_url": source.AvatarURL, "source_sha256": model.AnchorRecommendationSHA(source), "provider": result.Provider, "model": result.Model, "raw_text": result.Text, "prompt_source": motherSource, "validation_error": validation})
}
func (s *Server) systemAnchorRecommendationPublish(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, anchorRecommendationPermission)
	if !ok {
		return
	}
	var input struct {
		anchorRecommendationDraftInput
		Name         string         `json:"name"`
		Introduction string         `json:"introduction"`
		Summary      string         `json:"summary"`
		SourceSHA    string         `json:"source_sha256"`
		Generation   map[string]any `json:"generation"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "推荐内容格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Introduction = strings.TrimSpace(input.Introduction)
	input.Summary = strings.TrimSpace(input.Summary)
	if input.TenantID <= 0 || input.StyleID <= 0 || input.Name == "" || utf8.RuneCountInString(input.Name) > 80 || utf8.RuneCountInString(input.Introduction) > 500 || utf8.RuneCountInString(input.Summary) != 50 || len(input.SourceSHA) != 64 || strings.ContainsAny(input.Summary, "\r\n") {
		writeError(w, 400, "名称须为1到80字，介绍最多500字，特征简介须为50字；请先生成推荐简介")
		return
	}
	source, err := s.store.GetLiveAnchorStyle(r.Context(), input.TenantID, input.StyleID)
	if err != nil {
		writeError(w, 404, "源主播不存在")
		return
	}
	item, err := s.store.PublishAnchorRecommendation(r.Context(), source, actor.UserID, input.Name, input.Introduction, input.Summary, input.SourceSHA, input.Generation)
	if errors.Is(err, appdb.ErrAnchorRecommendationConflict) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "发布系统推荐失败："+err.Error())
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) systemAnchorRecommendationWithdraw(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, anchorRecommendationPermission)
	if !ok {
		return
	}
	id, ok := liveAnchorStylePathID(w, r, "recommendationID", "推荐主播")
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version_no"`
	}
	if readJSON(w, r, &input) != nil || input.Version <= 0 {
		writeError(w, 400, "推荐版本无效")
		return
	}
	err := s.store.WithdrawAnchorRecommendation(r.Context(), id, input.Version, actor.UserID)
	if errors.Is(err, appdb.ErrAnchorRecommendationConflict) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "下架推荐失败")
		return
	}
	writeJSON(w, 200, map[string]any{"withdrawn": true})
}
