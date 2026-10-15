package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
)

type fourLayerBodyCase struct {
	Name         string                 `json:"name"`
	SellingPoint string                 `json:"selling_point"`
	Angle        string                 `json:"angle"`
	Scope        string                 `json:"scope"`
	BaselineJSON string                 `json:"baseline_json"`
	Facts        []anchorcycle.HookFact `json:"facts"`
	Boundary     []anchorcycle.HookFact `json:"boundary"`
	Expected     []string               `json:"expected"`
}

type fourLayerWireRecorder struct{ calls []map[string]any }

func (r *fourLayerWireRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body, e := io.ReadAll(req.Body)
	if e != nil {
		return nil, e
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	var payload any
	if e = json.Unmarshal(body, &payload); e != nil {
		return nil, e
	}
	call := map[string]any{"payload": payload}
	r.calls = append(r.calls, call)
	// Only the known HTTPS model endpoint is used; never record credential headers.
	response, e := http.DefaultTransport.RoundTrip(req)
	if e != nil {
		call["transport_error"] = e.Error()
		return nil, e
	}
	raw, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil {
		return nil, e
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	call["status"] = response.StatusCode
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		call["response"] = parsed
	} else {
		call["response_text"] = string(raw)
	}
	return response, nil
}

// Explicit, paid, text-only provider replay. Never starts a live session,
// calls TTS, adopts business assets, or writes to the production database.
func TestFourLayerBodyProviderProbe(t *testing.T) {
	if os.Getenv("FOUR_LAYER_BODY_PROBE") != "1" {
		t.Skip("explicit paid four-layer body verification required")
	}
	out := os.Getenv("FOUR_LAYER_BODY_OUTPUT")
	if out == "" {
		t.Fatal("output path required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("model configuration unavailable")
	}
	defer store.Close()
	profileID := strings.TrimSpace(os.Getenv("FOUR_LAYER_BODY_PROFILE"))
	if profileID == "" {
		t.Fatal("explicit pinned profile required")
	}
	profile, e := store.ResolveSpeechModel(ctx, profileID)
	if e != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		t.Fatal("explicit model unavailable")
	}
	if profile.Model != "qwen3.7-flash" || profile.Protocol != "openai_chat" || profile.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatal("unexpected pinned model or endpoint")
	}
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	policy, e := contentguard.Compile(p0)
	if e != nil {
		t.Fatal(e)
	}
	mother, e := interactionpolicy.Read("mainline-text-task.md")
	if e != nil {
		t.Fatal(e)
	}
	save := func(value any) {
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(out, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("FOUR_LAYER_BODY_MODE") == "snapshot" {
		tenant, _ := strconv.ParseInt(os.Getenv("FOUR_LAYER_BODY_TENANT"), 10, 64)
		room, _ := strconv.ParseInt(os.Getenv("FOUR_LAYER_BODY_ROOM"), 10, 64)
		if tenant <= 0 || room <= 0 {
			t.Fatal("explicit tenant and room required for snapshot")
		}
		plan, err := store.GetLiveAgentPlanForRoom(ctx, tenant, room)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := store.ListLiveAgentPlanFacts(ctx, tenant, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		links, err := store.ListLiveAgentPlanProductLinks(ctx, tenant, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		card, err := store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := store.GetActiveLiveAnchorStyleMetaPrompt(ctx, tenant, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		wheel, err := store.GetActiveLiveSellingPointWheel(ctx, tenant, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		save(map[string]any{"model": profile, "plan": plan, "facts": facts, "products": links, "baseline_json": card.EffectiveScorecardJSON(), "style_md": meta.EffectiveMetaPromptMD(), "wheel_json": wheel.EffectiveWheelJSON(), "p0": p0, "p0_source": source, "p0_sha256": policy.SHA256, "mainline_mother": string(mother), "database_writes": false})
		t.Logf("snapshot plan=%d facts=%d model=%s", plan.ID, len(facts), profile.Model)
		return
	}
	raw, e := os.ReadFile(os.Getenv("FOUR_LAYER_BODY_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	var cases []fourLayerBodyCase
	if json.Unmarshal(raw, &cases) != nil || len(cases) == 0 || len(cases) > 32 {
		t.Fatal("invalid explicit cases")
	}
	rows := make([]map[string]any, len(cases))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 2)
	for i, c := range cases {
		wg.Add(1)
		go func(i int, c fourLayerBodyCase) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			row := map[string]any{"case": c}
			rows[i] = row
			fail := func(err error) { row["error"] = err.Error() }
			task := anchorcycle.Task{Key: "offline-" + c.Name, SellingPoint: c.SellingPoint, Angle: c.Angle, AllowedLinksJSON: `["1"]`, FactScope: &anchorcycle.TaskFactScope{Facts: c.Facts, ProductBoundary: c.Boundary}}
			state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 300, BodySegments: 3, ComplianceScope: c.Scope})
			if err != nil {
				fail(err)
				return
			}
			state.Phase = "body"
			state.BaselineJSON = c.BaselineJSON
			if err = state.InstallP0(p0, source); err != nil {
				fail(err)
				return
			}
			if err = requireCycleP0(&state); err != nil {
				fail(err)
				return
			}
			messages, err := renderCycleTextMessages(&state)
			if err != nil {
				fail(err)
				return
			}
			if os.Getenv("FOUR_LAYER_BODY_DIRECT") == "1" {
				// Isolate the user's four-input formula from optional cycle/fission controls.
				packet, _ := json.Marshal(map[string]any{"本次任务": generationTaskView(task), "字数上限": 300, "本段授权的商品称呼": []string{"1号链接"}})
				for j := range messages {
					if messages[j].Role == "user" {
						messages[j].Content = string(packet)
					}
				}
				row["direct_four_input"] = true
			}
			disabled := false
			request := agentgateway.Request{Stage: "mainline_text", Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 1800, EnableThinking: false, ThinkingOverride: &disabled, CacheSystemPrompt: true, Timeout: 120 * time.Second}
			if profile.SystemPrompt != "" {
				request.Messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, request.Messages...)
			}
			request, err = agentgateway.PrepareTextRequest(*profile, request)
			if err != nil {
				fail(err)
				return
			}
			row["request"] = request
			row["p0_sha256"] = policy.SHA256
			wire := &fourLayerWireRecorder{}
			client := &http.Client{Transport: wire, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect rejected") }}
			provider := agentgateway.NewQwenProvider(agentgateway.QwenConfig{BaseURL: strings.TrimRight(profile.BaseURL, "/"), APIKey: profile.APIKey, Client: client, MaxTokenField: profile.ChatTokenField})
			started := time.Now()
			response, err := provider.Complete(ctx, request)
			row["response"] = response
			row["wire"] = wire.calls
			row["elapsed_ms"] = time.Since(started).Milliseconds()
			if err != nil {
				fail(err)
				return
			}
			row["chars"] = utf8.RuneCountInString(response.Text)
			row["control_leak"] = textControlLeak(response.Text)
			row["literal_p0_scan"] = policy.Scan(response.Text, c.Scope)
		}(i, c)
	}
	wg.Wait()
	save(map[string]any{"model": profile, "rows": rows, "p0_sha256": policy.SHA256, "mainline_mother": string(mother), "database_writes": false, "tts": false, "post_generation_rewrite": false, "semantic_model_self_review": false, "production_configuration_changed": false})
	for _, row := range rows {
		if row["error"] != nil {
			t.Errorf("case=%s: %s", row["case"].(fourLayerBodyCase).Name, row["error"])
		}
	}
	completed := 0
	for _, row := range rows {
		if row["error"] == nil {
			completed++
		}
	}
	t.Logf("raw body completed=%d requested=%d", completed, len(rows))
}
