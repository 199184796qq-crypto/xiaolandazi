package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type qaPrepareInput struct {
	Text    string                       `json:"text"`
	Targets []model.MaterialObjectTarget `json:"object_targets"`
	Items   []qapair.Entry               `json:"items"`
}

func validQATargets(items []qapair.Entry, links []db.QALink, stores []model.MaterialStoreObject) error {
	knownLinks := map[int64]bool{}
	knownStores := map[string]bool{}
	for _, l := range links {
		knownLinks[l.ID] = true
	}
	for _, s := range stores {
		knownStores[s.Name] = true
	}
	chars := 0
	for _, e := range items {
		chars += len([]rune(e.Question + e.Answer + e.Conditions + e.Exceptions + strings.Join(e.Aliases, "")))
		for _, t := range e.Targets {
			if t.Kind == "product" && !knownLinks[t.LinkID] || t.Kind == "store" && !knownStores[t.StoreName] {
				return errors.New("归属不属于当前方案，请刷新后选择")
			}
		}
	}
	if chars > 20000 {
		return errors.New("本次问答总内容超过20000字，请分批保存")
	}
	return nil
}

func (s *Server) planQAPairs(w http.ResponseWriter, r *http.Request) {
	_, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	records, err := s.store.PlanQAPairs(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 500, "读取问答对失败")
		return
	}
	objects, links, objectRev, err := s.store.PlanQAObjects(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 500, "读取方案对象失败")
		return
	}
	catalog := db.QACatalog(records, objectRev)
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"revision": catalog.Revision, "object_revision": objectRev, "items": catalog.Items, "links": links, "stores": objects.Stores})
		return
	}
	var input qaPrepareInput
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	items := input.Items
	if input.Text != "" {
		if len(items) > 0 {
			writeError(w, 400, "请只提交文本或预览条目之一")
			return
		}
		items, err = qapair.Parse(input.Text, input.Targets)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	proposals, err := qapair.Prepare(items, records)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	normalized := []qapair.Entry{}
	for _, p := range proposals {
		normalized = append(normalized, p.Item)
	}
	if err = validQATargets(normalized, links, objects.Stores); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"revision": catalog.Revision, "object_revision": objectRev, "proposals": proposals})
}

type qaRecallMatch struct {
	Entry qapair.Entry `json:"entry"`
	Score float64      `json:"score"`
}

