package httpapi

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/scriptplay"
)

func TestSpecifiedScriptMakesOneDirectWritingCall(t *testing.T) {
	const bodyPrefix = "话术骨架：招呼\n一句话上下文：介绍自提方式\n话术："
	unitText := strings.Repeat("原文样本。", 80)
	short := "一号链接徐香猕猴桃，九块九三斤。" + strings.Repeat("欢迎到店自提。", 43)
	long := strings.Repeat("欢迎进店，买好的商品可以到店自提。", 70)
	for _, tc := range []struct {
		name, reply string
		want        string
	}{
		{"below writing target", bodyPrefix + short, short},
		{"above writing target", bodyPrefix + long, long},
		{"annotation only", bodyPrefix + "【舞台提示】", ""},
		{"same as source", bodyPrefix + unitText, ""},
		{"invalid contract", "没有规定的输出字段", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := agentgateway.New("preview-test", "preview-test-model")
			var calls []agentgateway.Request
			gateway.Register(scriptPreviewTestProvider(func(req agentgateway.Request) agentgateway.Response {
				calls = append(calls, req)
				return agentgateway.Response{Text: tc.reply}
			}))
			material := scriptTextMaterial{
				version: policyVersion(), unit: volcengineScriptUnit{Text: unitText, SpecPrompt: "本片要求"},
				redLines: "不得报错价格", facts: json.RawMessage(`["不能发送这些全量事实"]`),
				persona: "方案人设不会发送", speechRequirements: "方案级写法不会发送",
			}
			result, err := (&Server{agentGateway: gateway}).generateScriptText(t.Context(), scriptGenerationSpecified,
				scriptTextGenerationInput{material: material, session: 887})
			if (err == nil) != (tc.want != "") || result.Script != tc.want {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if len(calls) != 1 || calls[0].Stage != "script_rewrite" || len(calls[0].Messages) != 1 {
				t.Fatalf("unexpected calls: %+v", calls)
			}
			if call := calls[0]; !call.EnableThinking || call.ReasoningEffort != "high" || !call.UseProviderOutputLimit || call.MaxTokens != 0 {
				t.Fatalf("point playback must explicitly request high with the provider default: %+v", call)
			}
			prompt := calls[0].Messages[0].Content
			prefix := "【原始话术】\n" + unitText + "\n\n【本片专属提示词】\n本片要求\n\n【红线禁止】\n不得报错价格"
			if !strings.HasPrefix(prompt, prefix) || !strings.HasSuffix(strings.TrimSpace(prompt), strings.TrimSpace(planprompt.RewriteOutputContract)) ||
				!strings.Contains(prompt, "目标约") || strings.Contains(prompt, "不会发送") || strings.Contains(prompt, "不能发送") {
				t.Fatalf("unexpected provider prompt: %s", prompt)
			}
		})
	}
}

