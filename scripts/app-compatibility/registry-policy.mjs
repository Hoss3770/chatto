// SPDX-License-Identifier: AGPL-3.0-or-later

/**
 * This module adds Chatto-specific semantic mappings to the generated external
 * contract inventory. It does not claim that a mapped contract is implemented.
 */

const methodMappings = new Map([
  ['api.test', ['Compatibility gateway health and argument echo', 1]],
  ['auth.test', ['Resolve the installed app credential and its Chatto principal', 1]],
  ['auth.revoke', ['Revoke one app installation token', 1]],
  ['apps.connections.open', ['Create a short-lived Socket Mode URL', 3]],
  [
    'apps.event.authorizations.list',
    ['List the installations authorized for one delivered event', 3]
  ],
  ['apps.manifest.create', ['Create an app definition from a manifest', 1]],
  ['apps.manifest.delete', ['Delete an app definition after install checks', 1]],
  ['apps.manifest.export', ['Export the preserved app manifest', 1]],
  ['apps.manifest.update', ['Validate and update an app definition', 1]],
  ['apps.manifest.validate', ['Validate a manifest without changing state', 1]],
  ['apps.uninstall', ['Revoke an app installation and its credentials', 1]],
  ['oauth.v2.access', ['Exchange an app authorization code for installation credentials', 1]],
  ['oauth.v2.exchange', ['Convert an installation to rotating credentials', 5]],
  ['oauth.v2.user.access', ['Exchange a user-only authorization code', 5]],
  ['openid.connect.token', ['Exchange an OpenID Connect authorization code', 5]],
  ['openid.connect.userInfo', ['Return claims for a Chatto human account', 5]],
  ['chat.appendStream', ['Append content to an agent streaming message', 6]],
  ['chat.delete', ['MessageService.DeleteMessage', 2]],
  ['chat.deleteScheduledMessage', ['Cancel a scheduled app message', 5]],
  ['chat.getPermalink', ['Build a URL from webserver.url and a message alias', 2]],
  ['chat.meMessage', ['MessageService.CreateMessage with me-message presentation', 2]],
  ['chat.postEphemeral', ['Create app-owned ephemeral content', 5]],
  ['chat.postMessage', ['MessageService.CreateMessage', 2]],
  ['chat.scheduleMessage', ['Create a scheduled app message', 5]],
  ['chat.scheduledMessages.list', ['List scheduled app messages', 5]],
  ['chat.startStream', ['Start an agent streaming message', 6]],
  ['chat.stopStream', ['Finish an agent streaming message', 6]],
  ['chat.unfurl', ['Submit app-owned link previews', 5]],
  ['chat.update', ['MessageService.UpdateMessage', 2]],
  ['conversations.archive', ['RoomService.ArchiveRoom', 2]],
  ['conversations.create', ['RoomService.CreateRoom', 2]],
  ['conversations.history', ['RoomService.GetRoomEvents', 2]],
  ['conversations.info', ['RoomDirectoryService.GetRoom', 2]],
  ['conversations.invite', ['RoomService.AddMember', 2]],
  ['conversations.join', ['RoomService.JoinRoom', 2]],
  ['conversations.kick', ['RoomService.RemoveMember', 2]],
  ['conversations.leave', ['RoomService.LeaveRoom', 2]],
  ['conversations.list', ['RoomDirectoryService.ListRooms', 2]],
  ['conversations.mark', ['RoomService.MarkRoomAsRead', 2]],
  ['conversations.members', ['RoomService.ListMembers', 2]],
  [
    'conversations.open',
    ['RoomService.StartDM for human grants; bots can use existing DMs only', 2]
  ],
  ['conversations.rename', ['RoomService.UpdateRoom', 2]],
  ['conversations.replies', ['RoomService.GetRoomEvents around one thread root', 2]],
  ['conversations.setPurpose', ['RoomService.UpdateRoom purpose mapping', 2]],
  ['conversations.setTopic', ['RoomService.UpdateRoom description mapping', 2]],
  ['conversations.unarchive', ['RoomService.UnarchiveRoom', 2]],
  ['users.info', ['UserService.GetUser', 2]],
  ['users.list', ['UserService.ListUsers', 2]],
  [
    'users.lookupByEmail',
    ['Restricted account lookup; do not expose email through public profiles', 2]
  ],
  ['users.conversations', ['RoomDirectoryService.ListRooms for one visible user', 2]],
  ['users.deletePhoto', ['Remove the authenticated user avatar', 5]],
  ['users.getPresence', ['Read current user presence', 5]],
  ['users.profile.get', ['UserService.GetUser', 2]],
  ['users.profile.set', ['UserService.UpdateUserProfile for the authenticated human or bot', 2]],
  ['users.setPhoto', ['Update the authenticated user avatar', 5]],
  ['users.setPresence', ['Update the authenticated user presence', 5]],
  ['bots.info', ['UserService.GetUser with bot metadata', 2]],
  ['files.delete', ['MessageService.DeleteAttachment', 5]],
  ['files.info', ['Read one Chatto attachment and its signed locator', 5]],
  ['files.list', ['RoomService.ListRoomAttachments', 5]],
  ['files.uploadV2', ['AssetUploadService plus MessageService.CreateMessage', 5]],
  ['pins.add', ['RoomService.CreatePinnedMessage', 5]],
  ['pins.list', ['RoomService.ListPinnedMessages', 5]],
  ['pins.remove', ['RoomService.DeletePinnedMessage', 5]],
  ['reactions.add', ['MessageService.AddReaction', 5]],
  ['reactions.get', ['MessageService.GetMessage and reaction users', 5]],
  ['reactions.list', ['Message reads plus reaction projections', 5]],
  ['reactions.remove', ['MessageService.RemoveReaction', 5]],
  ['search.messages', ['MessageSearchService.SearchMessages', 5]],
  ['team.info', ['ServerService.GetServerState and public server profile', 2]]
]);

