package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"os"
	"strings"
	"testing"
	"time"
)

type rephraseProbeFixture struct{ Name, Category, Facts, Before, RequestedChange, ExpectedInitial string }

func sentenceRephraseFixtures() []rephraseProbeFixture {
	return []rephraseProbeFixture{
		{"正常最好建议", "熟食", "即食卤炸鸭，未开封不抽真空常温2天。", "未开封不抽真空常温两天，最好两天内吃完。", "", "通过"},
		{"用户要求润词", "熟食", "即食卤炸鸭，未开封不抽真空常温2天。", "未开封不抽真空常温两天，最好两天内吃完。", "把最好改成自然建议，保持未开封、不抽真空、常温和两天不变。", "通过"},
		{"正常担心句", "熟食", "即食卤炸鸭开袋直接吃。", "买吃的最怕不新鲜，挑自己合口味的就行。", "", "通过"},
		{"正常主观口味", "熟食", "即食卤炸鸭，可选麻辣或五香。", "喜欢这个口味的肯定觉得好吃。", "", "通过"},
		{"无依据排名", "熟食", "即食卤炸鸭900g整只87.6元，无可核验价格排名。", "我们是全网最低价，谁都比不过。", "", "有问题"},
		{"到货保证", "熟食", "即食卤炸鸭，使用京东物流；一般下午6点前下单当天发，未确认到货时间。", "保证今天下单明天就收到。", "", "有问题"},
		{"无依据添加剂", "熟食", "即食卤炸鸭开袋直接吃，未确认是否含防腐剂。", "完全没有防腐剂，放心吃。", "", "有问题"},
		{"食品医疗主张", "熟食", "普通即食卤炸鸭，不是药品，没有疾病治疗依据。", "吃了能治胃病。", "", "有问题"},
		{"护肤绝对适用", "护肤品", "50g外用保湿霜只用于完整皮肤，敏感肌首次先局部测试，出现不适停用。", "零刺激，所有皮肤都能放心用。", "", "有问题"},
		{"课程退款承诺", "课程", "线上摄影课6次，每次60分钟199元，未开课可退款，开课后不可退款。", "上过课不喜欢，也能随时全额退。", "", "有问题"},
		{"咖啡未知赠品", "咖啡豆", "烘焙咖啡整豆200g39元，未确认赠送磨豆机。", "这次不送磨豆机。", "", "有问题"},
		{"服装机洗保证", "服装", "纯棉短袖129元，标签要求冷水手洗，不可漂白，未确认可机洗。", "放洗衣机随便洗，保证不变形。", "", "有问题"},
		{"家电未知功能", "家电", "3L电饭煲399元，用于煮饭，未确认煎炸功能。", "这个没有煎炸功能，不能拿来炸。", "", "有问题"},
	}
}

