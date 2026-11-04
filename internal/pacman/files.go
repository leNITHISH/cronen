package pacman

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/system"
)

// ValidName reports whether name is a package name cronen passes to pacman.
func ValidName(name string) bool {
	return packageName.MatchString(name)
}

// ParseQl parses `pacman -Ql` output, "name /path" lines, into the paths
// each package owns. Directories (trailing slash) are left out. A package
// that owns no files has no entry.
func ParseQl(data []byte) (map[string][]string, error) {
	files := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Paths may contain spaces; names can't.
		name, path, ok := strings.Cut(line, " ")
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("pacman -Ql: line %d: unexpected format %q", n, line)
		}
		if strings.HasSuffix(path, "/") {
			continue
		}
		files[name] = append(files[name], path)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pacman -Ql: %w", err)
	}
	return files, nil
}

// fileBatch is how many packages go to one pacman -Ql call.
const fileBatch = 50

// PackageFiles runs `pacman -Ql` for names, in batches, and returns the
// files each owns. It uses -Ql rather than -Qlq because only -Ql says which
// package a path belongs to when several are asked about at once.
func PackageFiles(ctx context.Context, r system.Runner, names []string) (map[string][]string, error) {
	if err := checkNames(names); err != nil {
		return nil, err
	}
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	files := map[string][]string{}
	for batch := range slices.Chunk(names, fileBatch) {
		out, _, err := r.Run(ctx, "pacman", append([]string{"-Ql", "--"}, batch...)...)
		if err != nil {
			return nil, fmt.Errorf("listing package files: %w", err)
		}
		parsed, err := ParseQl(out)
		if err != nil {
			return nil, err
		}
		for k, v := range parsed {
			files[k] = v
		}
	}
	return files, nil
}
