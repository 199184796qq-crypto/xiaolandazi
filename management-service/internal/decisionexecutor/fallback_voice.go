package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"

	"livecompanion/management/internal/factbound"
	"livecompanion/management/internal/fallbackline"
	"livecompanion/management/internal/model"
)

// boundaryLineStage is pinned to the operator's review model, like the rest of
// the interaction answers.
const boundaryLineStage = "interaction_boundary_line"

const boundaryRecentLimit = 8

// boundaryLine asks for four candidates in one call and keeps the best one: the
// program filters safety, on-topic mention, length and repetition before picking.
func (w *Worker) boundaryLine(ctx context.Context, session model.LiveRuntimeSession, class, question string, uncovered []string) string {
	if w == nil || w.agent == nil {
		return fallbackTemplate(class, nil)
	}
	for attempt := 0; attempt < 2; attempt++ {
		recent := w.recentBoundaryLines(session.RoomID)
		request := fallbackline.Request{Class: class, Question: question, Uncovered: uncovered, Recent: recent}
		system, user := fallbackline.CandidatesPrompt(request, 4)
		raw := w.completeWelcome(ctx, session, boundaryLineStage, system, user, 240)
		line, err := fallbackline.Pick(request, fallbackline.ParseCandidates(raw))
		if err != nil {
			continue
		}
		w.rememberBoundaryLine(session.RoomID, line)
		return line
	}
	line := fallbackTemplate(class, uncovered)
	w.rememberBoundaryLine(session.RoomID, line)
	return line
}

func fallbackTemplate(class string, uncovered []string) string {
	if line := factbound.BoundaryTemplateFor(class, uncovered); line != "" {
		return line
	}
	return interactionNoFactBoundary
}

func (w *Worker) recentBoundaryLines(roomID int64) []string {
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	return append([]string(nil), w.recentBoundary[roomID]...)
}

func (w *Worker) rememberBoundaryLine(roomID int64, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	if w.recentBoundary == nil {
		w.recentBoundary = map[int64][]string{}
	}
	lines := append(w.recentBoundary[roomID], line)
	if len(lines) > boundaryRecentLimit {
		lines = lines[len(lines)-boundaryRecentLimit:]
	}
	w.recentBoundary[roomID] = lines
}

// partialAnswerLine answers the viewer's actual question with the confirmed
// facts, then gives a generic next step for the unconfirmed part. The model
// must see the question and intent; facts alone can only produce a summary.
func (w *Worker) partialAnswerLine(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, question, facts string, missing []string) string {
	purpose := ""
	var intent struct {
		Purpose string `json:"purpose"`
	}
	if json.Unmarshal(item.PurposeKnowledge, &intent) == nil {
		purpose = strings.TrimSpace(intent.Purpose)
	}
	system := "你是直播口播合成器。观众的问题已经理解，请直接回答。" +
		"\n" + beijingTimeLine() +
		"\n先回答已确认事实；对未确认部分，可以给通用处理建议（例如联系商家客服/店铺客服申请或核实），但不得说已经批准；具体结果以客服/实际确认为准。" +
		"\n涉及订单修改、延期、售后，优先引导商家客服/店铺客服；不要指定具体物流商客服或平台客服，除非事实明确。" +
		"\n不要罗列无关事实，不要反问，不要编造退款、赔偿、免费服务或保证日期。" +
		"\n只输出一句自然、简短、可播出的回答。"
	user := "观众问题：" + question + recentContextLine(item)
	if purpose != "" {
		user += "\n意图：" + purpose
	}
	user += "\n已确认事实：\n" + factbound.SpeakableFacts(facts)
	if len(missing) > 0 {
		user += "\n未确认：" + strings.Join(missing, "、")
	}
	user += "\n请先答已确认部分，再针对未确认部分给下一步，并说明需要确认。"
	text := strings.TrimSpace(w.completeWelcome(ctx, session, "interaction_partial_answer", system, user, 140))
	return strings.TrimSpace(stripAwkwardAnswerLead(text))
}

// coveredFactsLine is the second try when the covered answer consisted only of
// hedging: state the confirmed facts, nothing else.
func (w *Worker) coveredFactsLine(ctx context.Context, session model.LiveRuntimeSession, facts string, avoid []string) string {
	system := "只陈述下面事实里已确认的信息，用一两句口语，不要评价问题。" +
		"\n禁止出现“没核实、没确认、说不好、不确定”这些词，也不要提到：" + strings.Join(avoid, "、") + "。"
	user := "已确认事实：\n" + factbound.SpeakableFacts(facts)
	text := strings.TrimSpace(w.completeWelcome(ctx, session, "interaction_covered_facts", system, user, 80))
	return strings.TrimSpace(stripAwkwardAnswerLead(text))
}

// stripHedges removes the covered answer's own hedging, because the boundary
// line says it better and saying it twice sounds like a robot.
var coveredHedgeWords = []string{"没核实", "没确认", "未确认", "未核实", "没有核实", "没有确认", "还没核实", "还没确认", "尚未核实", "尚未确认", "说不好", "不确定", "需要再确认", "无法确认"}

func stripHedges(text string) string {
	kept := make([]string, 0, 6)
	for _, sentence := range splitTTSSentences(text) {
		if strings.TrimSpace(sentence) == "\n" {
			kept = append(kept, sentence)
			continue
		}
		hedged := false
		for _, word := range coveredHedgeWords {
			if strings.Contains(sentence, word) {
				hedged = true
				break
			}
		}
		if !hedged {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

// softenTerms replaces an unconfirmed value with a vague reference, so the
// covered answer keeps the confirmed policy but never names the missing scope.
func softenTerms(text string, terms []string) string {
	out := strings.TrimSpace(text)
	for _, term := range terms {
		if strings.TrimSpace(term) == "" {
			continue
		}
		out = strings.ReplaceAll(out, term, "这个范围")
	}
	return strings.TrimSpace(out)
}
