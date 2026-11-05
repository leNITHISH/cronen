package bandwidth

import "testing"

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func TestDefaultInterface(t *testing.T) {
	tests := []struct {
		name   string
		route  string
		want   string
		wantOK bool
	}{
		{"empty", "", "", false},
		{"header only", routeHeader, "", false},
		{
			"one default route",
			routeHeader +
				"wlan0\t00000000\t0102A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
				"wlan0\t0002A8C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n",
			"wlan0", true,
		},
		{
			"the lowest metric wins",
			routeHeader +
				"wlan0\t00000000\t0102A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
				"enp3s0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n",
			"enp3s0", true,
		},
		{
			"a route that is down is skipped",
			routeHeader +
				"enp3s0\t00000000\t0101A8C0\t0002\t0\t0\t100\t00000000\t0\t0\t0\n" +
				"wlan0\t00000000\t0102A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n",
			"wlan0", true,
		},
		{
			"no default route",
			routeHeader + "wlan0\t0002A8C0\t00000000\t0001\t0\t0\t600\t00FFFFFF\t0\t0\t0\n",
			"", false,
		},
		{
			"a name that could be an option is refused",
			routeHeader + "-bad\t00000000\t0102A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n",
			"", false,
		},
		{"garbage lines are skipped", "not a route table\n1 2 3\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DefaultInterface([]byte(tt.route))
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("DefaultInterface = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestValidDevice(t *testing.T) {
	for name, want := range map[string]bool{
		"wlan0": true, "enp3s0": true, "br-1a2b": true, "eth0.100": true, "veth@if3": true,
		"": false, "-x": false, "a b": false, "a/b": false, "a;b": false, "--help": false,
		"averyveryveryveryverylonginterfacename0": false,
	} {
		if got := ValidDevice(name); got != want {
			t.Errorf("ValidDevice(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestParseMetered(t *testing.T) {
	tests := []struct {
		out  string
		want Metered
	}{
		{"yes\n", MeteredYes},
		{"yes (guessed)\n", MeteredYes},
		{"no\n", MeteredNo},
		{"no (guessed)\n", MeteredNo},
		{"unknown\n", MeteredUnknown},
		{"", MeteredUnknown},
		{"YES\n", MeteredYes},
		{"nope\n", MeteredUnknown},
	}
	for _, tt := range tests {
		if got := ParseMetered([]byte(tt.out)); got != tt.want {
			t.Errorf("ParseMetered(%q) = %v, want %v", tt.out, got, tt.want)
		}
	}
}
