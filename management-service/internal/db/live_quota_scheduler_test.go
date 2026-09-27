package db

import "testing"

func TestFairLeaseAllocationSharesTailAcrossRooms(t *testing.T) {
	requests := []LiveQuotaLeaseRequest{
		{RoomID: 1, RuntimeSessionID: 101, RequestedSeconds: 60},
		{RoomID: 2, RuntimeSessionID: 102, RequestedSeconds: 60},
	}
	got := fairLeaseAllocation(10, requests)
	if got[101] != 5 || got[102] != 5 {
		t.Fatalf("expected 5/5, got %d/%d", got[101], got[102])
	}
}

func TestFairLeaseAllocationCapsNormalMinuteLease(t *testing.T) {
	requests := []LiveQuotaLeaseRequest{
		{RoomID: 1, RuntimeSessionID: 101, RequestedSeconds: 60},
		{RoomID: 2, RuntimeSessionID: 102, RequestedSeconds: 60},
	}
	got := fairLeaseAllocation(200, requests)
	if got[101] != 60 || got[102] != 60 {
		t.Fatalf("expected 60/60, got %d/%d", got[101], got[102])
	}
}

func TestFairLeaseAllocationDistributesOddTailDeterministically(t *testing.T) {
	requests := []LiveQuotaLeaseRequest{
		{RoomID: 2, RuntimeSessionID: 102, RequestedSeconds: 60},
		{RoomID: 1, RuntimeSessionID: 101, RequestedSeconds: 60},
	}
	got := fairLeaseAllocation(61, requests)
	if got[101]+got[102] != 61 {
		t.Fatalf("expected all 61 seconds allocated, got %d", got[101]+got[102])
	}
	delta := int64(got[101]) - int64(got[102])
	if delta < -1 || delta > 1 {
		t.Fatalf("expected fair odd tail, got %d/%d", got[101], got[102])
	}
}
