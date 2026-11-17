# ADR 0001: Start with one process on one machine

Status: Accepted

## Context

Bloblin needs an understandable, self-hosted starting point with explicit local
storage semantics. Distributed ownership and failure handling would add separate
consistency and operational problems before the local storage model is proven.

## Decision

Initially run one server process on one machine with exclusive ownership of its
data directory and metadata catalog. Application lifecycle, HTTP serving, and
future workers share that process. Document strong read-after-write requirements
within this boundary.

## Consequences

Deployment and debugging remain simple. Multiple processes sharing a data root
are unsupported; this foundation does not implement a process lock. Clustering,
replication, and erasure coding are deferred. There is no high-availability
guarantee, and machine or disk loss requires a future backup/restore strategy.
