package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProgramRefreshAdapterPreservesContractAndConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Core-Token") != "token" || r.URL.Query().Get("tenant_id") != "14" {
			t.Error("missing internal identity")
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/internal/v1/rooms/15/audio/program" {
				t.Error(r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"program_id":"program-1","room_id":15,"version_id":20,"generation":3,"running":true}`))
			return
		}
		if r.URL.Path == "/internal/v1/rooms/15/audio/program/refresh/cancel" || r.URL.Path == "/internal/v1/rooms/15/audio/program/refresh/renew" {
			var control struct {
				JobID             string    `json:"job_id"`
				ExpectedProgramID string    `json:"expected_program_id"`
				ValidUntil        time.Time `json:"valid_until"`
			}
			if err := json.NewDecoder(r.Body).Decode(&control); err != nil {
				t.Error(err)
			}
			if control.JobID != "job-1" || control.ExpectedProgramID != "program-1" {
				t.Errorf("bad control: %+v", control)
			}
			_, _ = w.Write([]byte(`{"program_id":"program-1","room_id":15,"generation":3,"running":true}`))
			return
		}
		var input ProgramRefreshInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.JobID == "conflict" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if input.JobID == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if input.ExpectedProgramID != "program-1" || input.ExpectedVersionID != 20 || input.ExpectedGeneration != 3 || input.Generation != 4 || input.NewVersionID != 21 {
			t.Errorf("invalid contract: %+v", input)
		}
		_, _ = w.Write([]byte(`{"program_id":"program-1","room_id":15,"version_id":20,"generation":3,"pending_generation":4,"pending_job_id":"job-1","running":true}`))
	}))
	defer server.Close()
	client := New(server.URL, "token")
	snapshot, err := client.GetRoomProgramSnapshot(context.Background(), 14, 15)
	if err != nil || snapshot.Generation != 3 || !snapshot.Running {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	input := ProgramRefreshInput{JobID: "job-1", ExpectedProgramID: snapshot.ProgramID, ExpectedVersionID: 20, ExpectedGeneration: 3, Generation: 4, NewVersionID: 21, NewVersionNo: 2}
	result, err := client.RefreshRoomProgram(context.Background(), 14, 15, input)
	if err != nil || result.PendingJobID != "job-1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	input.JobID = "conflict"
	if _, err := client.RefreshRoomProgram(context.Background(), 14, 15, input); !errors.Is(err, ErrProgramRefreshConflict) {
		t.Fatalf("conflict was not typed: %v", err)
	}
	input.JobID = "missing"
	if _, err := client.RefreshRoomProgram(context.Background(), 14, 15, input); !errors.Is(err, ErrProgramRefreshNotFound) {
		t.Fatalf("404 was not typed: %v", err)
	}
	if _, err := client.RenewRoomProgramRefresh(context.Background(), 14, 15, "job-1", "program-1", time.Now().Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelRoomProgramRefresh(context.Background(), 14, 15, "job-1", "program-1"); err != nil {
		t.Fatal(err)
	}
}