const unsupportedFamilies = [
  [
    'admin.',
    'The source contract assumes organization and enterprise administration that Chatto does not have.'
  ],
  ['auditlogs.', 'Chatto does not expose the source platform audit-log service contract.'],
  ['calls.', 'The source call-registration model is not equivalent to Chatto voice calls.'],
  ['canvases.', 'Chatto has no canvas resource.'],
  ['datastore.', 'Chatto does not host app datastores.'],
  ['legal_holds.', 'Chatto has no legal-holds API.'],
  ['lists.', 'Chatto has no list resource.'],
  ['migration.', 'Workspace migration is not a Chatto app operation.'],
  ['rtm.', 'Chatto will use Request URLs and Socket Mode, not the legacy RTM protocol.'],
  ['scim.', 'Chatto has no compatible SCIM service.']
];

const methodFamilyPlans = [
  ['apps.manifest.', 1, 'Manifest service'],
  ['apps.connections.', 3, 'Socket hub'],
  ['apps.', 5, 'App lifecycle adapter'],
  ['openid.', 5, 'OpenID Connect service'],
  ['blocks.', 4, 'Block validator'],
  ['views.', 4, 'Interactive view service'],
  ['dialog.', 8, 'Legacy interaction shim'],
  ['files.', 5, 'Files adapter'],
  ['reactions.', 5, 'Reaction adapter'],
  ['pins.', 5, 'Pinned-message adapter'],
  ['bookmarks.', 5, 'No Chatto bookmark resource; requires product design'],
  ['reminders.', 5, 'No Chatto reminder resource; requires product design'],
  ['dnd.', 5, 'Presence and notification preference adapter'],
  ['emoji.', 5, 'No custom emoji resource; requires product design'],
  ['search.', 5, 'Message search adapter'],
  ['team.', 5, 'Server profile adapter'],
  ['usergroups.', 5, 'No user-group resource; requires product design'],
  ['assistant.', 6, 'Agent surface adapter'],
  ['agents.', 6, 'Agent surface adapter'],
  ['functions.', 7, 'Remote workflow function adapter'],
  ['workflows.', 7, 'Remote workflow adapter']
];

