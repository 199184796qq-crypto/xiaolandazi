package decisionexecutor

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// warmupStrategy continues the current mainline instead of asking the audience
// an empty engagement question. A warmup without real context is skipped.
type warmupStrategy struct {
	worker *Worker
}

func (s warmupStrategy) Respond(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) string {
	if s.worker == nil || s.worker.agent == nil {
		return ""
	}
	topic := strings.TrimSpace(item.OriginMainline)
	if topic == "" {
		topic = strings.TrimSpace(item.Summary)
	}
	if topic == "" {
		return ""
	}
	system := "你是直播主播，正在接着刚才的讲解往下说。" +
		"\n只补一句到两句自然的话，承接当前内容；不要欢迎，不要问“大家有什么需求/觉得怎么样”，不要空泛互动。" +
		"\n可以使用当前商品事实，但不要硬推，不要编造价格、库存、时效或承诺。"
	user := "刚才讲到：" + topic
	if len(item.SampleQuestions) > 0 {
		user += "\n当前商品/主题：" + strings.Join(item.SampleQuestions, "；")
	}
	if len(item.RecentAnswers) > 0 {
		var recent []string
		for _, answer := range item.RecentAnswers {
			if strings.TrimSpace(answer.Reply) != "" {
				recent = append(recent, answer.Reply)
			}
		}
		if len(recent) > 0 {
			user += "\n最近说过：" + strings.Join(recent, "；")
		}
	}
	user += "\n请接着说一句。"
	text := strings.TrimSpace(s.worker.completeWelcome(ctx, session, "interaction_warmup", system, user, 80))
	text = strings.TrimSpace(stripAwkwardAnswerLead(text))
	if text == "" || strings.Contains(text, "欢迎来到直播间") {
		return ""
	}
	return text
}
