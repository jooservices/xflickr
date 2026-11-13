This directory contains the shell helpers used by XFlickr's Makefile targets.
Go, Node, and Python commands go through `run.sh`, which uses the shared
runtime named by `WORKSPACES_RUNTIME` or the native tool when it is unset.

`make verify` runs the CI stages locally in `verify_stages.py` order and stops
at the first failure. `ci_parity.py` compares those stages with the Make targets
in `.github/workflows/ci.yml`; GitHub-only and local-only layers are listed in
`ci-parity-allowlist.json`.

The Go unit-test shards are grouped in `integration_shards.py`. Their coverage
profiles are merged before the package and changed-file gates run. Integration
tests use one shard per module. Locally, `make test-integration` recreates the
current worktree database before running those same modules. GitHub creates
isolated PostgreSQL, Redis and Mailpit services for each module shard; only the
search shard starts Elasticsearch. Other shards use a lightweight HTTP
placeholder because the workflow service set is static, and their integration
tests use the PostgreSQL search fallback when needed.

`.tool-versions` supplies the Go, Node, Python and golangci-lint pins. The
`ci_changes.py` path selector reads those pins and skips unrelated test jobs;
`ci_aggregate.py` checks required results and accepts only intentional skips.
