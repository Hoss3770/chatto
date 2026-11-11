# App Compatibility Platform

This directory contains the contract registry, design, implementation plan, and
operator guidance for Chatto's experimental app compatibility platform.

- [Discovery report](discovery-report.md) — repository, documentation, SDK,
  fixture, and event-inventory evidence.
- [Design](design.md) — architecture, object mapping, security, and decisions
  that need sign-off.
- [Coverage matrix](coverage-matrix.md) — generated contract-by-contract current
  support state.
- [Gap report](gap-report.md) — differences between current Chatto behavior and
  the target contract.
- [Failure and interleaving analysis](failure-interleavings.md) — required
  multi-replica invariants and tests.
- [Phased implementation brief](phased-implementation.md) — implementation,
  rollout, verification, and rollback plan.
- [Operator guide](operator-guide.md) — current Phase 1 behavior,
  compatibility levels, rollout rules, and real SDK configuration surfaces.

The Phase 1 foundation is implemented behind a disabled-by-default setting. The
generated coverage matrix is the authority for support. Later phases remain
plans and must not be presented as current behavior.
