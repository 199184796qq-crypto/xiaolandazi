package db

import "testing"

func TestEffectiveCustomerRoomLimit(t *testing.T) {
	cases := []struct {
		configured      int64
		membershipLimit int64
		want            int64
	}{
		{configured: 3, membershipLimit: 3, want: 3},
		{configured: 2, membershipLimit: 3, want: 2},
		{configured: 5, membershipLimit: 3, want: 3},
		{configured: 8, membershipLimit: 8, want: 8},
		{configured: 10, membershipLimit: 10, want: 10},
		{configured: 12, membershipLimit: 10, want: 10},
		{configured: 10, membershipLimit: 12, want: 10},
		{configured: 0, membershipLimit: 3, want: 0},
		{configured: 3, membershipLimit: 0, want: 3},
	}

	for _, tc := range cases {
		got := effectiveCustomerRoomLimit(tc.configured, tc.membershipLimit)
		if got != tc.want {
			t.Fatalf(
				"effectiveCustomerRoomLimit(%d, %d)=%d want %d",
				tc.configured,
				tc.membershipLimit,
				got,
				tc.want,
			)
		}
	}
}
