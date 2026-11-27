package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptchain"
)

const (
	scriptFactReference = "【引用事实："
	scriptFactLabel     = "【引用事实"
	scriptOriginalSlot  = "【粘贴用户原话术】"
)

type scriptPieceTemplateError struct{ message string }

func (e *scriptPieceTemplateError) Error() string { return e.message }

type scriptPieceTemplate struct {
	objects []string
	body    string
	useSlot bool
}

func parseScriptPieceTemplate(prompt string) (scriptPieceTemplate, error) {
	body := strings.TrimSpace(strings.ReplaceAll(prompt, "\r\n", "\n"))
	first, rest, _ := strings.Cut(body, "\n")
	var out scriptPieceTemplate
	if strings.HasPrefix(first, scriptFactLabel) {
		if !strings.HasPrefix(first, scriptFactReference) || !strings.HasSuffix(first, "】") {
			return out, &scriptPieceTemplateError{"事实引用首行须写成【引用事实：对象名】"}
		}
		for name := range strings.SplitSeq(first[len(scriptFactReference):len(first)-len("】")], "、") {
			name = strings.TrimSpace(name)
			if name == "" || name == "待指定" || strings.ContainsAny(name, "【】") {
				return out, &scriptPieceTemplateError{"请在引用事实标签中填写事实库里的对象名称，多个对象用顿号分隔"}
			}
			if !slices.Contains(out.objects, name) {
				out.objects = append(out.objects, name)
			}
		}
		body = strings.TrimSpace(rest)
		if !strings.HasSuffix(body, "\n原话术如下：\n"+scriptOriginalSlot) {
			return out, &scriptPieceTemplateError{"引用事实的提示词须以「原话术如下：」和「" + scriptOriginalSlot + "」两行结尾"}
		}
	}
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), scriptFactLabel) {
			return out, &scriptPieceTemplateError{"引用事实标签只能在提示词第一行出现一次"}
		}
	}
	slots := strings.Count(body, scriptOriginalSlot)
	if slots > 1 {
		return out, &scriptPieceTemplateError{"原话术占位符只能出现一次"}
	}
	out.body, out.useSlot = body, slots == 1
	return out, nil
}

// Read committed claims and their object directory together. The asynchronous
// rewrite cache also carries materials and can lag a just-confirmed edit.
func (s *Server) readScriptPieceFacts(ctx context.Context, tenant, plan int64) ([]rewriteFact, error) {
	if s.store == nil {
		return nil, errors.New("事实库不可用")
	}
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	return projectScriptPieceFacts(claims, catalog, links), nil
}

func projectScriptPieceFacts(claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink) []rewriteFact {
	claims = slices.DeleteFunc(claims, func(claim model.PlanMaterialClaim) bool {
		return claim.Destination != "" && claim.Destination != "fact"
	})
	facts, _ := projectRewriteFactsByObject(claims, catalog, links, true)
	return facts
}

func (s *Server) validatePlanSaveFactReferences(ctx context.Context, tenant, plan int64, generation map[string]any) (string, error) {
	script, ok := recordingScriptFromContext(generation)
	if !ok {
		return "", nil
	}
	var templates []scriptPieceTemplate
	for _, tab := range script.Tabs {
		for _, entry := range tab.Entries {
			if !entry.SpecPromptEnabled || !strings.Contains(entry.SpecPrompt, scriptFactLabel) {
				continue
			}
			template, err := parseScriptPieceTemplate(entry.SpecPrompt)
			if err != nil {
				return "", err
			}
			templates = append(templates, template)
		}
	}
	if len(templates) == 0 {
		return "", nil
	}
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, tenant, plan)
	if err != nil {
		return "", errors.New("读取当前事实清单失败，请重试")
	}
	revision := db.MaterialLibraryRevision(claims)
	facts := projectScriptPieceFacts(claims, catalog, links)
	for _, template := range templates {
		if _, err := template.selectFacts(facts); err != nil {
			return "", err
		}
	}
	return revision, nil
}

func (t scriptPieceTemplate) selectFacts(available []rewriteFact) (json.RawMessage, error) {
	if len(t.objects) == 0 {
		return nil, nil
	}
	selected := make([]rewriteFact, 0)
	for _, name := range t.objects {
		start := len(selected)
		for _, fact := range available {
			if fact.Object == name {
				selected = append(selected, fact)
			}
		}
		if len(selected) == start {
			return nil, &scriptPieceTemplateError{fmt.Sprintf("事实库中没有「%s」的已确认事实，请核对引用对象", name)}
		}
		// The database orders by last edit time. Incidental reordering must
		// not invalidate audio when the facts sent to the model are identical.
		slices.SortFunc(selected[start:], func(a, b rewriteFact) int {
			return cmp.Or(strings.Compare(a.Attribute, b.Attribute), strings.Compare(string(a.Value), string(b.Value)),
				strings.Compare(a.Polarity, b.Polarity), strings.Compare(a.Condition, b.Condition), strings.Compare(a.Exception, b.Exception))
		})
	}
	return json.Marshal(selected)
}

func scriptPieceFactsKey(facts json.RawMessage) string {
	if len(facts) == 0 {
		return ""
	}
	return scriptchain.ContentHash(string(facts))
}

func (s *Server) resolveScriptPieceFacts(ctx context.Context, tenant, plan int64, prompt string) (json.RawMessage, error) {
	template, err := parseScriptPieceTemplate(prompt)
	if err != nil || len(template.objects) == 0 {
		return nil, err
	}
	available, err := s.readScriptPieceFacts(ctx, tenant, plan)
	if err != nil {
		log.Printf("script piece facts unavailable tenant=%d plan=%d: %v", tenant, plan, err)
		return nil, &scriptPieceTemplateError{"读取方案事实库失败，请稍后重试"}
	}
	return template.selectFacts(available)
}

func (s *Server) checkScriptPieceFacts(ctx context.Context, tenant, plan int64, unit volcengineScriptUnit) error {
	if unit.SpecFactsKey == "" {
		return nil
	}
	current, err := s.resolveScriptPieceFacts(ctx, tenant, plan, unit.SpecPrompt)
	if err != nil {
		return err
	}
	if scriptPieceFactsKey(current) != unit.SpecFactsKey {
		return &scriptPieceTemplateError{"引用的方案事实刚刚更新，请重新点播"}
	}
	return nil
}

// Ordinary rotation may replay scoped versions. Refresh only for sessions
// with scoped pieces, and keep immutable unit slices for background workers.
func (s *Server) scriptPieceFactRefresh(tenant, plan int64, units []volcengineScriptUnit) func(context.Context, []volcengineScriptUnit) []volcengineScriptUnit {
	templates := make(map[int]scriptPieceTemplate)
	for index, unit := range units {
		if template, err := parseScriptPieceTemplate(unit.SpecPrompt); err == nil && len(template.objects) > 0 {
			templates[index] = template
		}
	}
	var next time.Time
	return func(ctx context.Context, units []volcengineScriptUnit) []volcengineScriptUnit {
		if len(templates) == 0 || time.Now().Before(next) {
			return units
		}
		next = time.Now().Add(15 * time.Second)
		read, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
		defer cancel()
		facts, err := s.readScriptPieceFacts(read, tenant, plan)
		if err != nil {
			log.Printf("script rotation facts unavailable tenant=%d plan=%d: %v", tenant, plan, err)
		}
		updated := slices.Clone(units)
		for index, template := range templates {
			selected, _ := template.selectFacts(facts)
			updated[index].SpecFactsKey = scriptPieceFactsKey(selected)
		}
		return updated
	}
}
