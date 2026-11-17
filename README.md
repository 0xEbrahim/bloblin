# Bloblin

Bloblin is a self-hosted object storage project written in Go. It starts as one
process on one machine. This foundation runs the native HTTP server; object
storage is the next milestone.

## Prerequisites

- Go 1.27 or newer (see `go.mod`).
- No database server or containers are needed. The first Go build downloads Gin
  and its transitive dependencies using `go.mod` and `go.sum`.
- GNU Make is optional. The Go commands below also work directly on Windows.

The existing module path is `bloblin`. When adopting a hosted repository, update
the module path and internal imports together to match its actual location.

## Run

From the repository root:

```sh
go run ./cmd/bloblin
```

The default address is `:8080` (all interfaces). For a loopback-only listener:

```sh
BLOBLIN_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/bloblin
```

In Windows PowerShell:

```powershell
$env:BLOBLIN_LISTEN_ADDR = '127.0.0.1:8080'
go run ./cmd/bloblin
```

Startup validates configuration, creates the data root if necessary, binds the
listener, and marks the application ready. A file in place of the data directory
or an unavailable address produces an actionable error and a nonzero exit status.
The data root is currently empty: startup does not open a catalog or write blobs,
and readiness does not certify disk writability or storage durability.

Logs are JSON via `log/slog` on stdout; configuration parsing errors go to stderr.
Each request receives a server-generated `X-Request-ID`. Request logs include that
ID, method, path (without the query), HTTP status, `duration_ms`, and
`response_bytes` (body bytes accepted by the writer, excluding headers).

Stop with Ctrl+C, or send SIGINT/SIGTERM to the built binary on systems that
support these signals. Readiness turns false before the listener closes. Active
requests keep their contexts and can finish until the shutdown deadline; after
that, remaining connections are forcibly closed and the process exits nonzero.
For service deployments, run the built binary directly so it receives signals.

## Health endpoints

