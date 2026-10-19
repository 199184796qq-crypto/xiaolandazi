package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"
	"unicode"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	lib "livecompanion/management/internal/speechlibrary"
)

var errFirstOpeningRepeated = errors.New("首次开场与近期本方案开场重复")
var errFirstOpeningPreparation = errors.New("首次开场避重准备未完成")

func firstOpeningNormalized(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
func firstOpeningRepeated(text string, history []string) bool {
	n := firstOpeningNormalized(text)
	if n == "" {
		return false
	}
	for _, previous := range history {
		old := firstOpeningNormalized(previous)
		if n == old || openingNearlyIdentical([]rune(n), []rune(old)) {
			return true
		}
	}
	return false
}

// Reject tiny edits to a long opening (punctuation, a nickname or a few words),
// without treating common product facts as evidence that two different scripts
// are duplicates. This is a narrow edit-distance guard, not semantic approval.
func openingNearlyIdentical(a, b []rune) bool {
	if min(len(a), len(b)) < 60 {
		return false
	}
	limit := max(6, max(len(a), len(b))/50)
	if len(a)-len(b) > limit || len(b)-len(a) > limit {
		return false
	}
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range a {
		prev := row[0]
		row[0] = i + 1
		for j, y := range b {
			old := row[j+1]
			cost := 0
			if x != y {
				cost = 1
			}
			row[j+1] = min(row[j+1]+1, row[j]+1, prev+cost)
			prev = old
		}
	}
	return row[len(b)] <= limit
}
func initialOpeningSegment(state *anchorcycle.State, segment anchorcycle.Segment) bool {
	if state.Surface != "formal_live" || segment.Phase != "opening" {
		return false
	}
	if segment.OpeningVersion == "完整版" {
		return true
	}
	return segment.OpeningVersion == "" && state.AcceptedOffset == 0 && len(state.Accepted) > 0 && state.Accepted[0].Selected.ID == segment.Selected.ID
}
func (s *Server) openingHistoryRepository() lib.OpeningHistoryRepository {
	if h, ok := s.speechLibrary.(lib.OpeningHistoryRepository); ok {
		return h
	}
	if s.store != nil {
		return s.store
	}
	return nil
}
func (s *Server) loadFirstOpeningHistory(ctx context.Context, state *anchorcycle.State) error {
	if state.FirstOpeningHistoryLoaded {
		return nil
	}
	h := s.openingHistoryRepository()
	if h == nil {
		state.FirstOpeningHistoryLoaded = true
		return nil
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: "opening", OpeningVersion: "完整版", Task: state.Task()})
	if e != nil {
		return e
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	history, e := h.RecentFirstOpenings(readCtx, sc, 5)
	if e != nil {
		return fmt.Errorf("读取本方案开场避重历史：%w", e)
	}
	state.FirstOpeningHistory, state.FirstOpeningHistoryLoaded = history, true
	return nil
}

// This wrapper leaves the shared generation implementation and its complete P0
// pipeline intact. Only the first opening carries a bounded user-side directive.
func (s *Server) completeLiveCycleText(ctx context.Context, state *anchorcycle.State) (agentgateway.Response, []agentgateway.Message, error) {
	if !state.FirstLiveOpening() {
		return s.completeCycleText(ctx, state)
	}
	if e := s.loadFirstOpeningHistory(ctx, state); e != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("%w：%v", errFirstOpeningPreparation, e)
	}
	mother, e := interactionpolicy.Read("first-live-opening.md")
	if e != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("%w：%v", errFirstOpeningPreparation, e)
	}
	return generateDistinctFirstOpening(state, string(mother), func() (agentgateway.Response, []agentgateway.Message, error) { return s.completeCycleText(ctx, state) })
}

func (s *Server) initialLegacyOpeningPrompt(ctx context.Context, state *anchorcycle.State, prompt string) (string, error) {
	if !state.FirstLiveOpening() {
		return prompt, nil
	}
	if e := s.loadFirstOpeningHistory(ctx, state); e != nil {
		return "", e
	}
	mother, e := interactionpolicy.Read("first-live-opening.md")
	if e != nil {
		return "", e
	}
	return prompt + "\n\n" + firstOpeningDirective(state, string(mother)), nil
}

