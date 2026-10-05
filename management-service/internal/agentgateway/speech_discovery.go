package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

type SpeechAvailableModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type SpeechModelList struct {
	Models    []SpeechAvailableModel `json:"models"`
	Truncated bool                   `json:"truncated"`
}

func speechModelsAddress(p SpeechModel) string {
	base := strings.TrimRight(p.BaseURL, "/")
	switch p.Protocol {
	case "openai_chat":
		base = strings.TrimSuffix(base, "/chat/completions")
	case "openai_responses":
		base = strings.TrimSuffix(base, "/responses")
	case "anthropic":
		base = strings.TrimSuffix(base, "/messages")
	case "gemini":
		if i := strings.LastIndex(base, "/models/"); i >= 0 && strings.HasSuffix(base, ":generateContent") {
			base = base[:i]
		}
	}
	return endpointPath(base, "/models")
}

func ListSpeechEndpointModels(ctx context.Context, p SpeechModel) (SpeechModelList, error) {
	if err := ValidateSpeechConnection(p); err != nil {
		return SpeechModelList{}, err
	}
	return listSpeechEndpointModels(ctx, p, speechHTTPClient)
}

// Metadata only: no completion, save, default switch, or key in URL.
func listSpeechEndpointModels(ctx context.Context, p SpeechModel, client *http.Client) (SpeechModelList, error) {
	result := SpeechModelList{Models: []SpeechAvailableModel{}}
	if strings.TrimSpace(p.APIKey) == "" || len(p.APIKey) > 4096 {
		return result, errors.New("请填写模型 API 密钥，或使用已保存的密钥")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	seen, cursors := map[string]bool{}, map[string]bool{}
	address, cursor := speechModelsAddress(p), ""
	const maxModels, maxPages = 2000, 10
	for page := 0; page < maxPages; page++ {
		u, err := url.Parse(address)
		if err != nil {
			return result, errors.New("API 地址无效")
		}
		q := u.Query()
		if p.Protocol == "gemini" {
			q.Set("pageSize", "1000")
			if cursor != "" {
				q.Set("pageToken", cursor)
			}
		} else if p.Protocol == "anthropic" {
			q.Set("limit", "1000")
			if cursor != "" {
				q.Set("after_id", cursor)
			}
		}
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return result, errors.New("API 地址无效")
		}
		req.Header.Set("Accept", "application/json")
		switch p.Protocol {
		case "gemini":
			req.Header.Set("x-goog-api-key", p.APIKey)
		case "anthropic":
			req.Header.Set("x-api-key", p.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		default:
			req.Header.Set("Authorization", "Bearer "+p.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return result, errors.New("获取模型列表失败或超时，请检查 API 地址和网络；仍可手动填写模型 ID")
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return result, fmt.Errorf("模型列表接口返回 HTTP %d：请检查密钥、地址或服务商是否支持列表接口；仍可手动填写模型 ID", resp.StatusCode)
		}
		if readErr != nil || len(raw) > 4<<20 {
			return result, errors.New("模型列表响应读取失败或过大")
		}
		var data struct {
			Data []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			Models []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Methods     []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
		}
		if json.Unmarshal(raw, &data) != nil || (p.Protocol == "gemini" && data.Models == nil) || (p.Protocol != "gemini" && data.Data == nil) {
			return result, errors.New("服务商返回的模型列表格式不符合所选协议；仍可手动填写模型 ID")
		}
		add := func(id, name string) {
			id = strings.TrimSpace(id)
			if id == "" || len(id) > 160 || strings.ContainsAny(id, "?#\\") || strings.IndexFunc(id, unicode.IsControl) >= 0 || strings.Contains(id, p.APIKey) || seen[id] {
				return
			}
			if len(result.Models) >= maxModels {
				result.Truncated = true
				return
			}
			seen[id] = true
			name = strings.TrimSpace(name)
			if name == "" || len([]rune(name)) > 160 || strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.Contains(name, p.APIKey) {
				name = id
			}
			result.Models = append(result.Models, SpeechAvailableModel{ID: id, Name: name})
		}
		cursor = ""
		if p.Protocol == "gemini" {
			for _, m := range data.Models {
				for _, method := range m.Methods {
					if method == "generateContent" {
						add(strings.TrimPrefix(m.Name, "models/"), m.DisplayName)
						break
					}
				}
			}
			cursor = data.NextPageToken
		} else {
			for _, m := range data.Data {
				name := m.Name
				if name == "" {
					name = m.DisplayName
				}
				add(m.ID, name)
			}
			if p.Protocol == "anthropic" && data.HasMore {
				cursor = data.LastID
				if cursor == "" {
					return result, errors.New("模型列表分页格式无效；仍可手动填写模型 ID")
				}
			}
		}
		if cursor == "" {
			break
		}
		if len(cursor) > 4096 || cursors[cursor] {
			return result, errors.New("模型列表分页异常；仍可手动填写模型 ID")
		}
		cursors[cursor] = true
		if page == maxPages-1 || len(result.Models) >= maxModels {
			result.Truncated = true
			break
		}
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
	return result, nil
}
