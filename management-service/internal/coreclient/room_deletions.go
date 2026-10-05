package coreclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

// DeleteRoomEverywhere clears process-local execution on every Core replica.
// Failover-to-one-node is insufficient: another replica may still be playing.
func (c *Client) DeleteRoomEverywhere(ctx context.Context, tenantID, roomID int64) error {
	query := url.Values{"tenant_id": {strconv.FormatInt(tenantID, 10)}}
	failures := make(chan error, len(c.baseURLs))
	var workers sync.WaitGroup
	for _, baseURL := range c.baseURLs {
		workers.Add(1)
		go func(baseURL string) {
			defer workers.Done()
			resp, err := c.doAt(ctx, baseURL, http.MethodDelete, fmt.Sprintf("/internal/v1/rooms/%d", roomID), query, nil)
			if err != nil {
				failures <- err
				return
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				failures <- fmt.Errorf("room cleanup node=%s status=%d", baseURL, resp.StatusCode)
			}
		}(baseURL)
	}
	workers.Wait()
	close(failures)
	var collected []error
	for err := range failures {
		collected = append(collected, err)
	}
	return errors.Join(collected...)
}
