# App Compatibility Platform Design

- **Status:** Approved; Phase 1 foundation implemented
- **Phase:** 1 — foundation
- **Date:** 2026-11-11

## Sign-off and support gate

A maintainer approved this design and authorized Phase 1 on 2026-11-11. The
Phase 1 foundation now implements app definitions, manifest administration,
bot installation OAuth, shared rate limits, and the foundation Web API methods.
Later sections remain the approved target design for later phases.

The coverage matrix reports the wire contract that works now. It does not count
a related Chatto domain feature as compatible. See the generated
[coverage matrix](coverage-matrix.md) and the [gap report](gap-report.md).

## Purpose

The app compatibility platform will let a self-hosted app use Chatto after an
operator imports its manifest and changes its credentials and API origin. The
platform will preserve Chatto's authorization, event-sourcing, privacy, and
multi-replica rules behind a compatibility adapter.

The product name stays **Chatto**. Product UI can make a factual API
compatibility statement, but it must not use another product's name or visual
identity as Chatto branding.

## Goals

- Support compatibility levels 1–4.
- Give operators one manifest import and install flow.
- Keep the public origin deployment-configurable through `webserver.url`.
- Use official SDKs without logic changes when they expose an origin setting.
- Give every contract an explicit `implemented`, `partial`, or `unsupported`
  state.
- Return a machine error for every unsupported method. Do not discard unknown
  operations silently.
- Keep Chatto's canonical domain services, EVT facts, projections, membership,
  policy, and RBAC as the authority.
- Keep credentials, app data, and delivery state correct across replicas and
  restarts.
- Show users and operators which external systems receive data.

## Non-goals

- Vendor-hosted Marketplace apps are not supported. Their vendor-controlled
  hosts and OAuth registrations target another service.
- Chatto will not proxy through a real workspace on another service.
- Chatto will not add a hosted Deno runtime in this design.
- Phase 0 does not add HTTP routes, storage resources, protobuf fields, UI, or
  runtime units.
- A target scope will not bypass Chatto permissions or membership.
- Compatibility code will not write storage or projections directly.

## Evidence baseline

The generated registry uses the public Markdown reference and these published
SDK versions:

| SDK source               | Version |
| ------------------------ | ------- |
| `@slack/web-api`         | 8.3.0   |
| `@slack/types`           | 3.3.0   |
| `@slack/bolt`            | 5.1.0   |
| `@slack/oauth`           | 4.0.0   |
| `@slack/socket-mode`     | 3.2.0   |
| Python `slack-sdk`       | 3.46.0  |
| Python `slack-bolt`      | 1.30.0  |
| Java API client and Bolt | 1.54.0  |

The snapshot contains:

- 357 documented methods and 61 additional SDK-only method names;
- 154 events, including 33 documented `message` subtype contracts;
- 126 scopes;
- 24 block types in the documentation and SDK union;
- 54 element wire types in the documentation and SDK union;
- 10 composition objects; and
- 139 manifest key paths.

The SDK method inventories differ. The current source contains 283 Node method
names, 332 Python method names, and 345 Java method names. The registry keeps
their union and exact case.

Research did not call a live app-platform method or use recorded traffic. It
used public documentation, published package source, and the inspectable Sentry
source tree at commit `b03ef92f59cb7a1d3689eb2f8208838c214c02a0`.

## Current Chatto foundations

Chatto already has useful domain behavior:

- managed bot accounts with a human owner;
- explicit bot permissions, bounded by the owner's current authority;
- multiple independently revocable bot API keys;
- bot incoming webhook credentials;
- channel and direct-message rooms;
- threads, edits, deletion, reactions, pins, files, search, and read state;
- protobuf-first ConnectRPC services;
- semantic protobuf realtime events;
- one canonical event stream and projection barriers;
- durable workers for selected external effects; and
- a canonical public origin in `webserver.url`.

These features are not the target wire protocol. The compatibility platform
must adapt them. It must not expose internal protobuf messages as substitute
responses.

## Compatibility levels

