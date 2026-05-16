# dev/

Build- and test-tooling. Same scripts power both local development and
GitHub Actions CI, so a green local run is the same green run as remote.

## Quickstart

```bash
# Run every CI check locally (fast-fail order).
dev/tools/ci

# Run all checks even after a failure (collect every problem at once).
dev/tools/ci --keep-going

# Auto-fix formatting (gofmt + goimports).
dev/tools/fix-go-format

# Run the Kind + KWOK e2e suite (~3 min). Needs Docker + kind ≥ 0.20.
dev/tools/test-e2e
```

Missing tools (`golangci-lint`, `goimports`, `govulncheck`, `setup-envtest`)
auto-install into `$GOBIN` (or `$(go env GOPATH)/bin`) on first use. No
setup needed beyond a Go toolchain.

## Layout

```
dev/
├── tools/                  # entry points users run locally
│   ├── ci                  # aggregator — runs every check below
│   ├── vet                 # go vet ./...
│   ├── build               # go build ./...
│   ├── test-unit           # unit + envtest, with race + atomic coverage
│   ├── test-e2e            # Kind + KWOK e2e (wraps `make test-e2e`)
│   ├── lint-go             # golangci-lint (auto-installs pinned version)
│   ├── verify-go-format    # gofmt -s + goimports check (read-only)
│   ├── fix-go-format       # gofmt -s -w + goimports -w (auto-fix)
│   ├── verify-mod-tidy     # `go mod tidy` clean check
│   ├── verify-vuln         # govulncheck ./...
│   ├── add-license-headers # bulk-applier for the Apache 2.0 boilerplate
│   ├── common.sh           # shared bash helpers (ensure_tool, run_step)
│   └── .golangci.yml       # linter config
└── ci/
    └── presubmits/         # thin delegators called by .github/workflows/ci.yml
        ├── vet             # → dev/tools/vet
        ├── build           # → dev/tools/build
        ├── test-unit       # → dev/tools/test-unit
        ├── test-e2e        # → dev/tools/test-e2e
        ├── lint-go         # → dev/tools/lint-go
        ├── verify-go-format
        ├── verify-mod-tidy
        └── verify-vuln
```

## Why the wrapper layer?

Each presubmit is one line — `exec dev/tools/<name>` — but having both
sides explicitly means:

- the GitHub Actions YAML stays boring (just calls presubmits);
- the local entry points (`dev/tools/*`) work without needing to know
  anything about CI plumbing;
- the wrapper is the right place to put any CI-only setup (e.g.,
  exporting `GITHUB_STEP_SUMMARY` artifacts) without polluting the
  local script.

## Adding a new check

1. Write `dev/tools/<name>` — a self-contained bash script that
   sources `common.sh` and `cd`s to `repo_root`. Make it executable.
2. Add `dev/ci/presubmits/<name>` — copy any existing wrapper, change
   the path. Make it executable.
3. Add a job to `.github/workflows/ci.yml` that runs the presubmit.
4. Add the step to the `STEPS=(...)` array in `dev/tools/ci` so it
   shows up in the local aggregator.

## License headers

Every source file carries the full Apache 2.0 header attributed to
Google LLC. `golangci-lint`'s `goheader` linter enforces it on `.go`
files. For new shell / YAML / Python files, run
`dev/tools/add-license-headers` once — it's idempotent and normalizes
any existing header to the canonical form.

The canonical header template lives in three places that MUST stay in
sync:

- `dev/tools/.golangci.yml` — the `goheader.template` block (lint-time
  enforcement for `.go` files).
- `dev/tools/add-license-headers` — `HEADER_BODY` (the Python bulk
  applier for shell / YAML / Python).
- `hack/boilerplate.go.txt` — used by `controller-gen` when generating
  `zz_generated.*.go` files (see `make generate`).

If you change any one of them, change all three.
