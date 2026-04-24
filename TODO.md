TODO
[x] Separate player accounts from playable traders
Moved gameplay state out of player records so one player can own multiple traders.
Updated schema, persistence, API flow, and tests so location, balance, inventory, reputation, travel, and trade history belong to traders.

[x] Market engine consistency and trading foundations
Aligned models.Town.Inventory usage with internal/market/engine.go.
Fixed seeded town/player location and reputation IDs to use town_1.
Moved buy/sell execution into MarketEngine so trade validation and mutations live in one place.

[x] Trading API and local auth flow
Added POST /trade/sell.
Made trader auth work without PostgreSQL by keeping in-memory trader records.
Added GET /trade/history with in-memory fallback and API documentation updates.

[x] Trader persistence and trade storage
Kept token hashing flow consistent for trader creation/auth lookup.
Persisted DB-backed trades to traders, trader_inventory, town_inventory, and trade_history.
Loaded trader reputation from Postgres for DB-backed pricing.

[x] Test coverage added
Added engine tests for buy/sell success and failure paths.
Added API tests for create trader, buy, sell, and trade history in local non-DB mode.

[x] DB-backed town loading
Added PostgreSQL town loading from towns, town_inventory, town_supply_demand, and town_neighbors.
Wired startup to prefer database towns in DB mode and fall back to towns.json when loading fails or returns no rows.

[x] Tick persistence for DB mode
Added a town-state persistence hook to MarketEngine.
Persisted minute-tick town prosperity and inventory back to PostgreSQL in DB mode.

[x] Town details endpoint
Added GET /town/{town_id} for authenticated town details.
Exposed inventory, prices, production recipes, neighbors, and consumption settings.

[x] TODO format cleanup
Rewrote this file to use [x] for finished work and [] for unfinished work so future updates only flip task state.

[x] Price updates from resource processing
Synced town supply with actual inventory after refinement, consumption, optional consumption, and trade mutations.
Ensured price calculations reflect changed availability after processing.

[x] Add API failure-path tests
Cover insufficient funds, insufficient town stock, insufficient trader stock, invalid token, missing auth header, invalid bearer format, and capacity overflow.

[x] Add simulation tick tests
Cover performMinuteTick, refineResources, processConsumption, and processOptionalConsumption.
Verify prosperity changes, batch caps, bulletin updates, and inventory mutations across a full tick.

[x] Implement CI/CD pipeline on GitHub
Set up GitHub Actions workflow for linting, automated testing, and build verification.

[x] Implement trader movement system
Enable movement between towns to allow for market arbitrage and profit.
Assign X/Y location coordinates to towns in data storage.
Calculate travel time based on distance, equipment (feet, cart, horses), and current inventory weight penalty.
Implement "In Transit" state to block trading while moving.

[x] start making the app stateless. Ensure all changes of the internal state are reflected into the db.
Added DB-backed persistence for trader travel state and arrival resolution.
Extended schema/database loading with town X/Y coordinates and travel fields so state is recoverable from Postgres.

[x] Initiate database on startup if it is not initialized yet.
Added startup schema bootstrap that checks for existing tables and executes `schema.sql` automatically when needed.

[x] Fix PostgreSQL startup DB mismatch
Fix error: `2026-04-09 06:34:11.553 UTC [86] FATAL:  database "apetrader" does not exist`.
Added fallback DB name (`apetrader_db`) when `DB_NAME` is not set and automatic creation of a missing target database during startup connection.

[x] Remove Postgres persistence in Docker Compose
Removed the `postgres_data` data volume from `docker-compose.yaml` so local Postgres state is not persisted across container restarts.

[x] Register players with local credentials and IdP-ready identity model
Added `POST /players/register` with name, email, and password.
Implemented local password storage using salted + peppered hash and a provider/subject identity table so Google/OIDC identities can be plugged in later without changing player records.

[x] Bulletin board visibility and position checks
Ensure bulletin entries appear when requested in-town by generating a snapshot if missing/expired.
Restrict market/town/bulletin price visibility to traders physically located in that town and validate trader position is present.

[x] the trader needs equipment, it has a bag & travel-bits (<- rename) but later it will have more equipment parts. Change this in the database and ensure the model reflects this. The player model is already account-only; finish the trader equipment split next.
Grouped trader gear under an `equipment` model with `equipment.bag` carrying capacity.
Renamed active travel state wording from generic equipment to a travel method in code and database fields.