/** Return the planned Chatto mapping for one Web API method. */
export function methodPolicy(name, { documented, deprecated }) {
  const mapped = methodMappings.get(name);
  if (mapped) {
    return {
      status: 'unsupported',
      reason: 'The target Web API route does not exist yet.',
      target_phase: mapped[1],
      semantic_mapping: mapped[0]
    };
  }

  for (const [prefix, reason] of unsupportedFamilies) {
    if (name.startsWith(prefix)) {
      return {
        status: 'unsupported',
        reason,
        target_phase: null,
        semantic_mapping: null
      };
    }
  }

  for (const [prefix, phase, mapping] of methodFamilyPlans) {
    if (name.startsWith(prefix)) {
      return {
        status: 'unsupported',
        reason: 'The target Web API route does not exist yet.',
        target_phase: phase,
        semantic_mapping: mapping
      };
    }
  }

  if (!documented && deprecated) {
    return {
      status: 'unsupported',
      reason: 'This SDK-only legacy method is deprecated and has no Chatto contract.',
      target_phase: 8,
      semantic_mapping: 'Legacy compatibility decision per method'
    };
  }

  return {
    status: 'unsupported',
    reason: documented
      ? 'No safe Chatto semantic mapping has been approved for this method.'
      : 'The method appears in an SDK but not in the current documentation and has no approved mapping.',
    target_phase: 8,
    semantic_mapping: 'Review during platform breadth or legacy work'
  };
}

const eventMappings = new Map([
  ['app_mention', 'MessagePostedEvent with a direct bot mention'],
  ['channel_archive', 'RoomArchivedEvent'],
  ['channel_created', 'RoomCreatedEvent'],
  ['channel_deleted', 'RoomDeletedEvent'],
  ['channel_left', 'UserLeftRoomEvent for the installed principal'],
  ['channel_rename', 'RoomUpdatedEvent'],
  ['channel_unarchive', 'RoomUnarchivedEvent'],
  ['member_joined_channel', 'UserJoinedRoomEvent'],
  ['member_left_channel', 'UserLeftRoomEvent'],
  ['message', 'MessagePostedEvent, MessageEditedEvent, and MessageRetractedEvent'],
  ['message.bot_message', 'MessagePostedEvent by a managed bot or app'],
  ['message.file_share', 'MessagePostedEvent with canonical Chatto attachments'],
  ['message.me_message', 'MessagePostedEvent with me-message presentation'],
  ['message.message_changed', 'MessageEditedEvent'],
  ['message.message_deleted', 'MessageRetractedEvent'],
  ['message.message_replied', 'MessagePostedEvent in a thread'],
  ['message.reply_broadcast', 'Thread reply with parent-room presentation'],
  ['message.thread_broadcast', 'Thread reply with parent-room presentation'],
  ['message.channels', 'Message events in channel rooms'],
  ['message.groups', 'Message events in private channel rooms'],
  ['message.im', 'Message events in direct-message rooms'],
  ['message.mpim', 'Message events in group direct-message rooms'],
  ['pin_added', 'MessagePinnedEvent'],
  ['pin_removed', 'MessageUnpinnedEvent'],
  ['presence_change', 'PresenceChangedEvent'],
  ['reaction_added', 'ReactionAddedEvent'],
  ['reaction_removed', 'ReactionRemovedEvent'],
  ['team_join', 'UserAccountCreatedEvent'],
  ['user_change', 'UserProfileChangedEvent or account state change'],
  ['user_typing', 'UserTypingEvent']
]);

/** Return the planned Chatto mapping for one event contract. */
export function eventPolicy(name) {
  const mapping = eventMappings.get(name);
  if (mapping) {
    return {
      status: 'unsupported',
      reason: 'No Events API or Socket Mode adapter emits this event yet.',
      target_phase: 3,
      semantic_mapping: mapping
    };
  }

  if (
    name.startsWith('agent_') ||
    name.startsWith('assistant_') ||
    name === 'message.assistant_app_thread'
  ) {
    return {
      status: 'unsupported',
      reason: 'No agent event adapter emits this contract yet.',
      target_phase: 6,
      semantic_mapping: 'Agent and assistant event adapter'
    };
  }
  if (name.startsWith('function_') || name.startsWith('workflow_')) {
    return {
      status: 'unsupported',
      reason: 'No remote workflow event adapter emits this contract yet.',
      target_phase: 7,
      semantic_mapping: 'Remote workflow event adapter'
    };
  }

  const unsupportedPrefixes = ['canvas_', 'ekm_', 'external_org_', 'grid_', 'subteam_'];
  if (unsupportedPrefixes.some((prefix) => name.startsWith(prefix))) {
    return {
      status: 'unsupported',
      reason: 'Chatto has no equivalent durable fact or product resource.',
      target_phase: null,
      semantic_mapping: null
    };
  }

  return {
    status: 'unsupported',
    reason: 'No authorized event mapping has been approved for this contract.',
    target_phase: 8,
    semantic_mapping: 'Review after the core Events API adapter exists'
  };
}

