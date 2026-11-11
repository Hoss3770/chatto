import { AdminAppService } from '@chatto/api-types/admin/v1/apps_connect';
import {
  AppLifecycleState,
  ManifestIssueSeverity,
  type App as APIApp,
  type AppCredentials as APIAppCredentials,
  type ManifestIssue as APIManifestIssue,
  type ManifestSummary as APIManifestSummary
} from '@chatto/api-types/admin/v1/apps_pb';
import { createChattoClient, type ConnectAPIConfig } from '@chatto/client/api/connect';

export type AppLifecycle = 'not-installed' | 'installed' | 'uninstalled' | 'unknown';

export type AppManifestIssue = {
  pointer: string;
  line: number;
  code: string;
  message: string;
  severity: 'error' | 'warning';
};

export type AppManifestSummary = {
  name: string;
  description: string;
  botScopes: string[];
  userScopes: string[];
  events: string[];
  commands: string[];
  externalOrigins: string[];
  implementedContracts: number;
  partialContracts: number;
  unsupportedContracts: number;
};

export type AppManifestReview = {
  manifestJson: string;
  summary: AppManifestSummary;
  issues: AppManifestIssue[];
  valid: boolean;
};

export type ManagedApp = {
  id: string;
  ownerUserId: string;
  botUserId: string;
  name: string;
  description: string;
  lifecycle: AppLifecycle;
  manifestRevision: bigint;
  installGeneration: bigint;
  requestedBotScopes: string[];
  requestedUserScopes: string[];
  grantedBotScopes: string[];
  grantedUserScopes: string[];
  externalOrigins: string[];
  createdAt: string;
  updatedAt: string;
  manifestJson: string;
};

export type AppCredentials = {
  clientId: string;
  clientSecret: string;
  signingSecret: string;
  botToken: string;
  appToken: string;
  apiUrl: string;
  authorizeUrl: string;
};

export type AppMutationResult = { app: ManagedApp; credentials: AppCredentials };

/** Creates the Server Admin app-management API for one server connection. */
export function createAppAPI(config: ConnectAPIConfig) {
  const client = createChattoClient(AdminAppService, config);
  return {
    async platform(options: { signal?: AbortSignal } = {}) {
      const response = await client.getAppPlatform({}, { signal: options.signal });
      return {
        enabled: response.enabled,
        apiUrl: response.apiUrl,
        authorizeUrl: response.authorizeUrl
      };
    },
    async validateManifest(manifest: string) {
      return mapManifestReview(await client.validateAppManifest({ manifest }));
    },
    async list(options: { signal?: AbortSignal } = {}) {
      const apps: ManagedApp[] = [];
      let offset = 0;
      while (true) {
        const response = await client.listApps(
          { page: { offset, limit: 100 } },
          { signal: options.signal }
        );
        apps.push(...response.apps.map(mapApp));
        if (!response.page?.hasMore || response.apps.length === 0) return apps;
        offset += response.apps.length;
      }
    },
    async get(appId: string, options: { signal?: AbortSignal } = {}) {
      const response = await client.getApp({ appId }, { signal: options.signal });
      if (!response.app) throw new Error('App response was incomplete.');
      return mapApp(response.app);
    },
    async create(manifest: string) {
      const response = await client.createApp({ manifest });
      return mapMutationResult(response.app, response.credentials);
    },
    async updateManifest(appId: string, manifest: string) {
      const response = await client.replaceAppManifest({ appId, manifest });
      if (!response.app) throw new Error('App response was incomplete.');
      return mapApp(response.app);
    },
    async exportManifest(appId: string) {
      return (await client.exportAppManifest({ appId })).manifestJson;
    },
    async install(appId: string) {
      const response = await client.installApp({ appId });
      return mapMutationResult(response.app, response.credentials);
    },
    async uninstall(appId: string) {
      const response = await client.uninstallApp({ appId });
      if (!response.app) throw new Error('App response was incomplete.');
      return mapApp(response.app);
    },
    async regenerateCredentials(appId: string) {
      const response = await client.regenerateAppCredentials({ appId });
      return mapMutationResult(response.app, response.credentials);
    },
    async delete(appId: string) {
      await client.deleteApp({ appId });
    }
  };
}

function mapMutationResult(
  app: APIApp | undefined,
  credentials: APIAppCredentials | undefined
): AppMutationResult {
  if (!app || !credentials) throw new Error('App response was incomplete.');
  return { app: mapApp(app), credentials: mapCredentials(credentials) };
}

function mapApp(app: APIApp): ManagedApp {
  return {
    id: app.appId,
    ownerUserId: app.ownerUserId,
    botUserId: app.botUserId,
    name: app.name,
    description: app.description,
    lifecycle: lifecycleName(app.state),
    manifestRevision: app.manifestRevision,
    installGeneration: app.installGeneration,
    requestedBotScopes: [...app.requestedBotScopes],
    requestedUserScopes: [...app.requestedUserScopes],
    grantedBotScopes: [...app.grantedBotScopes],
    grantedUserScopes: [...app.grantedUserScopes],
    externalOrigins: [...app.externalOrigins],
    createdAt: app.createdAt?.toDate().toISOString() ?? '',
    updatedAt: app.updatedAt?.toDate().toISOString() ?? '',
    manifestJson: app.manifestJson
  };
}

function lifecycleName(state: AppLifecycleState): AppLifecycle {
  if (state === AppLifecycleState.NOT_INSTALLED) return 'not-installed';
  if (state === AppLifecycleState.INSTALLED) return 'installed';
  if (state === AppLifecycleState.UNINSTALLED) return 'uninstalled';
  return 'unknown';
}

function mapCredentials(credentials: APIAppCredentials): AppCredentials {
  return {
    clientId: credentials.clientId,
    clientSecret: credentials.clientSecret,
    signingSecret: credentials.signingSecret,
    botToken: credentials.botToken,
    appToken: credentials.appToken,
    apiUrl: credentials.apiUrl,
    authorizeUrl: credentials.authorizeUrl
  };
}

function mapManifestReview(review: {
  manifestJson: string;
  summary?: APIManifestSummary;
  issues: APIManifestIssue[];
  valid: boolean;
}): AppManifestReview {
  return {
    manifestJson: review.manifestJson,
    summary: mapManifestSummary(review.summary),
    issues: review.issues.map((issue) => ({
      pointer: issue.pointer,
      line: issue.line,
      code: issue.code,
      message: issue.message,
      severity: issue.severity === ManifestIssueSeverity.ERROR ? 'error' : 'warning'
    })),
    valid: review.valid
  };
}

function mapManifestSummary(summary: APIManifestSummary | undefined): AppManifestSummary {
  return {
    name: summary?.name ?? '',
    description: summary?.description ?? '',
    botScopes: [...(summary?.botScopes ?? [])],
    userScopes: [...(summary?.userScopes ?? [])],
    events: [...(summary?.events ?? [])],
    commands: [...(summary?.commands ?? [])],
    externalOrigins: [...(summary?.externalOrigins ?? [])],
    implementedContracts: summary?.implementedContracts ?? 0,
    partialContracts: summary?.partialContracts ?? 0,
    unsupportedContracts: summary?.unsupportedContracts ?? 0
  };
}
