package aur_test

import (
	"context"
	"slices"
	"testing"

	"github.com/leNITHISH/cronen/internal/aur"
	"github.com/leNITHISH/cronen/internal/fixtures"
)

func TestInfoCoMaintainers(t *testing.T) {
	c := aur.NewClient(fixtures.Fetcher(), fixtures.Runner())
	infos, err := c.Info(context.Background(), []string{"visual-studio-code-bin", "paru"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := infos["visual-studio-code-bin"].CoMaintainers; !slices.Equal(got, []string{"dallas"}) {
		t.Errorf("CoMaintainers = %v", got)
	}
	if got := infos["paru"].CoMaintainers; len(got) != 0 {
		t.Errorf("paru CoMaintainers = %v", got)
	}
}
