# App Compatibility Gap Report

- **Status:** Phase 0 proposal
- **Date:** 2026-11-11

This report compares the current Chatto implementation with the generated app
contract registry. It does not claim that planned behavior exists.

## Summary

Chatto has strong domain foundations for bots, messages, rooms, files,
reactions, pins, search, and realtime events. It does not have the target HTTP,
OAuth, manifest, event-delivery, Socket Mode, interaction, or structured-view
contracts.

The generated matrix has 935 contract rows:

- 0 implemented;
- 1 partial platform surface; and
- 934 unsupported contract rows.

The partial surface is the existing bot incoming webhook. It accepts compatible
text and destination fields, but it does not implement the complete install,
payload, path, response, rate-limit, or structured-message behavior.

## Gap matrix

| Area                                 | Current Chatto behavior                                                                                 | Required contract                                                                                                 | Gap and decision                                                                                                                              |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Public origin                        | `webserver.url` is the canonical external origin.                                                       | Every issued API, OAuth, webhook, Socket, upload, response, and permalink URL points to the deployment origin.    | Reuse `webserver.url`. Do not add a second canonical origin. Require HTTPS outside loopback tests.                                            |
| Web API                              | ConnectRPC services use protobuf messages.                                                              | `/api/{method}` accepts query, form, JSON, and multipart input and returns target JSON shapes.                    | Add an isolated API gateway and domain adapter. Native APIs stay unchanged.                                                                   |
| Method inventory                     | Chatto has no method registry.                                                                          | 357 documented methods plus 61 SDK-only names need explicit coverage.                                             | The generated registry and matrix now cover the union. Unsupported calls must return a machine error.                                         |
| App definitions                      | No app resource exists.                                                                                 | Apps have IDs, manifests, credentials, settings, scopes, and lifecycle.                                           | Add app aggregates and projections after design sign-off.                                                                                     |
| Installations                        | Bot accounts are created and managed directly.                                                          | An app install links an app, workspace, bot, scopes, tokens, webhooks, and delivery settings.                     | Create one managed bot per install. Keep the human installer as initial owner.                                                                |
| Manifest import                      | No parser or validator exists.                                                                          | JSON and YAML import, unknown-key round-trip, diff, validation, one-step install, and export.                     | Add a manifest service generated from registry facts. Preserve unknown keys.                                                                  |
| Bot identity                         | Bots are first-class Chatto users with direct grants and an owner ceiling.                              | Bot tokens act as app bot users.                                                                                  | Reuse the canonical bot principal. Do not add a parallel bot authority.                                                                       |
| User tokens                          | Chatto OAuth access tokens serve client applications and expire under Chatto session rules.             | App user tokens can be non-expiring or rotating and carry target scopes.                                          | Add an app-specific delegated credential type. It cannot satisfy fresh authentication.                                                        |
| App-level tokens                     | No equivalent exists.                                                                                   | `xapp-` tokens authorize `apps.connections.open`.                                                                 | Add a non-user app credential with only app-level scopes.                                                                                     |
| Token prefixes                       | Chatto uses `cht_` credential formats.                                                                  | Some SDKs check `xoxb-`, `xoxp-`, `xapp-`, and rotating prefixes.                                                 | Add typed compatibility presentations. Store only verifiers for access tokens.                                                                |
| OAuth client secrets                 | Chatto client registrations use separate OAuth behavior.                                                | App OAuth validates client ID, client secret, redirect URI, code, PKCE, and rotation.                             | Add an app OAuth client. Hash client secrets and show them once.                                                                              |
| OAuth authorize origin               | Existing Chatto OAuth is not the app install flow.                                                      | Browser authorize routes are on the Chatto origin.                                                                | Add app install consent routes. SDK guidance must configure the authorize URL separately from the API URL.                                    |
| OpenID Connect                       | Existing authentication does not expose the target claim contract.                                      | Discovery, authorize, token, user info, keys, RS256, and literal claim names.                                     | Planned for Phase 5. Keep separate from normal app scopes.                                                                                    |
| Scopes                               | Bot permissions use Chatto RBAC names.                                                                  | 126 target scopes gate methods and events.                                                                        | Use target scope as an additional gate, then apply normal Chatto authorization. Unknown scopes fail install.                                  |
| Email lookup                         | Public user profiles do not expose email.                                                               | `users.lookupByEmail` and `users:read.email`.                                                                     | Add proposed `user.read-email`, default deny. Show this sensitive grant during consent.                                                       |
| Workspace ID                         | No target `T` ID exists.                                                                                | SDKs expect a stable workspace ID.                                                                                | Create one durable alias per Chatto server.                                                                                                   |
| User and bot IDs                     | Chatto user IDs already use uppercase alphanumeric bodies.                                              | Users use `U`; bots also have `B`.                                                                                | Reuse the user body for `U`; derive and resolve a stable `B` alias for bot metadata.                                                          |
| Room IDs                             | Channel rooms use `R`; DMs use a stable 14-character body.                                              | Channels use `C`; DMs use `D`; group DMs use `G`.                                                                 | Use reversible type-prefix aliases. Do not expose Chatto room groups as conversations.                                                        |
| Message key                          | Chatto uses stable event IDs.                                                                           | Apps use channel plus a six-digit fractional `ts`.                                                                | Add a deterministic per-room logical-microsecond projection.                                                                                  |
| Pagination                           | ConnectRPC APIs use native page tokens and varied limits.                                               | Cursor and legacy numeric pagination have target error and response shapes.                                       | Wrap native tokens in authenticated method-bound cursors. Add numeric adapters where required.                                                |
| Rate limits                          | No target per-method app rate model exists.                                                             | Limits apply per method, workspace, app, and minute; messages also have channel limits.                           | Add shared KV CAS buckets. Do not use process-local correctness.                                                                              |
| Incoming webhooks                    | `/webhooks/incoming/:credential` posts text as a bot and accepts several aliases.                       | `/services/{T}/{B}/{secret}`, room-bound install URL, exact text responses, structured payloads, and rate limits. | Keep the old route. Add a new room-bound compatibility credential and handler.                                                                |
| Message posting                      | `MessageService.CreateMessage` supports text, threads, echo, files, and idempotency.                    | `chat.postMessage` has many formatting, identity, block, metadata, and unfurl options.                            | Core text and threads map well. Blocks, metadata, ephemeral behavior, identity overrides, and unfurls need later phases.                      |
| Ephemeral messages                   | No ephemeral message domain exists.                                                                     | Commands and `chat.postEphemeral` can send ephemeral content.                                                     | Product gap. A first implementation can keep command responses local to the interaction client, but general ephemeral history is unsupported. |
| Bot-created DMs                      | Bots cannot start or fetch a DM through `StartDM`.                                                      | Apps can open a DM by user ID.                                                                                    | Existing DMs can map. Creating a new bot DM needs separate FDR approval.                                                                      |
| Conversations                        | Room services support create, update, archive, membership, history, replies, pins, read state, and DMs. | `conversations.*` uses target objects, flags, errors, pagination, and legacy visibility rules.                    | Strong semantic base. Add object shaping, aliases, scope gates, and exact error mapping.                                                      |
| Users                                | User services support public profiles, list, avatar, and profile update.                                | User objects have many target fields, email scope, presence, locale, enterprise, and team metadata.               | Core profile maps. Enterprise fields are null. Email and presence need explicit policy.                                                       |
| Reactions and pins                   | Canonical services exist.                                                                               | Target methods and events use message timestamps and external IDs.                                                | Planned adapter work in Phase 5.                                                                                                              |
| Files                                | Upload sessions, assets, signed locators, processing, and deletion exist.                               | External upload URLs, completion, file objects, public URLs, remote files, and Bearer downloads.                  | Reuse storage and scan policy. Add idempotent external completion and separate public grants.                                                 |
| Search                               | Message search exists behind a provider boundary.                                                       | Numeric search pagination and target match objects.                                                               | Add an adapter. Results remain limited by current Chatto visibility.                                                                          |
| Native realtime                      | Protobuf WebSocket protocol version 4.                                                                  | Socket Mode uses JSON envelopes, app tokens, acks, retries, and up to 10 connections.                             | Add a separate Socket hub. Do not change the native realtime protocol.                                                                        |
| Events API                           | Existing bot outbound webhooks send selected messages with best-effort process-local retries.           | 154 event contracts, signed Request URLs, durable retries, rate limits, auto-pause, and authorization listing.    | Existing webhooks are not a sufficient base. Add a durable event materializer and delivery lifecycle stream.                                  |
| Request signing                      | Bot outbound webhooks can use signing secrets, but the contract differs.                                | Exact versioned HMAC headers and raw-byte signatures.                                                             | Reuse crypto primitives, not the existing payload contract. Add SDK verifier tests.                                                           |
| Slash commands                       | No command registry exists.                                                                             | Signed form delivery, three-second ack, response URL, trigger ID, and collision behavior.                         | Add an interaction service in Phase 3.                                                                                                        |
| Interactivity                        | No target actions, shortcuts, options, views, or response URLs exist.                                   | Structured payloads, single-use triggers, bounded response URLs, modal state, and exact response actions.         | Add in Phase 4. Current authorization remains effective for late responses.                                                                   |
| Block Kit                            | Chatto messages use text, attachments, and native formatting.                                           | The union has 24 blocks, 54 element wire types, and 10 composition objects, with surface-specific limits.         | Add a server validator and client renderers. Unknown objects need an explicit placeholder.                                                    |
| App Home and modals                  | No app-owned client surfaces exist.                                                                     | Views are published, updated, pushed, and submitted.                                                              | Add new app-surface state and UI after core interactions.                                                                                     |
| Link unfurls                         | Chatto creates native link previews.                                                                    | Apps receive link events and can submit custom unfurls.                                                           | Add an app-owned unfurl path. Prevent app and native preview races.                                                                           |
| Message metadata                     | Chatto does not expose the target metadata schema.                                                      | Apps declare metadata types and emit metadata events.                                                             | Additive message metadata needs a separate persisted contract. Planned in Phase 5.                                                            |
| Workflows and functions              | Runling is independent and is not a Chatto app runtime.                                                 | Hosted and remote workflow contracts, functions, steps, triggers, and datastores.                                 | Do not couple Runling. Remote workflows need a later product design. Hosted Deno needs explicit approval.                                     |
| Agents and MCP                       | Chatto has an independent MCP integration and ChattoBot.                                                | Target agent views, assistant events, status, streaming, and platform MCP contract.                               | Do not treat existing MCP as wire-compatible. Add separate adapters only after Phase 6 design.                                                |
| Calls, canvases, lists, rich objects | Chatto has voice calls but no equivalent target object model for the other resources.                   | Rich object APIs and events.                                                                                      | Intentionally unsupported until product-specific designs exist. Voice call semantics are not assumed equivalent.                              |
| Enterprise, SCIM, audit              | Chatto is one server, not an enterprise grid.                                                           | Organization installs, shared channels, admin APIs, SCIM, audit, legal holds, and global IDs.                     | Out of the initial product boundary. Review only in Phase 8.                                                                                  |
| Legacy methods and RTM               | No legacy compatibility edge exists.                                                                    | SDK-only channel, group, IM, MPIM, workflow, and RTM surfaces.                                                    | Keep in registry. Decide per method in Phase 8. Retired endpoints can return documented deprecation errors.                                   |
| Level 3 gateway                      | No egress interception package exists.                                                                  | DNS/private-CA and proxy routes for hard-coded hosts.                                                             | Add a separate restricted gateway in Phase 9. Browser OAuth needs managed device routing too.                                                 |
| Console                              | Bots have an administration UI, but apps do not.                                                        | Complete app settings, import, credentials, endpoint tests, install, consent, and diagnostics.                    | Reuse bot management concepts. Add an Apps area with honest unsupported states.                                                               |
| Privacy                              | Chatto protects PII and does not log raw identifiers or queries.                                        | Apps and remote media can send content to external systems.                                                       | Consent and app details must list destinations and data classes. Proxy remote media to avoid browser IP disclosure.                           |
| Legal                                | No compatibility marketing exists.                                                                      | Commercial release can create terms and branding risk.                                                            | Counsel must review before a commercial release. Use factual compatibility statements only.                                                   |

