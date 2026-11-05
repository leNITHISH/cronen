package linkage

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseLdConf parses the text of an ld.so.conf file into its directories and
// its include patterns, both in order. Comments, blank lines and the
// "hwcap" lines are skipped, and a directory may carry a "=" prefix
// (the "ld.so.conf" syntax for a library-class marker) which is dropped.
func ParseLdConf(data []byte) (dirs, includes []string) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case fields[0] == "include" && len(fields) > 1:
			includes = append(includes, fields[1:]...)
		case fields[0] == "hwcap":
		case strings.HasPrefix(line, "/"):
			// ld.so.conf allows directories separated by spaces, colons
			// or commas on one line.
			for _, d := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ':' || r == ',' || r == '\t' }) {
				if strings.HasPrefix(d, "/") {
					dirs = append(dirs, d)
				}
			}
		}
	}
	return dirs, includes
}
