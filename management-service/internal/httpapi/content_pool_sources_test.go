package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/contentpools"
)

func contentScopeTestSources() contentpools.Sources {
	return contentpools.Sources{
		Products: []contentpools.Product{{ID: 7, Link: "1号链接"}, {ID: 8, Link: "2号链接"}},
		Rows: []contentpools.SourceRow{
			{ID: "knowledge:66", Area: "knowledge", Fields: map[string]string{"分类": "location", "名称": "地址信息"}},
			{ID: "knowledge:63", Area: "knowledge", Fields: map[string]string{"分类": "per_capita", "名称": "人均消费"}},
		},
	}
}

func validateContentScopeFacts(t *testing.T, source contentpools.Sources, safe string, facts []contentpools.Fact) {
	t.Helper()
	set := contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: safe, Pools: contentpools.GroupPools(facts, source.Products, "scope-regression")}
	if err := contentpools.ValidateSet(set); err != nil {
		t.Fatalf("mapped facts still fail snapshot validation: %v", err)
	}
}

func TestContentFactsScopeUsesRealSources(t *testing.T) {
	for _, tc := range []struct {
		name, line, source string
		owner              int64
		products           []int64
		explicit           bool
	}{
		{"common", "- [用户指定公共；location/地址信息] 测试门店地址", "knowledge:66", 0, []int64{7, 8}, true},
		{"product", `- [用户指定商品="2号链接"；per_capita/人均消费] 人均约35元`, "knowledge:63", 8, []int64{8}, true},
		{"legacy", "- [location/地址信息] 测试门店地址", "knowledge:66", 0, []int64{7, 8}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := contentScopeTestSources()
			before := source.SHA()
			facts, err := contentFactsFromSafety(source, tc.line)
			if err != nil || len(facts) != 1 {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
			got := facts[0]
			if got.ID != tc.source || !reflect.DeepEqual(got.Sources, []string{tc.source}) || got.Owner != tc.owner || !reflect.DeepEqual(got.Products, tc.products) || got.Private != tc.explicit || got.Text != tc.line {
				t.Fatalf("scope, provenance or complete text changed: %+v", got)
			}
			validateContentScopeFacts(t, source, tc.line, facts)
			if source.SHA() != before {
				t.Fatal("mapping mutated the source snapshot")
			}
		})
	}
}

func TestContentFactsScopeReadsQuotedLinkDelimiters(t *testing.T) {
	source := contentScopeTestSources()
	link := `自定义"一号；]链接`
	source.Products[0].Link = link
	encoded, _ := json.Marshal(link)
	line := "- [用户指定商品=" + string(encoded) + "；location/地址信息] 专属地址"
	facts, err := contentFactsFromSafety(source, line)
	if err != nil || len(facts) != 1 || facts[0].Owner != 7 || !reflect.DeepEqual(facts[0].Products, []int64{7}) || facts[0].Sources[0] != "knowledge:66" {
		t.Fatalf("quoted link changed header boundary: %+v %v", facts, err)
	}
}

func TestContentFactsScopeRejectsUnresolvedOrigins(t *testing.T) {
	for _, tc := range []struct{ name, line, want string }{
		{"missing-source", "- [用户指定公共；location/不存在] 内容", "无法关联当前快照来源"},
		{"empty-link", `- [用户指定商品=""；location/地址信息] 内容`, "归属商品已不可用"},
		{"unknown-link", `- [用户指定商品="9号链接"；location/地址信息] 内容`, "不在当前快照中"},
		{"invalid-json", `- [用户指定商品=2号链接；location/地址信息] 内容`, "归属商品已不可用"},
		{"missing-separator", `- [用户指定商品="2号链接"location/地址信息] 内容`, "归属商品已不可用"},
		{"unclosed-header", "- [用户指定公共；location/地址信息", "标题缺失或未闭合"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Failure must not return a partial set that callers could save.
			safe := "- [location/地址信息] 有效内容\n" + tc.line
			facts, err := contentFactsFromSafety(contentScopeTestSources(), safe)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "第2行") || facts != nil || strings.Contains(err.Error(), "facts:line:") {
				t.Fatalf("invalid origin became an executable/anonymous fact: %+v %v", facts, err)
			}
		})
	}
}

func TestContentFactsScopeDoesNotGuessDuplicateOrRenamedOrigins(t *testing.T) {
	source := contentScopeTestSources()
	source.Rows = append(source.Rows, contentpools.SourceRow{ID: "knowledge:999", Area: "knowledge", Fields: map[string]string{"分类": "location", "名称": "地址信息"}})
	if _, err := contentFactsFromSafety(source, "- [用户指定公共；location/地址信息] 内容"); err == nil || !strings.Contains(err.Error(), "多个快照来源") {
		t.Fatalf("duplicate title silently selected a source: %v", err)
	}
	source = contentScopeTestSources()
	source.Rows[0].Fields["分类"], source.Rows[0].Fields["名称"] = "stores", "门店地址"
	if _, err := contentFactsFromSafety(source, "- [用户指定公共；location/地址信息] 内容"); err == nil {
		t.Fatal("stale display key guessed a renamed source")
	}
	facts, err := contentFactsFromSafety(source, "- [用户指定公共；stores/门店地址] 内容")
	if err != nil || len(facts) != 1 || facts[0].Sources[0] != "knowledge:66" {
		t.Fatalf("current rename changed stable source ID: %+v %v", facts, err)
	}
}