func (s *Server) recallPlanQAPairs(w http.ResponseWriter, r *http.Request) {
	_, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Question string `json:"question"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "召回请求格式不正确")
		return
	}
	question := strings.TrimSpace(input.Question)
	if question == "" || utf8.RuneCountInString(question) > 1000 {
		writeError(w, http.StatusBadRequest, "请输入 1 至 1000 字的弹幕")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	records, err := s.store.PlanQAPairs(ctx, tenant, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取问答对失败")
		return
	}
	result := struct {
		MinimumScore float64         `json:"minimum_score"`
		Matches      []qaRecallMatch `json:"matches"`
	}{MinimumScore: qapair.AnswerMinScore, Matches: []qaRecallMatch{}}
	now := time.Now()
	if !slices.ContainsFunc(records, func(record qapair.Record) bool { return qapair.Active(record.Entry, now) }) {
		writeJSON(w, http.StatusOK, result)
		return
	}

	embedder, _ := s.semanticMetrics.(danmakuEmbedder)
	if embedder == nil || !embedder.Enabled() || embedder.Model() == "" {
		writeError(w, http.StatusServiceUnavailable, "问答召回的向量服务暂不可用")
		return
	}
	modelName := embedder.Model()
	vectors, err := embedMaterialTexts(ctx, embedder, []string{question})
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成弹幕向量失败，请重试召回")
		return
	}
	entries := make(map[string]qapair.Entry, len(records))
	for _, record := range records {
		entries[record.Entry.ID] = record.Entry
	}
	for _, match := range qapair.PreviewCandidates(records, vectors[0], modelName, time.Now()) {
		result.Matches = append(result.Matches, qaRecallMatch{Entry: entries[match.ID], Score: match.Score})
	}
	writeJSON(w, http.StatusOK, result)
}

func qaOperationResponse(w http.ResponseWriter, op db.QAOperation) bool {
	if op.State == "done" {
		writeJSON(w, 200, op.Result)
		return true
	}
	if op.State == "unknown" {
		writeError(w, 409, db.ErrQAOperationUnknown.Error())
		return true
	}
	if op.State == "processing" {
		writeError(w, 409, db.ErrQAOperationPending.Error())
		return true
	}
	return false
}

func (s *Server) commitQAPairs(w http.ResponseWriter, r *http.Request) {
	actor, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input qapair.CommitInput
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`).MatchString(input.OperationID) || len(input.Revision) != 64 || len(input.ObjectRevision) != 64 {
		writeError(w, 400, "保存标识或版本不正确，请重新打开问答页")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	hash := qapair.Hash(input)
	op, found, err := s.store.QAOperation(ctx, tenant, plan, input.OperationID, hash)
	if err != nil {
		writeError(w, 409, "保存状态无法确认，请刷新，未重复调用向量")
		return
	}
	if found && qaOperationResponse(w, op) {
		return
	}
	if !found {
		records, e := s.store.PlanQAPairs(ctx, tenant, plan)
		if e != nil {
			writeError(w, 500, "读取原问答失败，尚未调用向量")
			return
		}
		objects, links, objectRev, e := s.store.PlanQAObjects(ctx, tenant, plan)
		if e != nil {
			writeError(w, 500, "读取对象失败，尚未调用向量")
			return
		}
		if objectRev != input.ObjectRevision || db.QACatalog(records, objectRev).Revision != input.Revision {
			writeError(w, 409, db.ErrQAPairsConflict.Error())
			return
		}
		var writes []qapair.Mutation
		var remove []string
		kept := 0
		if input.DeleteID != "" {
			if len(input.Items) > 0 {
				writeError(w, 400, "删除不能与保存同时提交")
				return
			}
			exists := false
			for _, r := range records {
				exists = exists || r.Entry.ID == input.DeleteID
			}
			if !exists {
				writeError(w, 409, "问答不存在，请刷新")
				return
			}
			remove = []string{input.DeleteID}
		} else {
			proposals, e := qapair.Prepare(input.Items, records)
			if e != nil {
				writeError(w, 400, e.Error())
				return
			}
			normalized := []qapair.Entry{}
			for _, p := range proposals {
				normalized = append(normalized, p.Item)
			}
			if e = validQATargets(normalized, links, objects.Stores); e != nil {
				writeError(w, 400, e.Error())
				return
			}
			writes, remove, kept, e = qapair.Changes(proposals, records, input.Choices)
			if e != nil {
				writeError(w, 400, e.Error())
				return
			}
			// Prepopulate exact unchanged question vectors before persisting the
			// operation. Model/config mismatch never mixes old and new vectors.
			for i := range writes {
				m := &writes[i]
				if m.Model != "" {
					continue
				}
				for _, old := range records {
					if old.Entry.ID == m.Entry.ID && qapair.Hash(qapair.Questions(old.Entry)) == qapair.Hash(qapair.Questions(m.Entry)) && old.Space == qapair.SpaceVersion && db.ValidateMaterialVectors(old.Vectors, len(qapair.Questions(old.Entry))) == nil {
						m.Model = old.Model
						m.Space = old.Space
						m.Vectors = old.Vectors
						break
					}
				}
			}
		}
		op = db.QAOperation{Writes: writes, Remove: remove, Kept: kept, Revision: input.Revision, ObjectRevision: input.ObjectRevision}
		var claimed bool
		op, claimed, err = s.store.BeginQAOperation(ctx, tenant, plan, actor.UserID, input.OperationID, hash, op)
		if err != nil {
			writeError(w, 409, "问答或对象已变化，尚未调用向量，请刷新核对")
			return
		}
		if !claimed {
			if qaOperationResponse(w, op) {
				return
			}
		} else {
			embedder, _ := s.semanticMetrics.(danmakuEmbedder)
			if err = fillQAVectors(ctx, op.Writes, embedder); err != nil {
				cleanup, c := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer c()
				_ = s.store.QAOperationUncertain(cleanup, tenant, plan, input.OperationID)
				writeError(w, 502, err.Error()+"；整批未采用，旧问答与输入保留。若重试可能再次计费")
				return
			}
			if err = s.store.QAOperationReady(ctx, tenant, plan, input.OperationID, op); err != nil {
				writeError(w, 500, "向量后保存状态待确认，旧问答保留；请保留输入并刷新")
				return
			}
		}
	}
	result, err := s.store.CommitQAOperation(ctx, tenant, plan, actor.UserID, input.OperationID)
	if errors.Is(err, db.ErrQAPairsConflict) {
		writeError(w, 409, "问答或对象已变化，整批未采用，原问答保留；已完成的向量没有重复请求")
		return
	}
	if err != nil {
		writeError(w, 500, "提交未确认，原问答保留；重试同一保存请求可恢复已完成的向量结果")
		return
	}
	writeJSON(w, 200, result)
}