```sh
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

On Windows, use `curl.exe` for the same commands, or:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

| Request | HTTP status | JSON body |
| --- | --- | --- |
| `GET /healthz` | 200 | `{"status":"ok"}` |
| `GET /readyz` when accepting work | 200 | `{"status":"ready"}` |
| `GET /readyz` before startup completes or during draining, if reachable | 503 | Error with code `not_ready` |
| Other methods on either endpoint | 405 | Error with code `method_not_allowed`; `Allow: GET` |
| Unknown paths | 404 | Error with code `not_found` |

Implemented endpoints return JSON with `Cache-Control: no-store`. Errors have one
shape, including the same ID as the response header:

```json
{"error":{"code":"not_ready","message":"server is not ready","request_id":"..."}}
```

Middleware recovers handler panics, logs the stack and request ID, and returns a
generic `500 internal_error`. If a response is already committed, it aborts the
stream: sending a replacement JSON body would corrupt the response. Neither
request nor response bodies are buffered by the middleware. Bodies can stream
through `io.Copy` and Gin's response writer; flushing remains available, and
`http.ResponseController` can unwrap the writer for deadlines, full duplex, and
other supported transport capabilities.
Malformed HTTP rejected by `net/http` before routing uses its own error format.

## Configuration

Settings come from the process environment. Unset settings use these defaults;
explicit empty values are invalid. Durations use Go syntax, such as `500ms`, `5s`,
or `1m`. `.env.example` is a reference only: Go and this application **do not load
`.env` files automatically**. Export variables in your shell or service manager.

| Variable | Default | Meaning / validation |
| --- | --- | --- |
| `BLOBLIN_LISTEN_ADDR` | `:8080` | `host:port`; numeric port 0–65535. Port 0 selects an ephemeral port, reported in the startup log. IPv6 uses brackets, e.g. `[::1]:8080`. |
| `BLOBLIN_DATA_DIR` | `./data` | Nonblank directory path, relative to the working directory unless absolute; created at startup. |
| `BLOBLIN_LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARN`, or `ERROR`, case-insensitive. |
| `BLOBLIN_HTTP_READ_HEADER_TIMEOUT` | `5s` | Positive deadline for reading request headers. |
| `BLOBLIN_HTTP_READ_TIMEOUT` | `0s` | Nonnegative deadline for reading the entire request; zero disables it. |
| `BLOBLIN_HTTP_WRITE_TIMEOUT` | `0s` | Nonnegative response write deadline; zero disables it. |
| `BLOBLIN_HTTP_IDLE_TIMEOUT` | `60s` | Positive keep-alive idle timeout. |
| `BLOBLIN_SHUTDOWN_TIMEOUT` | `30s` | Positive grace period for active requests to finish. |

Read and write timeouts default to zero because fixed deadlines would cap the
duration of future large uploads and downloads. Header and idle timeouts still
limit idle connections. Transfer-specific limits will be designed with the object
API. Existing data-directory permissions are preserved; new directories request
mode `0700` where the operating system supports Unix permissions.

## Architecture

```text
cmd/bloblin/          Configuration, logger, signals, exit status
internal/app/        Explicit dependency wiring and server lifecycle
internal/config/     Environment parsing and validation
internal/api/native/ HTTP routing, JSON responses, request middleware
docs/adr/            Architecture decision records
```

`main` constructs ordinary Go values and hands lifecycle ownership to `app`.
There is no dependency injection framework or global service registry. The HTTP
adapter currently depends only on a logger and a readiness function. Storage
logic belongs outside handlers.

The native adapter uses `gin.New()` with explicitly registered request ID,
request logging, and panic recovery middleware. Gin runs in release mode so
debug route output does not mix with the structured logs; application verbosity
is controlled by `BLOBLIN_LOG_LEVEL`. The Gin engine is an `http.Handler` on the
configured `http.Server`; `app` retains control of timeouts and shutdown.

Within `internal/api/native`, `router.go` registers routes, `handler.go` contains
the health/error handlers and JSON responses, and `middleware.go` contains the
middleware. `/healthz` and `/readyz` stay outside the `/v1` application group.
There are no application routes yet. As bucket, object, and upload features are
implemented, add their handlers and route registration by feature in this package
and register them on `/v1`; do not create placeholder endpoints.

Gin-specific types stay inside the HTTP adapter. Future handlers translate HTTP
inputs into ordinary service values and pass `c.Request.Context()` to service
calls for deadlines and cancellation. Uploads pass `c.Request.Body` as a stream;
downloads copy service readers to `c.Writer` without reading whole files into
memory. Services must not depend on `*gin.Context`, and streaming routes must not
use binding helpers that read and retain the entire object body.

Future boundaries are documented here and will become packages when implemented:

| Future package | Responsibility |
| --- | --- |
| `internal/object` | Transport-independent object operations and orchestration; owns the narrow storage/catalog interfaces it consumes. No HTTP or SQLite dependency. |
| `internal/blob/fs` | Local immutable blob files, internal blob identifiers, streaming, durable publication, and file cleanup primitives. Implements the object service's blob interface. |
| `internal/catalog/sqlite` | SQLite metadata and transaction handling, implementing catalog interfaces consumed by the object service. Bytes remain outside SQLite. |
| `internal/recovery` | Reconciliation of interrupted operations and orphaned blobs, using explicit storage/catalog capabilities. Lifecycle belongs to `app`. |

`app` will construct these implementations, inject them into the object service,
and inject the service into transport adapters. The native API comes first. S3
is the final adapter and must share the same core behavior. See the
[storage invariants](docs/invariants.md) and decisions on
[single-node scope](docs/adr/0001-single-node.md),
[filesystem and SQLite](docs/adr/0002-filesystem-and-sqlite.md),
[transport-independent logic](docs/adr/0003-transport-independent-core.md), and
[S3 sequencing](docs/adr/0004-s3-final-adapter.md).

## Development

With GNU Make on a POSIX shell:

```sh
make run          # go run ./cmd/bloblin
make build        # bin/bloblin
make fmt          # format Go sources
make fmt-check    # fail if formatting changes are needed
make vet
make test
make check        # formatting, vet, tests, build
```

Equivalent commands in PowerShell (no Make required):

```powershell
go run ./cmd/bloblin
go build -o bin/bloblin.exe ./cmd/bloblin
gofmt -w cmd internal
$unformatted = gofmt -l cmd internal
if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
if ($unformatted) { $unformatted; throw 'Run gofmt -w cmd internal' }
go vet ./...
go test ./...
```

On other systems the same Go commands work; use `bin/bloblin` for the build
output. Run `./bin/bloblin` or `.\bin\bloblin.exe` to start the built server.
CI checks formatting, vets, tests, and builds on both Ubuntu and Windows.
Tests cover configuration errors, health/readiness transitions, request logging,
panic recovery, streaming, request cancellation, startup failures, and active-request shutdown with
both successful draining and deadline expiry.

## Current scope and next milestone

Implemented: configuration, logging, native health/readiness HTTP endpoints,
streaming-safe middleware, and application startup/shutdown.

Deferred: bucket/object operations, filesystem storage algorithms, the SQLite
catalog, multipart uploads, authentication, recovery workers, and S3. Clustering,
replication, erasure coding, versioning, and lifecycle policies are also deferred.
No object durability or consistency guarantees exist yet.

Next milestone: define the transport-independent object service contracts and
implement the filesystem blob publication primitives with streaming and crash
durability tests. Add the SQLite catalog and native object operations in subsequent
increments; S3 remains the final phase.
