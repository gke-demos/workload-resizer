# Contributing to workload-resizer

Thanks for your interest in contributing! This file is the table of contents — most of the detail lives in [`dev/README.md`](./dev/README.md) and [`AGENTS.md`](./AGENTS.md).

By participating in this project you agree to abide by the [Code of Conduct](./CODE_OF_CONDUCT.md).

## Reporting bugs and requesting features

- **Bugs:** [open an issue](https://github.com/gke-demos/workload-resizer/issues/new) and include your Kubernetes version, the node instance types involved, the controller logs around the failure, and a minimal `Deployment` + `ConfigMap` that reproduces the problem.
- **Feature requests:** check the [open issues](https://github.com/gke-demos/workload-resizer/issues) first — your idea may already be tracked. If not, file an issue with the use case (what you're trying to do) before the proposed solution.
- **Questions / discussion:** [GitHub Discussions](https://github.com/gke-demos/workload-resizer/discussions).

## Pull requests

### Before you start

For anything beyond a typo fix or one-line bug, open an issue first so we can agree on the approach. PRs that are aligned upfront merge faster than ones that surface a design disagreement at review time.

### Workflow

1. Fork and create a short-lived feature branch off `main` (e.g. `feat/per-pod-baseline`, `fix/resize-retry`, `docs/install`).
2. Make your change. Keep the diff focused; unrelated cleanup belongs in a separate PR.
3. Run the full local CI before pushing:
   ```bash
   dev/tools/ci
   ```
   This is the same script that runs in GitHub Actions — green locally means green remotely. See [`dev/README.md`](./dev/README.md) for the full layout and how to add new checks.
4. Open the PR against `main`. CI runs on the PR; the required status checks (`test`, `lint`, `go mod tidy is clean`, `govulncheck`, `e2e`) gate the merge. Docs-only PRs satisfy the Go checks via a companion no-op workflow without running the full pipeline.

### Commit messages — Conventional Commits

Subject lines follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — user-visible new functionality
- `fix:` — user-visible bug fix
- `docs:` — documentation only
- `test:` — tests only
- `refactor:` — code change that's neither a feature nor a fix
- `chore:` / `build:` / `ci:` — repo plumbing

Optional scope in parens: `feat(controller): ...`, `fix(e2e): ...`. Keep the subject under ~70 chars; put detail in the body explaining *why* and what verification you did.

### Developer Certificate of Origin (DCO)

All commits must be **signed off** under the [Developer Certificate of Origin](https://developercertificate.org/). The DCO is a lightweight assertion that you wrote the patch (or have the right to submit it under the project's Apache-2.0 license) — it's a `Signed-off-by:` trailer in the commit message, not a cryptographic signature.

Sign off by passing `-s` to `git commit`:

```bash
git commit -s -m "feat(controller): support per-pod baseline override"
```

…which appends:

```
Signed-off-by: Your Name <you@example.com>
```

The name and email must match your `git config user.name` / `user.email`. If you forget, amend with `git commit --amend -s` (single commit) or rebase with `-x 'git commit --amend -s --no-edit'` (multiple).

### License headers

Every source file carries the full Apache 2.0 header attributed to Google LLC:

```
// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
```

`golangci-lint` enforces this on `.go` files automatically via the `goheader` linter. For new shell, YAML, or Python files, run `dev/tools/add-license-headers` once — it's idempotent and normalizes any existing header to the canonical form.

### Tests

The suite has three layers; see [`AGENTS.md`](./AGENTS.md#test-layers) for details.

- **Unit** (`internal/resize/`) — pure logic, no K8s.
- **envtest** (`internal/controller/`) — real API server (K8s 1.35 binaries via `make setup-envtest`), no scheduler.
- **e2e** (`test/e2e/`) — Kind + KWOK, deploys the controller and exercises real Deployments. Runs on every PR by default; see `test/e2e/README.md` for how to run against a non-Kind cluster.

A new feature without a test is not done. A new bug fix without a regression test makes it easy for the bug to come back.

## Project layout

- `cmd/main.go` — manager entry point.
- `internal/controller/` — Pod reconciler.
- `internal/config/` — ConfigMap-based controller config + polling refresher.
- `internal/resize/` — pure ratio + clamp logic (separately unit-tested).
- `config/` — kustomize manifests for deployment, RBAC, samples.
- `test/e2e/` — Kind + KWOK e2e suite.
- `dev/` — local + CI tooling (run from here, don't reinvent).
- `docs/site/` — Hugo source for the published documentation site.
- `.github/workflows/` — thin delegators to `dev/ci/presubmits/` (plus docs + image build).

For deeper context on conventions and gotchas, read [`AGENTS.md`](./AGENTS.md).

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](./LICENSE).
