# ADR 0004: Implement S3 compatibility as the final adapter

Status: Accepted

## Context

S3 has a substantial protocol surface, including signing, error formats, and
multipart semantics. Implementing it before the storage core would mix protocol
work with unproven persistence behavior.

## Decision

Build the native HTTP API first. Establish the object service and local storage
behavior before adding S3 exclusively in the final phase. The eventual S3
adapter invokes the same transport-independent service and never owns a separate
storage implementation.

## Consequences

Native routes can evolve while core semantics are tested. The foundation offers
no S3 endpoints or compatibility claims. S3 protocol tests and compatibility scope
will be defined in that phase. Existing native routes remain independently useful;
S3 does not determine the core's package dependencies.
