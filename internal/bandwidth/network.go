package bandwidth

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultInterface returns the interface carrying the default route from
// the contents of /proc/net/route: the up route to 0.0.0.0/0 with the lowest
// metric. The file is tab-separated, with a header line:
//
//	Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
//
// Destination, Gateway and Mask are hexadecimal, Flags is hexadecimal too
// (bit 1 is RTF_UP) and Metric is decimal. ok is false without a default
// route, or when the interface name isn't one cronen passes to nmcli.
func DefaultInterface(route []byte) (iface string, ok bool) {
	bestMetric := -1
	for line := range strings.Lines(string(route)) {
		f := strings.Fields(line)
		if len(f) < 8 || f[0] == "Iface" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&1 == 0 {
			continue
		}
		if f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		metric, err := strconv.Atoi(f[6])
		if err != nil || metric < 0 {
			continue
		}
		if bestMetric < 0 || metric < bestMetric {
			bestMetric, iface = metric, f[0]
		}
	}
	if bestMetric < 0 || !ValidDevice(iface) {
		return "", false
	}
	return iface, true
}

var devicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,31}$`)

// ValidDevice reports whether name is a network interface name cronen
// passes to nmcli. It can't start with a dash, so it is never an option.
func ValidDevice(name string) bool {
	return devicePattern.MatchString(name)
}

// Metered is what NetworkManager says about a connection.
type Metered int

// The values of GENERAL.METERED that matter.
const (
	MeteredUnknown Metered = iota
	MeteredNo
	MeteredYes
)

// ParseMetered parses the output of `nmcli -t -g GENERAL.METERED device
// show <dev>`: "yes", "no", "yes (guessed)", "no (guessed)" or "unknown".
// Both yes forms are metered: NetworkManager guesses from the connection
// type, for example a phone's hotspot.
func ParseMetered(out []byte) Metered {
	s := strings.ToLower(strings.TrimSpace(string(out)))
	switch {
	case s == "yes" || strings.HasPrefix(s, "yes "):
		return MeteredYes
	case s == "no" || strings.HasPrefix(s, "no "):
		return MeteredNo
	}
	return MeteredUnknown
}
