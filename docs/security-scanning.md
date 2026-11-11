# Security scanning

## Current coverage

The `security` job in `.github/workflows/ci.yml` audits the web dependencies
on the workflow's existing `push` and `pull_request` events. It uses Node 24
and runs `npm audit --audit-level=high` from `web`, where
`web/package-lock.json` defines the dependency tree. The audit includes both
production and development dependencies; CI does not run `npm ci` for this
check and needs no secret.

An audit result with a high or critical finding fails the job. Lower severity
findings do not fail it. Registry and network errors also fail the job because
the audit step propagates npm's exit status. There is no allowlist or
`continue-on-error` behavior. A shell test uses a fake `npm` executable to
verify the wrapper's working directory, arguments, and exit status handling.

## Remaining coverage

This is one slice of JOOXF-76, not the complete security scanning plan. The
following scanners remain for later work:

- Gitleaks for repository content and history, with reviewed exceptions in
  versioned configuration.
- `govulncheck` for backend Go dependencies and its finding policy.
- Trivy for the built image, failing on high and critical findings with
  reasoned, versioned exceptions.

Pinning newly added actions by commit SHA and completing the security job's
full scanner coverage also remain open.
