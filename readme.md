This README is designed to give an AI Agent (like Cursor, GitHub Copilot, or a custom GPT) the necessary context, rules, and technical constraints to start building the **ApeTrader** backend.

---

# README.md: ApeTrader API

## Project Overview
ApeTrader is an **API-first, multiplayer economic simulation game**. Players start as low-level traders with nothing but the clothes on their backs (literally, their "pants" act as their first inventory) and 100 Apes (`@`), the local currency. The goal is to exploit price disparities between towns, navigate deep supply chains, and build reputation across a region of interconnected town economies.

## Core Game Mechanics
* **The Economy:** Tick-based (1-minute intervals). Every tick, towns consume resources, refiners process goods (e.g., Ore + Coal → Iron), and Market Makers update prices based on supply/demand.
* **Information "Fog of War":** Towns have bulletin boards showing prices of neighboring towns, but this data is a "snapshot" and becomes stale over time.
* **The "Pants" Inventory System:** Movement is restricted by two physical constraints: **Weight** and **Volume**. Players must manage both.
* **Reputation:** NPC Market Makers offer better buy/sell spreads to players with higher local reputation.
* **API-First:** The backend is a pure Go-based JSON API. A Web Front-end will be built later.

## Technical Stack
* **Language:** Go (Golang)
* **Architecture:** RESTful API (API-First)
* **Concurrency:** Heavy use of Goroutines for the global "Minute Tick" and background economic simulation.
* **Data Format:** JSON
* **Database:** PostgreSQL (with Docker Compose setup)
* **Documentation:** OpenAPI 3.0.0 specification

## API Documentation

Complete API specification is available in [openapi.yaml](openapi.yaml). This includes:
- All endpoints with request/response schemas
- Example values and error codes
- Resource definitions and constraints

When API routes or models change, update these files together in the same change:
- `openapi.yaml`
- `postman_collection.json`
- this endpoint section in `readme.md`

Current endpoints:
- `POST /players/register` — Create a player with local credentials (name, email, password)
- `GET /market/{town_id}` — Real-time market prices with optional reputation bonuses
- `GET /town/{town_id}` — Authenticated town details including inventory, production, and consumption
- `GET /bulletin/{town_id}` — Stale price snapshots (updated once per minute tick, expire after 24 hours)
- `GET /trade/history` — Authenticated trade audit trail for the trader's player
- `POST /trade/buy` — Execute purchases with validation (location, funds, physics)
- `POST /trade/sell` — Execute sales with validation (location, inventory)

## Analysis Scripts

Generate a no-trader price simulation graph grouped by town:

```bash
go run ./scripts/price_graph.go
```

Simulate more minutes if you want a longer run:

```bash
go run ./scripts/price_graph.go -minutes 120
```

This writes a standalone HTML report to `artifacts/price_graph.html`, with one chart per town and one line pair per item.

## Running the Application

### With Docker Compose (Recommended)
```bash
docker-compose up -d
```
This starts both PostgreSQL and the Go API server.

### Local Development
```bash
go run main.go
```

Optional auth env var for local credential hashing:

```bash
export AUTH_PASSWORD_PEPPER="set-a-strong-secret-pepper"
```

Identity model note:
- Local credentials are stored as provider=`local` identities.
- Future Google/OIDC SSO can be added by storing provider-specific identities (e.g. provider=`google`, subject=`<google-sub>`) for the same player.

See [DOCKER.md](DOCKER.md) for detailed Docker setup instructions.

## Data Structures (Draft)
* **Currency:** `@` (Apes), represented as `int64` to avoid floating-point errors.
* **Resources:** Defined by `Weight` and `Volume`.
* **Supply Chain:** * *Raw:* Wood, Stone, Ore, Coal, Grain.
    * *Refined:* Planks, Metal Ingots, Leather, Flour.
    * *Enhanced:* Furniture, Tools, Weapons, Armor.
* **Towns:** Nodes containing `Inventory`, `Prosperity`, and `MarketMaker` logic.

## Modifying Towns

Town configuration is in `towns.json` (used as fallback/default town data and for local simulation scripts).

For each town:
- `inventory`: Starting stock on hand.
- `supply_demand.<resource>.supply`: Natural local availability/production pressure for that resource.
- `supply_demand.<resource>.demand`: Local consumption need/market pull for that resource.
- `consumption.required`: Hard requirements that affect prosperity each cycle.
- `optional_consumption.optional`: Luxury consumption (no penalty if unavailable).

When tuning specialization, keep `supply` and `demand` aligned with town identity:
- Producer town: `supply > demand` for its specialty resources.
- Import-dependent town: `supply < demand` for resources it should buy in (for example, Smithy coal).

Tip:
- Keep `inventory` broadly consistent with `supply_demand.supply` at scenario start to avoid surprising first-tick price jumps.

## AI Agent Coding Instructions
When generating code for this project, follow these rules:
1.  **Strict Typing:** Use Go's strong typing. No `interface{}` unless absolutely necessary.
2.  **Concurrency Safety:** Ensure town inventories and player balances are protected by Mutexes or handled via channels during the "Minute Tick."
3.  **Validation:** Every trade must validate location (is the player there?), funds (can they afford it?), and physics (does it fit in their pants/bag?).
4.  **Modular Design:** Separate the `MarketLogic`, `TickEngine`, and `APIHandlers` into different packages.

## First Milestone: "The Humble Beginning"
1.  Initialize a Go module with a router (Gin or Chi).
2.  Implement a `Town` struct and a `Player` struct.
3.  Create a `GET /market` endpoint showing local prices.
4.  Create a `POST /trade/buy` endpoint that checks Weight/Volume capacity.
5.  Implement a basic `Ticker` that simulates resource consumption every 60 seconds.

---

### Suggested Resource Attributes for Initial Logic
| Item | Weight (kg) | Volume (L) |
| :--- | :--- | :--- |
| Wood | 5 | 10 |
| Stone | 20 | 8 |
| Ore | 30 | 5 |
| Coal | 10 | 10 |
| **Initial Pants Capacity** | **50kg** | **40L** |
