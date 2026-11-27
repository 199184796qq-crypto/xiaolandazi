package qapair

import (
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/model"
	"math"
	"strings"
	"testing"
	"time"
)

func fixtureEntry(q, a string, ids ...int64) Entry {
	e := Entry{Question: q, Answer: a, Enabled: true}
	if len(ids) == 0 {
		e.Targets = []model.MaterialObjectTarget{{Kind: "plan"}}
	} else {
		for _, id := range ids {
			e.Targets = append(e.Targets, model.MaterialObjectTarget{Kind: "product", LinkID: id})
		}
	}
	return e
}

func TestParseCompleteMultilineOriginalAndNoInventedAnswer(t *testing.T) {
	text := "问题：能怎样处理？\n回答：第一行，保留条件。\n第二行包含“问题：其它内容”的引用。\n\n问题: 另一问？\n回答: 对应回答\n相似问法：换一种问法|另一种问法\n适用条件：指定范围\n例外：例外必须保留"
	rows, err := Parse(text, []model.MaterialObjectTarget{{Kind: "plan"}})
	if err != nil || len(rows) != 2 || !strings.Contains(rows[0].Answer, "第二行") || len(rows[1].Aliases) != 2 || rows[1].Exceptions != "例外必须保留" || !strings.Contains(rows[0].Original, "第一行") {
		t.Fatal(rows, err)
	}
	for _, text := range []string{"随便帮我回答", "问题：一问\n问题：二问\n回答：二答", "问题：缺答案", "问题：一问\n回答：一答\n回答：二答", "问题：一问\n回答：一答\n有效期：下个月"} {
		if _, err := Parse(text, nil); err == nil {
			t.Fatal("ambiguous/incomplete accepted", text)
		}
	}
}

func TestScopesDuplicatesAndExplicitConflictChoices(t *testing.T) {
	one := fixtureEntry("多少钱？", "87.6元", 1, 2)
	one.ID = "old"
	one.Revision = 3
	one.Original = "原文"
	old := Record{Entry: one, Model: "qwen", Space: SpaceVersion, Vectors: [][]float32{{1, 0}}}
	new := fixtureEntry("多少钱？", "88元", 1)
	new.ClientID = "new"
	p, err := Prepare([]Entry{new}, []Record{old})
	if err != nil || len(p[0].Conflicts) != 1 {
		t.Fatal(p, err)
	}
	if _, _, _, err = Changes(p, []Record{old}, nil); err == nil {
		t.Fatal("silent overwrite")
	}
	writes, remove, kept, err := Changes(p, []Record{old}, map[string]string{"new": "keep"})
	if err != nil || len(writes) != 0 || len(remove) != 0 || kept != 1 {
		t.Fatal(writes, remove, kept, err)
	}
	writes, remove, _, err = Changes(p, []Record{old}, map[string]string{"new": "replace"})
	if err != nil || len(writes) != 2 || len(remove) != 1 || writes[0].Entry.Answer != "87.6元" || writes[0].Entry.Targets[0].LinkID != 2 || writes[0].Model != "qwen" {
		t.Fatal(writes, remove, err)
	}
	different := fixtureEntry("多少钱？", "其它链接价格", 3)
	p, err = Prepare([]Entry{different}, []Record{old})
	if err != nil || len(p[0].Conflicts) != 0 {
		t.Fatal("cross-owner duplicate", p, err)
	}
	if _, err = Prepare([]Entry{new, new}, nil); err == nil {
		t.Fatal("batch duplicate accepted")
	}
	global := fixtureEntry("多少钱？", "通用说明")
	p, err = Prepare([]Entry{global}, []Record{old})
	if err != nil || len(p[0].Conflicts) != 0 {
		t.Fatal("global inherited duplicate", p, err)
	}
	one.Targets = append(one.Targets, model.MaterialObjectTarget{Kind: "plan"})
	if _, err = Normalize(one); err == nil {
		t.Fatal("global mixed scope")
	}
	unknown := fixtureEntry("问", "答")
	unknown.Targets = nil
	n, err := Normalize(unknown)
	if err != nil || n.Enabled {
		t.Fatal("unassigned enabled", n, err)
	}
}

