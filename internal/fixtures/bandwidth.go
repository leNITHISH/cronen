package fixtures

import (
	"strings"
	"time"

	"github.com/leNITHISH/cronen/internal/bandwidth"
	"github.com/leNITHISH/cronen/internal/system"
)

// The bandwidth helper's fixtures are HAND-WRITTEN, pending capture from a
// real Arch system. See HAND_WRITTEN.txt in testdata/.
const (
	ProcNetRoute         = "proc_net_route.txt"
	NmcliMeteredNo       = "nmcli_metered_no.txt"
	NmcliMeteredYes      = "nmcli_metered_yes.txt"
	NmcliMeteredGuessed  = "nmcli_metered_yes_guessed.txt"
	NmcliMeteredUnknown  = "nmcli_metered_unknown.txt"
	PacmanQlRepo         = "pacman_Ql-repo.txt"
	MeteredDevice        = "wlan0"
	NmcliMeteredCommand  = "nmcli -t -g GENERAL.METERED device show " + MeteredDevice
	CheckupdatesDownload = "checkupdates -d"
)

// bandwidthFiles are the files the bandwidth helper reads, for FS.
func bandwidthFiles() map[string][]byte {
	return map[string][]byte{"/proc/net/route": Bytes(ProcNetRoute)}
}

// lastUsed is when firefox and vim were last started, by the access times
// of the files FS holds for them. ripgrep's files get no time, so it has
// no "last used".
var lastUsed = map[string]time.Time{
	"/usr/bin/firefox":                        Now.AddDate(0, 0, -2),
	"/usr/share/applications/firefox.desktop": Now.AddDate(0, 0, -400),
	"/usr/bin/vim":                            Now.AddDate(0, 0, -130),
	"/usr/share/applications/vim.desktop":     Now.AddDate(0, 0, -200),
}

func setBandwidthFS(f *system.FakeFS) {
	for p, t := range lastUsed {
		_ = f.WriteFile(p, nil, 0o644)
		f.SetAccessTime(p, t)
	}
}

// DemoUpgradeSamples and DemoPrefetchSamples are the timing samples --demo
// starts with, so the header shows an estimate.
func DemoUpgradeSamples() []bandwidth.Sample {
	var out []bandwidth.Sample
	for i, mib := range []int64{420, 380, 700, 650, 510, 900} {
		out = append(out, bandwidth.Sample{
			Time:  Now.AddDate(0, 0, -30*(6-i)),
			Bytes: mib << 20, Packages: int(mib / 10), Seconds: float64(mib)/1.5 + float64(mib/10)*4,
		})
	}
	return out
}

// DemoPrefetchSamples are the prefetches the demo estimate measures the
// download speed with.
func DemoPrefetchSamples() []bandwidth.Sample {
	return []bandwidth.Sample{
		{Time: Now.AddDate(0, 0, -90), Bytes: 600 << 20, Packages: 60, Seconds: 600 / 1.5},
		{Time: Now.AddDate(0, 0, -60), Bytes: 480 << 20, Packages: 48, Seconds: 480 / 1.4},
	}
}

// nmcliResult is the fake result of nmcli printing the contents of file.
func nmcliResult(file string) system.FakeResult {
	return system.FakeResult{Stdout: Bytes(file)}
}

// qlRepo answers pacman -Ql for whether names are all in the repo -Ql fixture.
func qlRepo(names []string) (system.FakeResult, bool) {
	var out strings.Builder
	for _, n := range names {
		found := false
		for l := range strings.Lines(string(Bytes(PacmanQlRepo))) {
			if strings.HasPrefix(l, n+" ") {
				out.WriteString(l)
				found = true
			}
		}
		if !found {
			return system.FakeResult{}, false
		}
	}
	return system.FakeResult{Stdout: []byte(out.String())}, true
}
