# AGENTS.md — cadence

The pattern layer of the VibrantGio design system: eighteen composed
application patterns assembled from prism components and pulse effects —
shell, navbar, sidebar, table, tabs, pagination, modal, popover, tooltip,
toast, alert, card, accordion, breadcrumb, hero, feature, pricing and
testimonial.

**Layer.** Tier 4 of ADR-001's stack, `mvu → spectrum → prism → pulse →
cadence → markdown`, alongside markdown. It imports `prism/button`,
`prism/coordination`, `prism/layout`, `prism/list`, `prism/theme` and
`prism/tokens`, plus `pulse/depth` and `pulse/tween`; mvu it uses only
indirectly, through those. Nothing in the design system imports cadence —
the workbench applications are its consumers.

**Read the canonical guide before you write code against this module.** It is
the organization's one agent guide — the module inventory with current tags,
the application skeleton, the MVU loop and rx semantics, typography, and the
pitfalls that are not guessable. It lives exactly once, in `vibrantgio/.github`,
and this file links it rather than copying it:

    https://raw.githubusercontent.com/vibrantgio/.github/master/llms.txt

**Module.** `github.com/vibrantgio/cadence`, one module at the repository
root.

**Build and test.** From the repository root:

    go build ./... && go test ./...
