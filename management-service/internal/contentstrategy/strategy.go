// Package contentstrategy adds fact-backed communication tasks to existing
// generation. It owns no scheduling, playback, interruption or resume state.
package contentstrategy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/interactionpolicy"
)

const Kind = "fact_purpose_angles_v1"

type Fact struct {
	ID   string `json:"事实编号"`
	Text string `json:"完整事实记录"`
}
type Purpose struct {
	ID      string   `json:"目的编号"`
	Name    string   `json:"目的"`
	Concern string   `json:"观众顾虑"`
	FactIDs []string `json:"关联事实"`
}
type Row struct {
	PurposeID string   `json:"目的编号"`
	Angle     string   `json:"角度"`
	Task      string   `json:"讲述任务"`
	FactIDs   []string `json:"关联事实"`
	When      string   `json:"适用条件"`
}
type Purposes struct {
	Items []Purpose `json:"目的列表"`
}
type Table struct {
	Rows []Row `json:"目的角度表"`
}

// Immutable, tenant/plan-scoped analysis asset. Runtime copies live in the
// existing run, never in this database asset.
type Snapshot struct {
	Version       int       `json:"version"`
	Enabled       bool      `json:"enabled"`
	FactsSHA      string    `json:"facts_sha256"`
	Facts         []Fact    `json:"facts"`
	Purposes      []Purpose `json:"purposes"`
	Rows          []Row     `json:"rows"`
	PromptSources []string  `json:"prompt_sources"`
	PurposeRaw    string    `json:"purpose_raw"`
	AngleRaw      string    `json:"angle_raw"`
	Model         string    `json:"model"`
}

func SHA(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }

// Only an explicit, valid disabled version is a plan-level opt-out. Absence
// of a field or malformed JSON must never silently disable an existing asset.
func Disabled(raw string) bool {
	var s Snapshot
	if Decode(raw, &s) != nil || s.Version != 1 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil {
		return false
	}
	return strings.TrimSpace(string(fields["enabled"])) == "false"
}

// A record includes all continuation lines and expression controls. In
// particular, numbers in a forbidden example must not become product specs.
func Facts(text string) []Fact {
	var records []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [") {
			records = append(records, line)
		} else if len(records) > 0 && line != "" && !strings.HasPrefix(line, "边界：") && !strings.HasPrefix(line, "#") {
			records[len(records)-1] += "\n" + line
		}
	}
	result := make([]Fact, 0, len(records))
	for i, text := range records {
		result = append(result, Fact{fmt.Sprintf("F%03d", i+1), text})
	}
	return result
}

func Decode(raw string, target any) error {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if at := strings.IndexByte(raw, '\n'); at >= 0 {
			raw = strings.TrimSpace(raw[at+1:])
			raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
		}
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("策略返回包含多余内容")
	}
	return nil
}
func validText(text string, limit int) bool {
	return strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= limit
}
func validateRefs(refs []string, known map[string]bool) error {
	if len(refs) == 0 || len(refs) > 20 {
		return fmt.Errorf("关联事实须为1至20项")
	}
	seen := map[string]bool{}
	for _, id := range refs {
		if !known[id] || seen[id] {
			return fmt.Errorf("关联事实不存在或重复：%s", id)
		}
		seen[id] = true
	}
	return nil
}
func Validate(facts []Fact, purposes []Purpose, rows []Row, requireRows bool) error {
	known := map[string]bool{}
	for _, f := range facts {
		if !validText(f.ID, 40) || strings.TrimSpace(f.Text) == "" || known[f.ID] {
			return fmt.Errorf("事实记录无效")
		}
		known[f.ID] = true
	}
	if len(purposes) == 0 || len(purposes) > 40 {
		return fmt.Errorf("目的数量须为1至40项")
	}
	ids := map[string]bool{}
	for _, p := range purposes {
		if !validText(p.ID, 40) || ids[p.ID] || !validText(p.Name, 40) || !validText(p.Concern, 200) {
			return fmt.Errorf("目的字段缺失、重复或过长")
		}
		ids[p.ID] = true
		if err := validateRefs(p.FactIDs, known); err != nil {
			return err
		}
	}
	if requireRows && (len(rows) == 0 || len(rows) > 160) {
		return fmt.Errorf("目的角度表须为1至160项")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if !ids[r.PurposeID] || !validText(r.Angle, 60) || !validText(r.Task, 240) || !validText(r.When, 200) {
			return fmt.Errorf("角度字段缺失、引用未知目的或过长")
		}
		if err := validateRefs(r.FactIDs, known); err != nil {
			return err
		}
		key := r.PurposeID + "/" + r.Angle
		if seen[key] {
			return fmt.Errorf("目的下存在重复角度")
		}
		seen[key] = true
	}
	return nil
}
func Parse(raw, factPackage string) (*Snapshot, error) {
	var s Snapshot
	if err := Decode(raw, &s); err != nil {
		return nil, err
	}
	if s.Version != 1 || s.FactsSHA != SHA(factPackage) {
		return nil, fmt.Errorf("策略版本或正式事实指纹不匹配")
	}
	actual, _ := json.Marshal(Facts(factPackage))
	saved, _ := json.Marshal(s.Facts)
	if string(actual) != string(saved) {
		return nil, fmt.Errorf("策略关联事实与正式快照不一致")
	}
	if err := Validate(s.Facts, s.Purposes, s.Rows, true); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *Snapshot) Render(mode, allowedFacts string) (string, error) {
	if s == nil || !s.Enabled {
		return "", nil
	}
	allowed := map[string]bool{}
	for _, f := range s.Facts {
		if strings.Contains(allowedFacts, f.Text) {
			allowed[f.ID] = true
		}
	}
	rows := []Row{}
	purposeIDs := map[string]bool{}
	used := map[string]bool{}
	for _, r := range s.Rows {
		ok := true
		for _, id := range r.FactIDs {
			if !allowed[id] {
				ok = false
				break
			}
		}
		if ok {
			rows = append(rows, r)
			purposeIDs[r.PurposeID] = true
			for _, id := range r.FactIDs {
				used[id] = true
			}
		}
	}
	if len(rows) == 0 {
		return "", nil
	}
	purposes := []Purpose{}
	facts := []Fact{}
	for _, p := range s.Purposes {
		if purposeIDs[p.ID] {
			p.FactIDs = nil
			purposes = append(purposes, p)
		}
	}
	for _, f := range s.Facts {
		if used[f.ID] {
			facts = append(facts, f)
		}
	}
	data, _ := json.Marshal(map[string]any{"使用路径": mode, "目的列表": purposes, "目的角度表": rows, "完整关联事实": facts})
	raw, err := interactionpolicy.Read("fact-purpose-angle-generation.md")
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), "{{STRATEGY_DATA}}", string(data)), nil
}
