# App Compatibility Operator Guide

- **Status:** Experimental Phase 1 foundation
- **Date:** 2026-11-11

The generated [coverage matrix](coverage-matrix.md) is the authority for current
support. Do not infer support from a related Chatto feature.

## Enable the platform

The app platform is disabled by default. Deploy the new readers to every serving
replica before you enable app writes. Then set one of these values:

```toml
[apps]
enabled = true
```

```text
CHATTO_APPS_ENABLED=true
```

Set `webserver.url` to the canonical public HTTPS origin before startup. Chatto
accepts HTTP only when the canonical host is loopback, such as `localhost`, a
`.localhost` name, `127.0.0.1`, or `::1`.

After the first app fact is written, do not roll back to a binary that does not
understand the app event schema. To pause the feature, disable the setting on
all replicas. Keep a schema-capable binary in service.

## Origin

The API base is:

```text
{{CHATTO_ORIGIN}}/api/
```

`{{CHATTO_ORIGIN}}` is the value of Chatto's canonical `webserver.url`. It is not
a fixed Chatto-hosted domain. Keep the trailing slash on the API base for SDKs
that concatenate method names.

The OAuth authorize URL is:

```text
{{CHATTO_ORIGIN}}/oauth/v2/authorize
```

An app often needs both values. Changing only the Web API base does not change
where its browser installation starts.

## Current Phase 1 support

The compatibility gateway implements these Web API methods:

- `api.test`;
- `auth.test`;
- `auth.revoke`; and
- `apps.uninstall`.

`oauth.v2.access` supports bot installation and exact retry of one code
exchange. It does not support user tokens or enterprise installation fields.
All other method routes return the machine error `not_implemented`.

The Server Admin Apps page supports manifest review, create and install, update,
export, reinstall, uninstall, credential replacement, and delete. Chatto
preserves unknown manifest keys. Known behavior gaps appear as warnings. A
manifest that requests a user scope is rejected in this phase.

Event delivery, Request URLs, Socket Mode, slash commands, interactions,
structured message rendering, compatible file upload, and most Web API methods
are not implemented.

## Compatibility levels

### Level 1 — Incoming webhook

The operator replaces the complete webhook URL. No app code changes.

Chatto has a partial version at:

```text
{{CHATTO_ORIGIN}}/webhooks/incoming/{credential}
```

It accepts compatible `text` and `channel` fields, Chatto aliases, and selected
third-party payloads. It is not the complete app installation webhook contract.
It does not support every structured field or exact target response. Phase 1
app installation does not create this URL.

### Level 2 — Configurable self-hosted app

For the Phase 1 foundation, the operator:

1. opens Server Admin, then **Apps**;
2. pastes a JSON or YAML manifest;
3. reviews scopes, external origins, and compatibility warnings;
4. creates and installs the app;
5. copies the show-once credentials; and
6. sets the app's API and OAuth authorize origins.

This level becomes useful only when the app needs the methods that the coverage
matrix marks as implemented or partial.

### Level 3 — Hard-coded self-hosted app

Unsupported in Phase 1. A future restricted egress gateway can support:

- split-horizon DNS and a private CA that the app host trusts; or
- an HTTPS proxy when the HTTP client honors proxy configuration.

A gateway changes server-side traffic only. It does not change browser
navigation to a hard-coded OAuth authorize URL. A no-source-change browser flow
also needs managed DNS and private-CA trust on the user's device, or direct
credential issue when the app can accept it.

This level has significant operational and security cost. Prefer Level 2 or 4
when possible.

### Level 4 — Source-editable app

The owner adds the official SDK's API origin option and, when needed, its OAuth
authorize option. This is usually a small configuration change, not an app logic
rewrite. The app still works only for contracts that the coverage matrix marks
as implemented or partial.

### Level 5 — Vendor-hosted Marketplace app

Unsupported. The operator cannot change the vendor host, OAuth registration, or
destination. Chatto must not imply that these apps work.

## SDK configuration

These snippets configure the Phase 1 compatibility origin. They do not add
support for a method that the coverage matrix marks as unsupported.

### Node Web API

```javascript
const client = new WebClient(token, {
  slackApiUrl: `${chattoOrigin}/api/`,
  allowAbsoluteUrls: false
});
```

Set `allowAbsoluteUrls: false` when the app can receive a dynamic absolute
method URL. This prevents that value from bypassing the configured API base.

### Node Bolt

```javascript
const app = new App({
  token,
  signingSecret,
  clientOptions: {
    slackApiUrl: `${chattoOrigin}/api/`,
    allowAbsoluteUrls: false
  },
  installerOptions: {
    authorizationUrl: `${chattoOrigin}/oauth/v2/authorize`
  }
});
```

Socket Mode can pass the same Web API options through `clientOptions`, but
Chatto Socket Mode is not implemented.

### Node OAuth