func TestContentFactsExplicitScopeBypassesModelAndCachedScope(t *testing.T) {
	source := contentScopeTestSources()
	safe := "- [用户指定公共；location/地址信息] 公共地址\n" + `- [用户指定商品="2号链接"；per_capita/人均消费] 人均约35元`
	facts, err := contentFactsFromSafety(source, safe)
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]contentpools.Fact{}
	for _, f := range facts {
		old := f
		old.Products = []int64{7} // Earlier inferred scope must not override the user.
		previous[f.ID] = old
	}
	// No store/provider is configured: explicit scope must require no model call.
	scoped, _, err := (&Server{}).classifySimplePoolFacts(context.Background(), "", source, facts, "", previous)
	if err != nil || len(scoped) != len(facts) {
		t.Fatalf("explicit scope changed: %+v %v", scoped, err)
	}
	expected := map[string]contentpools.Fact{}
	for _, f := range facts {
		expected[f.ID] = f
	}
	for _, f := range scoped {
		if !reflect.DeepEqual(f, expected[f.ID]) {
			t.Fatalf("explicit scope changed during classification: %+v", f)
		}
	}
	validateContentScopeFacts(t, source, safe, scoped)
}

// Replay captured production inputs locally. The fixture stays outside Git
// because it contains merchant data. This test never connects to a provider/DB.
func TestContentFactsProductionScopeReplay(t *testing.T) {
	path := os.Getenv("CONTENT_FACT_SCOPE_REPLAY_PATH")
	if path == "" {
		t.Skip("set CONTENT_FACT_SCOPE_REPLAY_PATH to the captured read-only-data.json")
	}
	var captured struct {
		Observations []struct{ Current contentpools.Sources }
		Exchanges    []struct {
			Stage   string
			Request json.RawMessage
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &captured) != nil || len(captured.Observations) != 1 {
		t.Fatalf("invalid captured snapshot: %v", err)
	}
	var proof struct {
		Candidates []struct {
			Header string
			Source string `json:"expected_source_id"`
		} `json:"scoped_candidates"`
	}
	raw, err = os.ReadFile(filepath.Join(filepath.Dir(path), "source-mapping-proof.json"))
	if err != nil || json.Unmarshal(raw, &proof) != nil || len(proof.Candidates) != 9 {
		t.Fatalf("invalid nine-fact expectation: %v", err)
	}
	expected := map[string]string{}
	for _, candidate := range proof.Candidates {
		expected[candidate.Header] = candidate.Source
	}
	seen := map[string]bool{}
	lines := []string{}
	want := []string{}
	for _, exchange := range captured.Exchanges {
		if exchange.Stage != "content_pool_fact_scope" {
			continue
		}
		request := exchange.Request
		var encoded string
		if json.Unmarshal(request, &encoded) == nil {
			request = []byte(encoded)
		}
		var packet struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(request, &packet); err != nil {
			t.Fatal(err)
		}
		for _, message := range packet.Messages {
			if message.Role != "user" {
				continue
			}
			_, payload, ok := strings.Cut(message.Content, "\n")
			var input struct{ Candidate string }
			if !ok || json.Unmarshal([]byte(payload), &input) != nil || input.Candidate == "" || seen[input.Candidate] {
				continue
			}
			seen[input.Candidate] = true
			header := strings.SplitN(strings.TrimPrefix(input.Candidate, "- ["), "]", 2)[0]
			if expected[header] == "" {
				t.Fatalf("captured candidate has no independent expected source: %s", header)
			}
			lines, want = append(lines, input.Candidate), append(want, expected[header])
		}
	}
	source := captured.Observations[0].Current
	safe := strings.Join(lines, "\n")
	facts, err := contentFactsFromSafety(source, safe)
	if err != nil || len(lines) != 9 || len(facts) != 9 {
		t.Fatalf("nine-fact replay: lines=%d facts=%d err=%v", len(lines), len(facts), err)
	}
	for i, f := range facts {
		owner := map[string]int64{"knowledge:62": 7, "knowledge:63": 7, "knowledge:65": 8}[want[i]]
		products := []int64{7, 8}
		if owner != 0 {
			products = []int64{owner}
		}
		if !reflect.DeepEqual(f.Sources, []string{want[i]}) || f.Owner != owner || !reflect.DeepEqual(f.Products, products) || !f.Private {
			t.Fatalf("production fact lost its origin/scope: %+v wantSource=%s owner=%d", f, want[i], owner)
		}
	}
	validateContentScopeFacts(t, source, safe, facts)
	t.Log("9 captured facts retain real sources and explicit scopes; ValidateSet passed; no model or DB calls")
}
