# App Compatibility Phased Implementation Brief

- **Status:** Proposed; implementation is blocked on design sign-off
- **Date:** 2026-11-11

Each phase ends with a real, testable app workflow. Keep the generated registry
and coverage matrix current in every pull request. Do not change a row to
`implemented` until its named conformance test passes.

## Phase 0 — Discovery

### Scope

- Inspect Chatto's stack, domain model, APIs, authorization, storage, realtime,
  and frontend boundaries.
- Generate the method, event, scope, Block Kit, manifest, and SDK registry.
- Create the coverage matrix.
- Map target objects and operations to Chatto.
- Analyze failures and cross-replica interleavings.
- Define the architecture, gaps, operator migration, and phased plan.

### Deliverables

- `spec/registry/` and generator scripts;
- `docs/compat/coverage-matrix.md`;
- `docs/compat/design.md`;
- `docs/compat/gap-report.md`;
- `docs/compat/failure-interleavings.md`;
- `docs/compat/operator-guide.md`; and
- this implementation brief.

### Exit

The registry check passes. No feature implementation exists. A maintainer gives
explicit design sign-off.

## Phase 1 — Foundation

### Scope

- Add the app aggregate, projections, credentials, and install generation.
- Add the compatibility API gateway shell.
- Add external ID aliases and the message timestamp projection.
- Add bot and app-level tokens.
- Add OAuth authorize and exchange for bot installation.
- Add `api.test`, `auth.test`, `auth.revoke`, and `apps.uninstall`.
- Add shared rate-limit state.
- Add manifest parse, validate, create, update, export, delete, and one-step
  install.
- Add the Server Admin **Apps** shell and one-time credentials view.

### Chatto core changes

- New app-domain EVT protobufs and subjects.
- New app and alias projections.
- New app credential authentication path that resolves to the canonical bot.
- New runtime-state records for authorization codes, exchange outcomes, and
  rate buckets.
- No change to existing ConnectRPC or native realtime behavior.

### Rollout

1. Ship readers, projections, and the disabled edge.
2. Verify all replicas report the app event-schema capability.
3. Enable app writes.
4. After the first app fact, block rollback to a pre-feature reader.

### Verification

- Registry and coverage checks.
- App aggregate OCC and projection replay tests.
- Credential creation, show-once, hash, revoke, and leader-read tests.
- Concurrent OAuth exchange and lost-response tests.
- Alias rebuild and same-microsecond message tests.
- Shared rate-limit CAS tests.
- Manifest round-trip for all 139 known paths and unknown nested keys.
- Node and Python Bolt startup with configured API and authorize origins.
- Java API client and Java Bolt token-mode startup with a configured API origin.
- Janan HR manifest import and install acceptance test.

### Exit

Node and Python Bolt can authenticate and boot with the configured API and
OAuth origins. Java Bolt can boot in token mode with the configured API origin.
Janan HR imports and installs. No messaging method is claimed yet.

### Rollback

Disable the edge and app writes. Preserve app facts and credentials. A binary
that knows the new event schema can roll back application code. A pre-feature
binary cannot safely read the data after the first app fact.

## Phase 2 — Messaging core

### Scope

- Implement core `chat.*`, `conversations.*`, `users.*`, and bot object methods.
- Implement text and `mrkdwn` conversion needed by the core methods.
- Implement the room-bound `/services/` incoming webhook.
- Implement basic structured message objects: section, divider, header, image,
  context, actions, button, and text composition.
- Add channel, DM, thread, user, bot, and message object shaping.
- Add exact error mapping and cursor pagination.

### Chatto core changes

- Add `user.read-email` after permission and privacy review.
- Add app-owned structured message data only for the approved core subset.
- Do not let bots create new DMs unless a separate FDR is approved.

### Verification

- Plain Node, Python, and Java SDK calls for every implemented method.
- Post, update, delete, history, replies, list, info, membership, and existing-DM
  tests.
- Current membership and RBAC tests for every read and write.
- `users.lookupByEmail` consent, permission, and PII-log tests.
- Incoming webhook exact response and rate-limit tests.
- Block validation and visual tests for the core subset.
- Lost-response tests for calls with and without a stable request ID.

### Exit

An SDK sample posts, updates, deletes, replies in threads, lists conversations,
and opens an existing DM. Email-based DM setup works only when the caller has
the new sensitive authority. The matrix states the new-DM limitation.

### Rollback

Disable compatibility handlers by method group. Native Chatto APIs and stored
messages remain valid. Readers must continue to preserve any additive
structured message data.

## Phase 3 — Inbound delivery

### Scope

- Add `APP_DELIVERIES` and the app delivery projection.
- Add the durable EVT materializer.
- Add signed Events API Request URL delivery.
- Add retry headers, redirects, delayed mode, rate events, and auto-pause.
- Add `apps.event.authorizations.list`.
- Add Socket Mode, app-level token validation, claims, acks, pings, refresh,
  and up to 10 connections.
- Add slash commands, trigger IDs, and response URLs.

### Chatto core changes

- New bounded stream, protobufs, consumers, runtime-state keys, and admin
  diagnostics.
- No new message-domain facts for delivery attempts.

### Verification