func firstOpeningDirective(state *anchorcycle.State, mother string) string {
	// 历史开场原文不再注入 prompt：完整历史片段被模型当范文照抄，是「开场方式锁死 + 正文复读」
	// 的主要复读源。卖点/角度/开场方式已由程序随机，模型无需参考历史原文去避重。
	// 这里只保留「这是第一次开场、现场写新表达」的母版指令，不携带任何历史片段。
	_ = state
	return mother
}

func generateDistinctFirstOpening(state *anchorcycle.State, mother string, generate func() (agentgateway.Response, []agentgateway.Message, error)) (agentgateway.Response, []agentgateway.Message, error) {
	previousFeedback := state.PendingFeedback
	defer func() { state.PendingFeedback = previousFeedback }()
	// 只注入「第一次开场、现场写新表达」的指令（first-live-opening.md），不注入历史开场原文。
	state.PendingFeedback = previousFeedback + "\n" + firstOpeningDirective(state, mother)
	var response agentgateway.Response
	var messages []agentgateway.Message
	var e error
	for attempt := 0; attempt < 2; attempt++ {
		response, messages, e = generate()
		// firstOpeningRepeated 仍做程序侧去重校验（生成后比对历史，不把历史原文喂给模型）。
		if e != nil || !firstOpeningRepeated(response.Text, state.FirstOpeningHistory) {
			return response, messages, e
		}
	}
	return response, messages, errFirstOpeningRepeated
}
func (s *Server) rememberFirstLiveOpening(ctx context.Context, state *anchorcycle.State, candidate anchorcycle.Candidate) error {
	h := s.openingHistoryRepository()
	if h == nil {
		return nil
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: "opening", OpeningVersion: "完整版", Task: state.Task(), Selected: candidate})
	if e == nil {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		e = h.RememberFirstOpening(persist, sc, cycleVoiceText(candidate))
	}
	return e
}

// openingModes 是开场「8 类开场方式」的枚举。第一次选材调用只允许模型
// 从这 8 个里选一个，程序据此决定第二次写话术的钩子方向。
var openingModes = []string{"欢迎型", "利益钩子型", "悬念型", "痛点型", "身份背书型", "互动型", "自嘲亲和型", "直接开讲型"}

// openingAngleCandidate 是开场选材候选池里单个「角度」候选，含程序侧归属信息。
// Common 标记该卖点是否公共卖点（门店/停车/环境等跨商品共享），LinkKey 是商品链接（商品专属卖点才有）。
type openingAngleCandidate struct {
	Angle         string
	TargetAudience string
	PainPoint     string
	LinkKey       string
	Common        bool
}

// openingSelectionPool 从候选池（state.Queue）提取「卖点×角度」候选，按卖点归并。
// 返回「卖点 → 该卖点下的角度候选列表」，每个角度候选带 LinkKey（商品链接）与 Common（是否公共卖点），
// 供程序按 RunID 轮换选卖点、模型在选中卖点内挑角度，并据此分流事实卡（商品专属按 LinkKey 筛、公共放开 branding）。
// 只取非 lecture 的任务，去重（卖点×角度去重）。
func openingSelectionPool(state *anchorcycle.State) map[string][]openingAngleCandidate {
	seen := map[string]bool{}
	// order 记录卖点首次出现顺序，保证结果稳定（map 遍历顺序随机）。
	var order []string
	byPoint := map[string][]openingAngleCandidate{}
	for _, task := range state.Queue {
		if task.ProductMode == "lecture" {
			continue
		}
		if task.SellingPoint == "" {
			continue
		}
		key := task.SellingPoint + "×" + task.Angle
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, exists := byPoint[task.SellingPoint]; !exists {
			order = append(order, task.SellingPoint)
		}
		// 记录该角度绑定的商品链接（裸 id，如 "1"/"2"），供写话术对齐事实卡。
		// 用 ProductLinks()（= AllowedLinksJSON）而非 LinkKey，因为 HookFacts 筛选事实用的正是它。
		linkKey := ""
		if links := task.ProductLinks(); len(links) > 0 {
			linkKey = strings.Join(links, ",")
		}
		byPoint[task.SellingPoint] = append(byPoint[task.SellingPoint], openingAngleCandidate{
			Angle:          task.Angle,
			TargetAudience: task.TargetAudience,
			PainPoint:      task.PainPoint,
			LinkKey:        linkKey,
			Common:         task.Common,
		})
	}
	// 用有序结构返回，避免 map 随机遍历导致轮换不稳定。
	pool := map[string][]openingAngleCandidate{}
	for _, p := range order {
		pool[p] = byPoint[p]
	}
	return pool
}

