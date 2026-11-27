package decisionexecutor

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"livecompanion/management/internal/model"
)

var likeThanksTemplates = [...]string{
	"谢谢%s的点赞支持。",
	"谢谢%s送来的赞。",
	"%s，点赞收到啦，谢谢支持。",
	"感谢%s给咱们点赞。",
	"%s，谢谢点赞，咱们接着聊。",
	"收到%s的点赞啦，感谢支持。",
	"谢谢%s，给咱们点了赞。",
	"%s的赞收到啦，谢谢。",
	"感谢%s的点赞，咱们继续。",
	"谢谢%s给直播间点赞。",
}

var followThanksTemplates = [...]string{
	"谢谢%s的关注，欢迎常来。",
	"感谢%s关注咱们直播间。",
	"%s，谢谢关注，欢迎常来逛逛。",
	"谢谢%s，关注收到啦。",
	"%s，感谢关注，咱们接着聊。",
	"收到%s的关注啦，谢谢支持。",
	"欢迎%s常来，谢谢关注。",
	"谢谢%s的关注支持。",
	"感谢%s关注，欢迎一起聊聊。",
	"谢谢%s关注咱们，欢迎常来。",
}

// Each new decision gets an independent, equal-weight dice roll. Seed the local
// generator with immutable task identity so retries, including after a worker
// restart, keep the same paid text. Live cursors, deadlines and wall time are
// deliberately excluded. This stores no unbounded per-task random state.
func behaviorThanksText(session model.LiveRuntimeSession, decision, template, address string) (string, error) {
	var choices []string
	switch template {
	case "like_v1":
		choices = likeThanksTemplates[:]
	case "follow_v1":
		choices = followThanksTemplates[:]
	default:
		return "", fmt.Errorf("未知点赞关注模板")
	}
	if session.TenantID <= 0 || session.RoomID <= 0 || session.ID <= 0 || strings.TrimSpace(decision) == "" || strings.TrimSpace(address) == "" {
		return "", fmt.Errorf("点赞关注模板缺少场次、任务或称呼")
	}
	identity, _ := json.Marshal([]any{"behavior-thanks-v1", session.TenantID, session.RoomID, session.ID, session.ExternalID, decision, template})
	seed := sha256.Sum256(identity)
	roll := rand.New(rand.NewChaCha8(seed)).IntN(len(choices))
	return fmt.Sprintf(choices[roll], address), nil
}