func TestSpecifiedScriptInlineFactsAcceptEitherOutput(t *testing.T) {
	const customPrompt = `一、事实边界
只使用下面给出的事实。1号链接：徐香猕猴桃；9.9元3斤。全门店：到店自选，7店通用。

二、输出要求
只输出改写后的完整口播话术。不要输出修正点、待确认、解释或前后缀。保留自然段落。

原话术如下：
【粘贴用户原话术】`
	const sample = "这是本片原始话术。"
	s := &Server{}
	if facts, err := s.resolveScriptPieceFacts(t.Context(), 1, 2, customPrompt); err != nil || len(facts) != 0 {
		t.Fatal("an inline template must not require a fact library read", facts, err)
	}
	first := "直播间一号链接徐香猕猴桃，九块九三斤。" + strings.Repeat("果子可以到店自选。", 23)
	last := strings.Repeat("七家门店都可以用券。", 26)
	plain := first + "\n\n" + last
	structured := "话术骨架：报价格→门店权益\n一句话上下文：一号链接价格和通用权益\n话术：" + first + last
	for _, tc := range []struct {
		name  string
		reply string
		want  string
		live  bool
	}{
		{name: "structured preview", reply: structured, want: first + last},
		{name: "plain preview", reply: "\ufeff" + strings.ReplaceAll(plain, "\n", "\r\n"), want: plain},
		{name: "plain point playback", reply: plain, want: first + last, live: true},
		{name: "short plain preview", reply: "一号链接九块九，三斤。\n\n可以到店自提。", want: "一号链接九块九，三斤。\n\n可以到店自提。"},
		{name: "partial structured reply", reply: "话术骨架：只输出了骨架\n话术：" + plain, live: true},
		{name: "plain with correction notes", reply: plain + "\n修正点：略", live: true},
		{name: "structured with correction notes", reply: structured + "\n待确认：没有", live: true},
		{name: "fenced response", reply: "```text\n" + plain + "\n```", live: true},
		{name: "short plain response", reply: "一号链接九块九，三斤。", want: "一号链接九块九，三斤。", live: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := agentgateway.New("preview-test", "preview-test-model")
			var calls []agentgateway.Request
			gateway.Register(scriptPreviewTestProvider(func(req agentgateway.Request) agentgateway.Response {
				calls = append(calls, req)
				return agentgateway.Response{Text: tc.reply}
			}))
			input := scriptTextGenerationInput{material: scriptTextMaterial{
				unit: volcengineScriptUnit{Text: sample, SpecPrompt: customPrompt}, redLines: "禁止虚构库存",
				facts: json.RawMessage(`[{"无关事实":"不注入销量"}]`),
			}}
			if tc.live {
				input.session = 17
			}
			result, err := (&Server{agentGateway: gateway}).generateScriptText(t.Context(), scriptGenerationSpecified, input)
			if result.Script != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("body = %q, err = %v", result.Script, err)
			}
			if len(calls) != 1 || len(calls[0].Messages) != 1 || (tc.live && (!calls[0].EnableThinking || !calls[0].UseProviderOutputLimit)) {
				t.Fatalf("expected a single writing request: %+v", calls)
			}
			prompt := calls[0].Messages[0].Content
			if !strings.Contains(prompt, "1号链接：徐香猕猴桃；9.9元3斤") || !strings.Contains(prompt, "全门店：到店自选，7店通用") ||
				!strings.Contains(prompt, "原话术如下：\n"+sample) || !strings.Contains(prompt, "【红线禁止】\n禁止虚构库存") ||
				!strings.HasSuffix(prompt, planprompt.RewriteOutputContract) || strings.Count(prompt, sample) != 1 ||
				strings.Contains(prompt, scriptOriginalSlot) || strings.Contains(prompt, "无关事实") {
				t.Fatalf("unexpected prompt: %s", prompt)
			}
		})
	}
}

const testScopedScriptPrompt = "【引用事实：1号链接、全门店】\n\n先讲1号链接，再用全门店通用权益收口。\n\n原话术如下：\n【粘贴用户原话术】"

