package events

import "testing"

func TestUserDimensionKeyFormats(t *testing.T) {
	if got := userKey(44, "user123"); got != "livecompanion:room:44:user:user123" {
		t.Fatalf("userKey = %q", got)
	}
	if got := roomUsersKey(44); got != "livecompanion:room:44:users" {
		t.Fatalf("roomUsersKey = %q", got)
	}
}

func TestAtoi64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"123", 123},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"-5", -5},
	}
	for _, c := range cases {
		if got := atoi64(c.in); got != c.want {
			t.Errorf("atoi64(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
