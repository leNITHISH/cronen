package preflight

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/leNITHISH/cronen/internal/pacman"
	"github.com/leNITHISH/cronen/internal/risk"
	"github.com/leNITHISH/cronen/internal/system"
)

// Where the facts live.
const (
	MountsPath    = "/proc/self/mounts"
	FstabPath     = "/etc/fstab"
	OSReleasePath = "/proc/sys/kernel/osrelease"
	ModulesDir    = "/usr/lib/modules"
	LockPath      = "/var/lib/pacman/db.lck"
	BootDir       = "/boot"
)

// Gather reads the system side of the facts: free space, the mounts, the
// lock, whether a reboot is pending, and the size of the updating kernels'
// images in /boot. updated are the names of the pending repo updates.
//
// A fact that can't be read is left unknown and reported in the error,
// which lists every such failure on one line; the facts that could be read are still
// returned. A file that doesn't exist is a fact (no fstab entry, no lock),
// not a failure.
//
// Needs checking on Arch: the paths above, and the kernel image names in
// /boot. See PLAN.md and the M9 pull request.
func Gather(fsys system.FS, disk system.Disk, updated []string) (Facts, error) {
	f := Facts{Updated: updated}
	var errs []error
	space := func(p string) (Space, bool) {
		u, err := disk.Statfs(p)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			return Space{}, false
		}
		return Space{Path: p, Free: u.FreeBytes, ID: u.ID}, true
	}

	f.Root, _ = space("/")
	f.Boot, _ = space(BootDir)

	conf, err := fsys.ReadFile(pacman.ConfPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, dir := range pacman.CacheDirs(conf) {
		if s, ok := space(path.Clean(dir)); ok {
			f.Caches = append(f.Caches, s)
		}
	}

	mounts, err := fsys.ReadFile(MountsPath)
	if err != nil {
		errs = append(errs, err)
	} else {
		f.BootMounted = slices.Contains(mountPoints(mounts), BootDir)
	}
	fstab, err := fsys.ReadFile(FstabPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	f.BootDeclared = slices.Contains(mountPoints(fstab), BootDir)

	if _, err := fsys.Stat(LockPath); err == nil {
		f.LockExists = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}

	f.RebootPending, f.RunningKernel, err = rebootPending(fsys)
	if err != nil {
		errs = append(errs, err)
	}

	f.BootBytes = map[string]int64{}
	for _, k := range f.Kernels() {
		if n, ok := bootBytes(fsys, k); ok {
			f.BootBytes[k] = n
		}
	}

	if len(errs) > 0 {
		return f, readErrors(errs)
	}
	return f, nil
}

// readErrors reports every fact that couldn't be read on one line, and
// unwraps to each of them.
type readErrors []error

func (e readErrors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return "preflight: " + strings.Join(msgs, "; ")
}

func (e readErrors) Unwrap() []error { return e }

// rebootPending reports whether the running kernel's module directory is
// gone, which means an upgrade replaced the kernel since boot.
func rebootPending(fsys system.FS) (pending bool, release string, err error) {
	b, err := fsys.ReadFile(OSReleasePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	release = strings.TrimSpace(string(b))
	// The release comes from the kernel, but it ends up in a path.
	if release == "" || release == "." || release == ".." || strings.ContainsAny(release, "/\x00") {
		return false, "", nil
	}
	if _, err := fsys.Stat(path.Join(ModulesDir, release)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, release, nil
		}
		return false, release, err
	}
	return false, release, nil
}

// bootBytes adds up the sizes of a kernel package's images in /boot. ok is
// false when its vmlinuz isn't there.
func bootBytes(fsys system.FS, kernel string) (int64, bool) {
	if !risk.IsKernel(kernel) || strings.ContainsAny(kernel, "/\x00") {
		return 0, false
	}
	var total int64
	found := false
	for i, name := range []string{
		"vmlinuz-" + kernel,
		"initramfs-" + kernel + ".img",
		"initramfs-" + kernel + "-fallback.img",
	} {
		fi, err := fsys.Stat(path.Join(BootDir, name))
		if err != nil {
			continue
		}
		total += fi.Size()
		found = found || i == 0
	}
	return total, found
}

// mountPoints returns the mount point (second field) of every entry of an fstab or
// /proc/self/mounts file, with its octal escapes decoded and trailing
// slashes trimmed.
func mountPoints(data []byte) []string {
	var out []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		p := unescape(f[1])
		if len(p) > 1 {
			p = strings.TrimRight(p, "/")
		}
		out = append(out, p)
	}
	return out
}

var mountEscaper = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func unescape(s string) string {
	return mountEscaper.Replace(s)
}