## Source differences

The registry resolves known documentation and SDK differences as follows:

- send `message_action`, not `message_actions`;
- use `view` for `views.publish` and accept `views` as an input alias;
- use `metadata.message:read` and accept the older manifest alias;
- use `approval`, while preserving an unknown `slack_approval` field;
- accept workflow `input_parameters` in object form;
- use the union of documented and SDK block types; and
- preserve unknown manifest keys for export.

The current block union includes documentation-only `data_visualization` and
`layout`, SDK-only `auth_provider`, and Python SDK-only `call`.

## Real fixture assessment

### Janan HR

The prompt supplies a production manifest fixture, not an executable source
tree. Phase 1 can use it for manifest import, validation, credentials, scopes,
events, and console acceptance tests. Full runtime conformance waits for an
executable fixture.

### Self-hosted Sentry

The inspected Sentry version is not configurable enough for Level 2:

- Web API calls use the Python SDK default base URL.
- OAuth authorize is hard-coded.
- OAuth token exchange is hard-coded.
- Sentry does support Request URLs and verifies standard request signatures.

No-source-change operation needs the Level 3 gateway on the Sentry host and
managed DNS plus private-CA trust on install browsers. A source-editable setup
can reach Level 4 with explicit SDK and OAuth origin settings.

Sentry also uses blocks, actions, commands, response URLs, user lookup,
conversations, link unfurls, files, and message updates. It cannot be a Phase 1
or Phase 2 completion fixture. The source plan correctly places full Sentry
support in Phase 5.

## Changes to Chatto core that need approval

The design proposes these core changes in later phases:

1. New app-domain EVT events and projections.
2. New `APP_DELIVERIES` durable stream and consumers.
3. New app and user access credential types.
4. New `user.read-email` permission.
5. Bot deactivation and reactivation for uninstall, without crypto-shredding
   history.
6. A compatibility message timestamp projection.
7. New normalized structured-message data for app surfaces.
8. Optional app-owned message metadata.

Bot-created new DMs are not included. They need a separate behavior decision.

## Current compatibility statement

Chatto does not yet support a complete app compatibility level. The existing
incoming webhook is a useful partial Level 1 path. All other levels remain
planned.
