package ops

import (
	"reflect"
	"testing"
)

func TestPrefetchArgv(t *testing.T) {
	sudo := []string{"sudo"}
	tests := []struct {
		name      string
		priv      []string
		needsRoot bool
		override  string
		want      []string
		wantErr   bool
	}{
		{"user can write the cache: no privilege command", sudo, false, "", []string{"checkupdates", "-d"}, false},
		{"cache needs root", sudo, true, "", []string{"sudo", "checkupdates", "-d"}, false},
		{"doas", []string{"doas"}, true, "", []string{"doas", "checkupdates", "-d"}, false},
		{"needs root but no privilege command", nil, true, "", nil, true},
		{"override is used as written, even without privilege", sudo, true, "my-fetch --all", []string{"my-fetch", "--all"}, false},
		{"override with its own sudo", sudo, false, "  sudo   checkupdates -d ", []string{"sudo", "checkupdates", "-d"}, false},
		{"override that is a shell is refused", sudo, false, "sh -c checkupdates", nil, true},
		{"override that is a shell behind sudo is refused", sudo, false, "sudo /bin/bash -c x", nil, true},
		{"override with a control character is refused", sudo, false, "checkupdates\x01 -d", nil, true},
		{"override with a NUL is refused", sudo, false, "checkupdates\x00", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PrefetchArgv(tt.priv, tt.needsRoot, tt.override)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argv = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrefetchPlan(t *testing.T) {
	ok := PrefetchInput{Priv: []string{"sudo"}, NeedsRoot: true, Previewed: true, Download: 1 << 30}
	plan, err := PrefetchPlan(ok)
	if err != nil || plan.Kind != KindPrefetch || !reflect.DeepEqual(plan.Command, []string{"sudo", "checkupdates", "-d"}) {
		t.Fatalf("plan = %+v, %v", plan, err)
	}

	noPreview := ok
	noPreview.Previewed = false
	nothing := ok
	nothing.Download = 0
	badOverride := ok
	badOverride.Override = "bash -c x"
	for name, in := range map[string]PrefetchInput{"no preview": noPreview, "nothing to download": nothing, "bad override": badOverride} {
		plan, err := PrefetchPlan(in)
		if err == nil || len(plan.Command) != 0 {
			t.Errorf("%s: plan %+v, err %v; want no plan", name, plan, err)
		}
	}
}