const scopePermissions = new Map([
  ['app_mentions:read', ['message.read-interactions']],
  ['assistant:write', []],
  ['channels:history', ['message.read']],
  ['channels:join', ['room.join']],
  ['channels:manage', ['room.create', 'room.manage']],
  ['channels:read', ['room.list']],
  ['chat:write', ['message.post']],
  ['chat:write.customize', ['message.post']],
  ['chat:write.public', ['message.post', 'room.join']],
  ['commands', []],
  ['connections:write', []],
  ['files:read', ['message.read']],
  ['files:write', ['message.attach', 'message.post']],
  ['groups:history', ['message.read']],
  ['groups:read', ['room.list']],
  ['groups:write', ['room.manage']],
  ['im:history', ['message.read']],
  ['im:read', ['message.read']],
  ['im:write', ['message.post']],
  ['links:read', ['message.read']],
  ['links:write', ['message.post']],
  ['mcp:connect', []],
  ['metadata.message:read', ['message.read']],
  ['mpim:history', ['message.read']],
  ['mpim:read', ['message.read']],
  ['mpim:write', ['message.post']],
  ['pins:read', ['message.read']],
  ['pins:write', ['message.pin']],
  ['reactions:read', ['message.read']],
  ['reactions:write', ['message.react']],
  ['search:read', ['message.read']],
  ['triggers:read', []],
  ['triggers:write', []],
  ['users:read', []],
  ['users:read.email', ['user.read-email']],
  ['users.profile:read', []],
  ['users.profile:write', []],
  ['workflows.templates:read', []],
  ['workflows.templates:write', []]
]);

const scopeTargetPhases = new Map([
  ['app_mentions:read', 3],
  ['assistant:write', 6],
  ['chat:write.customize', 5],
  ['commands', 3],
  ['connections:write', 3],
  ['mcp:connect', 6],
  ['metadata.message:read', 5],
  ['triggers:read', 7],
  ['triggers:write', 7],
  ['users:read.email', 2],
  ['workflows.templates:read', 7],
  ['workflows.templates:write', 7]
]);

/** Return the planned Chatto permission mapping for one OAuth scope. */
export function scopePolicy(name, compatibleMethods = []) {
  const permissions = scopePermissions.get(name);
  if (permissions) {
    return {
      status: 'unsupported',
      reason: 'The app OAuth scope grant model does not exist yet.',
      target_phase:
        scopeTargetPhases.get(name) ??
        Math.min(
          ...compatibleMethods.map(
            (method) =>
              methodPolicy(method, { documented: true, deprecated: false }).target_phase ?? 8
          ),
          8
        ),
      chatto_permissions: permissions,
      semantic_mapping:
        permissions.length === 0
          ? 'This scope has no direct Chatto RBAC equivalent; current resource authorization remains mandatory.'
          : 'The app scope permits a method; Chatto membership, policy, and effective RBAC remain mandatory.'
    };
  }

  return {
    status: 'unsupported',
    reason: 'No Chatto permission mapping has been approved for this scope.',
    target_phase: null,
    chatto_permissions: [],
    semantic_mapping: null
  };
}

const coreBlocks = new Set([
  'actions',
  'context',
  'divider',
  'header',
  'image',
  'input',
  'rich_text',
  'section'
]);
const coreElements = new Set([
  'button',
  'checkboxes',
  'datepicker',
  'datetimepicker',
  'image',
  'multi_channels_select',
  'multi_conversations_select',
  'multi_external_select',
  'multi_static_select',
  'multi_users_select',
  'overflow',
  'plain_text_input',
  'radio_buttons',
  'static_select',
  'timepicker',
  'users_select'
]);
const coreCompositionObjects = new Set([
  'confirmation_dialog',
  'conversation_filter',
  'dispatch_action_configuration',
  'option',
  'option_group',
  'text'
]);

