package agentstudio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	workspaceIDPattern = regexp.MustCompile(`^(ws_[A-Za-z0-9_-]{3,128}|llm-[A-Za-z0-9-]{3,128})$`)
	resourceIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{3,160}$`)
	regionPattern      = regexp.MustCompile(`^[a-z0-9-]{3,48}$`)
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type AgentSnapshot struct {
	ID          string  `json:"id"`
	Version     int     `json:"version"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	System      string  `json:"system"`
	ArchivedAt  *string `json:"archived_at"`
	Model       struct {
		ID string `json:"id"`
	} `json:"model"`
}

type Session struct {
	ID            string        `json:"id"`
	Status        string        `json:"status"`
	Agent         AgentSnapshot `json:"agent"`
	EnvironmentID string        `json:"environment_id"`
	Title         string        `json:"title"`
	CreatedAt     string        `json:"created_at"`
}

type Environment struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
}

type TurnResult struct {
	Text       string
	RawText    string
	StopReason string
	EventIDs   []string
}

type streamEvent struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Role    string `json:"role"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Content []struct {
		Type string          `json:"type"`
		Text string          `json:"text"`
		Data json.RawMessage `json:"data"`
	} `json:"content"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func New(workspaceID, region, apiKey string, timeout time.Duration) (*Client, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	region = strings.TrimSpace(strings.ToLower(region))
	if !workspaceIDPattern.MatchString(workspaceID) {
		return nil, errors.New("Agent Studio Workspace ID 无效")
	}
	if !regionPattern.MatchString(region) || region != "cn-beijing" {
		return nil, errors.New("Agent Studio 地域无效")
	}
	baseURL := fmt.Sprintf("https://%s.%s.maas.aliyuncs.com/api/v1/agentstudio", workspaceID, region)
	return NewWithBaseURL(baseURL, apiKey, timeout)
}

func NewWithBaseURL(baseURL, apiKey string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Agent Studio Endpoint 无效")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("所选模型配置没有可用的阿里云 API Key")
	}
	if timeout < 30*time.Second {
		timeout = 120 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func ValidResourceID(value, prefix string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, prefix) && resourceIDPattern.MatchString(value)
}

func (c *Client) CreateSession(ctx context.Context, agentID, environmentID, title string, metadata map[string]string) (Session, error) {
	if !ValidResourceID(agentID, "agent_") {
		return Session{}, errors.New("Agent ID 无效")
	}
	if !ValidResourceID(environmentID, "env_") {
		return Session{}, errors.New("Environment ID 无效")
	}
	body := map[string]any{
		"agent":          strings.TrimSpace(agentID),
		"environment_id": strings.TrimSpace(environmentID),
		"title":          strings.TrimSpace(title),
		"metadata":       metadata,
	}
	var session Session
	if err := c.doJSON(ctx, http.MethodPost, "/sessions", body, &session); err != nil {
		return Session{}, err
	}
	if !ValidResourceID(session.ID, "sesn_") {
		return Session{}, errors.New("Agent Studio 返回了无效的 Session ID")
	}
	return session, nil
}

