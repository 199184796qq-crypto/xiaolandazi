package httpapi

import (
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"net/http"
	"strings"
	"time"
)

func voiceLibraryQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	library := strings.TrimSpace(r.URL.Query().Get("model"))
	if library != "" && library != ttsgateway.ProviderQwen && library != ttsgateway.TencentPremiumModel && !ttsgateway.IsCosyVoiceModel(library) {
		writeError(w, http.StatusBadRequest, "声音库模型无效")
		return "", false
	}
	return library, true
}

func profileLibraryModel(profile model.VoiceProfile) string {
	value, _ := profile.Config["target_model"].(string)
	return strings.TrimSpace(value)
}

func filterVoiceLibrary(library string, profiles []model.VoiceProfile) []model.VoiceProfile {
	items := make([]model.VoiceProfile, 0, len(profiles))
	for _, profile := range profiles {
		if ttsgateway.VoiceLibraryMatches(library, profileLibraryModel(profile)) {
			items = append(items, profile)
		}
	}
	return items
}

func validateVoiceLibraryBinding(profile model.VoiceProfile, target string) error {
	configured := profileLibraryModel(profile)
	if ttsgateway.IsCosyVoiceModel(configured) || ttsgateway.IsCosyVoiceModel(target) {
		if configured != target {
			return errors.New("这个声音属于其他模型，请在所选模型的声音库中重新上传或设计声音")
		}
	}
	return nil
}

// A provider-issued ID cannot be reassigned to another engine. Cross-engine
// compatibility exists only for an actual, server-created clone of the sample.
func validateReclonedVoiceBinding(profile model.VoiceProfile, binding model.VoiceModelBinding) error {
	if err := validateVoiceLibraryBinding(profile, binding.Model); err != nil {
		verified, _ := binding.Config["sample_recloned"].(bool)
		if !verified || profile.SampleAssetID == nil || binding.SampleAssetID != *profile.SampleAssetID || binding.VoiceID == profile.VoiceID || binding.Provider != ttsgateway.ProviderForModel(binding.Model) {
			return err
		}
	}
	return nil
}

func (s *Server) liveDesignVoiceProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	var input struct {
		Name        string `json:"name"`
		Model       string `json:"model"`
		Description string `json:"description"`
		AvatarKey   string `json:"avatar_key"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音设计参数格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	configuredModel, valid := s.configuredCustomVoiceModel(w, r, input.Model)
	if !valid {
		return
	}
	input.Model = configuredModel
	if input.Name == "" || len([]rune(input.Name)) > 80 || !ttsgateway.IsCosyVoiceModel(input.Model) || input.Description == "" || ttsgateway.CharacterCount(input.Description) > 500 {
		writeError(w, 400, "请填写声音名称和500字符以内的声音描述；当前声音配置需支持描述生成")
		return
	}
	if input.AvatarKey == "" {
		input.AvatarKey = "female_young"
	}
	if !validVoiceAvatarKey(input.AvatarKey) {
		writeError(w, 400, "请选择一个声音头像")
		return
	}
	limit, err := s.store.GetEffectiveCustomerRoomLimit(r.Context(), tenant)
	if err != nil {
		writeError(w, 500, "读取自定义声音额度失败")
		return
	}
	used, err := s.store.CountActiveVoiceProfiles(r.Context(), tenant)
	if err != nil {
		writeError(w, 500, "读取自定义声音数量失败")
		return
	}
	if used >= limit {
		writeError(w, 409, fmt.Sprintf("自定义声音已达到直播间权限上限（%d个）", limit))
		return
	}
	result, err := ttsgateway.NewFromEnv().DesignVoice(r.Context(), ttsgateway.DesignRequest{Model: input.Model, PreferredName: fmt.Sprintf("v%d%d", actor.UserID%10000, time.Now().Unix()%100000000), Description: input.Description, PreviewText: voicePreviewText})
	if err != nil {
		writeError(w, 502, "声音设计失败："+err.Error())
		return
	}
	assetID, duration, err := s.archiveGeneratedVoiceBytes(r.Context(), tenant, actor.UserID, result.PreviewAudio, "audio/wav", result.VoiceID, result.Model, "voice_design", supportRoomForMedia(r))
	if err != nil {
		writeError(w, 502, "归档设计声音失败："+err.Error())
		return
	}
	profile, err := s.store.SaveVoiceProfile(r.Context(), tenant, actor.UserID, 0, model.VoiceProfileInput{Name: input.Name, Provider: result.Provider, VoiceID: result.VoiceID, SampleAssetID: &assetID, CloneStatus: "ready", Config: map[string]any{"target_model": result.Model, "source": "customer_voice_design", "avatar_key": input.AvatarKey, "voice_description": input.Description, "sample_duration_ms": duration, "support_room_id": supportRoomForMedia(r)}})
	if err != nil {
		writeError(w, 500, "保存设计声音失败")
		return
	}
	binding, err := s.store.CreateVoiceModelBinding(r.Context(), tenant, actor.UserID, model.VoiceModelBindingInput{ProfileID: profile.ID, SampleAssetID: assetID, Provider: result.Provider, Model: result.Model, VoiceID: result.VoiceID, Rate: 1, Status: "ready", Config: map[string]any{"source": "customer_voice_design"}})
	if err != nil {
		writeError(w, 500, "保存设计声音模型绑定失败")
		return
	}
	profile.Config["binding_id"] = binding.ID
	writeJSON(w, http.StatusCreated, profile)
}