func TestNoopRequiresSameAnswerOriginalAndScope(t *testing.T) {
	e := fixtureEntry("能寄吗？", "可以寄。")
	e.ID = "old"
	e.Revision = 2
	e.Original = "原文"
	n := e
	n.ID = ""
	n.Revision = 0
	n.ClientID = "row"
	p, err := Prepare([]Entry{n}, []Record{{Entry: e}})
	if err != nil || !p[0].Identical {
		t.Fatal(p, err)
	}
	n.Original = "另一处原文"
	p, _ = Prepare([]Entry{n}, []Record{{Entry: e}})
	if p[0].Identical {
		t.Fatal("source silently discarded")
	}
	n = e
	n.Revision = 1
	if _, err = Prepare([]Entry{n}, []Record{{Entry: e}}); err == nil {
		t.Fatal("stale edit")
	}
	if QuestionKey("能寄吗") == QuestionKey("包邮吗") || QuestionKey("能切吗") == QuestionKey("不要切") {
		t.Fatal("different meanings collapsed")
	}
}

func TestQARecallOwnerAliasConditionValidityAndBudget(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	links := []danmakutest.Link{{ID: 1, Name: "1号链接"}, {ID: 2, Name: "2号链接"}}
	makeRecord := func(id string, e Entry) Record {
		e.ID = id
		vs := [][]float32{}
		for range Questions(e) {
			vs = append(vs, []float32{1, 0})
		}
		return Record{e, "qwen", SpaceVersion, vs}
	}
	e := fixtureEntry("贵吗？", "认可的价值解释", 1)
	e.Aliases = []string{"为什么贵", "有点贵"}
	r := makeRecord("one", e)
	ask := danmakutest.Ask{Text: "有点贵", Subjects: []string{"1号链接"}}
	recall := func(records []Record, ask danmakutest.Ask) []Match {
		return Retrieve(records, links, nil, []danmakutest.Ask{ask}, []string{ask.Text}, [][]float32{{1, 0}}, "qwen", now)[0].Matches
	}
	got := recall([]Record{r}, ask)
	if len(got) != 1 || got[0].Status != "可参考" {
		t.Fatal(got)
	}
	ask.Subjects = nil
	if len(recall([]Record{r}, ask)) != 0 {
		t.Fatal("unknown product became global")
	}
	store := fixtureEntry("贵吗", "门店说明")
	store.Targets = []model.MaterialObjectTarget{{Kind: "store", StoreName: "王家店"}}
	rs := makeRecord("store", store)
	ask.Subjects = []string{"2号链接"}
	groups := Retrieve([]Record{rs}, links, []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{2}}}, []danmakutest.Ask{ask}, []string{ask.Text}, [][]float32{{1, 0}}, "qwen", now)
	if len(groups[0].Matches) != 1 {
		t.Fatal("store relation", groups)
	}
	g := fixtureEntry("有点贵", "全局解释")
	g.Conditions = "仅本次活动"
	got = recall([]Record{makeRecord("condition", g)}, ask)
	if len(got) != 1 || got[0].Status != "待核对" || got[0].Conditions == "" {
		t.Fatal(got)
	}
	for _, kind := range []string{"expired", "future", "disabled", "model", "space"} {
		bad := makeRecord(kind, fixtureEntry("贵吗", "不能使用"))
		switch kind {
		case "expired":
			bad.Entry.ValidUntil = now.Format(time.RFC3339)
		case "future":
			bad.Entry.ValidFrom = now.Add(time.Hour).Format(time.RFC3339)
		case "disabled":
			bad.Entry.Enabled = false
		case "model":
			bad.Model = "other"
		case "space":
			bad.Space = "old"
		}
		if len(recall([]Record{bad}, ask)) != 0 {
			t.Fatal(kind)
		}
	}
	large := makeRecord("long", fixtureEntry("贵吗", strings.Repeat("字", 6001)))
	got = recall([]Record{large}, ask)
	if len(got) != 1 || got[0].Status != "未采用" || len([]rune(got[0].Answer)) != 6001 {
		t.Fatal("truncated answer", len(got))
	}
}

