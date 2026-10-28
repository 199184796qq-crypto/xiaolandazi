// Command addressing-grey-probe runs the program rules plus one batched semantic
// judgement on real nicknames, and prints the validated base for each name.
// It reads "userID<TAB>name" lines from stdin and never writes to the database.
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/addressing"
)

type profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	APIKey    string `json:"api_key"`
	MaxTokens int    `json:"max_tokens"`
	TimeoutMS int    `json:"timeout_ms"`
}

type settings struct {
	ReviewDefaultID string    `json:"review_default_id"`
	Profiles        []profile `json:"profiles"`
}

func main() {
	dry := flag.Bool("dry", false, "skip the model call and print program decisions only")
	emitDir := flag.String("emit", "", "write system.txt/user.txt for the grey batch and exit")
	rawPath := flag.String("raw", "", "read a saved model response and validate it")
	flag.Parse()

	rules := addressing.LoadDefault()
	if path := os.Getenv("NICKNAME_RULES_CONFIG"); path != "" {
		if loaded, err := addressing.Load(path); err == nil {
			rules = loaded
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var items []addressing.GreyCandidate
	type row struct {
		userID string
		raw    string
		status addressing.Status
		reason string
		text   string
	}
	var rows []row
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		userID, name := "", line
		if parts := strings.SplitN(line, "\t", 2); len(parts) == 2 {
			userID, name = parts[0], parts[1]
		}
		decision := rules.Check(name)
		rows = append(rows, row{userID: userID, raw: name, status: decision.Status, reason: decision.Reason, text: decision.Text})
		if decision.Status == addressing.StatusGrey {
			items = append(items, addressing.GreyCandidate{Viewer: addressing.Viewer{UserID: userID, RawName: name}, Candidate: decision.Text})
		}
	}

	var decisions []addressing.GreyDecision
	if *emitDir != "" && len(items) > 0 {
		system, user := addressing.GreyPrompt(items, rules)
		_ = os.WriteFile(*emitDir+"/system.txt", []byte(system), 0o644)
		_ = os.WriteFile(*emitDir+"/user.txt", []byte(user), 0o644)
		fmt.Printf("grey_items=%d system_bytes=%d user_bytes=%d\n", len(items), len(system), len(user))
		return
	}
	if *rawPath != "" && len(items) > 0 {
		raw, err := os.ReadFile(*rawPath)
		if err != nil {
			fmt.Printf("READ ERROR: %v\n", err)
			os.Exit(1)
		}
		decisions, err = addressing.ParseGrey(string(raw), items, rules)
		if err != nil {
			fmt.Printf("PARSE ERROR: %v\nraw=%s\n", err, truncate(string(raw), 500))
			os.Exit(1)
		}
	} else if len(items) > 0 && !*dry {
		system, user := addressing.GreyPrompt(items, rules)
		raw, err := callReviewModel(system, user)
		if err != nil {
			fmt.Printf("MODEL ERROR: %v\n", err)
			os.Exit(1)
		}
		decisions, err = addressing.ParseGrey(raw, items, rules)
		if err != nil {
			fmt.Printf("PARSE ERROR: %v\nraw=%s\n", err, raw)
			os.Exit(1)
		}
	}

	greyIndex := 0
	fmt.Printf("%-3s %-30s %-10s %-14s %s\n", "#", "原昵称", "判定", "原因", "基础昵称")
	for i, r := range rows {
		base, reason := "", r.reason
		status := string(r.status)
		switch r.status {
		case addressing.StatusSafe, addressing.StatusRewritten:
			base = r.text
		case addressing.StatusGrey:
			if greyIndex < len(decisions) {
				d := decisions[greyIndex]
				if d.OK {
					base = d.Base
					reason = "model_ok"
					status = "grey->ok"
				} else {
					reason = d.Reason
					status = "grey->drop"
				}
			}
			greyIndex++
		}
		if base == "" {
			base = "（集体称呼）"
		}
		fmt.Printf("%-3d %-30s %-10s %-14s %s\n", i+1, r.raw, status, reason, base)
	}
}

func callReviewModel(system, user string) (string, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"))
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow("SELECT config_json FROM mgmt_anchor_model_settings LIMIT 1").Scan(&raw); err != nil {
		return "", err
	}
	var cfg settings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", err
	}
	var chosen *profile
	for i := range cfg.Profiles {
		if cfg.Profiles[i].ID == cfg.ReviewDefaultID {
			chosen = &cfg.Profiles[i]
			break
		}
	}
	if chosen == nil {
		return "", fmt.Errorf("审核模型未配置")
	}
	timeout := 20 * time.Second
	if chosen.TimeoutMS > 0 {
		timeout = time.Duration(chosen.TimeoutMS) * time.Millisecond
	}
	body, _ := json.Marshal(map[string]any{
		"model":           chosen.Model,
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	url := strings.TrimRight(chosen.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+chosen.APIKey)
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("status=%d body=%s", response.StatusCode, truncate(string(payload), 200))
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("模型返回为空")
	}
	return decoded.Choices[0].Message.Content, nil
}

func truncate(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}
