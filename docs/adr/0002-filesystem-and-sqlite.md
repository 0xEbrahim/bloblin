# ADR 0002: Store immutable blobs in files and metadata in SQLite

Status: Accepted

## Context

Large object streams and transactional metadata have different access patterns.
A single-node deployment should not require an external database service.

## Decision

Use local immutable filesystem blobs for bytes and an embedded SQLite catalog for
bucket/object metadata and references to internal blob identifiers. Logical keys
never become paths. The object service coordinates durable blob publication
before committing the visible catalog reference.

## Consequences

Bytes can stream without passing through SQLite. Catalog transactions provide the
future atomic replacement boundary, but filesystem and SQLite updates cannot be
one shared transaction. Crashes may leave orphans, so recovery and reclamation
must be designed explicitly. Filesystem synchronization and SQLite settings need
platform-specific validation before durability claims. This foundation adds no
SQLite driver, schema, or blob algorithm; see [invariants](../invariants.md).