func TestSentenceRephraseUsesConfiguredAuditRole(t *testing.T) {
	if os.Getenv("SENTENCE_REPHRASE_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid offline rewrite test")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.ResolveTextRole(ctx, "mainline_p0_context")
	if err != nil || reviewID == "" {
		t.Fatal("configured audit role unavailable")
	}
	reviewer, err := store.ResolveSpeechModel(ctx, reviewID)
	if err != nil || reviewer == nil {
		t.Fatal("audit model unavailable")
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
	}
	p0Source, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(p0Source)
	if err != nil {
		t.Fatal(err)
	}
	p0Mother, err := interactionpolicy.Read("model-content-p0-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	p0 := strings.ReplaceAll(string(p0Mother), "{{p0_rules}}", policy.JSON())
	reviewMother, err := interactionpolicy.Read("final-fact-boundary-review.md")
	if err != nil {
		t.Fatal(err)
	}
	rephraseMother, err := interactionpolicy.Read("p0-one-sentence-rephrase.md")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	rows := []map[string]any{}
	archive := func() {
		raw, e := json.MarshalIndent(map[string]any{"rows": rows, "review_model": reviewer.Model, "review_profile_id": reviewID, "writer_model": writer.Model, "p0_source_sha": derivedHash(p0Source), "p0_rules_count": len(policy.Data.Rules), "p0_model_contract": p0, "review_mother": string(reviewMother), "rewrite_mother": string(rephraseMother), "offline": true, "adopted": false, "played": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("SENTENCE_REPHRASE_PROVIDER_OUTPUT"), raw, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}
	defer archive()
	call := func(profile *agentgateway.SpeechModel, stage, mother, input string) (agentgateway.Response, agentgateway.Request, error) {
		temperature := 0.2
		r := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: input}}, ResponseFormat: agentgateway.ResponseText, MaxTokens: 700, Timeout: 20 * time.Second, EnableThinking: false, Temperature: &temperature}
		id, e := s.beginModelExchange(ctx, stage, profile.ID, r)
		if e != nil {
			return agentgateway.Response{}, r, e
		}
		started := time.Now()
		callCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		res, e := agentgateway.CompleteSpeechEndpoint(callCtx, *profile, r)
		res.Provider, res.Model = "anchor:"+profile.ID, profile.Model
		e = s.finishModelExchange(ctx, id, res, res.Text, started, res.InputTokens, res.OutputTokens, e)
		return res, r, e
	}
	for _, f := range sentenceRephraseFixtures() {
		row := map[string]any{"fixture": f}
		rows = append(rows, row)
		prefix, suffix := "老乡，看这里。", "按需要选择。"
		source := prefix + f.Before + suffix
		scope := "all"
		if f.Category == "熟食" || f.Category == "咖啡豆" {
			scope = "ordinary_food"
		}
		hasHardHit := func(text string) bool {
			scan := policy.Scan(text, scope)
			if scan.Error != "" {
				t.Fatal(scan.Error)
			}
			for _, hit := range scan.Hits {
				if hit.ReviewMode == "hard" {
					return true
				}
			}
			return false
		}
		initialHard := hasHardHit(source)
		row["initial_hard_hit"] = initialHard
		target, e := anchorcycle.SelectSentenceRephraseTarget(source, len([]rune(prefix)), len([]rune(prefix+f.Before)))
		if e != nil {
			t.Fatal(e)
		}
		row["source"], row["target"] = source, target
		input := fmt.Sprintf("商品类别：%s\n本次完整确认事实：%s\n\n待核对正文：%s", f.Category, f.Facts, f.Before)
		initial, request, e := call(reviewer, "offline_sentence_rephrase_audit", string(reviewMother), input)
		row["initial_audit"], row["initial_request"] = initial, request
		decision := factBoundaryVerdict(initial.Text)
		row["initial_decision"] = decision
		if e != nil {
			row["action"] = "skip_audit_incomplete"
			t.Error("initial audit incomplete")
			archive()
			continue
		}
		if decision == "通过" && f.RequestedChange == "" && !initialHard {
			row["action"] = "keep"
			row["final_text"] = source
			archive()
			continue
		}
		if decision != "有问题" && f.RequestedChange == "" && !initialHard {
			row["action"] = "skip_uncertain"
			archive()
			continue
		}
		goal := f.RequestedChange
		if goal == "" {
			goal = "按审核指出的问题，仅改这句，回到已确认事实；不改变对象、条件和数字。"
		}
		rewriteInput := fmt.Sprintf("商品类别：%s\n本次完整确认事实：%s\n原句：%s\n修改目标：%s\n审核意见仅供核对，不能当新事实：%s", f.Category, f.Facts, f.Before, goal, initial.Text)
		rewritten, writeRequest, e := call(writer, "offline_sentence_rephrase_content", string(rephraseMother), rewriteInput)
		row["rewrite"], row["rewrite_request"] = rewritten, writeRequest
		if e != nil {
			row["action"] = "skip_rewrite_incomplete"
			t.Error("rewrite incomplete")
			archive()
			continue
		}
		body, e := anchorcycle.ApplySentenceRephrase(source, target, rewritten.Text)
		if e != nil {
			t.Fatal(e)
		}
		row["assembled_text"] = body
		row["outside_target_unchanged"] = strings.HasPrefix(body, prefix) && strings.HasSuffix(body, suffix)
		finalInput := fmt.Sprintf("商品类别：%s\n本次完整确认事实：%s\n\n待核对组装后全文：%s", f.Category, f.Facts, body)
		final, finalRequest, e := call(reviewer, "offline_sentence_rephrase_final_audit", string(reviewMother), finalInput)
		row["final_audit"], row["final_request"] = final, finalRequest
		row["final_decision"] = factBoundaryVerdict(final.Text)
		row["final_hard_hit"] = hasHardHit(body)
		if e == nil && row["final_decision"] == "通过" && row["final_hard_hit"] == false {
			row["action"] = "replace"
			row["final_text"] = body
		} else {
			row["action"] = "skip_after_rewrite"
		}
		if e != nil {
			t.Error("final audit incomplete")
		}
		archive()
		t.Logf("completed %s action=%s", f.Name, row["action"])
	}
}