func TestSpecifiedScriptSelectsConfirmedFactsAndReplacesOriginalSlot(t *testing.T) {
	legacy := model.PlanMaterialClaim{Status: "active", ScopeKind: "product", ProductLinkID: 7, Attribute: "价格", Value: "9.9 元 3 斤"}
	shared := model.PlanMaterialClaim{Status: "active", Attribute: "核销", Value: "仅到店"}
	shared.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 7}, {Kind: "product", LinkID: 8}}
	directory := model.MaterialObjectCatalog{Directory: model.MaterialObjectDirectory{GlobalDisplayName: "全门店"}}
	available, _ := projectRewriteFactsByObject([]model.PlanMaterialClaim{
		legacy, shared,
		{Status: "active", ScopeKind: "plan", Attribute: "售后", Value: "未用可退"},
		{Status: "draft", ScopeKind: "plan", Attribute: "运费", Value: "草稿承诺"},
		{Status: "active", ScopeKind: "product", ProductLinkID: 8, Attribute: "价格", Value: "其他链接价格"},
	}, directory, []db.QALink{{ID: 7, LinkKey: "1"}, {ID: 8, LinkKey: "2"}}, true)
	template, err := parseScriptPieceTemplate(strings.ReplaceAll(testScopedScriptPrompt, "\n", "\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := template.selectFacts(available)
	if err != nil {
		t.Fatal(err)
	}
	unit := volcengineScriptUnit{Text: "原文仅在此出现", SpecPrompt: testScopedScriptPrompt}
	valid := strings.Repeat("欢迎进店，买好的商品可以到店自提。", 30)
	gateway := agentgateway.New("preview-test", "preview-test-model")
	var calls []agentgateway.Request
	gateway.Register(scriptPreviewTestProvider(func(req agentgateway.Request) agentgateway.Response {
		calls = append(calls, req)
		return agentgateway.Response{Text: "话术骨架：招呼\n一句话上下文：介绍自提方式\n话术：" + valid}
	}))
	s := &Server{agentGateway: gateway}
	for _, session := range []int64{0, 887} {
		if _, err := s.generateScriptText(t.Context(), scriptGenerationSpecified, scriptTextGenerationInput{
			material: scriptTextMaterial{unit: unit, facts: facts, redLines: "不得报错价格"}, session: session,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected one model call per entry point, got %d", len(calls))
	}
	for _, call := range calls {
		prompt := call.Messages[0].Content
		for _, want := range []string{`"对象":"1号链接"`, `"对象":"全门店"`, "9.9 元 3 斤", "仅到店", "未用可退", "原话术如下：\n" + unit.Text, "【红线禁止】\n不得报错价格", strings.TrimSpace(planprompt.RewriteOutputContract)} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("missing %q in %s", want, prompt)
			}
		}
		for _, forbidden := range []string{scriptFactReference, scriptOriginalSlot, "2号链接", "其他链接价格", "草稿承诺"} {
			if strings.Contains(prompt, forbidden) {
				t.Fatalf("unexpected %q in %s", forbidden, prompt)
			}
		}
		if strings.Count(prompt, unit.Text) != 1 {
			t.Fatal("original script must be inserted exactly once", prompt)
		}
	}
}

func TestSpecifiedScriptRejectsUnresolvedTemplateBeforeCallingModel(t *testing.T) {
	for _, prompt := range []string{
		strings.Replace(testScopedScriptPrompt, "1号链接、全门店", "待指定", 1),
		strings.Replace(testScopedScriptPrompt, scriptOriginalSlot, "遗漏占位", 1),
		"改写正文\n" + testScopedScriptPrompt,
		testScopedScriptPrompt + "\n" + scriptOriginalSlot,
		testScopedScriptPrompt,
	} {
		_, err := (&Server{}).generateScriptText(t.Context(), scriptGenerationSpecified,
			scriptTextGenerationInput{material: scriptTextMaterial{unit: volcengineScriptUnit{Text: "原话术", SpecPrompt: prompt}}})
		var templateError *scriptPieceTemplateError
		if !errors.As(err, &templateError) {
			t.Fatalf("expected template error for %q, got %v", prompt, err)
		}
		w := httptest.NewRecorder()
		scriptPreviewError(w, err, 1, 2, "p")
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatal(w.Code, w.Body)
		}
	}
	template, err := parseScriptPieceTemplate(testScopedScriptPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := template.selectFacts([]rewriteFact{{Object: "1号链接", Attribute: "价格", Value: json.RawMessage(`"9.9元"`)}}); err == nil || !strings.Contains(err.Error(), "全门店") {
		t.Fatal("must report missing objects", err)
	}
}

func TestSpecifiedScriptPoolChecksReferencedFactVersion(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "片段原文", SpecPrompt: testScopedScriptPrompt, SpecPromptEnabled: true}
	policyFixture(t, root, v, u, 1, u.Text, 60000, scriptplay.KindOriginal)
	scoped := policyFixture(t, root, v, u, 2, "指定版本", 60000, scriptplay.KindRewrite)
	pool, _ := loadScriptVoicePool(root, v.PlanID, u.poolID())
	pool.Versions[1].SpecHash = scriptPiecePromptHash(u)
	if _, ok := scriptVersionAudio(root, u, v, pool.Versions[1]); ok {
		t.Fatal("old versions generated before facts injection must not replay")
	}
	u.SpecFactsKey = scriptPieceFactsKey(json.RawMessage(`["引用的原事实"]`))
	pool.Versions[1].SpecHash = scriptPieceVersionHash(u)
	if err := saveScriptVoicePool(root, v.PlanID, u.poolID(), pool); err != nil {
		t.Fatal(err)
	}
	audio, ok := scriptVersionAudio(root, u, v, pool.Versions[1])
	if !ok || audio.SpecHash != pool.Versions[1].SpecHash || audio.AssetID != 2 || scoped.AudioRef != "2" {
		t.Fatal("matching specified version must be playable and carry its marker", audio)
	}
	u.SpecFactsKey = scriptPieceFactsKey(json.RawMessage(`["更新后的引用事实"]`))
	if versions := scriptCompatiblePool(root, v, u).Versions; len(versions) != 1 || versions[0].Kind != scriptplay.KindOriginal {
		t.Fatal("changed facts must expire only the specified rewrite", versions)
	}
}

