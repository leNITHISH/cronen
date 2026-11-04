package config

import "testing"

func TestRebuildScanELF(t *testing.T) {
	if !Default().RebuildScanELF {
		t.Error("the ELF scan is on by default")
	}
	cfg, err := Parse([]byte("rebuild_scan_elf = false\n"))
	if err != nil || cfg.RebuildScanELF {
		t.Errorf("Parse = %+v, %v; want the scan off", cfg, err)
	}
}
