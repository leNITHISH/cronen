# cronen: notes for Claude

Read `PLAN.md` before doing anything. It is the spec.

- Work only on the milestone you were asked to do. Don't start the next one.
- Use Bubble Tea **v2** (`charm.land/*/v2` imports). Never mix in v1 `github.com/charmbracelet/*` imports. When unsure about an API, read the module source in `$(go env GOMODCACHE)` instead of guessing.
- This environment is probably not Arch Linux. Never require `pacman`, `checkupdates`, `vercmp`, or the network in tests; use the fakes and fixtures.
- Only `internal/system` may touch `os/exec`, the filesystem, or the network.
- Before finishing: run `make test` and `make lint`, and make sure both pass.
- If you can't run a check locally (missing tool, version mismatch), say so explicitly in the PR description. Never claim a check passes that you didn't run.
- Tool versions are pinned in one place. golangci-lint's version lives in the `Makefile` (`GOLANGCI_LINT_VERSION`); `make lint` installs exactly that version into `bin/`, and CI runs `make lint`. Don't use a golangci-lint from PATH.
- Keep commits small, with clear messages. Open a PR per milestone.
