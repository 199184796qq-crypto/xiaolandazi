package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/db"
)

func TestLiveOpsOverviewChecksCoreRuntime(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenant_id") != "7" {
			t.Errorf("unexpected room scope: %s", r.URL)
		}
		switch r.URL.Path {
		case "/internal/v1/rooms/1/agent-runtime":
			fmt.Fprint(w, `{"state":"working"}`)
		case "/internal/v1/rooms/2/agent-runtime":
			fmt.Fprint(w, `{"state":"stopped"}`)
		case "/internal/v1/rooms/3/agent-runtime":
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected Core route: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer core.Close()
	server := &Server{core: coreclient.New(core.URL, "test")}
	ctx := context.Background()
	rooms := []db.LiveOpsRoomKey{{TenantID: 7, RoomID: 1}, {TenantID: 7, RoomID: 2}}
	if count := server.countLiveOpsOnlineRooms(ctx, nil); count == nil || *count != 0 {
		t.Fatal("no sessions should report zero")
	}
	if count := server.countLiveOpsOnlineRooms(ctx, rooms); count == nil || *count != 1 {
		t.Fatal("only an agent that is working in Core should count")
	}
	if count := server.countLiveOpsOnlineRooms(ctx, append(rooms, db.LiveOpsRoomKey{TenantID: 7, RoomID: 3})); count != nil {
		t.Fatal("a failed Core lookup should be reported as unavailable")
	}
}