| Level | Planned operator experience                                 | Required app change    | Phase 0 result                                                                                            |
| ----- | ----------------------------------------------------------- | ---------------------- | --------------------------------------------------------------------------------------------------------- |
| 1     | Replace an incoming webhook URL.                            | None                   | Partial today. The existing webhook supports text and destination aliases, but not the complete contract. |
| 2     | Import a manifest, set credentials, and set the API origin. | None                   | Planned.                                                                                                  |
| 3     | Route a self-hosted app through the Chatto egress gateway.  | None                   | Planned. Browser OAuth also needs managed DNS and trust, or direct token issue.                           |
| 4     | Set one official SDK constructor option.                    | One configuration line | Planned.                                                                                                  |
| 5     | Use a vendor-hosted Marketplace app.                        | Vendor change          | Out of reach. Chatto must say so.                                                                         |

Chatto does not claim levels 1–4 until their conformance tests pass. The
coverage matrix is the support authority.

## SDK origin configuration

Official SDKs do not read one standard environment variable for the API host.
Operator guidance must use the actual configuration surface.

| SDK             | API origin configuration                                                                  | OAuth authorize configuration                                                                                     | Result                                                                      |
| --------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Node Web API    | `new WebClient(token, { slackApiUrl })`                                                   | Not applicable                                                                                                    | First-class setting.                                                        |
| Node Bolt       | `clientOptions: { slackApiUrl }`                                                          | `installerOptions: { authorizationUrl }`                                                                          | First-class settings.                                                       |
| Node OAuth      | `clientOptions: { slackApiUrl }`                                                          | `authorizationUrl`                                                                                                | First-class settings.                                                       |
| Python Web API  | `WebClient(base_url=...)`                                                                 | `AuthorizeUrlGenerator(authorization_url=...)`                                                                    | First-class settings.                                                       |
| Python Bolt     | `App(client=WebClient(base_url=...))`                                                     | `OAuthSettings(authorization_url=...)`                                                                            | First-class settings.                                                       |
| Java API client | Set `SlackConfig.methodsEndpointUrlPrefix`, or call `MethodsClient.setEndpointUrlPrefix`. | Not applicable                                                                                                    | First-class Web API setting.                                                |
| Java Bolt       | Supply a `Slack` instance with a configured `SlackConfig`.                                | `App.buildAuthorizeUrl` hard-codes the external authorize URL. Use a custom install route or override the method. | Web API is configurable. OAuth needs app code or the Level 3 browser route. |

The Java result is important. Java Bolt does not have a first-class authorize
origin field in the inspected release. Do not document one.

## Sentry fixture result

The inspected Sentry version has three separate host constraints:

1. `SlackSdkClient` creates `WebClient(token=...)` without `base_url`.
2. Its identity provider hard-codes the OAuth authorize URL.
3. The same provider hard-codes the OAuth token exchange URL.

A no-source-change deployment therefore needs the Level 3 gateway for Sentry
server traffic and managed DNS plus the private CA on each install browser.
`HTTPS_PROXY` alone does not intercept the browser authorize navigation.

A source-editable Sentry deployment can use Level 4 after it adds `base_url`
and configurable OAuth URLs. This is more practical than managed browser DNS.
The operator guide must state both choices.

## Object mapping

One Chatto server maps to one target workspace.

| Target object             | Chatto authority                    | Mapping rule                                                                              |
| ------------------------- | ----------------------------------- | ----------------------------------------------------------------------------------------- |
| Workspace                 | Chatto server                       | Create one stable external `T` ID when the app platform is initialized.                   |
| Enterprise                | None                                | Return null or an unsupported error. Do not invent an organization.                       |
| App                       | New app definition                  | Use a durable app ID and preserve the imported manifest.                                  |
| Installation              | App plus one Chatto server          | One active bot installation per app and server. User grants are separate.                 |
| Bot user                  | Managed Chatto bot account          | The app install owns one canonical bot account. The human installer is its initial owner. |
| User                      | Chatto human account                | Keep the native stable user body and expose a target `U` alias.                           |
| Public or private channel | Chatto channel room                 | Replace the native room type prefix with `C`. Visibility still uses Chatto policy.        |
| Direct message            | Chatto DM room                      | Prefix the stable DM room body with `D`.                                                  |
| Group direct message      | Chatto group DM                     | Prefix the stable DM body with `G`. Do not expose Chatto room groups as conversations.    |
| Message                   | Chatto message event                | Use channel plus a stable compatibility timestamp. Keep the native event ID internally.   |
| File                      | Chatto message attachment           | Replace the asset type prefix with `F`; authorization still resolves the owning message.  |
| User group                | No equivalent                       | Unsupported until Chatto has a user-group product model.                                  |
| View                      | New interaction state               | Use a short-lived or durable `V` alias as required by the surface.                        |
| Call or huddle            | Chatto voice call is not equivalent | Unsupported until a separate semantic design exists.                                      |

