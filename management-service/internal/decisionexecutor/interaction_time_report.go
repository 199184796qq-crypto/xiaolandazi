package decisionexecutor

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type timeReportStrategy struct {
	worker *Worker
}

func (s timeReportStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	now := time.Now().In(beijingZone)
	part := timeOfDay(now)
	if s.worker == nil || s.worker.agent == nil {
		return timeReportFallback(part)
	}
	system := "你是直播主播。现在要做一次自然的时间互动，不要像报时钟一样只报几点。" +
		"\n结合当前时间、刚才讲的内容和观众氛围，随口带出时间：可以是问候、陪伴、提醒吃饭/休息、下播安排，或一句时间感慨。" +
		"\n称呼可有可无，不要每次都硬加称呼；只输出一两句口语，不要反问，不要推销，不要编造具体下播时间、福利或承诺。" +
		"\n当前北京时间：" + now.Format("2006-01-02 15:04")
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
		return timeReportFallback(part)
	}
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
