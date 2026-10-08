package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"livecompanion/core/internal/strategycenter"
)

func TestRoomInteractionPreferencesManagementWireContract(t *testing.T) {
	fixtures := []struct{ Saved, Payload json.RawMessage }{{
		Saved:   json.RawMessage(`{"tenant_id":14,"room_id":27,"overall_interaction":0,"question_preference":100,"welcome_preference":75,"auto_heat":false,"updated_by_user_id":372}`),
		Payload: json.RawMessage(`{"tenant_id":14,"room_id":27,"overall_interaction":0,"question_preference":100,"welcome_preference":75,"auto_heat":false}`),
	}}
	if path := os.Getenv("CORE_PREFERENCES_CONTRACT_FIXTURE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &fixtures); err != nil {
			t.Fatal(err)
		}
		if len(fixtures) != 2 {
			t.Fatal("expected actual V1 and V2 management HTTP payloads")
		}
	}
	for _, fixture := range fixtures {
		request := httptest.NewRequest(http.MethodPut, "/internal/v1/rooms/27/interaction-preferences?tenant_id=14", bytes.NewReader(fixture.Saved))
		var input strategycenter.RoomInteractionPreferences
		if err := readJSON(httptest.NewRecorder(), request, &input); err == nil || !strings.Contains(err.Error(), `unknown field "updated_by_user_id"`) {
			t.Fatalf("original saved record should reproduce strict decoder rejection: %v", err)
		}
		request = httptest.NewRequest(http.MethodPut, "/internal/v1/rooms/27/interaction-preferences?tenant_id=14", bytes.NewReader(fixture.Payload))
		input = strategycenter.RoomInteractionPreferences{}
		if err := readJSON(httptest.NewRecorder(), request, &input); err != nil {
			t.Fatalf("management runtime payload rejected: %v", err)
		}
		var expected strategycenter.RoomInteractionPreferences
		if err := json.Unmarshal(fixture.Saved, &expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(input, expected) {
			t.Fatal("runtime payload changed saved settings")
		}
		if input.Schedule != nil {
			if err := strategycenter.ValidateInteractionSchedule(*input.Schedule); err != nil {
				t.Fatal(err)
			}
			if input.Schedule.Version != 7 || input.Schedule.PlanID != 4 {
				t.Fatal("saved schedule version changed")
			}
		}
		policies := strategycenter.New()
		policies.PutRoomInteractionPreferences(27, input)
		result := policies.RoomInteractionPreferences(27)
		if result.TenantID != 14 || result.OverallInteraction != 0 || result.QuestionPreference != 100 || result.WelcomePreference != 75 || result.AutoHeat {
			t.Fatalf("runtime settings not applied: %#v", result)
		}
	}
}
