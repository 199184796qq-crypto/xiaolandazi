package db

import "testing"

func TestSupportPlanAccessRequiresRoomGrantAndProtectsSharedWrites(t *testing.T) {
	for _, tc := range []struct {
		name            string
		scope           liveSupportPlanScope
		roomID, staffID int64
		write, want     bool
	}{
		{"bound read", liveSupportPlanScope{RoomIDs: []int64{12}, AuthorizedRoomIDs: map[int64]bool{12: true}}, 12, 9, false, true},
		{"missing grant", liveSupportPlanScope{RoomIDs: []int64{12}}, 12, 9, false, false},
		{"revoked grant", liveSupportPlanScope{RoomIDs: []int64{12}, AuthorizedRoomIDs: map[int64]bool{12: false}}, 12, 9, true, false},
		{"another room grant", liveSupportPlanScope{RoomIDs: []int64{12}, AuthorizedRoomIDs: map[int64]bool{13: true}}, 12, 9, false, false},
		{"unrelated plan", liveSupportPlanScope{RoomIDs: []int64{13}, AuthorizedRoomIDs: map[int64]bool{12: true, 13: true}}, 12, 9, false, false},
		{"shared read is scoped", liveSupportPlanScope{RoomIDs: []int64{12, 13}, AuthorizedRoomIDs: map[int64]bool{12: true}}, 12, 9, false, true},
		{"shared write denied", liveSupportPlanScope{RoomIDs: []int64{12, 13}, AuthorizedRoomIDs: map[int64]bool{12: true}}, 12, 9, true, false},
		{"all shared grants", liveSupportPlanScope{RoomIDs: []int64{12, 13}, AuthorizedRoomIDs: map[int64]bool{12: true, 13: true}}, 12, 9, true, true},
		{"own unbound draft", liveSupportPlanScope{CreatorID: 9, AuthorizedRoomIDs: map[int64]bool{12: true}}, 12, 9, true, true},
		{"customer unbound draft private", liveSupportPlanScope{CreatorID: 10, AuthorizedRoomIDs: map[int64]bool{12: true}}, 12, 9, false, false},
		{"creator needs current grant", liveSupportPlanScope{CreatorID: 9}, 12, 9, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveSupportPlanScopeAllowed(tc.scope, tc.roomID, tc.staffID, tc.write); got != tc.want {
				t.Fatalf("access=%v want=%v", got, tc.want)
			}
		})
	}
}