// openingPointForRun 用 RunID 做确定性哈希，从卖点列表里轮换选一个卖点。
// 同一场直播 RunID 相同 → 卖点稳定；不同场 RunID 不同 → 卖点轮换，
// 避免角度多的卖点（如「6人餐内容详解」6个角度）被模型反复选中。
func openingPointForRun(runID string, points []string) string {
	if len(points) == 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(runID + ":opening-point"))
	return points[int(h.Sum32()%uint32(len(points)))]
}

// completeOpeningSelection 是首场开场「两次调用」的第一次：程序按 RunID 轮换定「卖点」，
// 模型在该卖点下挑一个最勾人的「角度」，输出纯 JSON。
// 结果（卖点=程序定 + 角度=模型挑 + 开场方式=程序定）写回 state.OpeningSelection，第二次写话术据此聚焦。
func (s *Server) completeOpeningSelection(ctx context.Context, state *anchorcycle.State, profile agentgateway.SpeechModel) error {
	pool := openingSelectionPool(state)
	if len(pool) == 0 {
		// 候选池为空（无卖点×角度），不选材，直接走原路径。
		return nil
	}
	// 卖点由程序按 RunID 轮换：模型每次倾向选角度最多的卖点（如「6人餐内容详解」6个角度），
	// 导致卖点维度锁死。这里先程序定卖点，再让模型在该卖点内挑角度。
	points := make([]string, 0, len(pool))
	for p := range pool {
		points = append(points, p)
	}
	sort.Strings(points) // 排序保证轮换稳定（map 遍历顺序随机）
	point := openingPointForRun(state.RunID, points)
	if point == "" {
		return nil
	}
	angles := pool[point]
	// 给模型看的「可说」只暴露角度/参考人群/观众顾虑，不暴露 LinkKey/Common 这类程序元数据。
	modelAngles := make([]map[string]string, 0, len(angles))
	for _, a := range angles {
		modelAngles = append(modelAngles, map[string]string{
			"角度":   a.Angle,
			"参考人群": a.TargetAudience,
			"观众顾虑": a.PainPoint,
		})
	}
	selection := map[string]any{
		"必说": "你是直播开场策划。开场卖点已由程序选定为「" + point + "」，请从下方该卖点的「角度候选」里挑一个最能勾住观众的角度作为开场主线。只输出一个 JSON 对象，不要任何其他文字、不要 markdown 代码块。JSON 格式：{\"角度\":\"...\"}。",
		"可说": modelAngles,
		"不提": fourLayerNotMentionText,
		"改写": "只做选择，不写正文、不解释、不编候选之外的内容、不输出除 JSON 外的任何字符。",
	}
	raw, _ := json.Marshal(selection)
	// 选材加随机性：temperature 非零，让模型每次从该卖点的角度里挑出不同的角度，
	// 否则默认确定性输出，永远选中同一个「最优」答案。
	selTemp := 0.8
	req := agentgateway.Request{
		Stage: "opening_selection",
		Model: profile.Model,
		Messages: []agentgateway.Message{{
			Role:    "user",
			Content: string(raw),
		}},
		ResponseFormat:  agentgateway.ResponseText,
		MaxTokens:       300,
		EnableThinking:  false,
		Temperature:     &selTemp,
		CacheSystemPrompt: true,
		Timeout:         time.Duration(profile.TimeoutMS) * time.Millisecond,
	}
	req, err := agentgateway.PrepareTextRequest(profile, req)
	if err != nil {
		return err
	}
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(ctx, "opening_selection", profile.ID, req)
	if err != nil {
		return err
	}
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(ctx, profile, req)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	_ = s.finishModelExchange(ctx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	if err != nil {
		return err
	}
	sel, parseErr := parseOpeningSelection(response.Text)
	if parseErr != nil {
		// 选材解析失败不阻塞：保留空选材，走原路径（用现有 opening-hook 逻辑）。
		state.Warn("开场选材解析失败，退回原开场钩子流程：" + parseErr.Error())
		return nil
	}
	// 卖点由程序轮换决定（模型只挑了角度），开场方式也由程序轮换。
	sel.SellingPoint = point
	sel.OpeningMode = openingModeForRun(state.RunID)
	// 反查模型选中角度对应的商品链接 + 公共标记，写进 sel，供第二次写话术分流事实卡。
	// 否则卖点/角度换了，但写话术仍按队列原 task 筛事实，导致卖点↔事实错配（实测 25572/25612/25669）。
	if strings.TrimSpace(sel.Angle) != "" {
		for _, a := range angles {
			if a.Angle == sel.Angle {
				sel.LinkKey = a.LinkKey
				sel.Common = a.Common
				break
			}
		}
	}
	state.OpeningSelection = sel
	return nil
}

// openingModeForRun 用 RunID 做确定性哈希，从 8 类开场方式里轮换选一个。
// 同一场直播 RunID 相同 → 方式稳定；不同场 RunID 不同 → 方式轮换，避免模型锁死单一方式。
func openingModeForRun(runID string) string {
	if len(openingModes) == 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(runID + ":opening-mode"))
	return openingModes[int(h.Sum32()%uint32(len(openingModes)))]
}

