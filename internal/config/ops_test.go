package config

import (
	"reflect"
	"testing"
)

func TestPrivilegeAndProtected(t *testing.T) {
	cfg := Default()
	if got := cfg.UpgradeArgv(); !reflect.DeepEqual(got, []string{"sudo", "pacman", "-Syu"}) {
		t.Errorf("default upgrade = %v", got)
	}
	cfg.PrivilegeCommand = "doas"
	if got := cfg.UpgradeArgv(); !reflect.DeepEqual(got, []string{"doas", "pacman", "-Syu"}) {
		t.Errorf("doas upgrade = %v", got)
	}
	if got := cfg.PacdiffArgv(); !reflect.DeepEqual(got, []string{"doas", "pacdiff"}) {
		t.Errorf("pacdiff = %v", got)
	}
	cfg.PacdiffCommand = "run0 pacdiff -s"
	if got := cfg.PacdiffArgv(); !reflect.DeepEqual(got, []string{"run0", "pacdiff", "-s"}) {
		t.Errorf("pacdiff override = %v", got)
	}
	cfg.UpgradeCommand = "my upgrade"
	if got := cfg.UpgradeArgv(); !reflect.DeepEqual(got, []string{"my", "upgrade"}) {
		t.Errorf("override = %v", got)
	}

	cfg = Default()
	cfg.Protected = []string{"foo*", "glibc"}
	got := cfg.EffectiveProtected()
	if want := append(append([]string{}, DefaultProtected...), "foo*"); !reflect.DeepEqual(got, want) {
		t.Errorf("extend = %v, want %v", got, want)
	}
	cfg.ProtectedMode = WatchlistReplace
	if got := cfg.EffectiveProtected(); !reflect.DeepEqual(got, []string{"foo*", "glibc"}) {
		t.Errorf("replace = %v", got)
	}
}
