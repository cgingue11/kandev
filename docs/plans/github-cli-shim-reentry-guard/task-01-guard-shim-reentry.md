---
id: "01-guard-shim-reentry"
title: "Guard the gh shim against re-entry"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-INTEGRATIONS-GITHUB-AUTHENTICATION-001
acceptance_criteria:
  - AC-INTEGRATIONS-GITHUB-AUTHENTICATION-001.14
system_design:
  - ../../specs/integrations/system-design/github-authentication-01.md
  - ../../specs/integrations/system-design/github-authentication-03.md
---

# Task 01: Guard the gh shim against re-entry

## Summary

Stop the `gh` shim from launching another copy of itself when the real-CLI
lookup resolves to a shim, so a stale shim directory on `PATH` fails one
command instead of exhausting the host.

## In scope

- Add `lookPathSkippingExecutable`, which rejects any `PATH` candidate that is
  the same file as the running `agentctl`, and use it from `main.go`.
- Add the `KANDEV_GITHUB_CLI_SHIM_ACTIVE` marker: refuse to run when it is set,
  set it on the child environment otherwise.

## Out of scope

- Removing stale shim directories from `PATH`.
- Lease redemption, reissue, or `gh` configuration isolation.

## Acceptance

- A symlink to `agentctl` ahead of the real `gh` on `PATH` is skipped and the
  real `gh` is launched.
- A shim started with the marker set returns an error naming the re-entry and
  launches nothing.
- Existing shim behavior for a valid real CLI is unchanged.

## Verification

```bash
(cd apps/backend && go test ./cmd/agentctl -run 'TestGitHubCLIShim|TestLookPathSkippingExecutable' -count=1)
(cd apps/backend && go test ./cmd/agentctl -count=1)
```

## Files likely touched

- `apps/backend/cmd/agentctl/github_cli_shim.go`
- `apps/backend/cmd/agentctl/github_cli_shim_test.go`
- `apps/backend/cmd/agentctl/main.go`
- `docs/specs/integrations/requirements/github-authentication.md`
- `docs/specs/integrations/system-design/github-authentication-01.md`
- `docs/specs/integrations/system-design/github-authentication-03.md`

## Dependencies

None.

## Risks

- A real `gh` that is a hard link to `agentctl` cannot exist, so the identity
  check cannot reject a legitimate CLI.

## Parallelism

`sequential`

## Inputs

- `AC-INTEGRATIONS-GITHUB-AUTHENTICATION-001.14` and the managed-routing design in parts 1 and 3.

## Results

Implemented both guards. The self-skipping lookup reuses `lookPathIn`'s search
order through a shared `lookPathMatching` helper. Verification passed:

```bash
(cd apps/backend && go test ./cmd/agentctl -count=1)
```