func TestQARecallPreviewUsesTopTenAndAnswersUseAliases(t *testing.T) {
	now := time.Now()
	var records []Record
	for i := range 11 {
		entry := fixtureEntry("标准问题", "标准回答")
		entry.ID = string(rune('a' + i))
		score := float32(0.52) + float32(i)*0.02
		records = append(records, Record{entry, "qwen", SpaceVersion, [][]float32{{score, float32(math.Sqrt(1 - float64(score*score)))}}})
	}
	alias := fixtureEntry("主问句", "只供程序采用")
	alias.ID = "alias"
	alias.Aliases = []string{"另一种问法"}
	records = append(records, Record{alias, "qwen", SpaceVersion, [][]float32{{0, 1}, {1, 0}}})
	preview := PreviewCandidates(records, []float32{1, 0}, "qwen", now)
	if len(preview) != 10 || preview[9].ID != "c" || preview[9].Score >= AnswerMinScore {
		t.Fatal(preview)
	}
	got := AnswerCandidates(records, danmakutest.Ask{Text: "另一种问法"}, []float32{1, 0}, "qwen", now, nil, nil)
	if len(got) != 5 || got[0].ID != "alias" || got[0].Score != 1 || got[0].Answer != "只供程序采用" || got[4].ID != "h" {
		t.Fatal(got)
	}
}

func TestQARecallPreviewIncludesScoresBelowThreshold(t *testing.T) {
	now := time.Now()
	record := func(id string, vector []float32) Record {
		entry := fixtureEntry("问题", "回答")
		entry.ID = id
		return Record{entry, "qwen", SpaceVersion, [][]float32{vector}}
	}
	records := []Record{
		record("low", []float32{0.3, 1}),
		record("high", []float32{1, 0}),
		record("wrong-dimensions", []float32{1, 0, 0}),
		record("disabled", []float32{1, 0}),
		record("wrong-model", []float32{1, 0}),
	}
	records[3].Entry.Enabled = false
	records[4].Model = "other"
	query := []float32{1, 0}
	preview := PreviewCandidates(records, query, "qwen", now)
	if len(preview) != 2 || preview[0].ID != "high" || preview[0].Score != 1 ||
		preview[1].ID != "low" || preview[1].Score <= 0 || preview[1].Score >= AnswerMinScore {
		t.Fatal(preview)
	}
	if live := AnswerCandidates(records, danmakutest.Ask{Text: "问题"}, query, "qwen", now, nil, nil); len(live) != 1 || live[0].ID != preview[0].ID {
		t.Fatal(live)
	}
}

func TestQAAnswerCandidatesUseOnlyTheAsksObjectScope(t *testing.T) {
	now := time.Now()
	links := []danmakutest.Link{{ID: 1, Name: "一号链接"}, {ID: 2, Name: "二号链接"}}
	records := []Record{}
	for i, link := range links {
		entry := fixtureEntry("什么时候发货", link.Name+"次日发货", link.ID)
		entry.ID = link.Name
		entry.Conditions = "工作日"
		score := float32(i + 1)
		records = append(records, Record{entry, "qwen", SpaceVersion, [][]float32{{score, 1}}})
	}
	ask := danmakutest.Ask{Text: "什么时候发货", Subjects: []string{links[0].Name}}
	got := AnswerCandidates(records, ask, []float32{1, 0}, "qwen", now, links, nil)
	if len(got) != 1 || got[0].ID != links[0].Name || got[0].Conditions != "工作日" || got[0].Status != "待核对" {
		t.Fatal(got)
	}
	ask.Subjects = []string{links[0].Name, links[1].Name}
	if got := AnswerCandidates(records, ask, []float32{1, 0}, "qwen", now, links, nil); len(got) != 0 {
		t.Fatal(got)
	}
}
