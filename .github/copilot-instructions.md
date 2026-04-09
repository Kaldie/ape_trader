# ApeTrader API Guidelines

## Code Style
- **Strict Typing**: Use Go's strong typing. No `interface{}` unless absolutely necessary.
- **Concurrency Safety**: Protect shared state with Mutexes; use channels for tick engine.
- **Validation**: Every trade validates location, funds, and physics (weight/volume).
- **Modular Design**: Separate `api`, `market`, `db`, `models` packages.

See [readme.md](readme.md) for AI agent coding instructions.

## Architecture
- **Four-layer structure**: `api` (HTTP/Gin), `market` (game logic), `db` (PostgreSQL optional), `models` (domain types).
- **In-memory game state**: Towns/players in `MarketEngine` maps; optional DB for traders.
- **1-minute tick goroutine**: Async simulation loop for refinement, bulletins, consumption.
- **Bearer token authentication**: Traders get opaque token (hashed in DB).
- **Dynamic pricing**: Real-time prices based on supply/demand + reputation.

See [readme.md](readme.md) for core game mechanics and [openapi.yaml](openapi.yaml) for API spec.

## Build and Test
- **Docker (recommended)**: `docker-compose up -d` (includes PostgreSQL auto-init).
- **Local**: `go run main.go` or `go build -o apetrader .`.
- **Cleanup**: `docker-compose down -v`.
- **Testing**: Currently manual testing only; start adding unit tests for validation logic.

See [DOCKER.md](DOCKER.md) for detailed setup.

## Conventions
- **Type aliases for domain values**: `Currency`, `WeightKg`, `VolumeL` as `int64` (avoids floating-point errors).
- **Thread-safe inventory only**: `Inventory` has mutex; Town fields not synchronized.
- **Bulletin board write-once**: Price snapshots per tick, 24-hour expiry.
- **No penalty for missing luxury items**: Optional consumption has no prosperity downside.
- **Reputation clamped ±50%**: Price adjustments limited to prevent extreme spreads.
- **DB optional**: If `DB_HOST=""`, runs in-memory; check `s.db != nil`.
- **DB town bootstrap**: In DB mode, prefer PostgreSQL for initial town/inventory/supply-demand loading; only fall back to `towns.json` if DB loading fails or returns no towns.
- **DB tick persistence**: In DB mode, minute-tick prosperity and town inventory mutations should be persisted back to PostgreSQL.
- **Keep `TODO.md` current**: When a task is completed or new follow-up work is introduced, update `TODO.md` in the same change so the repo's next steps stay accurate.
- **Default to implementation when feasible**: If the user asks whether something can be done and it is safe and well-scoped, prefer implementing it directly instead of only answering yes/no.
- **Keep API artifacts in sync**: Whenever routes, auth flows, or request/response models change, update `openapi.yaml`, `postman_collection.json`, and the endpoint list in `readme.md` in the same change.

Critical pitfalls: Race conditions in tick goroutine, token hash mismatch, physics validation (both weight and volume).
