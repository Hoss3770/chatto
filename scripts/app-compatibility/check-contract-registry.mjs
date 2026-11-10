#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-or-later

/** Validate structural invariants in the generated compatibility registry. */

import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const registry = resolve(process.argv[2] || 'spec/registry');
const coveragePath = resolve(process.argv[3] || 'docs/compat/coverage-matrix.md');

async function readJson(name) {
  return JSON.parse(await readFile(`${registry}/${name}`, 'utf8'));
}

function assertUnique(items, key, label) {
  const values = items.map((item) => key(item));
  assert.equal(new Set(values).size, values.length, `${label} contains duplicate identities`);
}

function assertCoverage(items, label) {
  const allowed = new Set(['implemented', 'partial', 'unsupported']);
  for (const item of items) {
    assert.ok(item.compatibility, `${label} ${item.name || item.path} has no coverage`);
    assert.ok(
      allowed.has(item.compatibility.status),
      `${label} ${item.name || item.path} has invalid status ${item.compatibility.status}`
    );
    if (item.compatibility.status !== 'implemented') {
      assert.ok(item.compatibility.reason, `${label} ${item.name || item.path} has no reason`);
    }
  }
}

async function main() {
  const [
    summary,
    methods,
    events,
    scopes,
    blocks,
    elements,
    composition,
    manifest,
    surfaces,
    sdk,
    coverage
  ] = await Promise.all([
    readJson('summary.json'),
    readJson('methods.json'),
    readJson('events.json'),
    readJson('scopes.json'),
    readJson('blocks.json'),
    readJson('elements.json'),
    readJson('composition-objects.json'),
    readJson('manifest-keys.json'),
    readJson('surfaces.json'),
    readJson('sdk-compatibility.json'),
    readFile(coveragePath, 'utf8')
  ]);

  assert.equal(summary.counts.methods, methods.methods.length);
  assert.equal(summary.counts.events, events.events.length);
  assert.equal(summary.counts.events, 154);
  assert.equal(summary.source.documentation.selected_page_count, 719);
  assert.equal(summary.counts.scopes, scopes.scopes.length);
  assert.equal(summary.counts.blocks, blocks.blocks.length);
  assert.equal(summary.counts.elements, elements.elements.length);
  assert.equal(summary.counts.composition_objects, composition.composition_objects.length);
  assert.equal(summary.counts.manifest_keys, manifest.manifest_keys.length);
  assert.equal(
    summary.counts.documented_methods,
    methods.methods.filter((method) => method.documented).length
  );
  assert.equal(
    summary.counts.sdk_only_methods,
    methods.methods.filter((method) => !method.documented).length
  );

  assertUnique(methods.methods, (method) => method.name.toLowerCase(), 'methods');
  assertUnique(events.events, (event) => event.name, 'events');
  assertUnique(scopes.scopes, (scope) => scope.name, 'scopes');
  assertUnique(blocks.blocks, (block) => block.wire_type, 'blocks');
  assertUnique(elements.elements, (element) => element.wire_type, 'elements');
  assertUnique(
    composition.composition_objects,
    (object) => object.wire_type,
    'composition objects'
  );
  assertUnique(manifest.manifest_keys, (key) => key.path, 'manifest keys');

  assertCoverage(methods.methods, 'method');
  assertCoverage(events.events, 'event');
  assertCoverage(scopes.scopes, 'scope');
  assertCoverage(blocks.blocks, 'block');
  assertCoverage(elements.elements, 'element');
  assertCoverage(composition.composition_objects, 'composition object');
  assertCoverage(manifest.manifest_keys, 'manifest key');
  assertCoverage(
    surfaces.surfaces.map((surface) => ({ ...surface, compatibility: surface })),
    'surface'
  );

  const methodByName = new Map(methods.methods.map((method) => [method.name, method]));
  assert.ok(methodByName.has('chat.postMessage'));
  assert.ok(methodByName.has('apps.connections.open'));
  assert.ok(methodByName.has('workflows.stepCompleted'));
  assert.equal(methodByName.get('chat.postMessage').deprecated, false);
  assert.equal(methodByName.get('chat.postMessage').compatibility.target_phase, 2);
  assert.equal(methodByName.get('chat.scheduleMessage').compatibility.target_phase, 5);
  assert.equal(methodByName.get('chat.startStream').compatibility.target_phase, 6);
  assert.equal(methodByName.get('conversations.inviteShared').compatibility.target_phase, 8);
  assert.equal(methodByName.get('workflows.stepCompleted').compatibility.target_phase, 7);
  assert.equal(
    methodByName.get('chat.postMessage').sdk_signatures.node.argument_type,
    'ChatPostMessageArguments'
  );
  assert.equal(
    methodByName.get('chat.postMessage').sdk_signatures.java.request_type,
    'ChatPostMessageRequest'
  );
  assert.ok(methodByName.get('chat.postMessage').warnings.includes('message_truncated'));
  assert.ok(methodByName.get('oauth.v2.access').warnings.includes('slackbot_mcp_unavailable'));
  assert.ok(methodByName.get('chat.postMessage').response_examples.length > 0);
  assert.ok(events.events.find((event) => event.name === 'message').payload_examples.length > 0);
  const messageSubtypes = events.events.filter(
    (event) => event.event_type === 'message' && event.event_subtype
  );
  assert.equal(messageSubtypes.length, 33);
  assert.ok(messageSubtypes.some((event) => event.name === 'message.bot_message'));
  assert.ok(messageSubtypes.some((event) => event.name === 'message.message_changed'));
  assert.ok(
    events.events.find((event) => event.name === 'app_mention').scopes.includes('app_mentions:read')
  );
  const knownScopes = new Set(scopes.scopes.map((scope) => scope.name));
  for (const event of events.events) {
    for (const scope of event.scopes) {
      assert.ok(knownScopes.has(scope), `event ${event.name} has unknown scope ${scope}`);
    }
    for (const api of event.delivery_apis) {
      assert.ok(['Events', 'RTM'].includes(api), `event ${event.name} has unknown API ${api}`);
    }
  }
  const serializedExamples = JSON.stringify({
    method_arguments: methods.methods.map((method) => method.arguments),
    method_responses: methods.methods.map((method) => method.response_examples),
    events: events.events.map((event) => event.payload_examples)
  });
  for (const sensitiveSample of [
    'xoxb-',
    'xoxp-',
    'xoxe-',
    '@ghostbusters',
    '@example.com',
    'https://...'
  ]) {
    assert.ok(
      !serializedExamples.includes(sensitiveSample),
      `examples contain an unsanitized sample value: ${sensitiveSample}`
    );
  }

  const scopeByName = new Map(scopes.scopes.map((scope) => [scope.name, scope]));
  assert.equal(scopeByName.get('commands').compatibility.target_phase, 3);
  assert.equal(scopeByName.get('assistant:write').compatibility.target_phase, 6);
  assert.equal(scopeByName.get('users:read.email').compatibility.target_phase, 2);
  assert.deepEqual(scopeByName.get('users:read.email').compatibility.chatto_permissions, [
    'user.read-email'
  ]);

  const blockTypes = new Set(blocks.blocks.map((block) => block.wire_type));
  for (const type of ['auth_provider', 'call', 'data_visualization', 'layout']) {
    assert.ok(blockTypes.has(type), `block union is missing ${type}`);
  }
  const markdown = blocks.blocks.find((block) => block.wire_type === 'markdown');
  assert.deepEqual(
    markdown.fields.map((field) => field.name),
    ['type', 'text', 'block_id']
  );
  const section = blocks.blocks.find((block) => block.wire_type === 'section');
  assert.deepEqual(section.fields.find((field) => field.name === 'type').allowed_literals, [
    'section'
  ]);
  assert.deepEqual(section.fields.find((field) => field.name === 'text').allowed_literals, []);
  const emailInput = elements.elements.find((element) => element.wire_type === 'email_text_input');
  assert.deepEqual(emailInput.fields.find((field) => field.name === 'type').allowed_literals, [
    'email_text_input'
  ]);
  const card = blocks.blocks.find((block) => block.wire_type === 'card');
  assert.deepEqual(card.fields.find((field) => field.name === 'slack_icon').allowed_literals, []);

  const manifestPaths = new Set(manifest.manifest_keys.map((key) => key.path));
  assert.ok(manifestPaths.has('display_information.name'));
  assert.ok(manifestPaths.has('settings.event_subscriptions.request_url'));
  assert.ok(manifestPaths.has('external_auth_providers.options.identity_config.url'));
  assert.ok(!manifestPaths.has('url'));
  const manifestByPath = new Map(manifest.manifest_keys.map((key) => [key.path, key]));
  assert.ok(
    manifest.manifest_keys.every((key) =>
      ['array', 'boolean', 'integer', 'object', 'string'].includes(key.type_hint)
    ),
    'a manifest key has no recognized type hint'
  );
  assert.ok(
    manifest.manifest_keys.every((key) => key.versions.length > 0),
    'a manifest key has no schema version'
  );
  assert.deepEqual(manifestByPath.get('_metadata').versions, ['v1', 'v2']);
  assert.equal(manifestByPath.get('display_information.name').type_hint, 'string');
  assert.equal(manifestByPath.get('display_information.name').required, true);
  assert.equal(manifestByPath.get('display_information.name').constraints.max_length, 35);
  assert.equal(
    manifestByPath.get('app_directory.installation_landing_page').requirement,
    'Required (if `app_directory` subgroup is included)'
  );
  assert.equal(
    manifestByPath.get('app_directory.supported_languages').constraints.max_item_length,
    50
  );
  assert.deepEqual(
    manifestByPath.get('external_auth_providers.options.identity_config.http_method_type')
      .allowed_literals,
    ['GET', 'POST']
  );
  assert.equal(manifestByPath.get('display_information.name').compatibility.target_phase, 1);
  assert.equal(manifestByPath.get('settings.incoming_webhooks').compatibility.target_phase, 2);
  assert.equal(manifestByPath.get('settings.interactivity').compatibility.target_phase, 4);

  assert.equal(sdk.method_counts.node, 283);
  assert.equal(sdk.method_counts.python, 332);
  assert.equal(sdk.method_counts.java, 345);
  assert.equal(sdk.signature_counts.node, 283);
  assert.equal(sdk.signature_counts.python, 332);
  assert.equal(sdk.signature_counts.java, 344);
  assert.equal(sdk.parsed_source_files.length, 9);
  assertUnique(sdk.parsed_source_files, (file) => file.path, 'parsed SDK source files');
  assert.ok(
    sdk.parsed_source_files.every((file) => /^[a-f0-9]{64}$/.test(file.sha256)),
    'an SDK source hash is invalid'
  );
  assert.ok(
    sdk.api_and_oauth_configuration_evidence.every((item) => item.present),
    'an expected SDK configuration surface was not found'
  );

  const coverageItems = [
    ...methods.methods,
    ...events.events,
    ...scopes.scopes,
    ...blocks.blocks,
    ...elements.elements,
    ...composition.composition_objects,
    ...manifest.manifest_keys,
    ...surfaces.surfaces.map((surface) => ({ compatibility: surface }))
  ];
  const coverageCounts = { implemented: 0, partial: 0, unsupported: 0 };
  for (const item of coverageItems) coverageCounts[item.compatibility.status] += 1;
  assert.deepEqual(summary.coverage, coverageCounts);

  const allContracts = [
    ...methods.methods.map((item) => item.name),
    ...events.events.map((item) => item.name),
    ...scopes.scopes.map((item) => item.name),
    ...blocks.blocks.map((item) => item.wire_type),
    ...elements.elements.map((item) => item.wire_type),
    ...composition.composition_objects.map((item) => item.wire_type),
    ...manifest.manifest_keys.map((item) => item.path),
    ...surfaces.surfaces.map((item) => item.name)
  ];
  for (const contract of allContracts) {
    assert.ok(
      coverage.includes(`\`${contract.replaceAll('|', '\\|')}\``),
      `coverage matrix is missing ${contract}`
    );
  }

  console.log(
    `Registry check passed: ${allContracts.length} contracts, ${summary.counts.documented_methods} documented methods.`
  );
}

main().catch((error) => {
  console.error(error.stack || error);
  process.exitCode = 1;
});