func fillQAVectors(ctx context.Context, writes []qapair.Mutation, e danmakuEmbedder) error {
	texts := []string{}
	positions := map[string]int{}
	for _, m := range writes {
		if !m.Entry.Enabled || len(m.Entry.Targets) == 0 || m.Model != "" {
			continue
		}
		for _, q := range qapair.Questions(m.Entry) {
			if _, ok := positions[q]; !ok {
				positions[q] = len(texts)
				texts = append(texts, q)
			}
		}
	}
	if len(texts) == 0 {
		return nil
	}
	if e == nil || !e.Enabled() {
		return errors.New("服务端向量服务不可用，尚未发起向量请求")
	}
	modelName := e.Model()
	if modelName == "" {
		return errors.New("向量配置不完整")
	}
	vectors, err := embedMaterialTexts(ctx, e, texts)
	if err != nil {
		return errors.New("向量请求失败或结果待确认")
	}
	if db.ValidateMaterialVectors(vectors, len(texts)) != nil || e.Model() != modelName {
		return errors.New("向量结果不完整或配置变化")
	}
	for i := range writes {
		m := &writes[i]
		if !m.Entry.Enabled || len(m.Entry.Targets) == 0 || m.Model != "" {
			continue
		}
		m.Model = modelName
		m.Space = qapair.SpaceVersion
		m.Vectors = [][]float32{}
		for _, q := range qapair.Questions(m.Entry) {
			m.Vectors = append(m.Vectors, vectors[positions[q]])
		}
	}
	return nil
}

func (s *Server) organizeQAPairs(w http.ResponseWriter, r *http.Request) {
	_, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if strings.TrimSpace(input.Text) == "" || len([]rune(input.Text)) > 8000 {
		writeError(w, 400, "大白话整理限1至8000字，请分批")
		return
	}
	objects, links, _, err := s.store.PlanQAObjects(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 500, "读取对象失败，尚未调用模型")
		return
	}
	objectText, _ := json.Marshal(map[string]any{"links": links, "stores": objects.Stores})
	if len([]rune(string(objectText))) > 4000 {
		writeError(w, 400, "方案对象目录过长，请使用标准问答格式并手工选择归属")
		return
	}
	settings, err := s.store.OperatorSpeechModels(r.Context())
	if err != nil {
		writeError(w, 500, "读取审核模型失败")
		return
	}
	profile := settings.ReviewDefaultID
	if profile == "" {
		writeError(w, 400, "请先配置审核模型")
		return
	}
	prompt, err := interactionpolicy.Read("qa-pairs-organize.md")
	if err != nil {
		writeError(w, 500, "读取整理规则失败")
		return
	}
	raw, _ := json.Marshal(map[string]any{"text": input.Text, "objects": json.RawMessage(objectText)})
	result, _, err := s.completeAnchorStyleWorkbench(r.Context(), profile, "qa_pairs_organize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: string(raw)}}, 6000, false)
	if err != nil {
		writeError(w, 502, "整理失败，输入保留，尚未入库")
		return
	}
	if strings.EqualFold(result.FinishReason, "length") {
		writeError(w, 502, "整理被截断，请缩小输入；尚未入库")
		return
	}
	value, err := parseLooseJSONValue(result.Text)
	if err != nil {
		writeError(w, 502, "整理结果不完整，请手工填写或分批")
		return
	}
	data, _ := json.Marshal(value)
	var out struct {
		Items     []qapair.Entry `json:"items"`
		Questions []string       `json:"questions"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Items) > 20 {
		writeError(w, 502, "整理结果格式不正确或条目过多，尚未入库")
		return
	}
	for i, item := range out.Items {
		item.ID = ""
		item.Revision = 0
		item.ClientID = ""
		item.Enabled = true
		item.Original = input.Text
		normalized, e := qapair.Normalize(item)
		if e != nil {
			writeError(w, 502, "整理结果含不完整问题或回答，请缩小输入或手工填写")
			return
		}
		out.Items[i] = normalized
	}
	if validQATargets(out.Items, links, objects.Stores) != nil {
		writeError(w, 502, "整理结果归属不明确，请手工核对")
		return
	}
	writeJSON(w, 200, out)
}