The adapter must not expose a Chatto room to an app unless the installed
principal can see that room through normal authorization.

### Message timestamps

Apps use `channel` plus `ts` as the message key. Chatto will maintain a
compatibility timestamp projection.

For each room, process message creation facts in EVT order:

1. Convert the durable creation time to integer microseconds.
2. Set the logical value to the larger of that value and the previous room
   value plus one.
3. Format it as `seconds.microseconds` with six fraction digits.
4. Map the value to the canonical message event ID.

This rule gives a stable, sortable, unique value per room. Projection rebuild
from the same EVT order gives the same mapping. Edits and deletion never change
the value. The compatibility gateway waits for this projection before it
returns a write response.

History import currently creates new channels and writes records in archive
order. That rule keeps imported message aliases stable. A future design for
backdated merge into an existing room must preserve already issued aliases.

## Architecture boundary

Compatibility code will live behind one application adapter. Domain services
must not import target wire types.

```text
external app
    |
HTTP or WebSocket compatibility edge
    |
contract registry -> authentication -> scope gate -> rate limit
    |
Chatto app-domain adapter
    |
canonical Chatto services and authorization
    |
EVT, projections, RUNTIME_STATE, assets
```

### Components

| Component             | Chatto responsibility                                                                                                                                       |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| API gateway           | Parse query, form, JSON, and multipart input. Resolve a method case-insensitively. Apply auth, scopes, rate limits, and response shaping from the registry. |
| App identity service  | Own app definitions, installations, OAuth clients, grants, token lifecycle, and consent.                                                                    |
| Manifest service      | Preserve JSON or YAML input, validate known keys, retain unknown keys, calculate changes, and apply one install plan.                                       |
| Domain adapter        | Convert external IDs and method arguments into canonical Chatto service calls. This is the only compatibility package that knows both models.               |
| Alias projection      | Resolve workspace, app, room, user, bot, file, view, and message aliases.                                                                                   |
| Delivery materializer | Consume durable Chatto facts and create idempotent delivery requests for authorized subscriptions.                                                          |
| Delivery workers      | Sign and send HTTP requests, retry, record terminal outcomes, and enforce current access.                                                                   |
| Socket hub            | Issue short-lived WebSocket URLs, manage up to 10 connections per app, send one envelope to one connection, and process acknowledgements.                   |
| Interaction service   | Own commands, actions, shortcuts, options, views, trigger IDs, and response URLs.                                                                           |
| Block service         | Validate structured objects on the server and provide shared normalized data to client renderers.                                                           |
| Egress gateway        | Intercept configured server-side calls for Level 3 deployments. It is a separate deployable unit.                                                           |
| Console               | Import manifests, install apps, show one-time credentials, configure sections, test endpoints, and show safe diagnostics.                                   |

The existing native ConnectRPC and realtime APIs stay unchanged. The new edge
is additive.

## Public origin and routes

All server-issued URLs must derive from the parsed canonical `webserver.url`.
Do not add another primary origin setting.

Planned routes include:

- `/api/{method}`;
- `/oauth/v2/authorize`;
- `/oauth/v2_user/authorize`;
- `/openid/connect/authorize`;
- `/openid/connect/keys`;
- `/services/{workspace}/{bot}/{secret}`;
- `/app-socket/{ticket}`;
- upload URLs;
- response URLs; and
- message permalinks.

External app features require a public HTTPS `webserver.url`. Loopback HTTP can
remain available for development tests. Chatto must fail app-platform startup
or keep the feature disabled when it cannot create valid public URLs.

The primary incoming webhook URL stays on the canonical origin. An optional
host alias can route to the same handler when an operator configures DNS and
TLS for it. Chatto must not synthesize a `hooks.` hostname that is absent from
`webserver.url` and the deployment certificate. The Level 3 gateway can map a
legacy hook host to the canonical Chatto route.

Keep the existing `/webhooks/incoming/:credential` route. New app installation
webhooks use the `/services/` contract and a room-bound credential. This does
not change the current Chatto webhook's ability to select a destination.

## Durable app state

App definitions and installation lifecycle are durable product state. They
belong in EVT under new app aggregate subjects.

The proposed facts include these concepts:

- app created, updated, and deleted;
- manifest revision accepted;
- installation created, reauthorized, uninstalled, and reactivated;
- bot principal linked;
- scope grant changed;
- request URL verified, paused, resumed, or revoked;
- app token issued or revoked; and
- signing secret or client secret regenerated.

The exact protobufs need API and event-sourcing review in Phase 1. Do not put
raw secrets, message content, redirect query strings, or Request URL query
strings in EVT.

Use `RUNTIME_STATE` for short-lived or mutable coordination:

- authorization codes and idempotent exchange outcomes;
- PKCE challenges;
- rotating access and refresh-token state;
- Socket Mode connection tickets and delivery claims;
- trigger IDs and response URL use counters;
- shared rate-limit buckets; and
- endpoint verification attempts.

Use explicit expiry in each mutable record. Use leader-confirmed reads for a
rejection and revision checks for every consume, rotate, revoke, or claim race.

## Credential storage

- Hash bot, user, app-level, workflow, configuration, and access tokens.
- Hash OAuth client secrets. Show them only when Chatto creates or regenerates
  them. Chatto does not need plaintext to verify a client.
- Encrypt signing secrets because Chatto needs plaintext to sign outbound
  requests.
- Encrypt refresh tokens and other secrets that Chatto must return later.
- Use an app-owned DEK and the existing data-cryptography boundary for encrypted
  app configuration.
- Never log a token, secret, authorization code, reset URL, full query string,
  or raw app payload.

A compatibility bot token is a typed presentation of the canonical managed bot
credential. It must not create a second bot principal or a second permission
model.

## Tokens and authorization

### Token types

Phase 1 starts with non-expiring bot and app-level tokens. Later phases add user
tokens, rotation, workflow tokens, and configuration tokens.

Each token record binds:

- app ID;
- installation ID;
- Chatto server ID;
- principal type and principal ID;
- granted target scopes;
- token generation;
- issue and optional expiry times; and
- revocation state.

Non-rotating tokens do not expire. Revocation, uninstall, bot deactivation,
owner deletion, and app deletion invalidate them.

### Scope and RBAC order

Every method follows this order:

1. Validate the token through a leader-confirmed credential read when needed.
2. Check token type.
3. Check required target scopes from the generated registry.
4. Resolve external IDs without revealing hidden resources.
5. Call the canonical Chatto service.
6. Apply current membership, policy, bot owner ceiling, and RBAC.
7. Shape the result or machine error.

A target scope only makes a method eligible. It never grants a Chatto
permission. Bot calls still need the bot's explicit allowlist and the owner's
current authority. User calls still need the human user's current authority.

Manifest installation maps known scopes to proposed Chatto permissions. If a
scope has no safe mapping, installation fails with `invalid_scope` or keeps the
feature disabled. Do not accept a scope and then omit its data silently.

Email lookup needs new sensitive authority. The proposed permission is
`user.read-email`. It is default-deny, can be granted to a bot only through the
normal owner ceiling, and is also gated by `users:read.email`. This change needs
FDR and permission-catalog review before Phase 2.

### Bot direct messages

Chatto bots cannot start DMs today. Phase 2 will support a compatible
`conversations.open` only for an existing DM that contains the bot. Creating a
new bot DM needs a separate product decision because it changes
[FDR-038](../fdr/FDR-038-bot-accounts.md). Until approved, the matrix must mark
that method partial after implementation.

## OAuth and install flow

### Manifest import

The console flow is:

1. Paste or upload JSON or YAML.
2. Parse known fields and preserve unknown fields.
3. Show scopes, events, commands, shortcuts, external destinations, and
   unsupported features.
4. Create the app definition and its managed bot in one guarded operation.
5. Verify a Request URL when one is configured.
6. Ask for one install confirmation.
7. Apply the approved bot grants through canonical bot-management operations.
8. Show a one-time credential block.

If URL verification fails, keep the app definition and mark the endpoint
unverified. Do not enable delivery.

The credential block includes the bot token, app token when needed, signing
secret, client ID, client secret, API URL, authorize URL, and webhook URL. It
also includes SDK-specific configuration snippets.

### Browser OAuth

The authorize route requires a current Chatto human session. The consent page
shows:

- the app identity and owner;
- bot and user scopes;
- the exact Chatto actions and data each scope maps to;
- Request URL and other external origins that can receive data;
- incoming webhook destination; and
- unsupported or optional features.

