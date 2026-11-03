package decisionexecutor

import (
	"context"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type timeReportStrategy struct {
	worker *Worker
}

func (s timeReportStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	now := time.Now().In(beijingZone)
	if s.worker != nil && s.worker.now != nil {
		now = s.worker.now().In(beijingZone)
	}
	part := timeOfDay(now)
	log.Printf("time report start room=%d tenant=%d part=%s now=%s mainline=%t", session.RoomID, session.TenantID, part, now.Format("2006-01-02 15:04:05"), strings.TrimSpace(item.OriginMainline) != "")
	if s.worker == nil || s.worker.agent == nil {
		log.Printf("time report fallback room=%d part=%s reason=no_agent", session.RoomID, part)
		return timeReportFallback(part)
	}
	system := "你是直播主播。现在要做一次自然的时间互动，不要像报时钟一样只报几点。" +
		"\n结合当前时间、刚才讲的内容和观众氛围，随口带出时间：可以是问候、陪伴、提醒吃饭/休息、下播安排，或一句时间感慨。" +
		"\n称呼可有可无，不要每次都硬加称呼；只输出一两句口语，不要反问，不要推销，不要编造具体下播时间、福利或承诺。" +
		"\n当前北京时间：" + now.Format("2006-01-02 15:04")
	effective, err := policy.LoadGeneration()
	if err != nil {
		log.Printf("time report fallback room=%d part=%s reason=policy_unavailable", session.RoomID, part)
		return timeReportFallback(part)
	}
	system += "\n优先使用当前模糊时段；若说具体时刻，必须与给定当前时刻一致。\n" + effective.PromptText
	user := "当前时段：" + part
	if strings.TrimSpace(item.OriginMainline) != "" {
		user += "\n刚才讲到：" + strings.TrimSpace(item.OriginMainline)
	}
	if recent := recentContextLine(item); recent != "" {
		user += recent
	}
	user += "\n请自然说一句带时间感的话。"
	text := strings.TrimSpace(s.worker.completeWelcome(ctx, session, "interaction_time_report", system, user, 80))
	text = strings.TrimSpace(stripAwkwardAnswerLead(text))
	if text == "" {
		log.Printf("time report fallback room=%d part=%s reason=empty_text", session.RoomID, part)
		return timeReportFallback(part)
	}
	log.Printf("time report generated room=%d part=%s len=%d text=%q", session.RoomID, part, len([]rune(text)), text)
	return text
}

func timeOfDay(now time.Time) string {
	switch hour := now.Hour(); {
	case hour < 6:
		return "凌晨"
	case hour < 11:
		return "早上"
	case hour < 13:
		return "中午"
	case hour < 18:
		return "下午"
	default:
		return "晚上"
	}
}

func timeReportFallback(part string) string {
	return "这会儿都" + part + "了，感谢老乡们还在直播间陪着我。"
}
