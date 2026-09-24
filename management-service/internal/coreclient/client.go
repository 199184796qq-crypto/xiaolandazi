package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURLs []string
	token    string
	http     *http.Client
}

func New(baseURL string, token string) *Client {
	baseURLs := make([]string, 0)
	for _, item := range strings.Split(baseURL, ",") {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item != "" {
			baseURLs = append(baseURLs, item)
		}
	}
	if len(baseURLs) == 0 {
		baseURLs = append(baseURLs, "http://127.0.0.1:8081")
	}
	return &Client{
		baseURLs: baseURLs,
		token:    token,
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *Client) NodeCount() int {
	return len(c.baseURLs)
}

func (c *Client) Do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
) (*http.Response, error) {
	if method == http.MethodGet && len(c.baseURLs) > 1 {
		return c.DoAny(ctx, method, path, query, body)
	}
	return c.doAt(ctx, c.baseURLs[0], method, path, query, body)
}

// DoAny is for cluster-wide read operations whose source of truth is shared
// storage. It fails over across Core nodes on transport errors or 5xx responses.
func (c *Client) DoAny(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
) (*http.Response, error) {
	errorsSeen := make([]string, 0, len(c.baseURLs))
	for _, baseURL := range c.baseURLs {
		resp, err := c.doAt(ctx, baseURL, method, path, query, body)
		if err != nil {
			errorsSeen = append(errorsSeen, baseURL+": "+err.Error())
			continue
		}
		if resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
		_ = resp.Body.Close()
		errorsSeen = append(errorsSeen, fmt.Sprintf("%s: http %d", baseURL, resp.StatusCode))
	}
	return nil, fmt.Errorf("all core nodes unavailable: %s", strings.Join(errorsSeen, "; "))
}

func (c *Client) DoRoom(
	ctx context.Context,
	tenantID, roomID int64,
	method string,
	path string,
	query url.Values,
	body any,
) (*http.Response, error) {
	if !roomMethodCanFailOver(method) || len(c.baseURLs) <= 1 {
		return c.doAt(
			ctx,
			c.baseURLForRoom(tenantID, roomID),
			method,
			path,
			query,
			body,
		)
	}
	errorsSeen := make([]string, 0, len(c.baseURLs))
	for _, baseURL := range c.orderedBaseURLsForRoom(tenantID, roomID) {
		resp, err := c.doAt(ctx, baseURL, method, path, query, body)
		if err != nil {
			errorsSeen = append(errorsSeen, baseURL+": "+err.Error())
			continue
		}
		if resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
		_ = resp.Body.Close()
		errorsSeen = append(
			errorsSeen,
			fmt.Sprintf("%s: http %d", baseURL, resp.StatusCode),
		)
	}
	return nil, fmt.Errorf(
		"all core nodes unavailable for room: %s",
		strings.Join(errorsSeen, "; "),
	)
}

func roomMethodCanFailOver(method string) bool {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete:
		return true
	default:
		return false
	}
}

func (c *Client) Stream(
	ctx context.Context,
	path string,
	query url.Values,
) (*http.Response, error) {
	return c.streamAt(ctx, c.baseURLs[0], path, query)
}

func (c *Client) StreamRoom(
	ctx context.Context,
	tenantID, roomID int64,
	path string,
	query url.Values,
) (*http.Response, error) {
	errorsSeen := make([]string, 0, len(c.baseURLs))
	for _, baseURL := range c.orderedBaseURLsForRoom(tenantID, roomID) {
		resp, err := c.streamAt(ctx, baseURL, path, query)
		if err != nil {
			errorsSeen = append(errorsSeen, baseURL+": "+err.Error())
			continue
		}
		if resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
		_ = resp.Body.Close()
		errorsSeen = append(
			errorsSeen,
			fmt.Sprintf("%s: http %d", baseURL, resp.StatusCode),
		)
	}
	return nil, fmt.Errorf(
		"all core nodes unavailable for room stream: %s",
		strings.Join(errorsSeen, "; "),
	)
}

func (c *Client) baseURLForRoom(tenantID, roomID int64) string {
	if len(c.baseURLs) <= 1 {
		return c.baseURLs[0]
	}
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%d:%d", tenantID, roomID)
	return c.baseURLs[int(hash.Sum64()%uint64(len(c.baseURLs)))]
}

func (c *Client) orderedBaseURLsForRoom(tenantID, roomID int64) []string {
	if len(c.baseURLs) <= 1 {
		return c.baseURLs
	}
	primary := c.baseURLForRoom(tenantID, roomID)
	ordered := make([]string, 0, len(c.baseURLs))
	ordered = append(ordered, primary)
	for _, baseURL := range c.baseURLs {
		if baseURL != primary {
			ordered = append(ordered, baseURL)
		}
	}
	return ordered
}

func (c *Client) doAt(
	ctx context.Context,
	baseURL string,
	method string,
	path string,
	query url.Values,
	body any,
) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	endpoint := baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Core-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) streamAt(
	ctx context.Context,
	baseURL string,
	path string,
	query url.Values,
) (*http.Response, error) {
	endpoint := baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Core-Token", c.token)

	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("core stream failed: %w", err)
	}
	return resp, nil
}
