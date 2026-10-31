package paidpipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"livecompanion/core/internal/interactionruntime"
)

type preparedBatchItemV2 struct {
	Content interactionruntime.Content
	Err     error
}
type prepareInputV2 struct {
	Template        string                              `json:"template,omitempty"`
	Protocol        string                              `json:"protocol"`
	Task            string                              `json:"task"`
	Epoch           string                              `json:"epoch"`
	Revision        uint64                              `json:"revision"`
	Tenant          int64                               `json:"tenant_id"`
	Room            int64                               `json:"room_id"`
	Plan            int64                               `json:"plan_id"`
	Kind            string                              `json:"kind"`
	Text            string                              `json:"text"`
	Original        string                              `json:"original,omitempty"`
	Clarification   string                              `json:"clarification,omitempty"`
	Recent          []interactionruntime.Turn           `json:"recent,omitempty"`
	Context         []interactionruntime.ContextMessage `json:"context,omitempty"`
	Mainline        string                              `json:"current_mainline,omitempty"`
	EngagementToken string                              `json:"engagement_token,omitempty"`
	Count           int                                 `json:"event_count"`
	AllowClarify    bool                                `json:"allow_clarify"`
	Expires         time.Time                           `json:"expires_at"`
}

func (h HTTPPurposeResolver) prepareBatchV2(ctx context.Context, batch interactionruntime.Batch) ([]preparedBatchItemV2, error) {
	if len(batch.Works) < 1 || len(batch.Works) > 10 || batch.ID == "" {
		return nil, fmt.Errorf("invalid interaction batch")
	}
	first := batch.Works[0]
	items := make([]prepareInputV2, len(batch.Works))
	seen := map[string]bool{}
	for i, w := range batch.Works {
		if w.Scope != first.Scope || w.ID == "" || seen[w.ID] || w.Revision == 0 {
			return nil, fmt.Errorf("interaction batch scope changed")
		}
		seen[w.ID] = true
		items[i] = prepareInputV2{Protocol: interactionruntime.Protocol, Task: w.ID, Epoch: w.Scope.Epoch, Revision: w.Revision, Tenant: w.Scope.Tenant, Room: w.Scope.Room, Plan: w.Scope.Plan, Kind: w.Kind, Text: w.Text, Original: w.Original, Clarification: w.Clarification, Recent: w.Recent, Context: w.Context, Mainline: w.Mainline, EngagementToken: w.EngagementToken, Count: w.Count, AllowClarify: w.AllowClarify, Expires: w.Expires}
		items[i].Template = w.Template
	}
	payload := struct {
		Protocol string           `json:"protocol"`
		BatchID  string           `json:"batch_id"`
		Tenant   int64            `json:"tenant_id"`
		Room     int64            `json:"room_id"`
		Plan     int64            `json:"plan_id"`
		Epoch    string           `json:"epoch"`
		Items    []prepareInputV2 `json:"items"`
	}{interactionruntime.Protocol, batch.ID, first.Scope.Tenant, first.Scope.Room, first.Scope.Plan, first.Scope.Epoch, items}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.URL, "/")+"/internal/v1/interaction-purpose/resolve", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", h.Token)
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 29 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var detail struct {
			Error string `json:"error"`
		}
		if resp.StatusCode == 422 && json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 2048)).Decode(&detail) == nil && strings.HasPrefix(detail.Error, "当前互动模型输出上限") {
			return nil, fmt.Errorf("interaction batch preparation http %d: %s", resp.StatusCode, detail.Error)
		}
		return nil, fmt.Errorf("interaction batch preparation http %d", resp.StatusCode)
	}
	var response struct {
		Protocol string   `json:"protocol"`
		BatchID  string   `json:"batch_id"`
		Issues   []string `json:"issues"`
		Results  []struct {
			Task      string          `json:"task"`
			Revision  uint64          `json:"revision"`
			Knowledge json.RawMessage `json:"knowledge"`
			Error     string          `json:"error"`
		} `json:"results"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 256<<10)).Decode(&response); err != nil {
		return nil, err
	}
	if response.Protocol != interactionruntime.Protocol || response.BatchID != batch.ID || len(response.Results) > 20 {
		return nil, fmt.Errorf("interaction batch identity changed")
	}
	if len(response.Issues) > 0 {
		log.Printf("interaction v2 batch=%s format_issues=%d", batch.ID, len(response.Issues))
	}
	byID := map[string]int{}
	for i, w := range batch.Works {
		byID[w.ID] = i
	}
	out := make([]preparedBatchItemV2, len(batch.Works))
	counts := map[string]int{}
	for i := range out {
		out[i].Err = fmt.Errorf("interaction batch missing task")
	}
	for _, result := range response.Results {
		i, ok := byID[result.Task]
		if !ok {
			return nil, fmt.Errorf("interaction batch unknown task")
		}
		counts[result.Task]++
		if counts[result.Task] > 1 {
			out[i] = preparedBatchItemV2{Err: fmt.Errorf("interaction batch duplicated task")}
			continue
		}
		if result.Revision != batch.Works[i].Revision {
			out[i].Err = fmt.Errorf("interaction batch revision changed")
			continue
		}
		if result.Error != "" {
			out[i].Err = fmt.Errorf("interaction content: %s", result.Error)
			continue
		}
		var e struct {
			Template                    string `json:"template"`
			EventCount                  int    `json:"event_count"`
			Protocol, Task, Epoch, Body string
			Revision                    uint64
			Tenant                      int64                     `json:"tenant_id"`
			Room                        int64                     `json:"room_id"`
			Plan                        int64                     `json:"plan_id"`
			Intent                      interactionruntime.Intent `json:"intent"`
			Expires                     time.Time                 `json:"expires_at"`
		}
		w := batch.Works[i]
		if json.Unmarshal(result.Knowledge, &e) != nil || e.Protocol != interactionruntime.Protocol || e.Task != w.ID || e.Revision != w.Revision || e.Epoch != w.Scope.Epoch || e.Tenant != w.Scope.Tenant || e.Room != w.Scope.Room || e.Plan != w.Scope.Plan || !e.Expires.Equal(w.Expires) {
			out[i].Err = fmt.Errorf("interaction result identity changed")
			continue
		}
		if e.Template != w.Template || (w.Template != "" && e.EventCount != w.Count) {
			out[i].Err = fmt.Errorf("interaction template identity changed")
			continue
		}
		out[i] = preparedBatchItemV2{Content: interactionruntime.Content{Body: e.Body, Intent: e.Intent, Knowledge: result.Knowledge}}
	}
	return out, nil
}
