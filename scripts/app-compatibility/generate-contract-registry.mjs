#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-or-later

/**
 * Generate the app-compatibility contract registry from public Markdown
 * references and published SDK source. The generated files contain structured
 * facts and hashes, not copies of complete third-party documentation pages.
 */

import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, join, relative, resolve } from 'node:path';
import { promisify } from 'node:util';
import { format, resolveConfig } from 'prettier';
import {
  blockKitPolicy,
  eventPolicy,
  manifestPolicy,
  methodPolicy,
  scopePolicy,
  surfacePolicies
} from './registry-policy.mjs';

const execFileAsync = promisify(execFile);
const SITEMAP_URL = 'https://docs.slack.dev/llms-sitemap.md';
const DEFAULT_DOCS_DIR = '.context/app-compat-registry-source';
const DEFAULT_SDK_DIR = '.context/app-compat-registry-source/sdks';
const DEFAULT_OUTPUT_DIR = 'spec/registry';

const SDK_VERSIONS = {
  node: {
    '@slack/bolt': '5.1.0',
    '@slack/oauth': '4.0.0',
    '@slack/socket-mode': '3.2.0',
    '@slack/types': '3.3.0',
    '@slack/web-api': '8.3.0'
  },
  python: {
    'slack-bolt': '1.30.0',
    'slack-sdk': '3.46.0'
  },
  java: {
    bolt: '1.54.0',
    'slack-api-client': '1.54.0'
  }
};

function parseArguments(argv) {
  const options = {
    docsDir: DEFAULT_DOCS_DIR,
    sdkDir: DEFAULT_SDK_DIR,
    outputDir: DEFAULT_OUTPUT_DIR,
    fetchDocs: false,
    fetchSdks: false
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--docs-dir') options.docsDir = argv[++index];
    else if (argument === '--sdk-dir') options.sdkDir = argv[++index];
    else if (argument === '--output-dir') options.outputDir = argv[++index];
    else if (argument === '--fetch-docs') options.fetchDocs = true;
    else if (argument === '--fetch-sdks') options.fetchSdks = true;
    else if (argument === '--help') {
      console.log(`Usage: node scripts/app-compatibility/generate-contract-registry.mjs [options]

Options:
  --docs-dir PATH    Cached documentation source directory
  --sdk-dir PATH     Extracted published SDK source directory
  --output-dir PATH  Generated registry directory
  --fetch-docs       Refresh the documentation cache from the Markdown sitemap
  --fetch-sdks       Download and extract the pinned published SDK artifacts
`);
      process.exit(0);
    } else {
      throw new Error(`Unknown argument: ${argument}`);
    }
  }
  return options;
}

function sha256(value) {
  return createHash('sha256').update(value).digest('hex');
}

function optionalTextHash(value) {
  return typeof value === 'string' && value ? sha256(value) : null;
}

function uniqueSorted(values) {
  return [
    ...new Set(values.filter((value) => value !== undefined && value !== null && value !== ''))
  ].sort();
}

function parseScalar(raw) {
  const value = raw.trim();
  if (value === 'true') return true;
  if (value === 'false') return false;
  if (value === 'null') return null;
  if (/^-?\d+$/.test(value)) return Number(value);
  if (value.startsWith('[') && value.endsWith(']')) {
    try {
      return JSON.parse(value.replaceAll("'", '"'));
    } catch {
      return value
        .slice(1, -1)
        .split(',')
        .map((part) => part.trim().replace(/^['"]|['"]$/g, ''))
        .filter(Boolean);
    }
  }
  return value.replace(/^['"]|['"]$/g, '');
}

function parseFrontMatter(markdown) {
  if (!markdown.startsWith('---\n')) return {};
  const end = markdown.indexOf('\n---\n', 4);
  if (end < 0) return {};
  const lines = markdown.slice(4, end).split('\n');
  const result = {};
  let activeKey = null;
  let activeKind = null;
  for (const line of lines) {
    const top = line.match(/^([A-Za-z0-9_]+):(?:\s*(.*))?$/);
    if (top) {
      activeKey = top[1];
      if (top[2]) {
        result[activeKey] = parseScalar(top[2]);
        activeKind = null;
      } else {
        result[activeKey] = null;
        activeKind = null;
      }
      continue;
    }
    if (!activeKey) continue;
    const listItem = line.match(/^\s{2}-\s+(.*)$/);
    if (listItem) {
      if (activeKind !== 'array') {
        result[activeKey] = [];
        activeKind = 'array';
      }
      result[activeKey].push(parseScalar(listItem[1]));
      continue;
    }
    const mapItem = line.match(/^\s{2}([A-Za-z0-9_]+):\s*(.*)$/);
    if (mapItem) {
      if (activeKind !== 'object') {
        result[activeKey] = {};
        activeKind = 'object';
      }
      result[activeKey][mapItem[1]] = parseScalar(mapItem[2]);
    }
  }
  return result;
}

function extractSection(markdown, headingPattern) {
  const match = headingPattern.exec(markdown);
  if (!match) return '';
  const start = match.index + match[0].length;
  const nextHeading = markdown.slice(start).search(/^##\s+/m);
  return nextHeading < 0 ? markdown.slice(start) : markdown.slice(start, start + nextHeading);
}

function parseMethodArguments(markdown) {
  const section = extractSection(markdown, /^## Arguments[^\n]*$/m);
  if (!section) return [];
  const matches = [
    ...section.matchAll(/^\*\*`([^`]+)`\*\*(?:`([^`]+)`)?(Required|Optional)?\s*$/gm)
  ];
  return matches.map((match, index) => {
    const end = index + 1 < matches.length ? matches[index + 1].index : section.length;
    const body = section.slice(match.index + match[0].length, end);
    const requirement =
      match[3] || (/\bRequired\b/.test(body.slice(0, 120)) ? 'Required' : 'Optional');
    const example = body.match(/_Example:_\s*`([^`]+)`/i)?.[1] ?? null;
    return {
      name: match[1],
      type: match[2] || null,
      required: requirement === 'Required',
      example: example === null ? null : ['true', 'false'].includes(example) ? example : '<string>'
    };
  });
}

function parseStandaloneCodes(section) {
  return uniqueSorted([...section.matchAll(/^`([^`\n]+)`\s*$/gm)].map((match) => match[1]));
}

function shapeOf(value, depth = 0) {
  if (value === null) return 'null';
  if (Array.isArray(value)) {
    if (value.length === 0) return [];
    return [shapeOf(value[0], depth + 1)];
  }
  if (typeof value === 'object') {
    if (depth >= 5) return 'object';
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, shapeOf(value[key], depth + 1)])
    );
  }
  return typeof value;
}

function sanitizeJsonExample(value, key = '') {
  if (Array.isArray(value)) return value.map((item) => sanitizeJsonExample(item, key));
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([childKey, childValue]) => [
        childKey,
        sanitizeJsonExample(childValue, childKey)
      ])
    );
  }
  if (typeof value !== 'string') return value;
  if (['error', 'warning', 'warnings', 'type', 'subtype', 'token_type'].includes(key)) {
    return value;
  }
  return '<string>';
}

