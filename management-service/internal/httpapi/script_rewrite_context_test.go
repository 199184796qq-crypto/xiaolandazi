package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScriptRewriteContextThreeTurnsSurvivesRestartAndOnlyInjectsLatestMetadata(t *testing.T) {
	root := t.TempDir()
	scope := newScriptRewriteContextScope(14, 4, "同一原始片", "同一事实")
	savedPrompt := "样本：{{样本话术}}\n历史：{{近期写作记录}}\n开场：{{开场方式}}\n{{已有开场}}"
	responses := []string{
		"话术骨架：先送礼再包装\n一句话上下文：从走亲戚切入，重点讲礼品包装。\n改写内容：第一版仅供声音使用的正文。",
		"话术骨架：先食用再口感\n一句话上下文：从怎么吃切入，重点讲口感。\n改写内容：第二版仅供声音使用的正文。",
		"话术骨架：先工艺再选择\n一句话上下文：从制作讲到如何选择规格。\n改写内容：第三版仅供声音使用的正文。",
	}
	var latest scriptRewriteOutput
	for i, raw := range responses {
		// Each turn rereads durable state; there is no process-local history cache.
		turn, err := beginScriptRewriteTurn(t.Context(), root, scope)
		if err != nil {
			t.Fatal(err)
		}
		prompt, err := renderScriptRewriteRequest(savedPrompt, "同一原始片", "原开头", "新方式", turn.Previous)
		if err != nil {
			turn.Close()
			t.Fatal(err)
		}
		if i == 0 {
			if !strings.Contains(prompt, "暂无上次写作记录") {
				t.Fatal(prompt)
			}
		} else {
			if !strings.Contains(prompt, latest.Skeleton) || !strings.Contains(prompt, latest.Context) || strings.Contains(prompt, latest.Script) || !strings.Contains(prompt, "尽量避开上次") {
				t.Fatal("last metadata missing or full body injected", prompt)
			}
		}
		if i == 2 && strings.Contains(prompt, "从走亲戚切入") {
			t.Fatal("all history accumulated instead of last response", prompt)
		}
		latest, err = parseScriptRewriteOutput(raw)
		if err != nil {
			turn.Close()
			t.Fatal(err)
		}
		if err = turn.Save(latest); err != nil {
			turn.Close()
			t.Fatal(err)
		}
		turn.Close()
		// TTS can fail or be skipped after this point without losing context.
	}
	turn, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	if turn.Previous != latest {
		t.Fatal("restart lost correspondence to last body", turn.Previous)
	}
}

func TestScriptRewriteContextIsolationAndLegacySummary(t *testing.T) {
	root := t.TempDir()
	scope := newScriptRewriteContextScope(14, 4, "原始片一", "单价八十七元")
	turn, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	result := scriptRewriteOutput{Skeleton: "结构", Context: "讲价格", Script: "已接受正文"}
	if err := turn.Save(result); err != nil {
		t.Fatal(err)
	}
	turn.Close()
	for _, other := range []scriptRewriteContextScope{
		newScriptRewriteContextScope(15, 4, "原始片一", "单价八十七元"),
		newScriptRewriteContextScope(14, 5, "原始片一", "单价八十七元"),
		newScriptRewriteContextScope(14, 4, "原始片二", "单价八十七元"),
		newScriptRewriteContextScope(14, 4, "原始片一", "单价九十九元"),
	} {
		next, err := beginScriptRewriteTurn(t.Context(), root, other)
		if err != nil {
			t.Fatal(err)
		}
		if next.Previous.Context != "" {
			t.Fatal("cross-scope context leaked", other)
		}
		next.Close()
	}
	// Voice/model are deliberately absent from this scope: changing voice
	// shares text metadata, just like the existing shared rewrite body cache.
	next, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	if next.Previous != result {
		t.Fatal(next.Previous)
	}
	if err := next.Save(scriptRewriteOutput{Script: "旧格式的新正文"}); err != nil {
		t.Fatal(err)
	}
	next.Close()
	next, err = beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Previous.Context != "" || next.Previous.Script != "旧格式的新正文" {
		t.Fatal("stale summary attached to legacy body", next.Previous)
	}
}

