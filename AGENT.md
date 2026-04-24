# ApeTrader Agent Guide

This is the single repo-level instruction file for coding agents working in this project.

## Purpose

Use this repo to build and maintain the ApeTrader backend: a Go API for a multiplayer economic simulation with towns, traders, optional PostgreSQL persistence, and a minute-tick market engine.

When in doubt:
- Prefer clean, current code over legacy compatibility glue.
- Keep the domain model consistent across code, schema, docs, and tests.
- Finish changes end to end instead of leaving the repo in a mixed state.

## Core Architecture

The codebase is organized into four main layers:
- `internal/api`: HTTP handlers and request/response wiring using Gin.
- `internal/market`: game logic, simulation, pricing, and travel behavior.
- `internal/db`: PostgreSQL persistence and schema bootstrap.
- `internal/models`: shared domain types.

Important architectural conventions:
- Player accounts own one or more traders.
- Traders are the playable units and hold gameplay state such as balance, inventory, location, reputation, equipment, and travel status.
- Town simulation runs independently from trader auth and persistence.
- In DB mode, PostgreSQL is the source of persisted state.
- In non-DB mode, the app runs in memory.

## Code Style

- Use Go’s strong typing. Avoid `interface{}` unless it is genuinely necessary.
- Keep code modular. Do not blur responsibilities between `api`, `market`, `db`, and `models`.
- Prefer explicit domain types such as `Currency`, `WeightKg`, and `VolumeL`.
- Protect shared mutable state carefully. `Inventory` is thread-safe; do not assume the rest of the model is.
- Validate all trade operations for location, funds, and carrying constraints.

## Behavior Rules

- Dynamic prices are based on supply, demand, and trader reputation.
- Reputation effects should remain bounded; avoid extreme spread behavior.
- Bulletin board entries are snapshots and should remain stale-by-design until refreshed by the existing rules.
- Optional consumption should not create prosperity penalties.
- Tick-driven town state changes should remain deterministic and easy to trace.

## Database Rules

- This repo is not in production. Do not add backward-compatibility shims, schema backfills, migration glue, or legacy fallback code unless the user explicitly asks for it.
- Prefer updating `schema.sql` and the current code to match the intended model exactly.
- If a schema/model change invalidates old local state, favor resetting local data rather than carrying compatibility code.
- In DB mode, persist the state that the runtime mutates.
- Keep schema bootstrap simple and aligned with the current schema only.

## Workflow Expectations

When making a change:
- Update the implementation, tests, schema, and docs together when they are affected.
- Keep `TODO.md` current in the same change when work is completed or new follow-up work appears.
- Keep API artifacts in sync when routes or payloads change:
  - `openapi.yaml`
  - `postman_collection.json`
  - endpoint documentation in `readme.md`
- Prefer implementing a safe, well-scoped request directly rather than only describing how to do it.

## Build And Test

Common commands:

```bash
go test ./...
go run main.go
go build -o apetrader .
docker-compose up -d
docker-compose down -v
```

Notes:
- Docker Compose is the easiest way to run the API with PostgreSQL.
- If `DB_HOST` is unset or empty, the app may run in-memory depending on current startup behavior.
- Run tests after meaningful code changes, especially when touching `market`, `db`, or `api`.

## Source Of Truth

Use these files together:
- `readme.md` for project context and high-level rules.
- `schema.sql` for the current database shape.
- `openapi.yaml` for the API contract.
- `TODO.md` for active follow-up work.

## Pitfalls To Avoid

- Mixed ownership models, especially confusing player-account state with trader gameplay state.
- Partial renames that update code but not schema, tests, or API docs.
- Race conditions around tick processing and mutable inventories.
- Token handling mismatches between issued tokens and stored hashes.
- Changing domain terminology in one layer only.