Redirect URI checks use the registered list and reject fragments. Authorization
codes expire after 10 minutes. A code is single-use, but the exchange stores one
short-lived encrypted outcome so an exact retry after a lost response returns
the same tokens. A changed client, redirect URI, verifier, or grant fails.

Repeat authorization is additive only after a new consent prompt. A scope
increase does not become active before the install fact and bot permission
changes commit.

### Uninstall

Uninstall revokes tokens, stops delivery, invalidates Socket Mode, and
deactivates the managed bot. It does not crypto-shred the bot or its historical
messages. A later reinstall can reactivate the same managed identity after
consent. Permanent app deletion is a separate confirmed operation.

This needs a bot lifecycle review. The existing bot delete operation is not a
valid uninstall operation because account deletion can remove access to
historical bot-authored content.

## API gateway behavior

The gateway accepts query arguments, form data, JSON objects, and multipart
input as declared by the registry. It accepts unknown arguments and records a
safe warning when the contract permits one. It accepts documented boolean
forms. It rejects tokens in query strings and JSON bodies.

Method lookup is case-insensitive as a safety net. Responses use the canonical
method spelling and exact machine error codes. Unsupported methods return
`{"ok":false,"error":"unknown_method"}` unless the registry marks a more
specific retired-method error.

The gateway adds accepted and granted scope headers. It maps ConnectRPC errors
without exposing whether a hidden room, message, or user exists.

Cursor values are opaque, authenticated, method-bound values. They bind the
app, principal, normalized filter, and native page token. A changed method or
filter returns `invalid_cursor`. Cursor expiry is configurable.

Rate limits use shared revision-checked buckets per server, app, method, and
minute. Channel message limits add a room key. A storage failure fails closed
with a temporary service error. It must not fall back to process-local limits.

Message creation reuses Chatto's canonical idempotency operation when the
request contains a stable client request ID or the optional Chatto idempotency
header. If the target contract has no request ID, each HTTP attempt is a new
operation. A retry after a lost response can create a second message. Do not
merge identical bodies because legitimate repeated posts are valid.

## Event delivery design

EVT is the source outbox. A source app event cannot exist without its Chatto
domain fact.

Add a bounded, backed-up `APP_DELIVERIES` stream for delivery request and
terminal outcome facts. Delivery facts contain IDs and resource references, not
message plaintext or credentials.

### Materialization

A shared durable EVT consumer will:

1. Wait until the app, room, user, message, and authorization projections
   include the source sequence.
2. Select subscriptions that were active at that source sequence.
3. Check event scope and installed-principal visibility.
4. Create one deterministic delivery request per app, event type, source event,
   and subscription generation.
5. Confirm that all request facts exist before it acknowledges the source.

A unique delivery subject plus expected-last-subject-sequence prevents a retry
from creating the same request twice. Subscription projections retain active
sequence intervals so a configuration change does not alter the target set for
an older source event.

Unknown source facts fail retryably. A new event schema uses a new consumer
generation. This prevents an older replica from acknowledging work it cannot
interpret.

### HTTP delivery

Workers hydrate the current authorized payload before each attempt. A deleted
message, revoked scope, lost membership, disabled endpoint, or deactivated app
causes a terminal suppression, not data delivery.

The first 2xx response within three seconds is terminal success. Follow at most
two redirects through the same SSRF checks. Retry immediately, after one
minute, and after five minutes. An opt-in delayed mode can retry hourly for 24
hours. Store a terminal fact before acknowledging the delivery request.

A crash after the remote app accepts a request but before terminal persistence
can cause a duplicate. Event IDs and retry headers stay stable. Receivers must
deduplicate.

Apply the documented event rate and failure thresholds. Auto-pause only after
the minimum traffic threshold. Record the pause as durable app state and show
it in the console. Chatto can send owner notifications, but it must not require
email configuration for correctness.

### Request signing

Sign the exact bytes with the app signing secret. Use the standard versioned
HMAC base string and timestamp headers. The console test uses the published test
vector. Socket Mode payloads are not signed.

### Socket Mode

`apps.connections.open` returns a fresh, short-lived URL on the canonical
origin. The ticket is single-use and bound to one app-level token generation.

Allow 10 live connections per app. Store live ownership and short claims in
`RUNTIME_STATE`; these records coordinate delivery but do not replace durable
request and terminal facts. Send each envelope to one connection. If an ack is
lost or the connection owner disappears, release the claim and send the same
envelope again.

