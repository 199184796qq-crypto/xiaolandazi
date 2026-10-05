package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/styleoverlay"
	"livecompanion/management/pkg/styleplugin"
)

type liveAnchorStylePluginCatalogItem struct {
	Manifest            styleplugin.Manifest `json:"manifest"`
	ActivationSupported bool                 `json:"activation_supported"`
	ActivationReason    string               `json:"activation_reason,omitempty"`
}

func (s *Server) liveAnchorStylePluginCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	if s.stylePlugins == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []liveAnchorStylePluginCatalogItem{}, "available": false})
		return
	}
	manifests := s.stylePlugins.Manifests()
	items := make([]liveAnchorStylePluginCatalogItem, 0, len(manifests))
	for _, manifest := range manifests {
		item := liveAnchorStylePluginCatalogItem{Manifest: manifest}
		switch {
		case manifest.Mode != styleplugin.ModeDeclarative:
			item.ActivationReason = "可信执行器已注册，但需要接入原子声音与字幕链路后才能在直播方案中启用"
		case !pluginSupportsScene(manifest, "mainline"):
			item.ActivationReason = "当前方案级插件必须支持主线场景"
		default:
			item.ActivationSupported = true
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "available": true})
}

func pluginSupportsScene(manifest styleplugin.Manifest, scene string) bool {
	for _, current := range manifest.Scenes {
		if current == scene {
			return true
		}
	}
	return false
}

