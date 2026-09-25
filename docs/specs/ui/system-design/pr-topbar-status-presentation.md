---
status: current
system: ui
requirements:
  - REQ-UI-PR-TOPBAR-BADGE-001
---

# PR topbar status presentation system design

## Purpose and boundaries

This design owns the layout and interaction of the GitHub PR badge in the task
top bar. `TaskPR` remains the source of state, and `getPRStatusColor` remains
the source of its base icon color. The shared GitHub glyph from
[the conflict indicator design](../../integrations/system-design/github-pr-conflict-indicator.md)
adds the independent conflict bubble. The GitHub detail panel and CI popover
continue to explain review, checks, and merge conditions.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| `REQ-UI-PR-TOPBAR-BADGE-001` | Single-PR rendering, Interaction and accessibility, Multiple PRs and phone, Verification |

## Single-PR rendering

`PRSingleButton` in `apps/web/components/github/pr-topbar-button.tsx` renders
`ChangeRequestTopbarContent` with the shared GitHub glyph and PR number. It
omits the `statusIcon` prop. The glyph receives `getPRStatusColor(pr)` and the
confirmed conflict observation. Remove local `PRStatusIcon` and
`mergeBlockerBadge` paths when all GitHub top-bar uses share the leading glyph.
`ChangeRequestTopbarContent` gains an optional leading glyph slot; its default
icon and optional `statusIcon` contract remain for registered providers.

Keep the single badge as one button containing the leading icon and number.
Do not add an invisible second button or reserve space for the removed icon.
The current `data-pr-state` and `data-pr-ready-to-merge` attributes remain so
existing status consumers and tests can inspect the same state.

## Interaction and accessibility

The existing click handler still opens the PR detail panel. Fine-pointer hover
and keyboard focus still open `PRCIPopover`, which shows text for review, CI,
and merge conditions. Give the single badge a localized accessible name or
description that includes its PR identity, lifecycle, checks, review, and
confirmed conflict when present. Derive that text from the same `TaskPR`
snapshot that drives the glyph; it does not replace `getPRStatusColor`
precedence. Use existing localized state terms where possible, and add
translated catalog entries only if needed.

## Multiple PRs and phone

`PRMultiButton` remains the count-and-chevron dropdown and keeps
`aggregatePRStatusColor(prs)`. Its leading shared glyph shows a conflict if
any open linked PR is conflicted. Each dropdown row uses the shared glyph with
that PR's own conflict state. The trailing chevron is an action cue. On phones,
`PRTopbarButton` is not mounted; `PRStatusChip` and its touch drawer remain the
status entry point. The nearest shipped phone exemplar is the PR status chip in
`mobile-pr-ci-chip.spec.ts`. The change does not alter phone composition,
scroll ownership, safe-area behavior, or touch targets.

## Verification

Color helper tests cover representative open, draft, failed, merged, and closed
PRs. Focused topbar component coverage checks the leading glyph, number,
conflict bubble, absent trailing status icon, localized accessible text, and
multi-PR chevron. Desktop Playwright checks a failed and conflicted single-PR
badge, keyboard disclosure, and click navigation. The existing mobile
Playwright PR status-chip scenario confirms the phone drawer remains available.

## Implementation plans

- [PR topbar badge simplification](../../../plans/pr-topbar-badge-simplification/plan.md)
- [GitHub PR conflict indicator](../../../plans/github-pr-conflict-indicator/plan.md)
