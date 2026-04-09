# ApeTrader API - Docker Setup

## Quick Start

To start both PostgreSQL and the Go application:

```bash
docker-compose up -d
```

The application will be available at `http://localhost:8080`
PostgreSQL will be available at `localhost:5432`

## Services

### postgres
- Image: `postgres:16-alpine`
- User: `apetrader`
- Password: `apetrader_pass`
- Database: `apetrader_db`
- Port: `5432`

### app
- Go application
- Port: `8080`
- Depends on: `postgres` (waits for health check)
- Automatically connects to PostgreSQL on startup

## Database

The schema is automatically initialized on first PostgreSQL startup from `schema.sql`.

Tables include:
- `resources` — material definitions
- `towns` — town records with neighbors
- `town_inventory` — town stock
- `town_supply_demand` — supply, demand, and base prices
- `players` — player records
- `player_inventory` — player items
- `player_reputation` — per-town reputation
- `bulletin_board_entries` — price snapshots
- `trade_history` — audit log

## Environment Variables

When running with Docker Compose, these are automatically set:
- `DB_HOST` — postgres
- `DB_PORT` — 5432
- `DB_USER` — apetrader
- `DB_PASSWORD` — apetrader_pass
- `DB_NAME` — apetrader_db

## Building Locally

To build the Go binary outside Docker:

```bash
go build -o apetrader .
```

To build the Docker images:

```bash
docker-compose build
```

## Stopping

```bash
docker-compose down
```

Data persists in the `postgres_data` volume. To remove all data:

```bash
docker-compose down -v
```
