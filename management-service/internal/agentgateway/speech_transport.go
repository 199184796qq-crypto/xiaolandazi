package agentgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func publicModelIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

// Resolve and validate at dial time, not just when the URL is saved. This also
// blocks DNS rebinding, metadata endpoints and redirects leaking credentials.
func modelHTTPClient() *http.Client {
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 120 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("模型地址无效")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("模型地址解析失败")
		}
		if len(ips) == 0 {
			return nil, errors.New("模型地址解析失败")
		}
		for _, ip := range ips {
			if !publicModelIP(ip.IP) {
				return nil, errors.New("模型地址不能访问内网或保留地址")
			}
		}
		for _, ip := range ips {
			conn, dialErr := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
		}
		return nil, errors.New("模型服务器连接失败")
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("模型 API 不允许重定向") }}
}

var speechHTTPClient = modelHTTPClient()

func endpointPath(base, suffix string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return base + suffix
}

func CompleteSpeechEndpoint(ctx context.Context, p SpeechModel, request Request) (Response, error) {
	if err := ValidateSpeechModel(p); err != nil {
		return Response{}, err
	}
	return completeSpeechEndpoint(ctx, p, request, speechHTTPClient)
}

func completeSpeechEndpoint(ctx context.Context, p SpeechModel, request Request, client *http.Client) (Response, error) {
	if p.APIKey == "" {
		return Response{}, errors.New("模型 API 密钥尚未配置")
	}
	if request.MaxTokens <= 0 {
		request.MaxTokens = p.MaxTokens
	}
	if request.Timeout <= 0 {
		request.Timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	if p.Protocol == "openai_chat" {
		base := strings.TrimSuffix(strings.TrimRight(p.BaseURL, "/"), "/chat/completions")
		var provider Provider = NewCompatibleProvider(QwenConfig{BaseURL: base, APIKey: p.APIKey, Client: client, MaxTokenField: p.ChatTokenField})
		if dashScopeQwenEndpoint(base, p.Model) {
			provider = NewQwenProvider(QwenConfig{BaseURL: base, APIKey: p.APIKey, Client: client, MaxTokenField: p.ChatTokenField})
		}
		response, err := provider.Complete(ctx, request)
		if err != nil {
			return Response{}, errors.New("模型调用失败，请检查地址、密钥、模型 ID、协议或超时")
		}
		return response, nil
	}
	var systems []string
	messages := []map[string]any{}
	contents := []map[string]any{}
	for _, message := range request.Messages {
		if len(message.ImageURLs) > 0 {
			return Response{}, errors.New("主播文字模型不支持图片输入")
		}
		if message.Role == "system" || message.Role == "developer" {
			systems = append(systems, message.Content)
			continue
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": message.Content})
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": []map[string]string{{"text": message.Content}}})
	}
	if len(messages) == 0 {
		return Response{}, errors.New("请输入测试问题或提示词")
	}
	system := strings.Join(systems, "\n\n")
	if request.ResponseFormat == ResponseJSON {
		system += "\n只返回合法 JSON，不要代码围栏或解释。"
	}
	address := ""
	payload := map[string]any{}
	switch p.Protocol {
	case "openai_responses":
		address = endpointPath(p.BaseURL, "/responses")
		payload = map[string]any{"model": p.Model, "input": messages, "instructions": system, "max_output_tokens": request.MaxTokens, "stream": false, "store": false}
	case "gemini":
		address = strings.TrimRight(p.BaseURL, "/")
		if !strings.HasSuffix(address, ":generateContent") {
			address += "/models/" + url.PathEscape(strings.TrimPrefix(p.Model, "models/")) + ":generateContent"
		}
		generation := map[string]any{"maxOutputTokens": request.MaxTokens}
		if request.ResponseFormat == ResponseJSON {
			generation["responseMimeType"] = "application/json"
		}
		payload = map[string]any{"contents": contents, "generationConfig": generation}
		if system != "" {
			payload["systemInstruction"] = map[string]any{"parts": []map[string]string{{"text": system}}}
		}
	case "anthropic":
		address = endpointPath(p.BaseURL, "/messages")
		payload = map[string]any{"model": p.Model, "messages": messages, "max_tokens": request.MaxTokens, "stream": false}
		if system != "" {
			payload["system"] = system
		}
	default:
		return Response{}, errors.New("不支持的模型协议")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, errors.New("模型请求编码失败")
	}
	callCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return Response{}, errors.New("模型请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	switch p.Protocol {
	case "gemini":
		req.Header.Set("x-goog-api-key", p.APIKey)
	case "anthropic":
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, errors.New("模型调用失败或超时，请检查网络和 API 地址")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("模型 API 返回 HTTP %d（检查密钥、模型权限、协议或额度）", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return Response{}, errors.New("模型响应读取失败或过大")
	}
	var decoded struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
			Total  int64 `json:"total_tokens"`
		} `json:"usage"`
		UsageMetadata struct {
			Input  int64 `json:"promptTokenCount"`
			Output int64 `json:"candidatesTokenCount"`
			Total  int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Response{}, errors.New("响应格式不符合所选 API 协议")
	}
	var parts []string
	switch p.Protocol {
	case "openai_responses":
		for _, output := range decoded.Output {
			for _, part := range output.Content {
				if part.Type == "output_text" {
					parts = append(parts, part.Text)
				}
			}
		}
	case "anthropic":
		for _, part := range decoded.Content {
			if part.Type == "text" {
				parts = append(parts, part.Text)
			}
		}
	case "gemini":
		if len(decoded.Candidates) > 0 {
			for _, part := range decoded.Candidates[0].Content.Parts {
				if !part.Thought {
					parts = append(parts, part.Text)
				}
			}
		}
		decoded.Usage.Input, decoded.Usage.Output, decoded.Usage.Total = decoded.UsageMetadata.Input, decoded.UsageMetadata.Output, decoded.UsageMetadata.Total
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return Response{}, errors.New("模型未返回文字，可能被拒绝或输出额度不足")
	}
	if decoded.Usage.Total == 0 {
		decoded.Usage.Total = decoded.Usage.Input + decoded.Usage.Output
	}
	return Response{Text: text, Model: p.Model, Provider: p.Protocol, LatencyMS: time.Since(started).Milliseconds(), InputTokens: decoded.Usage.Input, OutputTokens: decoded.Usage.Output, TotalTokens: decoded.Usage.Total}, nil
}

func dashScopeQwenEndpoint(baseURL, model string) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "qwen") {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "dashscope.aliyuncs.com" ||
		host == "dashscope-intl.aliyuncs.com" ||
		strings.HasSuffix(host, ".maas.aliyuncs.com")
}
