# ADR 0001: Backend and shared-service stack

- Status: Accepted
- Date: 2026-10-25

## Context

XFlickr needs a maintainable multi-user backend, durable crawl jobs, relational
data, and searchable photo metadata. Development and tests run on one
workstation with shared services; individual worktrees must remain isolated
without starting their own service containers.

## Decision

- Use Go for the backend and a single role-aware `xflickr` executable. Keep
  feature dependencies inward using domain, application, and adapter layers.
- Use PostgreSQL as the system of record and transaction boundary. Use `pgx` for
  database access, Goose for migrations, and `sqlc` for generated query code.
- Use Redis for shared cache and coordination needs. Put search behind the
  `SearchIndex` port, using shared Elasticsearch 9.x with PostgreSQL/pgvector as
  the fallback.
- Use the shared PostgreSQL and Redis services for development and integration
  tests. Give each worktree its own generated environment configuration and
  isolated database namespace. Do not use testcontainers or launch project-owned
  database/search/cache containers.
- Keep temporary downloads and thumbnails behind the `BlobStore` port on the WSL
  filesystem adapter first. An S3 adapter may be added later.
- Run captioning and embeddings through ports backed by native WSL Ollama in
  development; tests use fakes.
- The production web framework and frontend build stack are not established by
  the current repository skeleton; record that choice in a later ADR when
  selected.

## Consequences

- The domain and application layers remain testable without infrastructure.
  Adapters own database, cache, search, filesystem, and external API details.
- PostgreSQL migrations and generated query code are part of the backend
  workflow.
- Developers and integration tests depend on the shared workstation services and
  must follow `docs/ENVIRONMENT.md`; CI helpers must retain the documented
  runtime-wrapper behavior.
- Search and blob storage can change implementations behind their ports. The web
  stack remains an open decision.

## Alternatives considered

- **Per-worktree service containers:** rejected because all worktrees use the
  shared workstation services and isolated databases.
- **OpenSearch:** rejected because the shared search service is Elasticsearch
  9.x.
- **Garage for temporary files:** rejected because temporary files use the local
  filesystem adapter; S3 is optional later.
