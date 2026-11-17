# ADR 0003: Keep object behavior independent of transport

Status: Accepted

## Context

Native HTTP and eventual S3 requests must share storage guarantees. Putting
storage decisions in handlers would duplicate policy and tie correctness to a
particular protocol.

## Decision

Place object orchestration in a future `internal/object` service. It will own the
narrow blob/catalog interfaces it consumes and use ordinary Go values, contexts,
and streaming I/O. Filesystem and SQLite packages implement those interfaces.
HTTP adapters translate requests, results, and errors; `internal/app` constructs
and injects the concrete dependencies and owns their lifecycle.

The native HTTP adapter uses Gin, initialized with `gin.New()` and explicit
request ID, request logging, and panic recovery middleware. Route registration
is separate from handlers. Health endpoints are unversioned; application routes
belong to `/v1` and are organized by feature as they are implemented. The engine
runs through the configured `http.Server` owned by `app`.

Gin types remain within `internal/api/native`. Handlers pass
`c.Request.Context()` into service calls and stream bodies through standard Go
readers and writers. Core services never receive `*gin.Context` or depend on Gin.

## Consequences

Core tests can exercise object semantics without HTTP. Adapters cannot bypass
publication or transaction ordering. The core needs no framework or speculative
generic storage interface. Create the object, blob, catalog, and recovery packages only
when implementation starts; this foundation contains the HTTP/lifecycle boundary.