- Durable materialization after worker stop and restart.
- Partial fanout crash recovery.
- Scope, membership, retraction, uninstall, and endpoint-generation fences.
- Published request-signing vector and SDK verifier checks.
- Slow, 500, redirect, no-retry, DNS rebinding, and private-address tests.
- Socket hello, one-of-10 routing, unacked retry, process death, refresh, and
  reconnect tests.
- `/standup` over HTTP Request URL and Socket Mode.

### Exit

A standup command works end to end over both delivery transports. Delivery is
at least once and survives restart.

### Rollback

Pause all app endpoints, stop workers, and retain pending stream state. A
compatible binary can resume. Do not delete a durable consumer until it is
drained or explicitly retired.

## Phase 4 — Interactivity and app surfaces

### Scope

- Add buttons, checkboxes, radio buttons, selects, date and time pickers,
  shortcuts, options load, modals, and App Home.
- Add views open, publish, push, update, submission, close, and hash behavior.
- Complete the documented Block Kit union that has an approved Chatto surface.
- Add client renderers and accessible fallback behavior.

### Client placement

Run the `chatto-client-placement`, Svelte, and frontend-review skills before
implementation. Put shared app protocol state in `@chatto/client`. Keep Chatto
presentation in the bundled frontend.

### Verification

- Bolt listeners for action, shortcut, view, and options.
- Trigger expiry and one-use tests.
- Response URL count, expiry, and authorization tests.
- Visual and accessibility tests for each supported object on each surface.
- Modal concurrency and hash-conflict tests.
- Janan HR complete runtime fixture, when executable source is available.

### Exit

The supported modal and App Home samples pass. Unknown structured objects show
an explicit placeholder and fallback text.

### Rollback

Disable app surfaces while retaining normalized message data. Older clients
must continue to show fallback text.

## Phase 5 — Platform breadth

### Scope

- Files, external upload completion, private downloads, and public grants.
- Reactions, pins, bookmarks, reminders, DND, presence, user groups, emoji,
  search, scheduled messages, link unfurls, and message metadata.
- User token grants, token rotation, configuration tokens, and OpenID Connect.
- Sentry methods and interaction paths.

### Required separate decisions

- Bookmark, reminder, custom emoji, user-group, and scheduled-message product
  models.
- App-controlled unfurl precedence.
- Public file grants.
- Message metadata persistence and privacy.

### Verification

- SDK typed calls for each implemented method.
- File completion idempotency, scanning, size, content type, and deletion.
- Search and visibility tests.
- Rotation, refresh replay, and revoke-all races.
- OpenID Connect discovery, signature, and literal claim tests.
- Self-hosted Sentry container fixture.

### Exit

The approved Sentry feature set works. The fixture report lists any source
configuration or Level 3 routing it still needs.

## Phase 6 — Agents and rich objects

### Scope

- Agent and assistant views, status, streaming, and events.
- The target platform MCP contract as a separate adapter.
- Work Objects.
- A product decision for canvases, lists, and call registration.

Do not couple these features to Runling, ChattoBot, or the existing Chatto MCP
server. Reuse domain-neutral boundaries only after an explicit design.

### Exit

The supported Bolt Assistant sample passes. A target MCP client connects to the
new adapter. Unsupported rich resources remain explicit in the matrix.

## Phase 7 — Workflows

### Scope

- Remote workflow functions, triggers, custom steps, completion, and failure.
- Workflow management UI and manifest application.
- A later decision on hosted code, datastores, and Deno.

### Gate

Ask before starting any hosted runtime. Hosted app code changes the security,
resource-isolation, dependency, and operations boundary.

### Exit

A remote custom-step sample passes. Hosted behavior remains unsupported unless
a separate approved design exists.

## Phase 8 — Enterprise and legacy

### Scope

- Decide org-ready installs, global identities, and shared-channel semantics.
- Review `admin.*`, SCIM, audit, legal holds, and enterprise events.
- Add approved legacy channel, group, IM, MPIM, workflow, and RTM shims.
- Return explicit deprecation errors for retired methods.

### Exit

Every remaining registry row has a final implemented, partial, or unsupported
reason. No row is left as an unreviewed placeholder.

## Phase 9 — Hardening and Level 3 gateway

### Scope

- Package the restricted egress gateway.
- Add split-DNS/private-CA and HTTPS proxy deployment guides.
- Add managed browser OAuth guidance.
- Run load, soak, failure, backup, restore, and mixed-version tests.
- Finish operator and migration documentation.
- Complete legal review.

### Verification

- Gateway host and path allowlists.
- No credential or payload logs.
- CA rotation and trust failure tests.
- Browser OAuth through managed DNS.
- API, delivery, and Socket load across replicas.
- Restore with pending delivery deadlines.
- Level 1–4 fixture matrix.

### Exit

The definition of done is met for levels 1–4. Chatto still states that Level 5
is unsupported.

## Pull request discipline

Each implementation pull request must include:

- the registry rows it changes;
- conformance tests for each row moved to `implemented` or `partial`;
- failure/interleaving tests for the changed boundary;
- relevant FDR, ADR, glossary, and architecture inventory updates;
- the exact verification that ran; and
- an honest list of unsupported behavior.

Do not combine unrelated phases in one large change. Prefer one durable
contract, one adapter slice, and one executable fixture improvement per pull
request.
