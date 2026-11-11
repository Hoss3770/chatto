# ADR-122: Add External App Compatibility through an Additive Adapter

**Date:** 2026-11-11

## Context

Self-hosted external apps often expect a different HTTP API, OAuth installation
flow, manifest format, identifier scheme, and token format from Chatto's native
interfaces. Replacing Chatto's domain model or native protocols would create a
second authorization system and would weaken compatibility for existing Chatto
clients.

App definitions also contain recoverable secrets and external destinations.
Install and uninstall operations change an app and its managed bot together.
These changes must remain correct across replicas and after restart.

## Decision

Chatto provides app compatibility through one additive adapter. The adapter
translates external HTTP and OAuth contracts into the existing Chatto operation
models. Native ConnectRPC services and the native realtime protocol do not
change.

Each app installation owns one canonical managed bot. External scopes are an
additional gate. They do not replace Chatto permissions, membership, policy, or
request-time authorization.

Durable app lifecycle state is stored as app aggregate facts in `EVT`.
Recoverable manifests and signing secrets are encrypted with an app content key.
Raw access tokens and client secrets are not stored in durable facts. Mutable
OAuth exchange recovery and shared rate-limit buckets are stored in
`RUNTIME_STATE` with explicit expiry and revision checks.

All public app URLs are derived from `webserver.url`. Public app mode requires
HTTPS, except for loopback development origins. App writes and compatibility
routes are disabled by default. Operators must deploy readers to all serving
replicas before they enable writes. A pre-feature binary is not a safe rollback
after the first app fact is written.

The generated contract registry is the authority for support claims. A method
that is not implemented returns an explicit machine error.

## Consequences

- External apps can use a compatibility edge without changing Chatto's native
  API or domain authority.
- App and bot identity, permissions, history, and revocation use existing
  Chatto rules.
- The adapter has separate public contracts that need conformance tests and
  honest support states.
- Operators must manage an expand-then-enable rollout and a durable rollback
  boundary.
- Encrypted app facts require the deployment KMS boundary and app content-key
  records to remain available for replay.
- User-token OAuth, event delivery, Socket Mode, interactions, and most Web API
  methods remain separate phases.
