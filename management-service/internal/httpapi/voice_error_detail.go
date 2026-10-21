package httpapi

import (
	"fmt"
	"strings"

	"livecompanion/management/internal/errordetail"
	"livecompanion/management/internal/model"
)

func voiceAvailabilityError(name, status, kind string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "当前选定声音"
	} else {
		name = "“" + name + "”"
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "ready" {
		return nil
	}
	code, reason, action := "voice_not_ready", "尚不可用", "检查声音档案状态后重新选择并保存方案。"
	switch status {
	case "disabled":
		code, reason, action = "voice_disabled", "已停用", "重新选择可用声音并保存方案后再开始直播。"
	case "failed", "error", "rejected":
		code, reason, action = "voice_clone_failed", "制作失败", "查看声音制作失败记录，修复或重新制作声音后再保存方案。"
	case "pending", "processing", "training", "creating", "running", "cloning":
		code, reason, action = "voice_clone_pending", "仍在制作中", "等待声音状态变为可用，再保存方案并开始直播。"
	default:
		reason = fmt.Sprintf("状态不可用（%s）", errordetail.Safe(status))
	}
	return errordetail.New(code, "声音配置校验", name+kind+reason+"，无法使用该声音。", action, nil)
}

func selectedCloneProfileError(profile model.VoiceProfile) error {
	return voiceAvailabilityError(profile.Name, profile.CloneStatus, "声音")
}

func selectedCloneBindingError(name string, binding model.VoiceModelBinding) error {
	if err := voiceAvailabilityError(name, binding.Status, "所选模型绑定"); err != nil {
		return err
	}
	if strings.TrimSpace(binding.VoiceID) == "" {
		return errordetail.New("voice_id_missing", "声音配置校验", "所选声音模型绑定缺少音色编号。", "重新制作该模型的声音并保存方案。", nil)
	}
	return nil
}
