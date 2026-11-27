package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

func TestScriptPolishPreviewRequiresAuthentication(t *testing.T) {
	handler := (&Server{auth: auth.NewResolver("test", nil)}).Handler()
	for _, path := range []string{"/script/cache-preview", "/script/polish-preview"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/live-agent-plans/4"+path,
			strings.NewReader(`{"group_id":"t2","entry_id":"s1","sample":"母版话术","mode":"polish"}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d: %s", path, response.Code, http.StatusUnauthorized, response.Body)
		}
	}
}

func TestScriptPreviewModeResolvesBeforeLoadingRewriteMaterial(t *testing.T) {
	gateway := agentgateway.New("preview-test", "preview-test-model")
	gateway.Register(scriptPreviewTestProvider(func(agentgateway.Request) agentgateway.Response {
		t.Fatal("unavailable or invalid mode called a model")
		return agentgateway.Response{}
	}))
	s := &Server{agentGateway: gateway}
	for _, tc := range []struct {
		mode   scriptGenerationMode
		status int
	}{
		{scriptGenerationPolish, http.StatusNotImplemented},
		{"unknown", http.StatusBadRequest},
		{scriptGenerationRewrite, http.StatusConflict},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/script/cache-preview", nil)
		r.Header.Set("Accept", "text/event-stream")
		s.writePlanScriptTextPreview(w, r, tc.mode, scriptTextMaterial{}, func() (planScriptCacheContext, error) {
			if tc.mode != scriptGenerationRewrite {
				t.Fatal("unavailable or invalid mode loaded rewrite material")
			}
			return planScriptCacheContext{}, &planScriptCacheError{http.StatusConflict, "缺少事实材料"}
		})
		if w.Code != tc.status || w.Flushed || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s returned %d, flushed=%t, body=%s", tc.mode, w.Code, w.Flushed, w.Body)
		}
	}
}

func TestScriptCacheEditorSharesLiveEntryPool(t *testing.T) {
	const tenant, plan = 9, 4
	root := t.TempDir()
	sample := planScriptCacheInput{GroupID: "t2", EntryID: "s1", Sample: strings.Repeat("母版话术", 100)}
	facts := json.RawMessage(`[{"scope":"全局","facts":["支持到店自提"]}]`)
	const redLines, requirements, persona = "禁止自造价格", "自然衔接", "小林"
	editor := newPlanScriptCacheContext(tenant, plan, sample, facts, redLines, requirements, persona)
	live := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{
		ID: 123, TenantID: tenant, PlanID: plan, VoiceIdentity: playbackIdentity("live-voice"),
		GenerationContext: map[string]any{"recording_script": recordingScript{Version: 2, Tabs: []recordingScriptTab{
			{TabID: "t1", Entries: []recordingScriptEntry{{EntryID: "s1", Text: strings.Repeat("另一组的母版", 70)}}},
			{TabID: sample.GroupID, Entries: []recordingScriptEntry{{EntryID: sample.EntryID, Text: sample.Sample}}},
		}}},
	}, newScriptRewriteSnapshot(redLines, facts))
	ids := scriptFirstEntryPoolIDs(live)
	if len(ids) != 2 || ids[1] != editor.unit.poolID() || scriptRewriteFactsKey(live) != scriptRewriteFactsKey(editor.version) {
		t.Fatalf("editor and live identities diverged: live=%v editor=%s", ids, editor.unit.poolID())
	}
	unit := volcengineScriptUnit{Pool: ids[1], Text: sample.Sample}
	if _, found, err := scriptMissingSharedText(root, live, unit); err != nil || found {
		t.Fatalf("preview wrote to live pool: found=%v err=%v", found, err)
	}
	const text = "欢迎进店，买好的商品可以到店自提。"
	for range 2 {
		if err := saveScriptSharedText(root, editor.version, editor.unit, text); err != nil {
			t.Fatal(err)
		}
	}
	if entry, found, err := scriptMissingSharedText(root, live, unit); err != nil || !found || entry.Text != text {
		t.Fatalf("live supply could not read cached text: %#v found=%v err=%v", entry, found, err)
	}
	items, err := loadScriptSharedTextsLocked(root, live, unit)
	if err != nil || len(items.Versions) != 1 || len(items.Versions[0].RenderedAudio) != 0 {
		t.Fatalf("save should only add one text: %#v err=%v", items, err)
	}
	if _, found, err := scriptMissingSharedText(root, live, volcengineScriptUnit{Pool: ids[0]}); err != nil || found {
		t.Fatalf("cached text leaked to a different group: found=%v err=%v", found, err)
	}
	changed := newPlanScriptCacheContext(tenant, plan, sample, facts, redLines, "另一套话术要求", persona)
	changedFacts := newPlanScriptCacheContext(tenant, plan, sample, json.RawMessage(`[]`), redLines, requirements, persona)
	changedPersona := newPlanScriptCacheContext(tenant, plan, sample, facts, redLines, requirements, "小李")
	if editor.basis == changed.basis || editor.basis == changedFacts.basis || editor.basis == changedPersona.basis {
		t.Fatal("preview basis failed to detect changed requirements or facts")
	}
}

type scriptPreviewTestProvider func(agentgateway.Request) agentgateway.Response

func (scriptPreviewTestProvider) Name() string { return "preview-test" }

func (f scriptPreviewTestProvider) Complete(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
	return f(req), nil
}

func TestScriptCachePreviewStreamsCurrentModelStage(t *testing.T) {
	const analysis = `{"逻辑链":[{"名称":"使用说明","段序":["开场","介绍","收尾"]}]}`
	script := strings.Repeat("欢迎进店，买好的商品可以到店自提。", 18)
	const groupID, entryID = "t2", "s1"
	cfg := newPlanScriptCacheContext(9, 4, planScriptCacheInput{GroupID: groupID, EntryID: entryID, Sample: strings.Repeat("这是原话术", 80)}, json.RawMessage(`[{"scope":"全局","facts":["支持到店自提"]}]`), "禁止自造价格", "自然衔接", "小林")
	for _, tc := range []struct {
		name            string
		accept          string
		mode            scriptGenerationMode
		invalidAnalysis bool
	}{
		{name: "stream", accept: "text/event-stream"},
		{name: "analysis error", accept: "text/event-stream", invalidAnalysis: true},
		{name: "JSON client", accept: "application/json", mode: scriptGenerationRewrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			streaming := tc.accept == "text/event-stream"
			var calls []string
			gateway := agentgateway.New("preview-test", "preview-test-model")
			gateway.Register(scriptPreviewTestProvider(func(req agentgateway.Request) agentgateway.Response {
				calls = append(calls, req.Stage)
				body := recorder.Body.String()
				switch req.Stage {
				case "script_analysis":
					if streaming && (!recorder.Flushed || !strings.Contains(body, "正在分析母版") || strings.Contains(body, "正在仿写话术")) {
						t.Fatalf("wrong progress on entry to analysis: %s", body)
					}
					if tc.invalidAnalysis {
						return agentgateway.Response{Text: "invalid"}
					}
					return agentgateway.Response{Text: analysis}
				case "script_rewrite":
					if streaming && !strings.Contains(body, "正在仿写话术") {
						t.Fatalf("rewrite started without a progress frame: %s", body)
					}
					if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "目标字数") {
						t.Fatal("expected a length target in the writing prompt")
					}
					return agentgateway.Response{Text: "话术骨架：开场→介绍→收尾\n一句话上下文：介绍自提方式\n话术：" + script}
				default:
					t.Fatalf("unexpected model stage %s", req.Stage)
					return agentgateway.Response{}
				}
			}))
			r := httptest.NewRequest("POST", "/script/cache-preview", nil)
			r.Header.Set("Accept", tc.accept)
			loads := 0
			(&Server{agentGateway: gateway}).writePlanScriptTextPreview(recorder, r, tc.mode, cfg.scriptTextMaterial, func() (planScriptCacheContext, error) {
				loads++
				return cfg, nil
			})
			if loads != 1 {
				t.Fatalf("expected one plan material read, got %d", loads)
			}
			body := recorder.Body.String()
			if tc.invalidAnalysis {
				if len(calls) != 1 || !strings.Contains(body, "event: error") || strings.Contains(body, "event: complete") {
					t.Fatalf("failed analysis ran rewrite or completed: %v %s", calls, body)
				}
				return
			}
			if len(calls) != 2 || !strings.Contains(body, script) || !strings.Contains(body, cfg.basis) {
				t.Fatalf("missing final draft: %v %s", calls, body)
			}
			if streaming {
				if recorder.Header().Get("X-Accel-Buffering") != "no" || !strings.Contains(body, "event: complete") {
					t.Fatalf("stream buffering or terminal frame missing: %s", body)
				}
			} else if !json.Valid(recorder.Body.Bytes()) {
				t.Fatalf("legacy client must receive JSON: %s", body)
			}
		})
	}
}