func (c *Client) ListEnvironments(ctx context.Context) ([]Environment, error) {
	var page struct {
		Data []Environment `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/environments?limit=100", nil, &page); err != nil {
		return nil, err
	}
	result := make([]Environment, 0, len(page.Data))
	for _, item := range page.Data {
		if item.ArchivedAt == nil && ValidResourceID(item.ID, "env_") {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *Client) ListAgents(ctx context.Context) ([]AgentSnapshot, error) {
	var page struct {
		Data []AgentSnapshot `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/agents?limit=100", nil, &page); err != nil {
		return nil, err
	}
	result := make([]AgentSnapshot, 0, len(page.Data))
	for _, item := range page.Data {
		if item.ArchivedAt == nil && ValidResourceID(item.ID, "agent_") {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *Client) GetAgent(ctx context.Context, agentID string) (AgentSnapshot, error) {
	if !ValidResourceID(agentID, "agent_") {
		return AgentSnapshot{}, errors.New("Agent ID 无效")
	}
	var result AgentSnapshot
	if err := c.doJSON(ctx, http.MethodGet, "/agents/"+url.PathEscape(agentID), nil, &result); err != nil {
		return AgentSnapshot{}, err
	}
	if !ValidResourceID(result.ID, "agent_") {
		return AgentSnapshot{}, errors.New("Agent Studio 返回了无效的 Agent ID")
	}
	return result, nil
}

func (c *Client) ArchiveSession(ctx context.Context, sessionID string) error {
	if !ValidResourceID(sessionID, "sesn_") {
		return errors.New("Session ID 无效")
	}
	return c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/archive", map[string]any{}, nil)
}

// Closing SSE alone does not stop the managed runtime. Send an explicit user
// interrupt for a run that the owner has ended; never start another message.
func (c *Client) InterruptSession(ctx context.Context, sessionID string) error {
	if !ValidResourceID(sessionID, "sesn_") {
		return errors.New("Session ID 无效")
	}
	return c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/events", map[string]any{"input": []any{map[string]any{"role": "user", "type": "interrupt"}}}, nil)
}

func (c *Client) SendMessageAndWait(ctx context.Context, sessionID, message string) (TurnResult, error) {
	if !ValidResourceID(sessionID, "sesn_") {
		return TurnResult{}, errors.New("Session ID 无效")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return TurnResult{}, errors.New("本轮消息不能为空")
	}
	streamPath := "/sessions/" + url.PathEscape(sessionID) + "/events/stream"
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+streamPath, nil)
	if err != nil {
		return TurnResult{}, err
	}
	c.authorize(streamReq)
	streamReq.Header.Set("Accept", "text/event-stream")
	streamResp, err := c.httpClient.Do(streamReq)
	if err != nil {
		return TurnResult{}, fmt.Errorf("连接 Agent Studio 事件流失败: %w", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode < 200 || streamResp.StatusCode >= 300 {
		return TurnResult{}, responseError("连接 Agent Studio 事件流失败", streamResp)
	}

	scanner := bufio.NewScanner(streamResp.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	connected := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, ":") && strings.Contains(strings.ToLower(line), "connected") {
			connected = true
			break
		}
	}
	if !connected {
		if err := scanner.Err(); err != nil {
			return TurnResult{}, fmt.Errorf("读取 Agent Studio 事件流失败: %w", err)
		}
		return TurnResult{}, errors.New("Agent Studio 事件流未完成连接")
	}

	eventBody := map[string]any{
		"input": []any{map[string]any{
			"role": "user",
			"type": "message",
			"content": []any{map[string]any{
				"type": "text",
				"text": message,
			}},
		}},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/events", eventBody, nil); err != nil {
		return TurnResult{}, fmt.Errorf("投递 Agent Studio 消息失败: %w", err)
	}

	var result TurnResult
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if event.ID != "" && seen[event.ID] {
			continue
		}
		if event.ID != "" {
			seen[event.ID] = true
			result.EventIDs = append(result.EventIDs, event.ID)
		}
		if event.Type == "error" {
			if event.Error != nil {
				return TurnResult{}, fmt.Errorf("Agent Studio 运行失败（%s）: %s", event.Error.Code, event.Error.Message)
			}
			return TurnResult{}, errors.New("Agent Studio 运行失败")
		}
		if event.Type == "message" && event.Role == "assistant" {
			for _, block := range event.Content {
				if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
					if result.Text != "" {
						result.Text += "\n"
					}
					result.Text += block.Text
				}
			}
		}
		if event.Type != "session_status" {
			continue
		}
		for _, block := range event.Content {
			if block.Type != "data" || len(block.Data) == 0 {
				continue
			}
			var status struct {
				SessionStatus string `json:"session_status"`
				StopReason    struct {
					Type string `json:"type"`
				} `json:"stop_reason"`
			}
			if json.Unmarshal(block.Data, &status) == nil && (status.SessionStatus == "idle" || status.SessionStatus == "terminated") {
				result.StopReason = status.StopReason.Type
				result.RawText = result.Text
				result.Text = strings.TrimSpace(result.Text)
				if status.StopReason.Type != "end_turn" {
					return TurnResult{}, fmt.Errorf("Agent Studio 本轮未正常结束: %s", status.StopReason.Type)
				}
				if result.Text == "" {
					return TurnResult{}, errors.New("Agent Studio 本轮没有返回口播文本")
				}
				return result, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return TurnResult{}, fmt.Errorf("读取 Agent Studio 事件流失败: %w", err)
	}
	return TurnResult{}, errors.New("Agent Studio 事件流在本轮结束前断开")
}

func (c *Client) doJSON(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	c.authorize(req)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError("Agent Studio 请求失败", resp)
	}
	if output == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(output); err != nil {
		return fmt.Errorf("解析 Agent Studio 响应失败: %w", err)
	}
	return nil
}

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func responseError(prefix string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	message := strings.TrimSpace(string(raw))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s（HTTP %d）: %s", prefix, resp.StatusCode, message)
}
