package db

import (
	"context"
	"strings"
	"testing"
)

func TestRoomCustomerNamesAreContactFreeAndScoped(t *testing.T) {
	s := &Store{}
	names, err := s.GetRoomCustomerNames(context.Background(), []int64{0, -1})
	if err != nil || len(names) != 0 {
		t.Fatal("empty tenant scope should not query database")
	}
	if strings.Contains(roomCustomerName+roomCustomerJoin, "phone") {
		t.Fatal("room list name lookup includes phone")
	}
	if _, err := s.GetRoomCustomerContact(context.Background(), 0); err == nil {
		t.Fatal("invalid tenant scope accepted")
	}
}