function parseJsonExamples(section, limit = 4) {
  const examples = [];
  for (const match of section.matchAll(/```(?:json)?\s*\n([\s\S]*?)```/g)) {
    if (examples.length >= limit) break;
    let body = match[1].trim();
    if (!body.startsWith('{') && !body.startsWith('[')) continue;
    body = body.replace(/,\s*([}\]])/g, '$1');
    try {
      examples.push(sanitizeJsonExample(JSON.parse(body)));
    } catch {
      // Documentation examples can contain comments or template markers. A
      // malformed example remains represented by its source hash.
    }
  }
  return examples;
}

function parseMethodWarnings(markdown, examples) {
  const warnings = [];
  for (const example of examples) {
    if (!example || typeof example !== 'object' || Array.isArray(example)) continue;
    if (typeof example.warning === 'string') warnings.push(...example.warning.split(','));
    if (Array.isArray(example.response_metadata?.warnings)) {
      warnings.push(
        ...example.response_metadata.warnings.filter((warning) => typeof warning === 'string')
      );
    }
  }
  for (const match of markdown.matchAll(/`([a-z][a-z0-9_]+)`\s+warning\b/gi)) {
    warnings.push(match[1]);
  }
  for (const match of markdown.matchAll(
    /warning(?: field)?(?: of| is|:)\s*`([a-z][a-z0-9_]+)`/gi
  )) {
    warnings.push(match[1]);
  }
  return uniqueSorted(warnings.map((warning) => warning.trim()));
}

