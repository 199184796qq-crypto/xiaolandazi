// Isolated System One endpoint evaluation. Reads saved credentials; changes no business data.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type probeCase struct {
	Name     string          `json:"name"`
	Expected string          `json:"expected"`
	Category string          `json:"category"`
	Request  json.RawMessage `json:"request"`
}

func main() {
	profileID := flag.String("profile", "", "existing credential profile")
	endpoint := flag.String("endpoint", "", "official Alibaba System One endpoint")
	input := flag.String("input", "", "JSON cases")
	output := flag.String("output", "", "result archive")
	limit := flag.Int("limit", 0, "optional case limit")
	repeat := flag.Int("repeat", 1, "number of passes")
	flag.Parse()
	u, e := url.Parse(*endpoint)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || !(u.Hostname() == "dashscope.aliyuncs.com" || strings.HasSuffix(u.Hostname(), ".maas.aliyuncs.com")) || !strings.HasSuffix(u.Path, "/v1/systemone") {
		panic("official endpoint required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	store, e := db.Open(config.Load())
	if e != nil {
		panic("configuration database unavailable")
	}
	defer store.Close()
	profile, e := store.ResolveSpeechModel(ctx, *profileID)
	if e != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		panic("authorized credential unavailable")
	}
	purl, e := url.Parse(profile.BaseURL)
	if e != nil || !(purl.Hostname() == "dashscope.aliyuncs.com" || strings.HasSuffix(purl.Hostname(), ".maas.aliyuncs.com")) {
		panic("Alibaba credential required")
	}
	raw, e := os.ReadFile(*input)
	if e != nil {
		panic("input unavailable")
	}
	var cases []probeCase
	if json.Unmarshal(raw, &cases) != nil {
		panic("invalid probe cases")
	}
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}
	rows := []map[string]any{}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for pass := 0; pass < *repeat; pass++ {
		for _, c := range cases {
			req, e := http.NewRequestWithContext(ctx, "POST", *endpoint, bytes.NewReader(c.Request))
			if e != nil {
				panic("request invalid")
			}
			req.Header.Set("Authorization", "Bearer "+profile.APIKey)
			req.Header.Set("Content-Type", "application/json")
			started := time.Now()
			resp, err := client.Do(req)
			row := map[string]any{"name": c.Name, "expected": c.Expected, "category": c.Category, "pass": pass + 1, "request": json.RawMessage(c.Request), "request_sha256": fmt.Sprintf("%x", sha256.Sum256(c.Request)), "started_at": started.UTC().Format(time.RFC3339Nano), "endpoint_host": u.Hostname()}
			if err != nil {
				row["success"] = false
				row["error"] = "network request did not complete"
			} else {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
				resp.Body.Close()
				row["http_status"] = resp.StatusCode
				var decoded any
				if json.Unmarshal(body, &decoded) == nil {
					row["response"] = decoded
				} else {
					row["error"] = "non-JSON response"
				}
				row["success"] = readErr == nil && resp.StatusCode == 200 && decoded != nil
			}
			row["wall_latency_ms"] = time.Since(started).Milliseconds()
			rows = append(rows, row)
			saved, _ := json.MarshalIndent(rows, "", "  ")
			if os.WriteFile(*output, saved, 0600) != nil {
				panic("archive write failed")
			}
			fmt.Printf("case=%s pass=%d success=%v http=%v wall_ms=%v\n", c.Name, pass+1, row["success"], row["http_status"], row["wall_latency_ms"])
			if row["success"] != true {
				os.Exit(2)
			}
		}
	}
}
