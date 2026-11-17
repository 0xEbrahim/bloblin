# Intended storage invariants

These are requirements for the future object service and storage implementations.
The current foundation has no object operations, blob store, or metadata catalog;
it does **not** implement or prove the storage guarantees below.

## Bounded-memory streaming

Upload and download memory usage must be bounded independently of object size.
Adapters pass readers/writers and cancellation into the object service. The
filesystem implementation copies through bounded buffers and propagates partial
reads, write failures, cancellation, and client disconnects. Neither adapters nor
the service may load a whole object into memory. Metadata and explicit request
limits must also be bounded.

## Opaque keys never become filesystem paths

An object key is a logical identifier scoped to a bucket. It is never joined to a
data-directory path, normalized as a filesystem path, or passed to filesystem
operations. Keys such as `../x`, `/x`, or names containing platform separators
must never escape into path construction. The catalog maps logical keys to
internally generated blob identifiers; the filesystem adapter alone maps validated
internal identifiers to paths. Key encoding rules belong to transport adapters
and must preserve the core's key identity.

## Immutable published blobs

Once published, a blob's contents never change. Replacing an object creates a
new blob and changes a metadata reference. Readers that have already acquired a
blob must be allowed to finish, even if the key is replaced or deleted. Eventual
garbage collection must respect active readers and platform-specific file-handle
behavior. Unpublished temporary files are not readable objects.

## Durable data before visible metadata

The object service must finish writing and durably publishing the complete blob
before committing metadata that makes it visible. Durability includes the file
contents and the filesystem operations needed to make its name survive a crash;
an ordinary successful write or rename alone does not establish this guarantee.
The metadata transaction must itself be durable before acknowledging success.

Filesystem publication and SQLite transactions are separate commit domains. An
interrupted operation may leave an unreferenced blob, but committed metadata must
never point at an incomplete or unpublished blob. Recovery will reconcile
temporary files and orphans. Exact ordering, SQLite durability settings, supported
filesystems, and OS-specific synchronization guarantees must be established and
tested when these adapters are implemented. Disk-full, failed synchronization,
and ambiguous commit outcomes must never be silently reported as success.

## Atomic replacement

A successful overwrite changes the catalog reference in one transaction. Readers
observe either the previous complete object or the new complete object, never
partially written bytes or mixed metadata. Until the replacement commits, the
previous object remains the visible version. Concurrent mutations must have a
defined order at the catalog's commit boundary. Reclamation happens separately
and cannot invalidate the newly committed reference or readers of older blobs.
This implementation mechanism does not imply user-visible object versioning.

## Strong read-after-write on one node

After a write is acknowledged, a subsequent read or metadata lookup on that node
must observe that write, or a later committed mutation. Replacement and deletion
follow the same ordering rule. Reads already in progress may finish against the
blob they acquired before the mutation. Metadata queries and future listings must
not use stale caches that weaken these semantics. No cross-node or replication
guarantee is implied.

## What the foundation implements

The server validates configuration, prepares the data root, binds HTTP, exposes
process health/readiness, and drains active requests on shutdown. Middleware
streams through the response writer, preserves flushing and response-controller
access, and does not buffer bodies. Tests demonstrate streaming and lifecycle
behavior only. Readiness means startup has completed and HTTP work is accepted;
it does not mean any of the storage invariants have been implemented.

Future storage tests must exercise large streams, adversarial keys, concurrent
replacement/read behavior, failed I/O, and interruption at publication/commit
boundaries. Durability claims require evidence on each supported platform and
filesystem, beyond successful unit tests of normal operation.
