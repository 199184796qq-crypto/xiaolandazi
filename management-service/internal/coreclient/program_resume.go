package coreclient

import (
	"context"
	"net/http"
	"net/url"
)

// DoRoomOwner keeps a recovery transaction on the node owning its in-memory
// cursor. In particular, an unknown PUT result must never be replayed on a
// second Core process by the generic read/write failover policy.
func (c *Client) DoRoomOwner(ctx context.Context, tenantID, roomID int64, method, path string, query url.Values, body any) (*http.Response, error) {
	return c.doAt(ctx, c.baseURLForRoom(tenantID, roomID), method, path, query, body)
}
