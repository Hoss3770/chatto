# Compatibility Contract Registry

This directory contains generated contract facts for the app compatibility platform. The registry combines the current public Markdown reference with pinned published SDK source. It does not contain a support claim; see [the generated coverage matrix](../../docs/compat/coverage-matrix.md).

Generate a fresh source snapshot and registry:

```sh
mise x -- pnpm app-compat:generate-registry
```

Validate the checked-in registry and matrix:

```sh
mise x -- pnpm app-compat:check-registry
```

The generator records source URLs, versions, and SHA-256 hashes. It stores structured names, fields, shapes, errors, and constraints instead of complete copies of reference pages. String values in payload and response examples are sanitized, except for wire discriminators, errors, and warnings.
