package db

import "testing"

func TestSourceAgentMemoryID(t *testing.T) {
	cases := []struct {
		ref  string
		want int64
		ok   bool
	}{
		{"agent_memory:123", 123, true},
		{" agent_memory:42 ", 42, true},
		{"agent_memory:0", 0, false},
		{"agent_memory:-1", 0, false},
		{"agent_memory:nope", 0, false},
		{"question:123", 0, false},
	}
	for _, tc := range cases {
		got, ok := sourceAgentMemoryID(tc.ref)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("sourceAgentMemoryID(%q)=(%d,%v), want (%d,%v)", tc.ref, got, ok, tc.want, tc.ok)
		}
	}
}
