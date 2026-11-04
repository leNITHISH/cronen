package security

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leNITHISH/cronen/internal/system"
)

// numCmp compares dotted numeric versions, enough for these tables. The
// real comparison is vercmp, which tests never run.
func numCmp(a, b string) (int, error) {
	if a == "boom" || b == "boom" {
		return 0, errors.New("vercmp failed")
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = atoi(pa[i])
		}
		if i < len(pb) {
			y = atoi(pb[i])
		}
		if x != y {
			return x - y, nil
		}
	}
	return 0, nil
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestFixesFor(t *testing.T) {
	groups := []Group{
		{Name: "AVG-1", Packages: []string{"openssl"}, Status: "Fixed", Severity: "High", Fixed: "3.5", CVEs: []string{"CVE-1"}},
		{Name: "AVG-2", Packages: []string{"openssl"}, Status: "Fixed", Severity: "Low", Fixed: "3.4"},
		{Name: "AVG-3", Packages: []string{"openssl"}, Status: "Fixed", Severity: "Low", Fixed: "3.9"},
		{Name: "AVG-4", Packages: []string{"openssl"}, Status: "Vulnerable", Severity: "Low"},
		{Name: "AVG-5", Packages: []string{"nvidia"}, Status: "Fixed", Severity: "Medium", Fixed: "590.1"},
		{Name: "AVG-6", Packages: []string{"openssl"}, Status: "Not affected", Fixed: "3.5"},
		{Name: "AVG-7", Packages: []string{"other"}, Status: "Fixed", Fixed: "3.5"},
	}
	tests := []struct {
		name string
		p    Pending
		want []string
	}{
		{"fixed in the new version", Pending{Name: "openssl", Installed: "3.3", New: "3.5"}, []string{"AVG-1", "AVG-2"}},
		{"installed already has the fix", Pending{Name: "openssl", Installed: "3.5", New: "3.6"}, nil},
		{"fix newer than the update", Pending{Name: "openssl", Installed: "3.0", New: "3.1"}, nil},
		{"matches on pkgbase", Pending{Name: "nvidia-utils", Base: "nvidia", Installed: "580.0", New: "590.5"}, []string{"AVG-5"}},
		{"name alone doesn't match a different package", Pending{Name: "nvidia-utils", Installed: "580.0", New: "590.5"}, nil},
		{"no groups", Pending{Name: "zstd", Installed: "1", New: "2"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixes, err := FixesFor(groups, tt.p, numCmp)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range fixes {
				got = append(got, f.Group)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("fixes = %v, want %v", got, tt.want)
			}
		})
	}

	if _, err := FixesFor(groups, Pending{Name: "openssl", Installed: "boom", New: "9"}, numCmp); err == nil {
		t.Error("a failed comparison must be an error")
	}
}

func TestUnfixed(t *testing.T) {
	groups := []Group{
		{Name: "AVG-1", Packages: []string{"sudo"}, Status: "Vulnerable", Severity: "High", Affected: "1.9"},
		{Name: "AVG-2", Packages: []string{"vim"}, Status: "Vulnerable", Affected: "9.2"},
		{Name: "AVG-3", Packages: []string{"bash"}, Status: "Fixed", Affected: "1", Fixed: "2"},
		{Name: "AVG-4", Packages: []string{"zstd"}, Status: "Vulnerable", Affected: "1", Fixed: "5"},
		{Name: "AVG-5", Packages: []string{"gcc"}, Status: "Testing", Affected: "1", Fixed: "2"},
		{Name: "AVG-6", Packages: []string{"gnupg"}, Status: "Not affected", Affected: "1"},
		{Name: "AVG-7", Packages: []string{"curl"}, Status: "Unknown"},
	}
	installed := []Installed{
		{"sudo", "1.9.17"}, {"vim", "9.1"}, {"bash", "1.5"}, {"zstd", "1.5"},
		{"gcc", "3"}, {"gnupg", "2"}, {"curl", "8"}, {"unrelated", "1"},
	}
	got, err := Unfixed(groups, installed, numCmp)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Name+"/"+v.Group)
	}
	// sudo: affected and unfixed. vim: older than the affected range.
	// bash: status Fixed. zstd: a fix exists. gcc: fix not newer than the
	// installed version (so installed is past it). gnupg: not affected.
	// curl: no affected version recorded, so any installed one counts.
	want := "sudo/AVG-1,gcc/AVG-5,curl/AVG-7"
	if strings.Join(names, ",") != want {
		t.Errorf("unfixed = %v, want %s", names, want)
	}
}

const sample = `[
 {"name":"AVG-9","packages":["a","b"],"status":"Fixed","severity":"High","type":"x",
  "affected":"1.0","fixed":"1.1","issues":["CVE-2025-1\u0007"],"ticket":null},
 {"name":"AVG-10","packages":["c"],"status":"Vulnerable","severity":"Low","affected":"2","fixed":null,"issues":[]}
]`

func TestParse(t *testing.T) {
	gs, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].Name != "AVG-9" || gs[0].Fixed != "1.1" || gs[0].CVEs[0] != "CVE-2025-1" ||
		len(gs[0].Packages) != 2 || gs[1].Fixed != "" || gs[1].Status != "Vulnerable" {
		t.Errorf("groups = %+v", gs)
	}
	if _, err := Parse([]byte(`{"not":"an array"}`)); err == nil {
		t.Error("an object must be an error")
	}
}

func TestClient(t *testing.T) {
	f := system.NewFakeFetcher()
	f.Set(URL, system.FakeResponse{Body: []byte(sample)})
	gs, err := NewClient(f).Groups(context.Background(), true)
	if err != nil || len(gs) != 2 {
		t.Fatalf("%v, %v", gs, err)
	}
	if c := f.Calls(); len(c) != 1 || !c[0].Fresh {
		t.Errorf("calls = %+v", c)
	}

	f.Set(URL, system.FakeResponse{Status: 503})
	if _, err := NewClient(f).Groups(context.Background(), false); err == nil {
		t.Error("a 503 must be an error")
	}
}

func TestDescribe(t *testing.T) {
	f := Fix{Group: "AVG-2843", Severity: "High", CVEs: []string{"CVE-2025-1234", "CVE-2025-1235"}}
	if got, want := f.Describe(), "AVG-2843 (High): CVE-2025-1234, CVE-2025-1235"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
	if got := (Fix{Group: "AVG-1"}).Describe(); got != "AVG-1" {
		t.Errorf("Describe = %q", got)
	}
}
