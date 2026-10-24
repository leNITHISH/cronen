# Maintainer: leNITHISH

pkgname=cronen-git
_pkgname=cronen
pkgver=r0.0000000
pkgrel=1
pkgdesc='TUI that shows pending updates, Arch news and risky packages before pacman -Syu'
arch=('x86_64' 'aarch64')
url='https://github.com/leNITHISH/cronen'
license=('MIT')
depends=('glibc' 'pacman' 'pacman-contrib')
makedepends=('go' 'git')
optdepends=('xdg-utils: open upstream and changelog links'
            'paru: AUR upgrades'
            'yay: AUR upgrades')
provides=("$_pkgname")
conflicts=("$_pkgname")
source=("git+$url.git")
sha256sums=('SKIP')

pkgver() {
  cd "$_pkgname"
  if git describe --tags >/dev/null 2>&1; then
    git describe --long --tags --abbrev=7 | sed 's/^v//;s/\([^-]*-g\)/r\1/;s/-/./g'
  else
    printf 'r%s.%s' "$(git rev-list --count HEAD)" "$(git rev-parse --short=7 HEAD)"
  fi
}

prepare() {
  cd "$_pkgname"
  export GOFLAGS='-mod=readonly -modcacherw'
  go mod download
}

build() {
  cd "$_pkgname"
  export CGO_CPPFLAGS="$CPPFLAGS"
  export CGO_CFLAGS="$CFLAGS"
  export CGO_CXXFLAGS="$CXXFLAGS"
  export CGO_LDFLAGS="$LDFLAGS"
  export GOFLAGS='-buildmode=pie -trimpath -mod=readonly -modcacherw'
  # An explicit -ldflags replaces any in GOFLAGS, so keep them together.
  go build -ldflags "-linkmode=external -X main.version=$pkgver" -o "$_pkgname" ./cmd/cronen
}

check() {
  cd "$_pkgname"
  export GOFLAGS='-mod=readonly -modcacherw'
  go test ./...
}

package() {
  cd "$_pkgname"
  install -Dm755 "$_pkgname" "$pkgdir/usr/bin/$_pkgname"
  install -Dm644 doc/cronen.1 "$pkgdir/usr/share/man/man1/cronen.1"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$_pkgname/README.md"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