func (s *Server) liveAgentPlanStylePluginActivate(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, planID, ok := s.styleOverlayTenantAndPlan(w, r, true)
	if !ok {
		return
	}
	if s.stylePlugins == nil {
		writeError(w, http.StatusServiceUnavailable, "主播风格插件目录尚未加载")
		return
	}
	var input struct {
		ExpectedRevision int64          `json:"expected_revision"`
		PluginID         string         `json:"plugin_id"`
		PluginVersion    string         `json:"plugin_version"`
		Strength         int            `json:"strength"`
		Parameters       map[string]any `json:"parameters"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "插件实例格式错误")
		return
	}
	manifest, exists := s.stylePlugins.Resolve(strings.TrimSpace(input.PluginID), strings.TrimSpace(input.PluginVersion))
	if !exists {
		writeError(w, http.StatusNotFound, "插件或指定版本不存在")
		return
	}
	if manifest.Mode != styleplugin.ModeDeclarative {
		writeError(w, http.StatusConflict, "该插件需要可信执行链，当前不能作为提示词插件启用")
		return
	}
	if !pluginSupportsScene(manifest, "mainline") {
		writeError(w, http.StatusUnprocessableEntity, "该插件不支持主线场景")
		return
	}
	if input.Strength == 0 {
		input.Strength = 50
	}
	instanceToken, err := styleoverlay.NewID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成插件实例标识失败")
		return
	}
	instance := styleplugin.Instance{
		APIVersion: styleplugin.InstanceAPIVersion, InstanceID: "binding-" + instanceToken,
		PluginID: manifest.ID, PluginVersion: manifest.Version, Enabled: true,
		Strength: input.Strength, Parameters: input.Parameters,
	}
	if _, err := styleplugin.EffectiveParameters(manifest, instance); err != nil {
		writeError(w, http.StatusBadRequest, "插件参数无效："+err.Error())
		return
	}
	profile, err := s.store.GetLiveAgentPlanStyleOverlay(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格失败")
		return
	}
	if profile.Revision != input.ExpectedRevision {
		writeError(w, http.StatusConflict, appdb.ErrLiveAgentPlanStyleOverlayConflict.Error())
		return
	}
	for _, item := range profile.Items {
		if item.Plugin != nil && item.Plugin.PluginID == manifest.ID {
			writeError(w, http.StatusConflict, "该插件已经加入当前主播，可在叠加风格中暂停或删除")
			return
		}
		if item.Plugin != nil {
			existing, found := s.stylePlugins.Resolve(item.Plugin.PluginID, item.Plugin.PluginVersion)
			if found && styleplugin.ManifestsConflict(manifest, existing) {
				writeError(w, http.StatusConflict, fmt.Sprintf("插件 %s 与当前主播已启用的 %s 冲突", manifest.Name, existing.Name))
				return
			}
		}
	}
	item, err := buildDeclarativePluginOverlay(r.Context(), s.stylePlugins, manifest, instance)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "插件编译失败："+err.Error())
		return
	}
	items, err := styleoverlay.NormalizeItems(append(profile.Items, item))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	profile, err = s.store.SaveLiveAgentPlanStyleOverlay(r.Context(), tenantID, planID, actor.UserID, input.ExpectedRevision, items)
	if errors.Is(err, appdb.ErrLiveAgentPlanStyleOverlayConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播风格插件失败")
		return
	}
	indexed := s.indexConfirmedStyleOverlays(r.Context(), profile)
	if err := s.hotReloadLiveAgentPlanRooms(r.Context(), tenantID, planID, "style_plugin"); err != nil {
		writeError(w, http.StatusBadGateway, "插件已保存，直播间同步失败，请重试："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": profile, "semantic_indexed": indexed, "item": item})
}

func buildDeclarativePluginOverlay(ctx context.Context, registry *styleplugin.Registry, manifest styleplugin.Manifest, instance styleplugin.Instance) (model.LiveAnchorStyleOverlayItem, error) {
	mainline, err := registry.Compile(ctx, instance, styleplugin.CompileContext{Scene: "mainline", TargetChars: 1000})
	if err != nil {
		return model.LiveAnchorStyleOverlayItem{}, err
	}
	interactionInstruction := "短互动不启用此插件。"
	if pluginSupportsScene(manifest, "interaction") {
		interaction, compileErr := registry.Compile(ctx, instance, styleplugin.CompileContext{Scene: "interaction", TargetChars: 120})
		if compileErr != nil {
			return model.LiveAnchorStyleOverlayItem{}, compileErr
		}
		if strings.TrimSpace(interaction.Instruction) != "" {
			interactionInstruction = interaction.Instruction
		}
	}
	seriousInstruction := "投诉、售后和事实澄清等严肃场景停用此插件。"
	if pluginSupportsScene(manifest, "serious") {
		serious, compileErr := registry.Compile(ctx, instance, styleplugin.CompileContext{Scene: "serious", TargetChars: 160, Serious: true})
		if compileErr != nil {
			return model.LiveAnchorStyleOverlayItem{}, compileErr
		}
		if strings.TrimSpace(serious.Instruction) != "" {
			seriousInstruction = serious.Instruction
		}
	}
	parameters, err := styleplugin.EffectiveParameters(manifest, instance)
	if err != nil {
		return model.LiveAnchorStyleOverlayItem{}, err
	}
	maximum := pluginIntegerParameter(parameters, "max_per_1000_chars")
	if maximum > 20 {
		maximum = 20
	}
	interactionMaximum := 0
	if maximum > 0 && pluginSupportsScene(manifest, "interaction") {
		interactionMaximum = 1
	}
	overlayID, err := styleoverlay.NewID()
	if err != nil {
		return model.LiveAnchorStyleOverlayItem{}, err
	}
	rule := model.LiveAnchorStyleOverlayRule{
		Version: model.LiveAnchorStyleOverlayVersion, Category: overlayCategory(manifest.Category),
		Label: manifest.Name, Application: "occasional", Strength: instance.Strength,
		MainlineInstruction: mainline.Instruction, InteractionInstruction: interactionInstruction,
		SeriousInstruction: seriousInstruction, MainlineMinPer1000Chars: 0,
		MainlineMaxPer1000Chars: maximum, InteractionMaxOccurrences: interactionMaximum,
		MicroActions: append([]string(nil), mainline.MicroActions...),
		Avoid:        []string{"不要固定位置出现，不要连续堆叠，不要每段机械重复"}, Confidence: 100,
	}
	rule, err = styleoverlay.NormalizeRule(rule)
	if err != nil {
		return model.LiveAnchorStyleOverlayItem{}, err
	}
	return model.LiveAnchorStyleOverlayItem{
		ID: overlayID, SourceText: "启用插件：" + manifest.Name, ExplanationText: manifest.Description,
		Enabled: true, Rule: rule, InterpretationSource: "plugin:" + manifest.ID + "@" + manifest.Version,
		LearningBasis: "plugin_manifest",
		Plugin: &model.LiveAnchorStylePluginBinding{
			InstanceID: instance.InstanceID, PluginID: manifest.ID, PluginVersion: manifest.Version, Parameters: parameters,
		},
	}, nil
}

func overlayCategory(category string) string {
	switch category {
	case "humor", "rhythm", "lexical", "storytelling", "interaction_delivery":
		return category
	case "local_flow":
		return "structure"
	default:
		return "delivery_other"
	}
}

func pluginIntegerParameter(parameters map[string]any, name string) int {
	value, exists := parameters[name]
	if !exists {
		return 0
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	var result int
	if err := json.Unmarshal(raw, &result); err != nil || result < 0 {
		return 0
	}
	return result
}
