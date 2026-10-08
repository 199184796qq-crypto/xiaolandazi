package coreclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

func TestInteractionPreferencesRuntimePayloadKeepsSavedSettingsWithoutAuditFields(t *testing.T) {
	raw, err := os.ReadFile("../../../configs/prompts/anchor-style/interaction-scheduling.json")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := interactionpolicy.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []map[string]json.RawMessage
	for _, version := range []int{1, 2} {
		schedule.TemplateVersion = version
		schedule.PlanID, schedule.Version = 4, 7
		userID := int64(372)
		saved := model.RoomInteractionPreferences{
			TenantID: 14, RoomID: 27, Schedule: &schedule, UpdatedByUserID: &userID,
			OverallInteraction: 0, QuestionPreference: 100, WelcomePreference: 75,
			EngagementPreference: 25, ChatPreference: 41, ConversionPreference: 66,
			AutoHeat: false, UpdatedAt: time.Date(2026, 10, 8, 17, 49, 31, 0, time.UTC),
		}
		var body []byte
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/internal/v1/rooms/27/interaction-preferences" || r.URL.Query().Get("tenant_id") != "14" {
				t.Errorf("incorrect room-scoped request: %s %s", r.Method, r.URL)
			}
			body, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		}))
		response, err := New(receiver.URL, "fixture-token").PutRoomInteractionPreferences(context.Background(), saved)
		receiver.Close()
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		if _, found := fields["updated_by_user_id"]; found {
			t.Fatal("management audit field leaked to Core")
		}
		original, _ := json.Marshal(saved)
		fixtures = append(fixtures, map[string]json.RawMessage{"saved": original, "payload": body})
		// Decode back into the management record to check every saved runtime
		// field (including complete nested schedule and disabled/zero settings).
		var received model.RoomInteractionPreferences
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatal(err)
		}
		saved.UpdatedByUserID = nil
		if !reflect.DeepEqual(received, saved) {
			t.Fatalf("saved runtime configuration changed: %#v", received)
		}
	}
	// The Core contract test consumes the bytes captured from the actual HTTP
	// client, rather than another independently reconstructed payload.
	if path := os.Getenv("CORE_PREFERENCES_CONTRACT_FIXTURE"); path != "" {
		raw, _ := json.Marshal(fixtures)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInteractionPreferencesSyncRequiresRoomScope(t *testing.T) {
	client := New("http://127.0.0.1:1", "fixture-token")
	for _, item := range []model.RoomInteractionPreferences{{}, {TenantID: 14}, {RoomID: 27}} {
		if _, err := client.PutRoomInteractionPreferences(context.Background(), item); err == nil {
			t.Fatal("missing tenant/room scope accepted")
		}
	}
}