func TestScriptRewriteContextConcurrentTurnSeesLastFinishedResponse(t *testing.T) {
	root := t.TempDir()
	scope := newScriptRewriteContextScope(14, 4, "原文", "事实")
	first, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	started := make(chan struct{})
	got := make(chan *scriptRewriteTurn, 1)
	errCh := make(chan error, 1)
	go func() {
		close(started)
		second, err := beginScriptRewriteTurn(t.Context(), root, scope)
		if err != nil {
			errCh <- err
			return
		}
		got <- second
	}()
	<-started
	select {
	case second := <-got:
		second.Close()
		t.Fatal("same-source concurrent call did not wait")
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(20 * time.Millisecond):
	}
	latest := scriptRewriteOutput{Skeleton: "刚完成的结构", Context: "刚完成的上下文", Script: "最新正文"}
	if err := first.Save(latest); err != nil {
		t.Fatal(err)
	}
	first.Close()
	select {
	case second := <-got:
		if second.Previous != latest {
			t.Error("concurrent request read an older context", second.Previous)
		}
		second.Close()
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("waiting request did not resume")
	}
	scriptRewriteContextLocks.Lock()
	_, leaked := scriptRewriteContextLocks.entries[scope.path(root)]
	scriptRewriteContextLocks.Unlock()
	if leaked {
		t.Fatal("idle scope lock leaked")
	}
}

func TestScriptRewriteContextFailurePreservesBaselineAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	scope := newScriptRewriteContextScope(14, 4, "原文", "事实")
	first, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	latest := scriptRewriteOutput{Skeleton: "结构", Context: "有效上下文", Script: "有效正文"}
	if err := first.Save(latest); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(scope.path(root))
	if err := first.Save(scriptRewriteOutput{}); err == nil {
		t.Fatal("empty response replaced valid history")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := beginScriptRewriteTurn(ctx, root, scope); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	first.Close()
	// A model error or invalid KV is not saved; the prior response stays valid.
	failed, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseScriptRewriteOutput("话术骨架：不完整"); err == nil {
		t.Fatal("bad result accepted")
	}
	failed.Close()
	after, _ := os.ReadFile(scope.path(root))
	if string(after) != string(before) {
		t.Fatal("failed generation changed last response")
	}
	if err := os.WriteFile(scope.path(root), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := beginScriptRewriteTurn(t.Context(), root, scope); err == nil {
		t.Fatal("corrupt history silently discarded")
	}
	if err := os.WriteFile(scope.path(root), before, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := beginScriptRewriteTurn(t.Context(), root, scope)
	if err != nil {
		t.Fatal("failed read leaked the lock", err)
	}
	restored.Close()
	// Storage failure never reports a successful context receipt.
	badRoot := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(badRoot, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeVolcengineJSON(filepath.Join(badRoot, "state.json"), latest); err == nil {
		t.Fatal("failed storage reported success")
	}
}

func TestScriptRewriteContextRenderingFallbackAndLiteralPlaceholders(t *testing.T) {
	previous := scriptRewriteOutput{Skeleton: "顺序{{样本话术}}", Context: "角度{{已有开场}}", Script: "不要注入此正文"}
	for _, template := range []string{"样本：{{样本话术}}\n{{近期写作记录}}", "旧提示词，样本：{{样本话术}}"} {
		prompt, err := renderScriptRewriteRequest(template, "本片含{{近期写作记录}}字面文本", "开场数据", "开场方式", previous)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{previous.Skeleton, previous.Context, "本片含{{近期写作记录}}字面文本", "尽量避开上次", "相同事实"} {
			if !strings.Contains(prompt, want) {
				t.Fatal(want, prompt)
			}
		}
		if strings.Contains(prompt, previous.Script) {
			t.Fatal("full body leaked into next prompt")
		}
	}
	t.Setenv("SCRIPT_REWRITE_PREVIOUS_CONTEXT_PATH", filepath.Join(t.TempDir(), "missing.md"))
	if _, err := renderScriptRewriteRequest("{{近期写作记录}}", "样本", "", "", previous); err == nil {
		t.Fatal("missing required history template ignored")
	}
}
