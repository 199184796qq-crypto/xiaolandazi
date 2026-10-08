// Read-only operator utility. Reads the named Agent Studio sessions and their
// message history; it never creates sessions, sends messages or writes the DB.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/agentstudio"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

func main() {
	var input struct {
		Action      string   `json:"action"`
		ModelName   string   `json:"model_name"`
		WorkspaceID string   `json:"workspace_id"`
		ProfileID   string   `json:"credential_profile_id"`
		SessionIDs  []string `json:"session_ids"`
	}
	check(json.NewDecoder(os.Stdin).Decode(&input))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	store, err := appdb.Open(config.Load())
	check(err)
	defer store.Close()
	profile, err := store.ResolveSpeechModel(ctx, input.ProfileID)
	check(err)
	if profile == nil || profile.APIKey == "" {
		check(fmt.Errorf("no configured credential"))
	}
	// Reuse the production validation without exposing the decrypted credential.
	_, err = agentstudio.New(input.WorkspaceID, "cn-beijing", profile.APIKey, 60*time.Second)
	check(err)
	base := "https://" + input.WorkspaceID + ".cn-beijing.maas.aliyuncs.com/api/v1/agentstudio"
	client := &http.Client{Timeout: 60 * time.Second}
	get := func(path string, output any) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		check(err)
		req.Header.Set("Authorization", "Bearer "+profile.APIKey)
		response, err := client.Do(req)
		check(err)
		defer response.Body.Close()
		if response.StatusCode != 200 {
			check(fmt.Errorf("history request failed: HTTP %d", response.StatusCode))
		}
		check(json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(output))
	}
	if input.Action == "quotas" {
		base = strings.TrimSuffix(base, "/agentstudio")
		var quotas json.RawMessage
		query := url.Values{"page_no": {"1"}, "page_size": {"100"}}
		if input.ModelName != "" {
			query.Set("name", input.ModelName)
		}
		get("/models/limits?"+query.Encode(), &quotas)
		check(json.NewEncoder(os.Stdout).Encode(quotas))
		return
	}
	result := []map[string]any{}
	for _, id := range input.SessionIDs {
		if !agentstudio.ValidResourceID(id, "sesn_") {
			check(fmt.Errorf("invalid session ID"))
		}
		var session json.RawMessage
		get("/sessions/"+url.PathEscape(id), &session)
		events := []json.RawMessage{}
		cursor := ""
		visited := map[string]bool{}
		for {
			query := url.Values{"types": {"message"}, "order": {"asc"}, "limit": {"100"}}
			if cursor != "" {
				query.Set("page", cursor)
			}
			var page struct {
				Data     []json.RawMessage `json:"data"`
				NextPage *string           `json:"next_page"`
			}
			get("/sessions/"+url.PathEscape(id)+"/events?"+query.Encode(), &page)
			events = append(events, page.Data...)
			if page.NextPage == nil || strings.TrimSpace(*page.NextPage) == "" {
				break
			}
			cursor = *page.NextPage
			if visited[cursor] {
				check(fmt.Errorf("history pagination repeated a cursor"))
			}
			visited[cursor] = true
		}
		result = append(result, map[string]any{"session_id": id, "session": session, "events": events})
	}
	check(json.NewEncoder(os.Stdout).Encode(map[string]any{"sessions": result}))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
