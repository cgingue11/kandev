#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
deps_file="$(mktemp)"
trap 'rm -f "$deps_file"' EXIT

(
  cd "$repo_root/apps/backend"
  go list -deps ./cmd/agentctl
) >"$deps_file"

forbidden="$(grep -E '(^|/)internal/task/service$|^k8s\.io/' "$deps_file" || true)"
if [[ -n "$forbidden" ]]; then
  forbidden_count="$(printf '%s\n' "$forbidden" | wc -l | tr -d ' ')"
  printf 'agentctl dependency graph includes %s forbidden packages; first matches:\n' "$forbidden_count" >&2
  printf '%s\n' "$forbidden" | sed -n '1,12p' >&2
  exit 1
fi

printf 'agentctl dependency graph excludes task/service and Kubernetes packages\n'