func TestSpecifiedScriptSavedWithPieceAndPromptSwitch(t *testing.T) {
	v := policyVersion()
	v.GenerationContext = map[string]any{"recording_script": recordingScript{Version: 2, Tabs: []recordingScriptTab{
		{TabID: "t1", Entries: []recordingScriptEntry{
			{EntryID: "s1", Text: strings.Repeat("甲", 450), SpecPromptEnabled: true},
			{EntryID: "s2", Text: strings.Repeat("乙", 410), SpecPrompt: "   特定写法  ", SpecPromptEnabled: true, SpecPromptAt: "2026-11-24T06:00:00Z"},
			{EntryID: "s3", Text: strings.Repeat("丙", 490), SpecPrompt: "只保存不启用"},
		}},
	}}}
	if err := normalizeRecordingScript(v.GenerationContext); err != nil {
		t.Fatal(err)
	}
	units, err := scriptFirstUnits(v, t.TempDir())
	if err != nil || len(units) != 3 {
		t.Fatal(units, err)
	}
	if units[0].poolID() != "e-t1-s2" || units[0].SpecPrompt != "特定写法" || scriptPiecePromptHash(units[0]) == "" {
		t.Fatal("opening reorder mixed the piece prompts", units)
	}
	groups := scriptManualGroups(v, units)
	if pieces := groups[0].Pieces; pieces[0].Specified || !pieces[1].Specified || pieces[2].Specified {
		t.Fatal(pieces)
	}
}

func TestSpecifiedScriptPoolPlaybackAndRenewal(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "片段原文", SpecPrompt: "这里是专属写法", SpecPromptEnabled: true}
	units := []volcengineScriptUnit{u}
	now := time.Now().UTC()
	policyFixture(t, root, v, u, 1, u.Text, 40*60000, scriptplay.KindOriginal)
	if cap, retire, err := specifiedScriptSlot(root, v, units, 0, false, now, false); cap != 2 || retire != "" || err != nil {
		t.Fatal(cap, retire, err)
	}
	spec := policyFixture(t, root, v, u, 2, "指定版本", 40*60000, scriptplay.KindRewrite)
	p, _ := loadScriptVoicePool(root, v.PlanID, u.poolID())
	p.Versions[1].SpecHash = scriptPiecePromptHash(u)
	if err := saveScriptVoicePool(root, v.PlanID, u.poolID(), p); err != nil {
		t.Fatal(err)
	}
	pool := scriptCompatiblePool(root, v, u)
	if len(pool.Versions) != 2 || pool.Versions[1].SpecHash == "" {
		t.Fatal("specified version is missing from ordinary rotation", pool)
	}
	if _, found, err := scriptMissingSharedText(root, v, u); found || err != nil {
		t.Fatalf("specified text crossed to auto generation: found=%v err=%v", found, err)
	}
	if cap, retire, err := specifiedScriptSlot(root, v, units, 0, true, now, false); cap != 0 || retire != "" || err != nil {
		t.Fatal("a full young pool must replay", cap, retire, err)
	}
	for i := range p.Versions {
		p.Versions[i].PlayCount = rewriteRetireAfterPlays
		p.Versions[i].CompleteCount = rewriteRetireAfterPlays
	}
	if err := saveScriptVoicePool(root, v.PlanID, u.poolID(), p); err != nil {
		t.Fatal(err)
	}
	if cap, retire, err := specifiedScriptSlot(root, v, units, 0, true, now, false); cap != 2 || retire != spec.AudioRef || err != nil {
		t.Fatal("eligible batch did not admit a replacement", cap, retire, err)
	}
	rng := rand.New(rand.NewSource(1))
	reused := map[int64]bool{}
	for i := 0; i < 2; i++ {
		audio, _, ok := scriptCompatiblePoolAudio(root, v, u, "test auto", rng)
		if !ok {
			t.Fatal("automatic replay could not read compatible audio")
		}
		reused[audio.AssetID] = true
	}
	if !reused[1] || !reused[2] {
		t.Fatal("automatic rotation omitted a source", reused)
	}
	u.SpecPrompt += "改变"
	if len(scriptCompatiblePool(root, v, u).Versions) != 1 {
		t.Fatal("an edited prompt retained its stale specified version")
	}
}
