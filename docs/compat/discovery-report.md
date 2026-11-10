# App Compatibility Discovery Report

- **Status:** Phase 0 proposal
- **Date:** 2026-11-11

This report records the evidence used by the app compatibility design. It does
not claim that the app platform is implemented.

## Method

The discovery used:

- the Chatto repository and its current architecture records;
- the public Markdown developer reference sitemap;
- pinned published Node, Python, and Java SDK source archives; and
- source from one real self-hosted integration fixture.

The discovery did not call a live app-platform method or use recorded traffic.
It fetched public documentation and published source artifacts from package
registries. Generated registry entries record source URLs, versions, and
SHA-256 hashes.

## Chatto findings

### Deployment and protocols

- `webserver.url` is Chatto's canonical public origin.
- The server exposes ConnectRPC services and HTTP routes.
- Native realtime uses protobuf WebSocket protocol version 4.
- A JSON Socket Mode connection must be a separate adapter. It must not change
  the native realtime protocol.
- Chatto uses NATS and JetStream for durable EVT facts, projections, runtime
  state, and durable effects.

### Identity and authorization

- Bot accounts are first-class users.
- A bot has an owner and cannot have more effective authority than its owner.
- Bot grants, room membership, policy, and request-time authorization remain
  mandatory.
- Bots can have multiple API keys and incoming webhook credentials.
- Chatto client OAuth and sessions are not an app installation OAuth flow.
- Public profiles do not expose email. Email lookup needs a new sensitive
  permission and scope gate.

### Domain coverage

Chatto already has canonical services and facts for:

- channel rooms and direct messages;
- room membership and policy;
- messages, threads, edits, and retractions;
- reactions and pins;
- file upload sessions, assets, and private locators;
- message search;
- user profiles and avatars; and
- native realtime replay and recovery.

These services are the semantic base. Compatibility handlers must not write
projections or storage directly.

### Important gaps

Chatto does not have:

- app definitions or installations;
- target app OAuth grants or token types;
- `/api/{method}` JSON handlers;
- target IDs and message timestamps;
- a manifest service;
- signed Events API delivery;
- Socket Mode;
- slash commands, interactions, response URLs, triggers, or views;
- a complete Block Kit model and renderer;
- target rate limits; or
- a Level 3 egress gateway.

The existing incoming webhook is useful but only partial. Existing bot outbound
webhooks use a different contract and do not provide the required durable app
delivery lifecycle.

## Registry source snapshot

The selected documentation snapshot has 719 pages:

- 357 method pages;
- 154 event pages;
- 126 scope pages;
- 22 block pages, extended to 24 wire types by the published SDKs;
- 46 element pages, extended to 54 wire types by the published Node SDK;
- 10 composition object pages;
- 1 app manifest page; and
- 3 Block Kit category index pages.

The final registry contains:

- 418 method names, including 61 SDK-only names;
- 154 event contracts;
- 126 scopes;
- 24 block wire types;
- 54 element wire types;
- 10 composition object wire types;
- 139 manifest key paths; and
- 10 platform surfaces.

This produces 935 coverage rows.

## Event count reconciliation

The initial sitemap count was 154 event URLs. The first registry selected only
121 direct paths of this form:

```text
/reference/events/{event}.md
```

The omitted 33 pages were documented `message` subtype contracts of this form:

```text
/reference/events/message/{subtype}.md
```

The generator now includes both path forms. It gives each subtype a unique
contract identity such as `message.bot_message` or
`message.message_changed`. The registry also records its wire `event_type` and
`event_subtype`. Therefore, the reconciled event total is 154. No event URL in
the selected sitemap event tree is omitted.

## Published SDK evidence

The pinned source versions are:

| Ecosystem | Package              | Version |
| --------- | -------------------- | ------- |
| Node      | `@slack/web-api`     | 8.3.0   |
| Node      | `@slack/types`       | 3.3.0   |
| Node      | `@slack/bolt`        | 5.1.0   |
| Node      | `@slack/oauth`       | 4.0.0   |
| Node      | `@slack/socket-mode` | 3.2.0   |
| Python    | `slack-sdk`          | 3.46.0  |
| Python    | `slack-bolt`         | 1.30.0  |
| Java      | API client           | 1.54.0  |
| Java      | Bolt                 | 1.54.0  |

The method inventories contain 283 Node names, 332 Python names, and 345 Java
names. The registry records exact available method declaration types or
signatures for all 283 Node entries, all 332 Python entries, and 344 Java
entries. Java declares the legacy `files.comments.edit` constant but does not
expose a corresponding `MethodsClient` signature. The registry also records SDK
presence for every method in the union.

### API origin settings

- Node Web API: `WebClientOptions.slackApiUrl`.
- Node Bolt: `clientOptions` passes Web API options.
- Node OAuth: `authorizationUrl` changes the browser authorize URL.
- Python Web API: `WebClient(base_url=...)`.
- Python OAuth: `OAuthSettings(authorization_url=...)` and
  `AuthorizeUrlGenerator(authorization_url=...)`.
- Java Web API: `MethodsClient.setEndpointUrlPrefix()` or
  `SlackConfig.methodsEndpointUrlPrefix`.

The inspected Java Bolt release hard-codes its browser OAuth authorize URL in
`App.buildAuthorizeUrl`. The operator guide does not invent a setting that the
SDK does not expose.

## Source conflict policy

When sources disagree, the registry records the decision in
`spec/registry/source-conflicts.json`. Current decisions cover:

- message shortcut payload naming;
- `views.publish` argument naming;
- metadata scope aliases;
- manifest approval metadata;
- workflow input parameter shape;
- the Block Kit documentation and SDK union; and
- unknown manifest extensions.

Unknown manifest keys are preserved for round-trip. They are not treated as
implemented behavior.

## Real integration fixture

The inspected Sentry source creates the Python Web API client without a custom
API base. It also hard-codes the browser authorize URL and OAuth access URL.
Therefore:

- no-source-change operation needs Level 3 interception for server traffic and
  managed DNS and CA trust for browser OAuth; or
- source-editable operation needs explicit API, authorize, and access-token URL
  configuration.

Sentry also needs methods and surfaces that extend through Phase 5. It is not a
Phase 1 acceptance fixture.

## Phase 0 conclusion

Chatto has a strong semantic base, but almost none of the target wire contract
exists. The adapter can remain additive and can preserve Chatto authorization,
event sourcing, privacy, and native clients. The implementation must proceed in
small phases with generated coverage and executable fixtures.