Use WebSocket ping frames within the Node client's timeout. Send refresh and
disconnect envelopes before planned connection expiry. Token revocation closes
all matching connections on every replica through a best-effort live signal and
leader-confirmed periodic revalidation.

## Commands and interactions

Commands and interactive requests use the same signed HTTP delivery or Socket
Mode stream. Their three-second acknowledgement deadline is a user experience
boundary.

- A trigger ID is a single-use `RUNTIME_STATE` record with a three-second
  expiry.
- A response URL is a secret capability with five revision-checked uses and a
  30-minute expiry.
- A Socket Mode ack payload and an HTTP response body use one response parser.
- Late app work uses a background response URL, not the original HTTP request.

Chatto will not let a response URL bypass a later bot revocation, membership
loss, or permission loss. This differs from the source contract's broad
bypass. It preserves Chatto's current authorization rule. The coverage matrix
must mark affected behavior partial.

## Incoming webhooks

The existing Chatto webhook remains available. The app platform adds a
room-bound compatibility webhook created during installation.

The new handler:

- uses the canonical origin;
- accepts the documented JSON fields;
- calls the normal message command as the managed bot;
- enforces current bot membership, permission, room state, and rate limits;
- returns plain text `ok` on success; and
- returns explicit documented text errors.

A credential is a secret. Store only its verifier. Show the URL once. Revocation
must take effect on every replica before a new request succeeds.

## Structured messages and client placement

The server owns parsing, validation, normalized structured data, limits, and
fallback text. `@chatto/client` owns protocol-neutral projection and action
state that every frontend or bot client needs. The bundled frontend owns Chatto
presentation and accessibility details. Use the `chatto-client-placement` skill
before Phase 4 code.

Do not store remote image bytes in browser storage. Do not let an image URL make
the browser contact a third party directly. A future renderer can use a Chatto
proxy with no cookies or referrer, SSRF checks, size limits, media validation,
and an operator policy. This reveals the Chatto server IP and the requested URL
to the remote host; the consent and app details pages must state that.

Message text remains the accessible fallback. Unknown block types are retained
where the contract requires round-trip, but the UI shows an unsupported-content
placeholder. It never discards the complete message silently.

## Files

Compatibility file IDs map to Chatto assets. Upload URLs, download URLs, and
permalinks come from `webserver.url`.

The adapter uses the existing chunked upload lifecycle, scan and media policy,
message ownership, signed asset access, and deletion facts. The external upload
completion call is idempotent. A lost completion response must return the
existing file result instead of attaching a second asset.

A public file URL is a separate explicit grant. It must not reuse a private
Bearer URL. Public URL design stays deferred to Phase 5.

## SSRF and external systems

Treat every app URL as untrusted.

- Allow HTTPS by default.
- Resolve and validate every address at connection time.
- Block loopback, private, link-local, multicast, metadata, and other special
  ranges.
- Do not follow redirects to a blocked address.
- Permit a private destination only through an explicit operator allowlist.
- Keep the resolved address fixed for the connection to prevent DNS rebinding.
- Apply the same checks to Request URLs, options URLs, image proxy fetches,
  unfurls, manifest URLs, and redirects.

An external app can receive user IDs, room IDs, message content, file metadata,
interaction values, and the server's public IP. OAuth redirects also reveal the
user's IP to the app host. The consent page and app details page list each
external origin and data class. Chatto logs only app IDs, safe destination
origin hashes, delivery IDs, status classes, and machine failure categories.

## Level 3 egress gateway

The gateway is a separate deployment unit. It supports:

1. split-horizon DNS plus a private CA in the app host trust store; and
2. an explicit HTTPS proxy for clients that honor proxy configuration.

The gateway terminates TLS for the intercepted host, rewrites approved routes
to the configured Chatto origin, and returns Chatto server-issued URLs
unchanged. It must not become an open proxy. It has an allowlist of legacy
hosts and paths, no payload logs, strict destination pinning, request size
limits, and health diagnostics that contain no credentials.

Browser OAuth is separate. A proxy on the app host does not affect the user's
browser. A no-source-change browser flow needs managed DNS and private-CA trust
on the browser device, or the operator must issue credentials from the Chatto
console when the app can accept direct installation data.

## Multi-replica and rollout rules

- Every rejection based on missing or revoked mutable state uses a
  leader-confirmed read.
- Every consume, exchange, rate update, response use, trigger use, delivery
  claim, and rotation uses KV revision checks.