function parseContentTypes(markdown) {
  const match = markdown.match(/\*\*Content types\*\*([\s\S]*?)(?:\*\*[A-Z]|^## )/m);
  if (!match) return [];
  return uniqueSorted([...match[1].matchAll(/`([^`]+)`/g)].map((item) => item[1]));
}

function parseMethodName(markdown, frontMatter, sdkCanonicalNames, url) {
  const apiReference = markdown.match(/api_refs:\s*\n\s*-\s+name:\s*["']([^"']+)["']/)?.[1];
  if (apiReference) return apiReference;
  const heading = markdown.match(/^#\s+`?([A-Za-z0-9_.]+)`?\s+method/m)?.[1];
  if (heading) return heading;
  const slug = basename(new URL(url).pathname, '.md');
  return sdkCanonicalNames.get(slug.toLowerCase()) || slug;
}

function inferDeprecated(name, frontMatter, markdown, documented) {
  if (frontMatter.deprecated === true || frontMatter.status === 'deprecated') return true;
  if (!documented && /^(channels|groups|im|mpim)\./.test(name)) return true;
  const errorsStart = markdown.indexOf('## Errors');
  const introduction = markdown.slice(0, errorsStart < 0 ? 2000 : errorsStart);
  return /\b(?:this|the) (?:API )?(?:method|endpoint) (?:is|has been) deprecated\b/i.test(
    introduction
  );
}

function parseMethodPage(page, sdkCanonicalNames) {
  const frontMatter = parseFrontMatter(page.markdown);
  const name = parseMethodName(page.markdown, frontMatter, sdkCanonicalNames, page.url);
  const responseSection = extractSection(page.markdown, /^## Response[^\n]*$/m);
  const errorsSection = extractSection(page.markdown, /^## Errors[^\n]*$/m);
  const responseExamples = parseJsonExamples(responseSection);
  const rateTier = typeof frontMatter.rate_limit === 'string' ? frontMatter.rate_limit : null;
  const scopeObject =
    frontMatter.scopes &&
    typeof frontMatter.scopes === 'object' &&
    !Array.isArray(frontMatter.scopes)
      ? frontMatter.scopes
      : {};
  const botScopes = Array.isArray(scopeObject.bot) ? scopeObject.bot : [];
  const userScopes = Array.isArray(scopeObject.user) ? scopeObject.user : [];
  const deprecated = inferDeprecated(name, frontMatter, page.markdown, true);
  return {
    name,
    title_sha256: optionalTextHash(frontMatter.title),
    summary_sha256: optionalTextHash(frontMatter.description || frontMatter.summary),
    documented: true,
    source: {
      url: page.url,
      sha256: sha256(page.markdown)
    },
    sdk_presence: {},
    arguments: parseMethodArguments(page.markdown),
    token_types: uniqueSorted(Object.keys(scopeObject)),
    scopes: {
      bot: uniqueSorted(botScopes),
      user: uniqueSorted(userScopes),
      all: uniqueSorted([...botScopes, ...userScopes])
    },
    rate_tier: rateTier,
    content_types: parseContentTypes(page.markdown),
    response_examples: responseExamples,
    response_shapes: responseExamples.map((example) => shapeOf(example)),
    errors: parseStandaloneCodes(errorsSection),
    warnings: parseMethodWarnings(page.markdown, responseExamples),
    deprecated,
    compatibility: methodPolicy(name, { documented: true, deprecated })
  };
}

function parseEventFacts(markdown, label) {
  const marker = `**${label}**`;
  const start = markdown.indexOf(marker);
  if (start < 0) return [];
  const remainder = markdown.slice(start + marker.length);
  const end = remainder.search(/^(?:\*\*[^*]+\*\*|#{1,6}\s+)/m);
  const section = end < 0 ? remainder : remainder.slice(0, end);
  return [...section.matchAll(/\[([^\]]+)\]\([^)]+\)/g)].map((match) =>
    match[1].replace(/^`|`$/g, '')
  );
}

function canonicalEventScope(scope) {
  return scope.includes(':') ? scope : scope.replace(/\.(?=[^.]+$)/, ':');
}

function parseEventPage(page) {
  const frontMatter = parseFrontMatter(page.markdown);
  const path = new URL(page.url).pathname;
  const segments = path
    .replace(/^\/reference\/events\//, '')
    .replace(/\.md$/, '')
    .split('/');
  const name = frontMatter.event_name || segments.join('.');
  const usageSection = extractSection(page.markdown, /^## Usage info[^\n]*$/m) || page.markdown;
  const payloadExamples = parseJsonExamples(usageSection);
  const factScopes = parseEventFacts(page.markdown, 'Required Scopes').map((scope) =>
    canonicalEventScope(scope)
  );
  const factApis = parseEventFacts(page.markdown, 'Compatible APIs');
  return {
    name,
    event_type: segments.length > 1 ? segments[0] : name,
    event_subtype: segments.length > 1 ? segments.at(-1) : null,
    title_sha256: optionalTextHash(frontMatter.title),
    summary_sha256: optionalTextHash(frontMatter.summary),
    source: { url: page.url, sha256: sha256(page.markdown) },
    scopes: uniqueSorted([
      ...(Array.isArray(frontMatter.scopes)
        ? frontMatter.scopes.map((scope) => canonicalEventScope(scope))
        : []),
      ...factScopes
    ]),
    delivery_apis: uniqueSorted([
      ...(Array.isArray(frontMatter.apis) ? frontMatter.apis : []),
      ...factApis
    ]),
    payload_examples: payloadExamples,
    payload_shapes: payloadExamples.map((example) => shapeOf(example)),
    compatibility: eventPolicy(name)
  };
}

function parseScopePage(page) {
  const frontMatter = parseFrontMatter(page.markdown);
  const name =
    frontMatter.scope_name || basename(new URL(page.url).pathname, '.md').replaceAll('.', ':');
  const methods = uniqueSorted(
    Array.isArray(frontMatter.compatible_methods) ? frontMatter.compatible_methods : []
  );
  return {
    name,
    title_sha256: optionalTextHash(frontMatter.title),
    summary_sha256: optionalTextHash(frontMatter.summary),
    source: { url: page.url, sha256: sha256(page.markdown) },
    token_types: uniqueSorted(
      Array.isArray(frontMatter.token_types) ? frontMatter.token_types : []
    ),
    compatible_methods: methods,
    compatible_events: uniqueSorted(Array.isArray(frontMatter.events) ? frontMatter.events : []),
    compatibility: scopePolicy(name, methods)
  };
}

function parseFieldTable(markdown) {
  const fieldHeadingMatch = markdown.match(/^(#{2,4})\s+Fields?\b.*$/m);
  if (!fieldHeadingMatch) return [];
  const fieldHeading = fieldHeadingMatch.index;
  const headingLevel = fieldHeadingMatch[1].length;
  const bodyStart = fieldHeading + fieldHeadingMatch[0].length;
  const nextHeadingPattern = new RegExp(`^#{1,${headingLevel}}\\s+`, 'm');
  const nextHeading = markdown.slice(bodyStart).search(nextHeadingPattern);
  const section =
    nextHeading < 0
      ? markdown.slice(fieldHeading)
      : markdown.slice(fieldHeading, bodyStart + nextHeading);
  const lines = section.split('\n');
  const fields = [];
  for (let index = 0; index < lines.length; index += 1) {
    const field = lines[index].match(/^`([^`]+)`\s*$/)?.[1];
    if (!field || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(field)) continue;
    const body = [];
    let type = null;
    let requirement = null;
    for (let cursor = index + 1; cursor < lines.length; cursor += 1) {
      const line = lines[cursor].trim();
      if (/^#{2,4}\s+/.test(line) || /^`[^`]+`$/.test(line)) break;
      if (!line || ['Field', 'Type', 'Description', 'Required?'].includes(line)) continue;
      if (!type) {
        type = line;
        continue;
      }
      if (['Required', 'Optional', 'Preferred', 'Maybe'].includes(line)) {
        requirement = line;
        break;
      }
      body.push(line);
    }
    const description = body.join(' ');
    const lengths = [
      ...description.matchAll(
        /(?:minimum|min)\w* length(?: is| of)?\s*(\d+)|(?:maximum|max)\w* length(?: is| of)?\s*(\d+)/gi
      )
    ];
    const constraints = {};
    for (const length of lengths) {
      if (length[1]) constraints.min_length = Number(length[1]);
      if (length[2]) constraints.max_length = Number(length[2]);
    }
    const maximumItems = description.match(
      /maximum (?:number of )?(?:items|elements|options)(?: is| of)?\s*(\d+)/i
    )?.[1];
    if (maximumItems) constraints.max_items = Number(maximumItems);
    const fixedLiteral = description.match(
      /(?:always be|is always|must be|set to|value is)\s+`([^`]+)`/i
    )?.[1];
    const literalSection = description.match(
      /(?:can be either|one of(?: the following)?|allowed values are):?\s*([^.]*)/i
    )?.[1];
    const scalarType = /\b(?:string|boolean|number|integer)\b/i.test(type);
    const allowedLiterals = !scalarType
      ? []
      : fixedLiteral
        ? [fixedLiteral]
        : literalSection
          ? [...literalSection.matchAll(/`([^`]+)`/g)].map((match) => match[1])
          : [];
    fields.push({
      name: field,
      type,
      required: requirement === 'Required',
      requirement: requirement || 'unspecified',
      allowed_literals: uniqueSorted(allowedLiterals),
      constraints,
      description_sha256: sha256(description),
      description
    });
  }
  return fields;
}

function inferWireType(slug, fields) {
  const typeField = fields.find((field) => field.name === 'type');
  if (typeField?.allowed_literals.length) {
    const literal = typeField.allowed_literals.find((value) => /^[a-z][a-z0-9_]*$/.test(value));
    if (literal) return literal;
  }
  return slug
    .replace(/-(block|element|object)$/, '')
    .replaceAll('-', '_')
    .replace('date_picker', 'datepicker')
    .replace('datetime_picker', 'datetimepicker')
    .replace('time_picker', 'timepicker');
}

function parseBlockKitPage(page, kind) {
  const frontMatter = parseFrontMatter(page.markdown);
  const slug = basename(new URL(page.url).pathname, '.md');
  const fields = parseFieldTable(page.markdown);
  const wireType =
    kind === 'composition_object'
      ? slug.replace(/-object$/, '').replaceAll('-', '_')
      : inferWireType(slug, fields);
  return {
    name: slug,
    wire_type: wireType,
    title_sha256: optionalTextHash(frontMatter.title),
    summary_sha256: optionalTextHash(frontMatter.summary),
    source: { url: page.url, sha256: sha256(page.markdown), kind: 'documentation' },
    sdk_presence: {},
    available_in_surfaces: uniqueSorted(
      Array.isArray(frontMatter.available_in_surfaces) ? frontMatter.available_in_surfaces : []
    ),
    compatible_elements: uniqueSorted(
      Array.isArray(frontMatter.compatible_elements) ? frontMatter.compatible_elements : []
    ),
    fields: fields.map(({ description, ...field }) => field),
    compatibility: blockKitPolicy(kind, wireType)
  };
}

