<!--
@component

Server Admin app management. The editor validates complete JSON or YAML
manifests before create or update. Raw credentials stay only in the show-once
dialog returned by the mutation that created them.
-->
<script lang="ts">
  import { errorMessage, toastError } from '$lib/utils/errorMessage';
  import {
    createAppAPI,
    type AppCredentials,
    type AppManifestReview,
    type ManagedApp
  } from '$lib/api/apps';
  import { adminQueryKeys } from '$lib/query/admin';
  import { createMutation, createQuery, queryClient } from '$lib/query/client';
  import { useServerScope } from '$lib/state/server/scope.svelte';
  import { m } from '$lib/i18n/messages';
  import {
    ConfirmDialog,
    CopyButton,
    Dialog,
    EmptyState,
    Hint,
    LoadingFog,
    PaneContent,
    PageTitle,
    PaneHeader,
    Panel,
    Pill
  } from '$lib/ui';
  import { Button, TextArea } from '$lib/ui/form';

  const serverScope = useServerScope();
  let manifestDraft = $state('');
  let reviewedSource = $state('');
  let review = $state.raw<AppManifestReview | null>(null);
  let editingApp = $state.raw<ManagedApp | null>(null);
  let credentials = $state.raw<AppCredentials | null>(null);
  let confirmation = $state.raw<{
    kind: 'uninstall' | 'regenerate' | 'delete';
    app: ManagedApp;
  } | null>(null);

  const platformQuery = createQuery(() => ({
    queryKey: adminQueryKeys.appPlatform(serverScope.serverId, serverScope.connection),
    queryFn: ({ signal }) => serverScope.connection.getAPI(createAppAPI).platform({ signal })
  }));
  const appsQuery = createQuery(() => ({
    queryKey: adminQueryKeys.apps(serverScope.serverId, serverScope.connection),
    queryFn: ({ signal }) => serverScope.connection.getAPI(createAppAPI).list({ signal })
  }));
  const apps = $derived(appsQuery.data ?? []);
  const activeReview = $derived(reviewedSource === manifestDraft ? review : null);
  const platformEnabled = $derived(platformQuery.data?.enabled === true);

  const reviewMutation = createMutation(() => ({
    mutationFn: ({ manifest }: { manifest: string }) =>
      serverScope.connection.getAPI(createAppAPI).validateManifest(manifest),
    onSuccess: (result, variables) => {
      if (!serverScope.isCurrent()) return;
      reviewedSource = variables.manifest;
      review = result;
    },
    onError: (error) => {
      if (serverScope.isCurrent()) toastError(error);
    }
  }));

  const loadManifestMutation = createMutation(() => ({
    mutationFn: ({ appId }: { appId: string }) =>
      serverScope.connection.getAPI(createAppAPI).get(appId),
    onSuccess: (app) => {
      if (!serverScope.isCurrent()) return;
      editingApp = app;
      manifestDraft = app.manifestJson;
      reviewedSource = '';
      review = null;
      document.getElementById('app-manifest')?.scrollIntoView({ behavior: 'smooth' });
    },
    onError: (error) => {
      if (serverScope.isCurrent()) toastError(error);
    }
  }));

  const createMutationState = createMutation(() => ({
    mutationFn: ({ manifest }: { manifest: string }) =>
      serverScope.connection.getAPI(createAppAPI).create(manifest),
    onSuccess: (result) => {
      createMutationState.reset();
      if (!serverScope.isCurrent()) return;
      credentials = result.credentials;
      resetEditor();
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  const updateMutationState = createMutation(() => ({
    mutationFn: ({ appId, manifest }: { appId: string; manifest: string }) =>
      serverScope.connection.getAPI(createAppAPI).updateManifest(appId, manifest),
    onSuccess: () => {
      if (!serverScope.isCurrent()) return;
      resetEditor();
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  const installMutation = createMutation(() => ({
    mutationFn: ({ appId }: { appId: string }) =>
      serverScope.connection.getAPI(createAppAPI).install(appId),
    onSuccess: (result) => {
      installMutation.reset();
      if (!serverScope.isCurrent()) return;
      credentials = result.credentials;
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  const uninstallMutation = createMutation(() => ({
    mutationFn: ({ appId }: { appId: string }) =>
      serverScope.connection.getAPI(createAppAPI).uninstall(appId),
    onSuccess: () => {
      if (!serverScope.isCurrent()) return;
      confirmation = null;
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  const regenerateMutation = createMutation(() => ({
    mutationFn: ({ appId }: { appId: string }) =>
      serverScope.connection.getAPI(createAppAPI).regenerateCredentials(appId),
    onSuccess: (result) => {
      regenerateMutation.reset();
      if (!serverScope.isCurrent()) return;
      confirmation = null;
      credentials = result.credentials;
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  const deleteMutation = createMutation(() => ({
    mutationFn: ({ appId }: { appId: string }) =>
      serverScope.connection.getAPI(createAppAPI).delete(appId),
    onSuccess: () => {
      if (!serverScope.isCurrent()) return;
      confirmation = null;
      void refreshApps();
    },
    onError: (error) => {
      if (!serverScope.isCurrent()) return;
      toastError(error);
      void refreshApps();
    }
  }));

  function refreshApps() {
    return queryClient.invalidateQueries({
      queryKey: adminQueryKeys.apps(serverScope.serverId, serverScope.connection)
    });
  }

  function reviewManifest() {
    reviewMutation.mutate({ manifest: manifestDraft });
  }

  function submitManifest() {
    if (!activeReview?.valid || !platformEnabled) return;
    if (editingApp) {
      updateMutationState.mutate({ appId: editingApp.id, manifest: activeReview.manifestJson });
    } else {
      createMutationState.mutate({ manifest: activeReview.manifestJson });
    }
  }

  function editManifest(app: ManagedApp) {
    loadManifestMutation.mutate({ appId: app.id });
  }

  function resetEditor() {
    editingApp = null;
    manifestDraft = '';
    reviewedSource = '';
    review = null;
  }

  function exportManifest(app: ManagedApp) {
    void serverScope.connection
      .getAPI(createAppAPI)
      .exportManifest(app.id)
      .then((manifest) => {
        if (!serverScope.isCurrent()) return;
        const link = document.createElement('a');
        link.href = URL.createObjectURL(new Blob([manifest], { type: 'application/json' }));
        link.download = `${app.name.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || app.id}.json`;
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(link.href), 1_000);
      })
      .catch((error) => {
        if (serverScope.isCurrent()) toastError(error);
      });
  }

  function confirmAction() {
    if (!confirmation) return;
    const variables = { appId: confirmation.app.id };
    if (confirmation.kind === 'uninstall') uninstallMutation.mutate(variables);
    if (confirmation.kind === 'regenerate') regenerateMutation.mutate(variables);
    if (confirmation.kind === 'delete') deleteMutation.mutate(variables);
  }

  function confirmationPending() {
    return uninstallMutation.isPending || regenerateMutation.isPending || deleteMutation.isPending;
  }

  function lifecycleLabel(app: ManagedApp) {
    if (app.lifecycle === 'installed') return m('admin.apps.state_installed');
    if (app.lifecycle === 'uninstalled') return m('admin.apps.state_uninstalled');
    if (app.lifecycle === 'not-installed') return m('admin.apps.state_not_installed');
    return m('admin.common.unknown');
  }

  function lifecycleTone(app: ManagedApp): 'success' | 'neutral' | 'muted' {
    if (app.lifecycle === 'installed') return 'success';
    if (app.lifecycle === 'uninstalled') return 'neutral';
    return 'muted';
  }

  function credentialEntries(value: AppCredentials) {
    return [
      ['client_id', value.clientId],
      ['client_secret', value.clientSecret],
      ['signing_secret', value.signingSecret],
      ['bot_token', value.botToken],
      ['app_token', value.appToken],
      ['api_url', value.apiUrl],
      ['authorize_url', value.authorizeUrl]
    ].filter((entry): entry is [string, string] => entry[1] !== '');
  }

  function confirmationLabel(kind: 'uninstall' | 'regenerate' | 'delete') {
    if (kind === 'uninstall') return m('admin.apps.uninstall');
    if (kind === 'regenerate') return m('admin.apps.regenerate');
    return m('admin.neighbors.delete');
  }
</script>

<PageTitle title={m('admin.common.server_admin_page_title', { title: m('admin.apps.title') })} />

<div class="pane-page">
  <PaneHeader title={m('admin.apps.title')} subtitle={m('admin.apps.subtitle')} />

  <PaneContent>
    <div class="flex flex-col gap-6">
      {#if platformQuery.error}
        <Hint tone="danger">{errorMessage(platformQuery.error)}</Hint>
      {:else if platformQuery.data && !platformEnabled}
        <Hint tone="warning">{m('admin.apps.disabled')}</Hint>
      {/if}

      {#if platformQuery.data}
        <Panel title={m('admin.apps.platform')} icon="iconify icon-[uil--link]">
          <div class="grid gap-3 md:grid-cols-2">
            <div class="min-w-0 surface-box p-3">
              <div class="text-muted"><code dir="ltr">api_url</code></div>
              <code class="block truncate" dir="ltr">{platformQuery.data.apiUrl || '—'}</code>
            </div>
            <div class="min-w-0 surface-box p-3">
              <div class="text-muted"><code dir="ltr">authorize_url</code></div>
              <code class="block truncate" dir="ltr">{platformQuery.data.authorizeUrl || '—'}</code>
            </div>
          </div>
        </Panel>
      {/if}

      <Panel
        title={editingApp ? m('admin.apps.update_manifest') : m('admin.apps.manifest')}
        icon="iconify icon-[uil--file-edit-alt]"
      >
        <form
          class="flex flex-col gap-4"
          onsubmit={(event) => {
            event.preventDefault();
            submitManifest();
          }}
        >
          <TextArea
            id="app-manifest"
            label={m('admin.apps.manifest')}
            description={m('admin.apps.manifest_help')}
            rows={14}
            maxBytes={1 << 20}
            bind:value={manifestDraft}
            disabled={!platformEnabled ||
              createMutationState.isPending ||
              updateMutationState.isPending}
          />

          {#if activeReview}
            <div class="flex flex-col gap-4 surface-box p-4">
              <div class="flex flex-wrap items-center gap-2">
                <strong>{activeReview.summary.name || m('admin.apps.manifest')}</strong>
                <Pill tone="success"
                  >{m('admin.apps.implemented')}: {activeReview.summary.implementedContracts}</Pill
                >
                <Pill tone="neutral"
                  >{m('admin.apps.partial')}: {activeReview.summary.partialContracts}</Pill
                >
                <Pill tone="muted"
                  >{m('admin.apps.unsupported')}: {activeReview.summary.unsupportedContracts}</Pill
                >
              </div>
              {#if activeReview.summary.description}<p>{activeReview.summary.description}</p>{/if}
              {#if activeReview.summary.botScopes.length > 0}
                <div class="flex flex-col gap-2">
                  <code dir="ltr">bot_scopes</code>
                  <div class="flex flex-wrap gap-2">
                    {#each activeReview.summary.botScopes as scope (scope)}<Pill>{scope}</Pill
                      >{/each}
                  </div>
                </div>
              {/if}
              {#if activeReview.summary.userScopes.length > 0}
                <div class="flex flex-col gap-2">
                  <code dir="ltr">user_scopes</code>
                  <div class="flex flex-wrap gap-2">
                    {#each activeReview.summary.userScopes as scope (scope)}<Pill>{scope}</Pill
                      >{/each}
                  </div>
                </div>
              {/if}
              {#if activeReview.summary.externalOrigins.length > 0}
                <div class="flex flex-col gap-2">
                  <code dir="ltr">external_origins</code>
                  {#each activeReview.summary.externalOrigins as origin (origin)}
                    <code class="overflow-x-auto whitespace-nowrap" dir="ltr">{origin}</code>
                  {/each}
                </div>
              {/if}
              {#if activeReview.summary.events.length > 0}
                <div class="flex flex-col gap-2">
                  <code dir="ltr">events</code>
                  <div class="flex flex-wrap gap-2">
                    {#each activeReview.summary.events as event (event)}<Pill>{event}</Pill>{/each}
                  </div>
                </div>
              {/if}
              {#if activeReview.summary.commands.length > 0}
                <div class="flex flex-col gap-2">
                  <code dir="ltr">commands</code>
                  <div class="flex flex-wrap gap-2">
                    {#each activeReview.summary.commands as command (command)}<Pill>{command}</Pill
                      >{/each}
                  </div>
                </div>
              {/if}
              {#if activeReview.issues.length > 0}
                <div class="max-h-64 overflow-auto">
                  {#each activeReview.issues as issue (`${issue.pointer}:${issue.code}`)}
                    <div class="border-b border-text/10 py-2 last:border-0">
                      <div class={issue.severity === 'error' ? 'text-danger' : 'text-muted'}>
                        <code dir="ltr">{issue.pointer || '/'}</code>
                        {#if issue.line > 0}
                          · {issue.line}{/if}
                      </div>
                      <div>{issue.message}</div>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}

          <div class="flex flex-wrap justify-end gap-2">
            {#if editingApp}
              <Button type="button" variant="secondary" onclick={resetEditor}>
                {m('admin.neighbors.cancel')}
              </Button>
            {/if}
            <Button
              type="button"
              variant="secondary"
              onclick={reviewManifest}
              loading={reviewMutation.isPending}
              disabled={!platformEnabled || manifestDraft.trim() === ''}
            >
              {m('admin.apps.review')}
            </Button>
            <Button
              type="submit"
              loading={createMutationState.isPending || updateMutationState.isPending}
              disabled={!platformEnabled || !activeReview?.valid}
            >
              <span aria-hidden="true" class="iconify icon-[uil--check]"></span>
              {editingApp ? m('admin.apps.update') : m('admin.apps.create_install')}
            </Button>
          </div>
        </form>
      </Panel>

      <Panel title={m('admin.apps.list')} count={apps.length || undefined} noPadding>
        {#if appsQuery.error}
          <div class="p-4"><Hint tone="danger">{errorMessage(appsQuery.error)}</Hint></div>
        {:else if appsQuery.isPending && apps.length === 0}
          <LoadingFog class="h-40 w-full" />
        {:else if apps.length === 0}
          <EmptyState icon="icon-[uil--puzzle-piece]" title={m('admin.apps.empty')} />
        {:else}
          <div class="selectable-list">
            {#each apps as app (app.id)}
              <article class="flex flex-col gap-3 selectable-list-item p-4">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <strong><bdi>{app.name}</bdi></strong>
                      <Pill tone={lifecycleTone(app)}>{lifecycleLabel(app)}</Pill>
                    </div>
                    {#if app.description}<p class="mt-1 text-muted">
                        <bdi>{app.description}</bdi>
                      </p>{/if}
                    <code class="mt-1 block text-muted" dir="ltr">{app.id}</code>
                  </div>
                  <div class="flex flex-wrap justify-end gap-2">
                    <Button
                      size="sm"
                      variant="secondary"
                      onclick={() => editManifest(app)}
                      loading={loadManifestMutation.isPending &&
                        loadManifestMutation.variables?.appId === app.id}
                      disabled={loadManifestMutation.isPending}
                    >
                      {m('admin.neighbors.edit')}
                    </Button>
                    <Button size="sm" variant="secondary" onclick={() => exportManifest(app)}>
                      {m('admin.apps.export')}
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      onclick={() => (confirmation = { kind: 'regenerate', app })}
                      disabled={!platformEnabled}
                    >
                      {m('admin.apps.regenerate')}
                    </Button>
                    {#if app.lifecycle === 'installed'}
                      <Button
                        size="sm"
                        variant="danger"
                        onclick={() => (confirmation = { kind: 'uninstall', app })}
                        disabled={!platformEnabled}
                      >
                        {m('admin.apps.uninstall')}
                      </Button>
                    {:else}
                      <Button
                        size="sm"
                        onclick={() => installMutation.mutate({ appId: app.id })}
                        loading={installMutation.isPending &&
                          installMutation.variables?.appId === app.id}
                        disabled={!platformEnabled || installMutation.isPending}
                      >
                        {m('admin.apps.install')}
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        onclick={() => (confirmation = { kind: 'delete', app })}
                        disabled={!platformEnabled}
                      >
                        {m('admin.neighbors.delete')}
                      </Button>
                    {/if}
                  </div>
                </div>
                {#if app.requestedBotScopes.length > 0}
                  <div class="flex flex-col gap-2">
                    <code dir="ltr">requested_bot_scopes</code>
                    <div class="flex flex-wrap gap-2">
                      {#each app.requestedBotScopes as scope (scope)}<Pill>{scope}</Pill>{/each}
                    </div>
                  </div>
                {/if}
                {#if app.grantedBotScopes.length > 0}
                  <div class="flex flex-col gap-2">
                    <code dir="ltr">granted_bot_scopes</code>
                    <div class="flex flex-wrap gap-2">
                      {#each app.grantedBotScopes as scope (scope)}<Pill>{scope}</Pill>{/each}
                    </div>
                  </div>
                {/if}
                {#if app.externalOrigins.length > 0}
                  <div class="flex flex-col gap-2">
                    <code dir="ltr">external_origins</code>
                    {#each app.externalOrigins as origin (origin)}
                      <code class="overflow-x-auto whitespace-nowrap" dir="ltr">{origin}</code>
                    {/each}
                  </div>
                {/if}
              </article>
            {/each}
          </div>
        {/if}
      </Panel>
    </div>
  </PaneContent>
</div>

{#if credentials}
  {#snippet dismissCredentials()}
    <Button variant="secondary" onclick={() => (credentials = null)}>{m('admin.apps.close')}</Button
    >
  {/snippet}
  <Dialog
    visible
    title={m('admin.apps.credentials')}
    size="lg"
    dismissAction={dismissCredentials}
    oncloserequest={() => (credentials = null)}
  >
    <div class="flex flex-col gap-4">
      <Hint tone="warning">{m('admin.apps.credentials_warning')}</Hint>
      {#each credentialEntries(credentials) as [label, value] (label)}
        <div class="flex min-w-0 items-center gap-3 surface-box p-3">
          <div class="min-w-0 flex-1">
            <code class="block text-muted" dir="ltr">{label}</code>
            <code class="block overflow-x-auto whitespace-nowrap" dir="ltr">{value}</code>
          </div>
          <CopyButton {value} />
        </div>
      {/each}
    </div>
  </Dialog>
{/if}

{#if confirmation}
  <ConfirmDialog
    title={confirmationLabel(confirmation.kind)}
    actionLabel={confirmationLabel(confirmation.kind)}
    loading={confirmationPending()}
    onconfirm={confirmAction}
    onclose={() => (confirmation = null)}
  >
    <bdi>{confirmation.app.name}</bdi>
  </ConfirmDialog>
{/if}