// openingWelcomeTemplateForRun 用 RunID 做确定性哈希，从「开头欢迎 + 留人」样板库里轮换选一条。
// 同一场直播 RunID 相同 → 样板稳定；不同场 RunID 不同 → 样板轮换。
// 样板只做「欢迎 + 留人」两句的骨架，模型照着骨架组织措辞、把 {称呼} 换成主播自己的称呼风格，
// 后面的 8 类开场方式钩子逻辑不受影响（那是另一个字段、另一条轮换链）。
func openingWelcomeTemplateForRun(runID string) string {
	raw, err := interactionpolicy.Read("opening-welcome-templates.json")
	if err != nil {
		return ""
	}
	var templates []string
	if json.Unmarshal(raw, &templates) != nil || len(templates) == 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(runID + ":opening-welcome"))
	return templates[int(h.Sum32()%uint32(len(templates)))]
}

// parseOpeningSelection 从模型输出里抠出 JSON 并解析成 OpeningSelection。
func parseOpeningSelection(text string) (*anchorcycle.OpeningSelection, error) {
	text = strings.TrimSpace(text)
	// 去掉可能的 markdown 代码块包裹。
	if i := strings.Index(text, "{"); i >= 0 {
		text = text[i:]
		if j := strings.LastIndex(text, "}"); j >= 0 {
			text = text[:j+1]
		}
	}
	var sel anchorcycle.OpeningSelection
	if err := json.Unmarshal([]byte(text), &sel); err != nil {
		return nil, err
	}
	// 卖点由程序 openingPointForRun 轮换决定，模型只输出「角度」。
	if strings.TrimSpace(sel.Angle) == "" {
		return nil, fmt.Errorf("选材结果缺少角度")
	}
	// 开场方式也不由模型输出，由程序 openingModeForRun 轮换决定；
	// 若模型仍带回了「开场方式」，仅做宽松校验（非 8 类之一则忽略，由程序覆盖）。
	if sel.OpeningMode != "" {
		validMode := false
		for _, m := range openingModes {
			if sel.OpeningMode == m {
				validMode = true
				break
			}
		}
		if !validMode {
			sel.OpeningMode = ""
		}
	}
	return &sel, nil
}