function parseManifestFields(page) {
  const start = page.markdown.search(/^### Metadata\b/m);
  const markdown = start < 0 ? page.markdown : page.markdown.slice(start);
  const matches = [...markdown.matchAll(/^`([A-Za-z_][A-Za-z0-9_.\[\]-]*)`\s*$/gm)];
  const fields = matches.map((match, index) => {
    const end = index + 1 < matches.length ? matches[index + 1].index : markdown.length;
    const body = markdown.slice(match.index + match[0].length, end);
    const identityConfigFields = new Set([
      'url',
      'account_identifier',
      'headers',
      'body',
      'http_method_type'
    ]);
    const path = identityConfigFields.has(match[1])
      ? `external_auth_providers.options.identity_config.${match[1]}`
      : match[1];
    const lines = body
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);
    const requirement =
      lines.find((line) => line === 'Optional' || line.startsWith('Required')) || 'unspecified';
    const description =
      lines.find(
        (line) =>
          !['Field', 'Description', 'Required', 'Optional', 'v1', 'v2', '✅', '❌'].includes(line)
      ) || '';
    const normalizedDescription = description.toLowerCase();
    const typeHint = normalizedDescription.includes('array')
      ? 'array'
      : normalizedDescription.includes('boolean') || normalizedDescription.startsWith('whether ')
        ? 'boolean'
        : normalizedDescription.includes('integer')
          ? 'integer'
          : normalizedDescription.includes('object') ||
              normalizedDescription.includes('group of settings') ||
              normalizedDescription.includes('keyed by')
            ? 'object'
            : /\b(?:string|text|url|email address|hex value|link to)\b/.test(
                  normalizedDescription
                ) || normalizedDescription.startsWith('can be either ')
              ? 'string'
              : null;
    const numberFrom = (pattern) => {
      const value = description.match(pattern)?.[1];
      return value ? Number(value.replaceAll(',', '')) : null;
    };
    const minLength = numberFrom(/minimum length (?:is|of)\s*([\d,]+)/i);
    const maxLength = numberFrom(/maximum length (?:is|of)\s*([\d,]+)/i);
    const perItemLength = /\beach\b.*\bmaximum length\b/i.test(description);
    const keyLengthRange = description.match(/\bkey of\s*([\d,]+)\s+to\s+([\d,]+)\s+characters\b/i);
    const itemRange = description.match(
      /between\s*([\d,]+)\s+and\s+([\d,]+)\s+(?:items|entries|categories|languages)\b/i
    );
    const maxItems =
      numberFrom(
        /max(?:imum)?(?: of)?\s*([\d,]+)\s+(?:items|entries|categories|languages|servers|types|datastores|workflows)/i
      ) ?? numberFrom(/\bmax\s*([\d,]+)\s*\.?(?:\s|$)/i);
    const versionMarkers = lines.filter((line) => line === '✅' || line === '❌').slice(0, 2);
    const allowedLiteralSection = description.match(
      /(?:can be either|one of(?: the following)?):?\s*([^.]*)/i
    )?.[1];
    return {
      path,
      type_hint: typeHint,
      required: requirement.startsWith('Required'),
      requirement,
      versions: uniqueSorted([
        versionMarkers[0] === '✅' ? 'v1' : null,
        versionMarkers[1] === '✅' ? 'v2' : null
      ]),
      allowed_literals: allowedLiteralSection
        ? uniqueSorted([...allowedLiteralSection.matchAll(/`([^`]+)`/g)].map((value) => value[1]))
        : [],
      constraints: {
        ...(minLength !== null ? { min_length: minLength } : {}),
        ...(maxLength !== null && !perItemLength ? { max_length: maxLength } : {}),
        ...(maxLength !== null && perItemLength && typeHint === 'array'
          ? { max_item_length: maxLength }
          : {}),
        ...(maxLength !== null && perItemLength && typeHint === 'object'
          ? { max_key_length: maxLength }
          : {}),
        ...(keyLengthRange
          ? {
              min_key_length: Number(keyLengthRange[1].replaceAll(',', '')),
              max_key_length: Number(keyLengthRange[2].replaceAll(',', ''))
            }
          : {}),
        ...(itemRange
          ? {
              min_items: Number(itemRange[1].replaceAll(',', '')),
              max_items: Number(itemRange[2].replaceAll(',', ''))
            }
          : {}),
        ...(maxItems !== null ? { max_items: maxItems } : {})
      },
      description_sha256: sha256(description),
      compatibility: manifestPolicy(path)
    };
  });
  for (const field of fields) {
    if (
      field.type_hint === null &&
      fields.some((candidate) => candidate.path.startsWith(`${field.path}.`))
    ) {
      field.type_hint = 'object';
    }
  }
  return fields;
}

async function fileExists(path) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

async function fetchText(url, attempts = 5) {
  let lastError;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      const response = await fetch(url, {
        headers: { 'user-agent': 'Chatto compatibility registry generator' },
        redirect: 'follow'
      });
      if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
      return await response.text();
    } catch (error) {
      lastError = error;
      if (attempt < attempts) await new Promise((done) => setTimeout(done, attempt * 300));
    }
  }
  throw new Error(`Failed to fetch ${url}: ${lastError}`);
}

function selectDocumentationUrls(sitemap) {
  const urls = [...sitemap.matchAll(/^- (https:\/\/docs\.slack\.dev\/[^\s]+\.md)$/gm)].map(
    (match) => match[1]
  );
  return uniqueSorted(
    urls.filter((url) => {
      const path = new URL(url).pathname;
      return (
        /^\/reference\/(methods|scopes)\/[^/]+\.md$/.test(path) ||
        /^\/reference\/events\/(?:[^/]+\/)*[^/]+\.md$/.test(path) ||
        /^\/reference\/block-kit\/(blocks|block-elements|composition-objects)(\/[^/]+)?\.md$/.test(
          path
        ) ||
        path === '/reference/app-manifest.md'
      );
    })
  );
}

async function mapConcurrent(values, concurrency, operation) {
  const result = new Array(values.length);
  let next = 0;
  async function worker() {
    while (true) {
      const index = next++;
      if (index >= values.length) return;
      result[index] = await operation(values[index], index);
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, values.length) }, worker));
  return result;
}

async function refreshDocumentationCache(directory) {
  await mkdir(join(directory, 'pages'), { recursive: true });
  const sitemap = await fetchText(SITEMAP_URL);
  await writeFile(join(directory, 'llms-sitemap.md'), sitemap);
  const urls = selectDocumentationUrls(sitemap);
  console.log(`Fetching ${urls.length} documentation pages...`);
  const entries = await mapConcurrent(urls, 8, async (url, index) => {
    const markdown = await fetchText(url);
    const file = `${sha256(url).slice(0, 20)}.md`;
    await writeFile(join(directory, 'pages', file), markdown);
    if ((index + 1) % 100 === 0) console.log(`Fetched ${index + 1}/${urls.length}`);
    return { url, file, sha256: sha256(markdown) };
  });
  const index = { sitemap: { url: SITEMAP_URL, sha256: sha256(sitemap) }, pages: entries };
  await writeFile(join(directory, 'index.json'), `${JSON.stringify(index, null, 2)}\n`);
}

async function loadDocumentationCache(directory) {
  const indexPath = join(directory, 'index.json');
  if (!(await fileExists(indexPath))) {
    throw new Error(`Documentation cache is missing ${indexPath}. Run with --fetch-docs.`);
  }
  const index = JSON.parse(await readFile(indexPath, 'utf8'));
  const pages = await mapConcurrent(index.pages, 16, async (entry) => ({
    url: entry.url,
    markdown: await readFile(join(directory, 'pages', entry.file), 'utf8')
  }));
  return { index, pages };
}

async function downloadFile(url, path) {
  const response = await fetch(url, {
    headers: { 'user-agent': 'Chatto compatibility registry generator' }
  });
  if (!response.ok) throw new Error(`Failed to download ${url}: ${response.status}`);
  await writeFile(path, Buffer.from(await response.arrayBuffer()));
}

async function downloadAndExtractNpmPackage(root, name, version) {
  const metadata = await (
    await fetch(`https://registry.npmjs.org/${encodeURIComponent(name)}/${version}`)
  ).json();
  const safeName = name.replace(/^@/, '').replace('/', '-');
  const destination = join(root, 'npm', `${safeName}-${version}`);
  await rm(destination, { recursive: true, force: true });
  await mkdir(destination, { recursive: true });
  const archive = join(destination, 'package.tgz');
  await downloadFile(metadata.dist.tarball, archive);
  await execFileAsync('tar', ['-xzf', archive, '-C', destination]);
}

async function downloadAndExtractPythonPackage(root, name, version) {
  const metadata = await (await fetch(`https://pypi.org/pypi/${name}/${version}/json`)).json();
  const source = metadata.urls.find((item) => item.packagetype === 'sdist');
  if (!source) throw new Error(`No source distribution for ${name} ${version}`);
  const destination = join(root, 'python');
  await mkdir(destination, { recursive: true });
  const archive = join(destination, basename(new URL(source.url).pathname));
  await downloadFile(source.url, archive);
  await execFileAsync('tar', ['-xzf', archive, '-C', destination]);
}

async function downloadAndExtractJavaPackage(root, name, version) {
  const destination = join(root, 'java', name);
  await rm(destination, { recursive: true, force: true });
  await mkdir(destination, { recursive: true });
  const archive = join(root, 'java', `${name}-${version}-sources.jar`);
  const url = `https://repo1.maven.org/maven2/com/slack/api/${name}/${version}/${name}-${version}-sources.jar`;
  await downloadFile(url, archive);
  await execFileAsync('unzip', ['-q', '-o', archive, '-d', destination]);
}

async function refreshSdkSources(directory) {
  await rm(directory, { recursive: true, force: true });
  await mkdir(directory, { recursive: true });
  for (const [name, version] of Object.entries(SDK_VERSIONS.node)) {
    console.log(`Fetching ${name} ${version}...`);
    await downloadAndExtractNpmPackage(directory, name, version);
  }
  for (const [name, version] of Object.entries(SDK_VERSIONS.python)) {
    console.log(`Fetching ${name} ${version}...`);
    await downloadAndExtractPythonPackage(directory, name, version);
  }
  for (const [name, version] of Object.entries(SDK_VERSIONS.java)) {
    console.log(`Fetching Java ${name} ${version}...`);
    await downloadAndExtractJavaPackage(directory, name, version);
  }
}

async function walkFiles(root) {
  const files = [];
  async function walk(directory) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) await walk(path);
      else files.push(path);
    }
  }
  await walk(root);
  return files;
}

