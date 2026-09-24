package db

import "testing"

func TestSystemAgentLevelCode(t *testing.T) {
	cases := []struct {
		id   int64
		want string
	}{
		{1, "AL-000001"},
		{42, "AL-000042"},
		{999999, "AL-999999"},
		{1000000, "AL-1000000"},
	}

	for _, tc := range cases {
		if got := systemAgentLevelCode(tc.id); got != tc.want {
			t.Fatalf("systemAgentLevelCode(%d)=%q want %q", tc.id, got, tc.want)
		}
	}
}
