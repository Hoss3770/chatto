# App Compatibility Operator Guide

- **Status:** Phase 0 planning guide
- **Date:** 2026-11-11

The app compatibility platform is not implemented. Do not use this guide as a
support claim. The generated [coverage matrix](coverage-matrix.md) is the
current authority.

## Origin

The planned API base is:

```text
{{CHATTO_ORIGIN}}/api/
```

`{{CHATTO_ORIGIN}}` is the HTTPS value of Chatto's canonical `webserver.url`.
It is not a fixed Chatto-hosted domain. Keep the trailing slash on the API base
for SDKs that concatenate method names.

The planned OAuth authorize URL is:

```text
{{CHATTO_ORIGIN}}/oauth/v2/authorize
```

An app often needs both values. Changing only the Web API base does not change
where its browser install starts.

## Compatibility levels

### Level 1 — Incoming webhook

The operator replaces the complete webhook URL. No app code changes.

Chatto has a partial version today at:

```text
{{CHATTO_ORIGIN}}/webhooks/incoming/{credential}
```

It accepts compatible `text` and `channel` fields, Chatto aliases, and selected
third-party payloads. It is not the complete app install webhook contract. It
does not support every structured field or exact target response.

A future app install will issue this compatible path:

```text
{{CHATTO_ORIGIN}}/services/{workspace}/{bot}/{secret}
```

### Level 2 — Configurable self-hosted app

The operator will:

1. Import the app manifest in Chatto.
2. Review scopes and external destinations.
3. Install the app.
4. copy the one-time credentials;
5. set the app's API and OAuth authorize origins; and
6. set its Request URL or use the Socket Mode app token.

No app logic change is needed when the app already exposes these settings.

### Level 3 — Hard-coded self-hosted app

The future restricted egress gateway will support:

- split-horizon DNS and a private CA trusted by the app host; or
- an HTTPS proxy when the HTTP client honors proxy configuration.

The gateway changes server-side traffic only. It does not change a browser
navigation to a hard-coded OAuth authorize URL. A no-source-change browser flow
also needs managed DNS and private-CA trust on the user's device, or direct
credential issue from Chatto when the app can accept it.

This level has significant operational and security cost. Prefer Level 2 or 4
when possible.

### Level 4 — Source-editable app

The owner adds the official SDK's API origin option and, when needed, its OAuth
authorize option. This is usually one small configuration change, not an app
logic rewrite.

### Level 5 — Vendor-hosted Marketplace app

Unsupported. The operator cannot change the vendor host, OAuth registration,
or destination. Chatto must not imply that these apps work.

## SDK configuration

The snippets show planned configuration. They do not work until the Chatto
compatibility routes exist.

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

Socket Mode passes the same Web API options through `clientOptions`.

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
Use one of these routes:

- provide a custom OAuth install endpoint;
- override `buildAuthorizeUrl` in app code; or
- use the Level 3 managed browser route.

Do not document a Java Bolt authorize setting that the SDK does not have.

## Sentry

The inspected self-hosted Sentry version hard-codes both OAuth URLs and creates
the Python Web API client without `base_url`.

### No source changes

Required planned setup:

- Level 3 gateway for the Sentry server;
- split DNS for the hard-coded API and OAuth host;
- private-CA trust in the Sentry runtime; and
- the same managed DNS and CA trust on each browser that starts installation.

An HTTPS proxy on the Sentry server is not enough for browser OAuth.

### Source-editable setup

Add configuration for:

- `WebClient(base_url=...)`;
- the OAuth authorize URL; and
- the OAuth access-token URL.

This is the recommended route for most self-hosted deployments.

Sentry also needs methods, blocks, actions, commands, response URLs, link
unfurls, files, and user lookup. Full fixture support is planned for Phase 5,
not Phase 1.

## Manifest import experience

The planned Chatto flow is:

1. Open Server Admin, then **Apps**.
2. Select **Create app from manifest**.
3. Paste JSON or YAML.
4. Review supported, partial, and unsupported features.
5. Review every external origin and the data it can receive.
6. Select an incoming webhook room when requested.
7. Confirm installation.
8. Copy the one-time credential block.

Unknown manifest keys remain in the stored document and export. Chatto does not
silently apply a key that it does not support.

## Privacy review

Before installation, identify each external system that the app contacts.
Review at least:

- Request URLs;
- command and interaction URLs;
- options load URLs;
- OAuth redirect origins;
- image and media hosts;
- unfurl targets; and
- the Level 3 gateway.

Depending on scopes and subscriptions, these systems can receive user IDs,
room IDs, message text, file metadata, interaction input, and app install
identity. OAuth redirects also reveal the user's IP to the redirect host.
Remote media fetched through Chatto reveals the Chatto server IP and requested
URL to the media host.

Use the smallest scope set. Revoke an app when it no longer needs access.

## Diagnostics

The planned console will show:

- manifest revision;
- install and token generation;
- Request URL verification;
- recent safe delivery status and retry category;
- auto-pause state;
- last successful delivery time; and
- support state from the coverage matrix.

It will not show raw tokens, signing secrets, payloads, user email addresses,
IP addresses, or complete destination query strings.

## Legal note

The registry and design use public documentation and published SDK source. They
do not use recorded live API behavior. Counsel must review API terms, naming,
and compatibility marketing before a commercial release.