function findFileBySuffix(files, suffix, contains = null) {
  return files.find((file) => file.endsWith(suffix) && (!contains || file.includes(contains)));
}

function normalizedSdkMethodName(value) {
  return value.toLowerCase().replaceAll(/[^a-z0-9]/g, '');
}

function parsePythonSignatures(source) {
  const signatures = new Map();
  const starts = [...source.matchAll(/^ {4}def ([A-Za-z][A-Za-z0-9_]*)\(/gm)];
  for (const [index, start] of starts.entries()) {
    const end = index + 1 < starts.length ? starts[index + 1].index : source.length;
    const block = source.slice(start.index, end);
    const signature = block.match(
      /^ {4}def ([A-Za-z][A-Za-z0-9_]*)\(([\s\S]*?)^ {4}\) -> ([^:\n]+):/m
    );
    const method = block.match(/api_call\(\s*["']([^"']+)["']/)?.[1];
    if (!method || !signature) continue;
    signatures.set(method.toLowerCase(), {
      function: signature[1],
      parameters: signature[2].trim().replaceAll(/\s+/g, ' '),
      return_type: signature[3].trim()
    });
  }
  return signatures;
}

function parseNodeSignatures(source, methodNames) {
  const methodsByNormalizedName = new Map(
    methodNames.map((method) => [normalizedSdkMethodName(method), method])
  );
  const signatures = new Map();
  for (const match of source.matchAll(
    /^\s*([A-Za-z][A-Za-z0-9_]*):\s*(Method(?:WithRequiredArgument|WithOptionalArgument)?<[^;]+>);/gm
  )) {
    const typeNames = [...match[2].matchAll(/\b([A-Z][A-Za-z0-9]*(?:Arguments|Response))\b/g)].map(
      (typeMatch) => typeMatch[1]
    );
    const argumentType = typeNames.find((name) => name.endsWith('Arguments')) ?? null;
    const responseType = typeNames.find((name) => name.endsWith('Response')) ?? null;
    const normalizedArgument = argumentType
      ? normalizedSdkMethodName(argumentType.replace(/Arguments$/, ''))
      : null;
    const normalizedResponse = responseType
      ? normalizedSdkMethodName(responseType.replace(/Response$/, ''))
      : null;
    const normalizedLeaf = normalizedSdkMethodName(match[1]);
    let method = normalizedResponse ? methodsByNormalizedName.get(normalizedResponse) : null;
    method ||= normalizedArgument ? methodsByNormalizedName.get(normalizedArgument) : null;
    if (!method) {
      const candidates = methodNames.filter(
        (name) => normalizedSdkMethodName(name.split('.').at(-1)) === normalizedLeaf
      );
      if (candidates.length === 1) [method] = candidates;
      else if (normalizedArgument) {
        const argumentCandidates = candidates.filter((name) =>
          normalizedArgument.includes(normalizedSdkMethodName(name))
        );
        if (argumentCandidates.length === 1) [method] = argumentCandidates;
      }
    }
    if (!method) continue;
    signatures.set(method.toLowerCase(), {
      property: match[1],
      declaration: match[2],
      argument_type: argumentType,
      response_type: responseType
    });
  }
  return signatures;
}

function parseJavaSignatures(source, methodNames) {
  const methodsByNormalizedName = new Map(
    methodNames.map((method) => [normalizedSdkMethodName(method), method])
  );
  const signatures = new Map();
  for (const match of source.matchAll(
    /^\s*([A-Za-z][A-Za-z0-9]*Response)\s+([a-z][A-Za-z0-9]*)\(\s*([A-Za-z][A-Za-z0-9]*Request) req\)/gm
  )) {
    const method = methodsByNormalizedName.get(normalizedSdkMethodName(match[2]));
    if (!method) continue;
    signatures.set(method.toLowerCase(), {
      function: match[2],
      request_type: match[3],
      response_type: match[1]
    });
  }
  return signatures;
}

async function parseSdkSources(directory) {
  if (!(await fileExists(directory)))
    throw new Error(`SDK source directory does not exist: ${directory}`);
  const files = await walkFiles(directory);
  const nodeMethodsPath = findFileBySuffix(files, '/dist/methods.js', 'web-api');
  const nodeMethodsTypesPath = findFileBySuffix(files, '/dist/methods.d.ts', 'web-api');
  const nodeBlocksPath = findFileBySuffix(files, '/dist/block-kit/blocks.d.ts', 'types');
  const nodeElementsPath = findFileBySuffix(files, '/dist/block-kit/block-elements.d.ts', 'types');
  const pythonClientPath = findFileBySuffix(files, '/slack_sdk/web/client.py');
  const pythonAsyncClientPath = findFileBySuffix(files, '/slack_sdk/web/async_client.py');
  const pythonBlocksPath = findFileBySuffix(files, '/slack_sdk/models/blocks/blocks.py');
  const javaMethodsPath = findFileBySuffix(files, '/com/slack/api/methods/Methods.java');
  const javaMethodsClientPath = findFileBySuffix(
    files,
    '/com/slack/api/methods/MethodsClient.java'
  );
  const required = {
    nodeMethodsPath,
    nodeMethodsTypesPath,
    nodeBlocksPath,
    nodeElementsPath,
    pythonClientPath,
    pythonBlocksPath,
    javaMethodsPath,
    javaMethodsClientPath
  };
  for (const [name, path] of Object.entries(required)) {
    if (!path) throw new Error(`Could not locate ${name} under ${directory}`);
  }

  const nodeMethodsSource = await readFile(nodeMethodsPath, 'utf8');
  const pythonClientSource = await readFile(pythonClientPath, 'utf8');
  const pythonMethodsSource = `${pythonClientSource}\n${pythonAsyncClientPath ? await readFile(pythonAsyncClientPath, 'utf8') : ''}`;
  const nodeMethods = uniqueSorted(
    [
      ...nodeMethodsSource.matchAll(
        /bindApiCall(?:WithOptionalArgument)?\(this,\s*['"]([^'"]+)['"]\)/g
      )
    ].map((match) => match[1])
  );
  const pythonMethods = uniqueSorted(
    [...pythonMethodsSource.matchAll(/api_call\(\s*["']([^"']+)["']/g)].map((match) => match[1])
  );
  const javaMethodsSource = await readFile(javaMethodsPath, 'utf8');
  const javaMethods = uniqueSorted(
    [
      ...javaMethodsSource.matchAll(
        /public static final String [A-Z0-9_]+\s*=\s*"([A-Za-z0-9_.]+)";/g
      )
    ].map((match) => match[1])
  );
  const methodSignatures = {
    node: parseNodeSignatures(await readFile(nodeMethodsTypesPath, 'utf8'), nodeMethods),
    python: parsePythonSignatures(pythonClientSource),
    java: parseJavaSignatures(await readFile(javaMethodsClientPath, 'utf8'), javaMethods)
  };

  const nodeBlocksSource = await readFile(nodeBlocksPath, 'utf8');
  const nodeElementsSource = await readFile(nodeElementsPath, 'utf8');
  const pythonBlocksSource = await readFile(pythonBlocksPath, 'utf8');
  const nodeBlocks = uniqueSorted(
    [...nodeBlocksSource.matchAll(/\btype:\s*'([a-z][a-z0-9_]*)'/g)].map((match) => match[1])
  );
  const nodeElements = uniqueSorted(
    [...nodeElementsSource.matchAll(/\btype:\s*'([a-z][a-z0-9_]*)'/g)].map((match) => match[1])
  );
  const pythonBlocks = uniqueSorted(
    [
      ...pythonBlocksSource.matchAll(
        /^class\s+\w+Block\([^\n]+\):[\s\S]*?^\s{4}type\s*=\s*["']([^"']+)["']/gm
      )
    ].map((match) => match[1])
  );

  const sdkConfigurationEvidence = await collectSdkConfigurationEvidence(files, directory);
  const parsedSourceFiles = await Promise.all(
    [
      nodeMethodsPath,
      nodeMethodsTypesPath,
      nodeBlocksPath,
      nodeElementsPath,
      pythonClientPath,
      pythonAsyncClientPath,
      pythonBlocksPath,
      javaMethodsPath,
      javaMethodsClientPath
    ]
      .filter(Boolean)
      .map(async (path) => {
        const source = await readFile(path, 'utf8');
        return { path: relative(directory, path), sha256: sha256(source) };
      })
  );
  return {
    methods: { node: nodeMethods, python: pythonMethods, java: javaMethods },
    method_signatures: methodSignatures,
    block_kit: {
      node_blocks: nodeBlocks,
      node_elements: nodeElements,
      python_blocks: pythonBlocks
    },
    configuration: sdkConfigurationEvidence,
    parsed_source_files: parsedSourceFiles,
    source_versions: SDK_VERSIONS
  };
}

async function collectSdkConfigurationEvidence(files, root) {
  const evidenceSpecs = [
    ['node_web_api_host', '/dist/WebClient.d.ts', 'web-api', /slackApiUrl\?: string/],
    [
      'node_oauth_authorize',
      '/dist/install-provider-options.d.ts',
      'oauth',
      /authorizationUrl\?: string/
    ],
    ['node_bolt_host', '/dist/App.d.ts', 'bolt', /clientOptions\?: WebClientOptions/],
    [
      'node_socket_host',
      '/dist/src/SocketModeOptions.d.ts',
      'socket-mode',
      /clientOptions\?: Omit<WebClientOptions/
    ],
    ['python_web_api_host', '/slack_sdk/web/base_client.py', null, /base_url/],
    [
      'python_oauth_authorize',
      '/slack_sdk/oauth/authorize_url_generator/__init__.py',
      null,
      /authorization_url/
    ],
    [
      'python_bolt_oauth_authorize',
      '/slack_bolt/oauth/oauth_settings.py',
      null,
      /authorization_url/
    ],
    [
      'java_web_api_host',
      '/com/slack/api/methods/MethodsClient.java',
      null,
      /setEndpointUrlPrefix/
    ],
    [
      'java_bolt_authorize',
      '/com/slack/api/bolt/App.java',
      null,
      /https:\/\/slack\.com\/oauth\/v2\/authorize/
    ]
  ];
  const evidence = [];
  for (const [name, suffix, contains, pattern] of evidenceSpecs) {
    const path = findFileBySuffix(files, suffix, contains);
    if (!path) continue;
    const source = await readFile(path, 'utf8');
    evidence.push({
      name,
      present: pattern.test(source),
      source_path: relative(root, path),
      source_sha256: sha256(source)
    });
  }
  return evidence;
}

function mergeMethods(documentedMethods, sdkMethods, sdkSignatures) {
  const methods = new Map(documentedMethods.map((method) => [method.name.toLowerCase(), method]));
  for (const [sdk, names] of Object.entries(sdkMethods)) {
    for (const name of names) {
      const key = name.toLowerCase();
      let method = methods.get(key);
      if (!method) {
        const deprecated = inferDeprecated(name, {}, '', false);
        method = {
          name,
          title_sha256: null,
          summary_sha256: null,
          documented: false,
          source: null,
          sdk_presence: {},
          sdk_signatures: {},
          arguments: [],
          token_types: [],
          scopes: { bot: [], user: [], all: [] },
          rate_tier: null,
          content_types: [],
          response_examples: [],
          response_shapes: [],
          errors: [],
          warnings: [],
          deprecated,
          compatibility: methodPolicy(name, { documented: false, deprecated })
        };
        methods.set(key, method);
      }
      method.sdk_presence[sdk] = true;
      method.sdk_signatures ||= {};
      method.sdk_signatures[sdk] = sdkSignatures[sdk].get(key) ?? null;
    }
  }
  for (const method of methods.values()) {
    method.sdk_signatures ||= {};
    for (const sdk of Object.keys(sdkMethods)) {
      method.sdk_presence[sdk] ||= false;
      method.sdk_signatures[sdk] ??= sdkSignatures[sdk].get(method.name.toLowerCase()) ?? null;
    }
  }
  return [...methods.values()].sort((left, right) => left.name.localeCompare(right.name));
}

function mergeBlockKit(documented, sdkTypes, kind) {
  const objects = new Map(documented.map((object) => [object.wire_type, object]));
  for (const [sdk, types] of Object.entries(sdkTypes)) {
    for (const wireType of types) {
      let object = objects.get(wireType);
      if (!object) {
        object = {
          name: wireType.replaceAll('_', '-'),
          wire_type: wireType,
          title_sha256: null,
          summary_sha256: null,
          source: { url: null, sha256: null, kind: 'sdk_only' },
          sdk_presence: {},
          available_in_surfaces: [],
          compatible_elements: [],
          fields: [],
          compatibility: blockKitPolicy(kind, wireType)
        };
        objects.set(wireType, object);
      }
      object.sdk_presence[sdk] = true;
    }
  }
  for (const object of objects.values()) {
    for (const sdk of Object.keys(sdkTypes)) object.sdk_presence[sdk] ||= false;
  }
  return [...objects.values()].sort((left, right) => left.wire_type.localeCompare(right.wire_type));
}

const registryReadme = `# Compatibility Contract Registry

This directory contains generated contract facts for the app compatibility platform. The registry combines the current public Markdown reference with pinned published SDK source. It does not contain a support claim; see [the generated coverage matrix](../../docs/compat/coverage-matrix.md).

Generate a fresh source snapshot and registry:

\`\`\`sh
mise x -- pnpm app-compat:generate-registry
\`\`\`

Validate the checked-in registry and matrix:

\`\`\`sh
mise x -- pnpm app-compat:check-registry
\`\`\`

The generator records source URLs, versions, and SHA-256 hashes. It stores structured names, fields, shapes, errors, and constraints instead of complete copies of reference pages. String values in payload and response examples are sanitized, except for wire discriminators, errors, and warnings.
`;

const sourceConflicts = [
  {
    topic: 'Message shortcut payload',
    decision: 'Use message_action.',
    evidence:
      'The payload reference and Bolt types agree; an interaction guide uses message_actions.'
  },
  {
    topic: 'views.publish argument',
    decision: 'Use view and accept views as a compatibility alias.',
    evidence: 'The examples use view while one argument list uses views.'
  },
  {
    topic: 'Metadata event scope',
    decision:
      'Accept metadata.message:read and the older message_metadata:read alias in manifests.',
    evidence: 'The scope registry uses metadata.message:read.'
  },
  {
    topic: 'Approval metadata',
    decision: 'Use approval and preserve slack_approval as unknown input.',
    evidence: 'Schema and examples use approval.'
  },
  {
    topic: 'Workflow input_parameters',
    decision: 'Accept the properties object form.',
    evidence: 'Examples use an object although one table describes an array.'
  },
  {
    topic: 'Block inventory',
    decision: 'Use the union of documentation and published SDK source.',
    evidence: 'Documentation and SDK releases contain different new and legacy object types.'
  },
  {
    topic: 'Manifest extensions',
    decision: 'Preserve unknown keys and round-trip them.',
    evidence: 'Live exports can contain fields that are absent from the schema reference.'
  }
];

async function writeJson(path, value) {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    await format(JSON.stringify(value), { ...(await resolveConfig(path)), filepath: path })
  );
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const docsDir = resolve(options.docsDir);
  const sdkDir = resolve(options.sdkDir);
  const outputDir = resolve(options.outputDir);
  if (options.fetchDocs) await refreshDocumentationCache(docsDir);
  if (options.fetchSdks) await refreshSdkSources(sdkDir);

  const [{ index: documentationIndex, pages }, sdk] = await Promise.all([
    loadDocumentationCache(docsDir),
    parseSdkSources(sdkDir)
  ]);
  const sdkCanonicalNames = new Map(
    Object.values(sdk.methods)
      .flat()
      .map((name) => [name.toLowerCase(), name])
  );

  const methodPages = pages.filter((page) =>
    /^\/reference\/methods\/[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const eventPages = pages.filter((page) =>
    /^\/reference\/events\/(?:[^/]+\/)*[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const scopePages = pages.filter((page) =>
    /^\/reference\/scopes\/[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const blockPages = pages.filter((page) =>
    /^\/reference\/block-kit\/blocks\/[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const elementPages = pages.filter((page) =>
    /^\/reference\/block-kit\/block-elements\/[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const compositionPages = pages.filter((page) =>
    /^\/reference\/block-kit\/composition-objects\/[^/]+\.md$/.test(new URL(page.url).pathname)
  );
  const manifestPage = pages.find(
    (page) => new URL(page.url).pathname === '/reference/app-manifest.md'
  );
  if (!manifestPage)
    throw new Error('The documentation cache does not contain the app manifest reference.');

  const methods = mergeMethods(
    methodPages.map((page) => parseMethodPage(page, sdkCanonicalNames)),
    sdk.methods,
    sdk.method_signatures
  );
  const events = eventPages
    .map(parseEventPage)
    .sort((left, right) => left.name.localeCompare(right.name));
  const scopes = scopePages
    .map(parseScopePage)
    .sort((left, right) => left.name.localeCompare(right.name));
  const blocks = mergeBlockKit(
    blockPages.map((page) => parseBlockKitPage(page, 'block')),
    { node: sdk.block_kit.node_blocks, python: sdk.block_kit.python_blocks },
    'block'
  );
  const elements = mergeBlockKit(
    elementPages.map((page) => parseBlockKitPage(page, 'element')),
    { node: sdk.block_kit.node_elements },
    'element'
  );
  const compositionObjects = compositionPages
    .map((page) => parseBlockKitPage(page, 'composition_object'))
    .sort((left, right) => left.wire_type.localeCompare(right.wire_type));
  const manifestKeys = parseManifestFields(manifestPage).sort((left, right) =>
    left.path.localeCompare(right.path)
  );

  const coverageItems = [
    ...methods,
    ...events,
    ...scopes,
    ...blocks,
    ...elements,
    ...compositionObjects,
    ...manifestKeys,
    ...surfacePolicies.map((surface) => ({ compatibility: surface }))
  ];
  const coverage = { implemented: 0, partial: 0, unsupported: 0 };
  for (const item of coverageItems) coverage[item.compatibility.status] += 1;

  const source = {
    documentation: {
      sitemap_url: documentationIndex.sitemap.url,
      sitemap_sha256: documentationIndex.sitemap.sha256,
      selected_page_count: documentationIndex.pages.length
    },
    sdks: sdk.source_versions
  };
  const summary = {
    schema_version: 1,
    source,
    counts: {
      methods: methods.length,
      documented_methods: methods.filter((method) => method.documented).length,
      sdk_only_methods: methods.filter((method) => !method.documented).length,
      events: events.length,
      scopes: scopes.length,
      blocks: blocks.length,
      elements: elements.length,
      composition_objects: compositionObjects.length,
      manifest_keys: manifestKeys.length
    },
    coverage
  };

  await rm(outputDir, { recursive: true, force: true });
  await mkdir(outputDir, { recursive: true });
  await Promise.all([
    writeFile(join(outputDir, 'README.md'), registryReadme),
    writeJson(join(outputDir, 'summary.json'), summary),
    writeJson(join(outputDir, 'methods.json'), { schema_version: 1, source, methods }),
    writeJson(join(outputDir, 'events.json'), { schema_version: 1, source, events }),
    writeJson(join(outputDir, 'scopes.json'), { schema_version: 1, source, scopes }),
    writeJson(join(outputDir, 'blocks.json'), { schema_version: 1, source, blocks }),
    writeJson(join(outputDir, 'elements.json'), { schema_version: 1, source, elements }),
    writeJson(join(outputDir, 'composition-objects.json'), {
      schema_version: 1,
      source,
      composition_objects: compositionObjects
    }),
    writeJson(join(outputDir, 'manifest-keys.json'), {
      schema_version: 1,
      source: {
        ...source,
        manifest: { url: manifestPage.url, sha256: sha256(manifestPage.markdown) }
      },
      manifest_keys: manifestKeys
    }),
    writeJson(join(outputDir, 'sdk-compatibility.json'), {
      schema_version: 1,
      source,
      method_counts: Object.fromEntries(
        Object.entries(sdk.methods).map(([name, methods]) => [name, methods.length])
      ),
      signature_counts: Object.fromEntries(
        Object.entries(sdk.method_signatures).map(([name, signatures]) => [name, signatures.size])
      ),
      parsed_source_files: sdk.parsed_source_files,
      api_and_oauth_configuration_evidence: sdk.configuration
    }),
    writeJson(join(outputDir, 'source-conflicts.json'), {
      schema_version: 1,
      conflicts: sourceConflicts
    }),
    writeJson(join(outputDir, 'surfaces.json'), { schema_version: 1, surfaces: surfacePolicies })
  ]);

  console.log(`Generated registry in ${relative(process.cwd(), outputDir)}`);
  console.log(JSON.stringify(summary.counts));
}

main().catch((error) => {
  console.error(error.stack || error);
  process.exitCode = 1;
});