```javascript
const installer = new InstallProvider({
  clientId,
  clientSecret,
  stateSecret,
  authorizationUrl: `${chattoOrigin}/oauth/v2/authorize`,
  clientOptions: {
    slackApiUrl: `${chattoOrigin}/api/`,
    allowAbsoluteUrls: false
  }
});
```

### Python Web API

```python
client = WebClient(
    token=token,
    base_url=f"{chatto_origin}/api/",
)
```

The async client has the same `base_url` option.

### Python OAuth URL generation

```python
generator = AuthorizeUrlGenerator(
    client_id=client_id,
    scopes=scopes,
    authorization_url=f"{chatto_origin}/oauth/v2/authorize",
)
```

### Python Bolt

```python
client = WebClient(token=token, base_url=f"{chatto_origin}/api/")

oauth_settings = OAuthSettings(
    client_id=client_id,
    client_secret=client_secret,
    scopes=scopes,
    authorization_url=f"{chatto_origin}/oauth/v2/authorize",
)

app = App(client=client, oauth_settings=oauth_settings)
```

### Java API client

```java
SlackConfig config = new SlackConfig();
config.setMethodsEndpointUrlPrefix(chattoOrigin + "/api/");
Slack slack = Slack.getInstance(config);
```

A `MethodsClient` also exposes `setEndpointUrlPrefix`.

### Java Bolt

Configure the `Slack` instance as above and put it in `AppConfig`:

```java
AppConfig appConfig = AppConfig.builder()
    .slack(slack)
    .build();
App app = new App(appConfig);
```

The inspected Java Bolt release hard-codes its browser OAuth authorize URL in
`App.buildAuthorizeUrl`. It does not expose an equivalent first-class setting.
Use one of these options:

- provide a custom OAuth install endpoint;
- override `buildAuthorizeUrl` in app code; or
- use a future Level 3 managed browser route.

Do not document a Java Bolt authorize setting that the SDK does not have.

## Sentry

The inspected self-hosted Sentry version hard-codes both OAuth URLs and creates
the Python Web API client without `base_url`.

### No source changes

This option needs the future Level 3 gateway, split DNS, and private-CA trust in
both the Sentry runtime and every browser that starts installation. An HTTPS
proxy on the Sentry server is not enough for browser OAuth.

### Source-editable setup

Add configuration for:

- `WebClient(base_url=...)`;
- the OAuth authorize URL; and
- the OAuth access-token URL.

Sentry also needs methods, blocks, actions, commands, response URLs, link
unfurls, files, and user lookup. Full fixture support is not part of Phase 1.

## Manifest and installation flow

1. Open Server Admin, then **Apps**.
2. Paste the complete JSON or YAML manifest.
3. Select **Review manifest**.
4. Fix every error. Review every compatibility warning and external origin.
5. Select **Create and install**.
6. Copy every required value from the show-once credential view.
7. Store those values in the app's secret store. Do not put them in source
   control, shell history, logs, or issue text.

Unknown keys remain in the stored document and export. Chatto does not silently
apply a key that it does not support. The maximum manifest size is 1 MiB.
Duplicate JSON and YAML keys and YAML aliases are rejected.

Uninstall revokes access tokens and deactivates the managed bot. It keeps the
client and signing secrets, bot identity, and historical authorship. Reinstall
uses the same bot and issues new access tokens. Delete requires an inactive
installation. Chatto blocks bot deletion while the app definition exists.
Reassign the bot or delete the app before you delete the bot owner's account.

## Authorization

App administration and browser approval require a human with `server.manage`
and `bot.create`. The bot's effective permissions cannot exceed the current
authority of its human owner.

A target OAuth scope is an additional gate. It never grants a Chatto permission,
room membership, or visibility. Current Chatto authorization still applies to
every mapped operation.

## Privacy review

Before installation, identify each external system that the app contacts.
Review at least:

- OAuth redirect origins;
- Request URLs and event destinations that a later phase can enable;
- command, interaction, and options URLs in the preserved manifest;
- image and media hosts;
- unfurl targets; and
- any future Level 3 gateway.

Phase 1 redirects the user's browser to the exact registered OAuth callback.
This reveals the user's IP address to that callback host. Future delivery
features can also reveal user IDs, room IDs, message content, file metadata,
interaction values, and the server's public IP. A stored manifest does not mean
that Chatto sends data to every preserved destination.

Use the smallest scope set. Uninstall an app when it no longer needs access.

## Credentials and diagnostics

The Apps page shows lifecycle state, manifest revision, install generation,
requested and granted scopes, external origins, and the canonical API and OAuth
URLs. It does not show old raw credentials.

Chatto returns client secrets, signing secrets, bot tokens, and app tokens only
from the operation that creates them. The response uses `Cache-Control:
no-store`. Replace a credential if its raw value is lost.

Do not enable HTTP request logging at a proxy that records authorization
headers, form bodies, or full query strings. SDKs should prefer the
`Authorization: Bearer` header instead of a query `token` value.

## Legal note

The registry and design use public documentation and published SDK source. They
do not use recorded live API behavior. Counsel must review API terms, naming,
and compatibility marketing before a commercial release.
