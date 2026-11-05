package pacman

import (
	"context"
	"errors"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

func TestRealPending(t *testing.T) {
	ctx := context.Background()
	run := func(res system.FakeResult) ([]Differing, error) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qu", res)
		return RealPending(ctx, r)
	}

	t.Run("pending, with an ignored line dropped", func(t *testing.T) {
		got, err := run(system.FakeResult{Stdout: []byte("zlib 1.3-1 -> 1.3.1-1\nbash 5.3.3-1 -> 5.3.3-2 [ignored]\n")})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "zlib" || got[0].NewVersion != "1.3.1-1" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("only ignored is nothing", func(t *testing.T) {
		got, err := run(system.FakeResult{Stdout: []byte("bash 5.3.3-1 -> 5.3.3-2 [ignored]\n")})
		if err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("nothing pending: exit 1, no output", func(t *testing.T) {
		got, err := run(system.FakeResult{Code: 1})
		if err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
	t.Run("exit 1 with an error message is an error", func(t *testing.T) {
		_, err := run(system.FakeResult{Code: 1, Stderr: []byte("error: failed to initialize alpm library\n")})
		if err == nil {
			t.Error("want an error")
		}
	})
	t.Run("pacman missing", func(t *testing.T) {
		_, err := run(system.FakeResult{Err: errors.New("not found")})
		if err == nil {
			t.Error("want an error")
		}
	})
	t.Run("no dbpath and no sync", func(t *testing.T) {
		r := system.NewFakeRunner()
		r.Set("pacman -Qu", system.FakeResult{Code: 1})
		if _, err := RealPending(ctx, r); err != nil {
			t.Fatal(err)
		}
		if calls := r.Calls(); len(calls) != 1 || calls[0] != "pacman -Qu" {
			t.Errorf("calls = %q, want exactly [pacman -Qu]", calls)
		}
	})
}
