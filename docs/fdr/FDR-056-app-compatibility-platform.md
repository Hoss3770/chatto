# FDR-056: App Compatibility Platform

**Status:** Experimental
**Last reviewed:** 2026-11-11

## Overview

The app compatibility platform lets a server administrator import an external
app manifest, create a managed bot installation, and configure a self-hosted app
to use Chatto's compatibility API and OAuth origin. The platform keeps Chatto's
normal authorization and privacy rules. The generated compatibility matrix
states which external contracts work.

## Behavior

- Server administrators can review a JSON or YAML manifest before they store
  it. Chatto preserves unknown keys for export and reports known unsupported
  behavior.
- An administrator can create, install, reinstall, uninstall, export, and
  delete an app from Server Admin. The administrator can also replace its OAuth
  client secret and signing secret.
- Chatto shows raw credentials only in the response that creates or replaces
  them. Closing the credential view does not provide another way to recover the
  same raw value.
- One app installation owns one managed Chatto bot. Uninstall deactivates that
  bot and revokes its access tokens without removing its identity or message
  history. It keeps the client and signing secrets. Reinstall uses the same bot.
  Chatto blocks bot deletion while the app definition exists. An administrator
  must reassign the bot or delete the app before deleting the owner's account.
- Browser installation uses exact callback URLs from the current manifest.
  Phase 1 supports bot scopes only. A manifest that requests user scopes is
  rejected.
- The compatibility Web API supports `api.test`, `auth.test`, `auth.revoke`,
  and `apps.uninstall`. Bot OAuth exchange is partial. Other methods return
  `not_implemented`.
- Compatibility URLs use the server's canonical public origin. Responses that
  can contain credentials use a no-store cache policy.
- External scopes limit what the compatibility adapter can attempt. Chatto
  permissions, room membership, policy, and current account state still decide
  whether an operation is allowed.
- The existing bot incoming webhook remains a separate partial compatibility
  surface. App installation does not yet create an incoming webhook URL.

## Design Decisions

### 1. Keep one canonical Chatto principal

**Decision:** Each installation uses one managed bot account that is owned by a
human administrator.
**Why:** Bot ownership, permission ceilings, deactivation, and historical
authorship already have canonical Chatto behavior. See FDR-038 and ADR-122.
**Tradeoff:** External identity and scope models must map to Chatto's bot model.
Some external behaviors cannot be represented yet.

### 2. Treat scopes as an additional gate

**Decision:** A target scope can restrict an adapter method but cannot grant a
Chatto permission or resource visibility by itself.
**Why:** This prevents the compatibility edge from becoming a parallel
authorization system. See ADR-087 and ADR-122.
**Tradeoff:** An app can hold a scope and still receive a permission or
membership error.

### 3. Preserve manifests without claiming behavior

**Decision:** Chatto preserves unknown manifest keys and validates known keys.
It reports partial and unsupported contracts before a write.
**Why:** Operators need safe round-trip export without silent behavior claims.
**Tradeoff:** A stored manifest can contain settings that Chatto does not apply.

### 4. Use show-once credentials

**Decision:** Chatto returns raw client, signing, bot, and app credentials only
when it creates them.
**Why:** Durable facts need verifiers or encrypted recoverable values, not raw
bearer credentials. See ADR-081 and ADR-122.
**Tradeoff:** An operator who loses a value must replace the relevant
credential.

### 5. Disable app writes by default

**Decision:** App routes and writes require an explicit configuration setting.
Readers run before that setting is enabled.
**Why:** New durable app facts create a mixed-version deployment boundary. See
ADR-045 and ADR-122.
**Tradeoff:** Operators must complete an expand-then-enable rollout before they
create the first app.

## Gates

- `CHATTO_APPS_ENABLED` or `[apps].enabled` enables app writes, OAuth
  installation, and compatibility routes. The default is disabled.
- `webserver.url` must be an HTTPS public origin. HTTP is accepted only for a
  loopback development host.
- App administration and browser approval require a human account with
  `server.manage` and `bot.create`.
- Legal review is required before a commercial compatibility release.

## Related

- **ADRs:** ADR-033, ADR-042, ADR-045, ADR-068, ADR-081, ADR-087, ADR-122
- **FDRs:** FDR-023, FDR-038, FDR-045

## Open Questions

- Which message, room, user, file, and search methods will form the first
  supported application workflow?
- Which approved event types will be available through Request URLs and Socket
  Mode?
- Which structured message and interaction objects will have native Chatto
  client surfaces?