- Domain mutations use the canonical aggregate OCC boundary and request-time
  authorization.
- Delivery materialization acknowledges a source only after idempotent durable
  request creation.
- Delivery workers acknowledge only after a terminal outcome is durable.
- Process-local locks can reduce work. They are never the correctness boundary.
- All shared consumers have versioned names and explicit lifecycle ownership.

New app EVT events create a rollback boundary. Phase 1 must first deploy readers
to all replicas while app writes stay disabled. Operators can enable app writes
only after every serving replica has the new event schema. After the first app
fact commits, rollback to a binary that cannot read that schema is not safe.
The phased brief defines the expand, enable, and rollback sequence.

The native client connection floor does not need to increase. The app edge is
additive and independent of existing ConnectRPC clients.

## Console UX

Add an **Apps** area to server administration.

The first useful flow is **Create app from manifest**:

- paste JSON or YAML;
- show line and JSON-pointer validation errors;
- show supported, partial, and unsupported capabilities before save;
- show all requested external origins;
- let the operator choose the incoming webhook room;
- install with one confirmation; and
- show credentials once with copy buttons and SDK snippets.

The app detail page uses sections for basic information, Socket Mode,
interactivity, commands, OAuth and permissions, event subscriptions, incoming
webhooks, app home, agents, workflows, organization features, tokens, and
advanced settings. A section that Chatto does not support stays visible with an
explicit explanation. It does not show a control that cannot work.

Diagnostics show verification state, recent safe delivery outcomes, retry
count, last success time, auto-pause state, token generation, and scope changes.
They never show payload content or secrets.

## Conformance strategy

Each implemented contract needs an automated test before its matrix state can
change.

The suite will run:

- unmodified Node and Python Bolt apps in HTTP and Socket Mode;
- plain Node, Python, and Java SDK clients;
- SDK request-signature verifiers against exact raw bytes;
- OAuth install and retry cases;
- method request and response type checks;
- event retry, redirect, timeout, and no-retry cases;
- Socket Mode ack, retry, reconnect, refresh, and 10-connection cases;
- manifest round-trip and JSON-pointer validation cases;
- visual tests for each supported block on each surface;
- the Janan HR manifest fixture, plus an executable container if its source is
  supplied; and
- a container fixture for self-hosted Sentry.

The generator check makes sure that every registry item appears in the matrix.
A test result generator can change a contract to `implemented` only when its
named conformance case passes.

## Documentation and architecture records

Phase 0 adds design documents and generated contract data only. It does not
change the current runtime inventory.

After sign-off, Phase 1 must create:

- an ADR for the compatibility edge, app aggregates, aliases, and durable
  delivery boundary;
- an FDR for app creation, installation, consent, management, and support
  claims;
- glossary entries for **App**, **App installation**, **Request URL**, and
  **Socket Mode**; and
- architecture inventory updates when runtime components, streams, consumers,
  subjects, state keys, effects, and HTTP interfaces are implemented.

Public website documentation is not justified until an operator can install an
app. The internal operator guide is a Phase 0 design artifact and says that the
feature is not available yet.

## Decisions that need approval

Design sign-off approves these choices:

1. Use Chatto domain services behind one compatibility adapter.
2. Use EVT as the source outbox and a bounded `APP_DELIVERIES` lifecycle stream.
3. Create one managed bot per app installation and keep current owner-ceiling
   authorization.
4. Keep target scopes as an additional gate, never as Chatto authority.
5. Derive all public URLs from `webserver.url`.
6. Use deterministic target aliases and a rebuildable per-room timestamp
   projection.
7. Add sensitive `user.read-email` authority before email lookup.
8. Keep bot-created-new-DM behavior unsupported until a separate approval.
9. Keep response URLs subject to current Chatto authorization.
10. Hash client secrets rather than retain decryptable plaintext.
11. Deactivate an app bot on uninstall instead of deleting its history.
12. Require an expand-then-enable rollout before new app EVT facts can be
    written.
13. Keep Level 5 explicitly unsupported.
14. Require legal review before a commercial release.

## Design result

The design can support levels 1–4 without replacing Chatto's domain model. It
requires substantial new identity, manifest, delivery, interaction, rendering,
and console work. The largest semantic gaps are bot-created DMs, sensitive
email lookup, ephemeral responses, hosted workflows, enterprise resources, and
rich-object surfaces.

Phase 1 must not start without explicit approval.
