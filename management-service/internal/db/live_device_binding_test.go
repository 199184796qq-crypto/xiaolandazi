package db

import "testing"

func TestResolveLiveBindingRole(t *testing.T) {
	tests := []struct {
		name            string
		requested       string
		hasPrimary      bool
		primaryIsDevice bool
		sameRoom        bool
		want            string
	}{
		{name: "first device becomes primary", requested: "listener", want: "primary"},
		{name: "new device joins occupied room as listener", requested: "primary", hasPrimary: true, want: "listener"},
		{name: "listener can be promoted", requested: "primary", hasPrimary: true, sameRoom: true, want: "primary"},
		{name: "listener stays listener", requested: "listener", hasPrimary: true, sameRoom: true, want: "listener"},
		{name: "sole primary cannot demote itself", requested: "listener", hasPrimary: true, primaryIsDevice: true, sameRoom: true, want: "primary"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolveLiveBindingRole(test.requested, test.hasPrimary, test.primaryIsDevice, test.sameRoom)
			if got != test.want {
				t.Fatalf("resolve role = %q, want %q", got, test.want)
			}
		})
	}
}