/** Return coverage for a Block Kit object. */
export function blockKitPolicy(kind, wireType) {
  const planned =
    (kind === 'block' && coreBlocks.has(wireType)) ||
    (kind === 'element' && coreElements.has(wireType)) ||
    (kind === 'composition_object' && coreCompositionObjects.has(wireType));
  return {
    status: 'unsupported',
    reason: planned
      ? 'Chatto does not validate or render this structured object yet.'
      : 'This object has no approved Chatto surface or renderer.',
    target_phase: planned ? 4 : null,
    semantic_mapping: planned ? 'Chatto block validator and client renderers' : null
  };
}

/** Return coverage for one manifest field path. */
export function manifestPolicy(path) {
  const supportedPrefixes = [
    '_metadata',
    'display_information',
    'features.app_home',
    'features.bot_user',
    'features.shortcuts',
    'features.slash_commands',
    'features.unfurl_domains',
    'oauth_config',
    'settings.allowed_ip_address_ranges',
    'settings.event_subscriptions',
    'settings.incoming_webhooks',
    'settings.interactivity',
    'settings.socket_mode_enabled',
    'settings.token_rotation_enabled'
  ];
  const plannedPrefix = supportedPrefixes.find(
    (prefix) => path === prefix || path.startsWith(`${prefix}.`)
  );
  const phaseByPrefix = new Map([
    ['_metadata', 1],
    ['display_information', 1],
    ['features.bot_user', 1],
    ['oauth_config', 1],
    ['settings.incoming_webhooks', 2],
    ['features.slash_commands', 3],
    ['settings.allowed_ip_address_ranges', 3],
    ['settings.event_subscriptions', 3],
    ['settings.socket_mode_enabled', 3],
    ['features.app_home', 4],
    ['features.shortcuts', 4],
    ['settings.interactivity', 4],
    ['features.unfurl_domains', 5],
    ['settings.token_rotation_enabled', 5]
  ]);
  return {
    status: 'unsupported',
    reason: plannedPrefix
      ? 'The manifest service and console do not exist yet.'
      : 'Chatto has no equivalent hosted or enterprise feature for this key.',
    target_phase: plannedPrefix ? phaseByPrefix.get(plannedPrefix) : null,
    semantic_mapping: plannedPrefix
      ? 'Preserve, validate, and apply through the app definition'
      : 'Preserve unknown keys for round-trip only'
  };
}

/** Coverage for non-method platform surfaces. */
export const surfacePolicies = [
  {
    name: 'incoming_webhooks',
    status: 'partial',
    reason:
      'Chatto accepts compatible text and channel fields at /webhooks/incoming/:credential, but it does not implement the complete payload, response, rate-limit, or install contract.',
    target_phase: 2
  },
  {
    name: 'web_api',
    status: 'unsupported',
    reason: 'No /api/{method} compatibility gateway exists.',
    target_phase: 1
  },
  {
    name: 'oauth_v2',
    status: 'unsupported',
    reason: 'Chatto OAuth is a client-session flow, not an app installation flow.',
    target_phase: 1
  },
  {
    name: 'app_manifests',
    status: 'unsupported',
    reason: 'No app manifest import, validation, storage, or export service exists.',
    target_phase: 1
  },
  {
    name: 'request_url_events',
    status: 'unsupported',
    reason:
      'Existing outbound bot webhooks cover selected messages only and use a different best-effort contract.',
    target_phase: 3
  },
  {
    name: 'socket_mode',
    status: 'unsupported',
    reason: 'The native protobuf realtime protocol is not Socket Mode.',
    target_phase: 3
  },
  {
    name: 'slash_commands',
    status: 'unsupported',
    reason: 'No command registration or interaction delivery service exists.',
    target_phase: 3
  },
  {
    name: 'interactivity',
    status: 'unsupported',
    reason: 'No signed actions, shortcuts, options, views, or response_url service exists.',
    target_phase: 4
  },
  {
    name: 'block_kit',
    status: 'unsupported',
    reason:
      'Messages store Chatto text and attachments; clients do not render the target structured surfaces.',
    target_phase: 4
  },
  {
    name: 'level_3_egress_gateway',
    status: 'unsupported',
    reason: 'No host-interception gateway or private-CA deployment package exists.',
    target_phase: 9
  }
];
