package db

import (
	"ape-trader/internal/models"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/lib/pq"
)

type Database struct {
	conn *sql.DB
}

var ErrEmailAlreadyExists = errors.New("email already exists")

func New(host, port, user, password, dbname string) (*Database, error) {
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	conn, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	// Test the connection
	err = conn.Ping()
	if err != nil {
		conn.Close()
		if isDatabaseDoesNotExistError(err) {
			log.Printf("database %q not found, attempting auto-create", dbname)
			if createErr := ensureDatabaseExists(host, port, user, password, dbname); createErr != nil {
				return nil, fmt.Errorf("failed to create missing database %q: %w", dbname, createErr)
			}
			log.Printf("database %q created (or already exists), retrying connection", dbname)

			conn, err = sql.Open("postgres", psqlInfo)
			if err != nil {
				return nil, err
			}
			if err := conn.Ping(); err != nil {
				conn.Close()
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &Database{conn: conn}, nil
}

func isDatabaseDoesNotExistError(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code) == "3D000"
	}
	return strings.Contains(strings.ToLower(err.Error()), "does not exist")
}

func ensureDatabaseExists(host, port, user, password, dbname string) error {
	adminConn, err := sql.Open("postgres", fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		host, port, user, password))
	if err != nil {
		return err
	}
	defer adminConn.Close()

	if err := adminConn.Ping(); err != nil {
		return err
	}

	escapedName := strings.ReplaceAll(dbname, `"`, `""`)
	_, err = adminConn.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, escapedName))
	if err == nil {
		return nil
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "42P04" {
		return nil
	}
	return err
}

func (db *Database) Close() error {
	return db.conn.Close()
}

func (db *Database) GetConn() *sql.DB {
	return db.conn
}

func (db *Database) InitializeSchemaIfNeeded(schemaPath string) error {
	if schemaPath == "" {
		schemaPath = "schema.sql"
	}

	var resourcesTable sql.NullString
	if err := db.conn.QueryRow(`SELECT to_regclass('public.resources')`).Scan(&resourcesTable); err != nil {
		return fmt.Errorf("failed schema existence check: %w", err)
	}
	if resourcesTable.Valid {
		log.Printf("schema initialization skipped: resources table already exists")
		return nil
	}

	schemaSQL, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to read schema file %s: %w", schemaPath, err)
	}

	if _, err := db.conn.Exec(string(schemaSQL)); err != nil {
		return fmt.Errorf("failed to execute schema initialization: %w", err)
	}
	log.Printf("database schema initialized from %s", schemaPath)

	return nil
}

// CreateTrader inserts a new trader into the database
func (db *Database) CreateTrader(trader *models.Trader) error {
	query := `
		INSERT INTO traders (
			id, player_id, name, location, balance, bag_max_weight, bag_max_volume,
			travel_in_transit, travel_from_town, travel_to_town, travel_method, travel_started_at, travel_arrives_at,
			token_hash, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

	_, err := db.conn.Exec(query,
		trader.ID,
		trader.PlayerID,
		trader.Name,
		trader.Location,
		trader.Balance,
		trader.Equipment.Bag.MaxWeight,
		trader.Equipment.Bag.MaxVolume,
		trader.Travel.InTransit,
		nullIfEmpty(trader.Travel.FromTown),
		nullIfEmpty(trader.Travel.ToTown),
		nullIfEmpty(trader.Travel.Method),
		nullTime(trader.Travel.StartedAt),
		nullTime(trader.Travel.ArrivesAt),
		trader.Token,
		trader.CreatedAt,
		trader.UpdatedAt)

	return err
}

func (db *Database) CreatePlayerWithLocalIdentity(player *models.Player, email, passwordHash, passwordSalt string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO players (id, name, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())`,
		player.ID,
		player.Name,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT INTO player_identities (player_id, provider, subject, password_hash, password_salt, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		player.ID,
		"local",
		strings.ToLower(strings.TrimSpace(email)),
		passwordHash,
		passwordSalt,
	); err != nil {
		if isUniqueViolation(err) {
			return ErrEmailAlreadyExists
		}
		return err
	}

	return tx.Commit()
}

// GetTraderByTokenHash retrieves a trader by their token hash
func (db *Database) GetTraderByTokenHash(tokenHash string) (*models.Trader, error) {
	query := `
		SELECT id, player_id, name, location, balance, bag_max_weight, bag_max_volume,
		       travel_in_transit, travel_from_town, travel_to_town, travel_method,
		       travel_started_at, travel_arrives_at, token_hash, created_at, updated_at
		FROM traders
		WHERE token_hash = $1`

	var trader models.Trader
	var maxWeight, maxVolume int64
	var inTransit bool
	var fromTown, toTown, equipment sql.NullString
	var startedAt, arrivesAt sql.NullTime
	err := db.conn.QueryRow(query, tokenHash).Scan(
		&trader.ID,
		&trader.PlayerID,
		&trader.Name,
		&trader.Location,
		&trader.Balance,
		&maxWeight,
		&maxVolume,
		&inTransit,
		&fromTown,
		&toTown,
		&equipment,
		&startedAt,
		&arrivesAt,
		&trader.Token,
		&trader.CreatedAt,
		&trader.UpdatedAt)

	if err != nil {
		return nil, err
	}

	trader.Equipment = models.TraderEquipment{
		Bag: models.Capacity{
			MaxWeight: models.WeightKg(maxWeight),
			MaxVolume: models.VolumeL(maxVolume),
		},
	}
	trader.Travel = models.TravelStatus{
		InTransit: inTransit,
	}
	if fromTown.Valid {
		trader.Travel.FromTown = fromTown.String
	}
	if toTown.Valid {
		trader.Travel.ToTown = toTown.String
	}
	if equipment.Valid {
		trader.Travel.Method = equipment.String
	}
	if startedAt.Valid {
		trader.Travel.StartedAt = startedAt.Time
	}
	if arrivesAt.Valid {
		trader.Travel.ArrivesAt = arrivesAt.Time
	}

	inventory, err := db.getTraderInventory(trader.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load inventory: %v", err)
	}
	trader.Inventory = inventory

	trader.Reputation = make(map[string]int64)
	if err := db.loadTraderReputation(&trader); err != nil {
		return nil, fmt.Errorf("failed to load reputation: %v", err)
	}

	return &trader, nil
}

// GetPlayerByID retrieves a player account by ID.
func (db *Database) GetPlayerByID(playerID string) (*models.Player, error) {
	query := `
		SELECT id, name
		FROM players
		WHERE id = $1`

	var player models.Player
	err := db.conn.QueryRow(query, playerID).Scan(
		&player.ID,
		&player.Name,
	)

	if err != nil {
		return nil, err
	}

	return &player, nil
}

func (db *Database) GetTraderByID(traderID string) (*models.Trader, error) {
	query := `
		SELECT id, player_id, name, location, balance, bag_max_weight, bag_max_volume,
		       travel_in_transit, travel_from_town, travel_to_town, travel_method,
		       travel_started_at, travel_arrives_at, token_hash, created_at, updated_at
		FROM traders
		WHERE id = $1`

	var trader models.Trader
	var maxWeight, maxVolume int64
	var inTransit bool
	var fromTown, toTown, equipment sql.NullString
	var startedAt, arrivesAt sql.NullTime
	err := db.conn.QueryRow(query, traderID).Scan(
		&trader.ID,
		&trader.PlayerID,
		&trader.Name,
		&trader.Location,
		&trader.Balance,
		&maxWeight,
		&maxVolume,
		&inTransit,
		&fromTown,
		&toTown,
		&equipment,
		&startedAt,
		&arrivesAt,
		&trader.Token,
		&trader.CreatedAt,
		&trader.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	trader.Equipment = models.TraderEquipment{
		Bag: models.Capacity{
			MaxWeight: models.WeightKg(maxWeight),
			MaxVolume: models.VolumeL(maxVolume),
		},
	}
	trader.Travel = models.TravelStatus{InTransit: inTransit}
	if fromTown.Valid {
		trader.Travel.FromTown = fromTown.String
	}
	if toTown.Valid {
		trader.Travel.ToTown = toTown.String
	}
	if equipment.Valid {
		trader.Travel.Method = equipment.String
	}
	if startedAt.Valid {
		trader.Travel.StartedAt = startedAt.Time
	}
	if arrivesAt.Valid {
		trader.Travel.ArrivesAt = arrivesAt.Time
	}

	inventory, err := db.getTraderInventory(traderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load inventory: %v", err)
	}
	trader.Inventory = inventory

	trader.Reputation = make(map[string]int64)
	if err := db.loadTraderReputation(&trader); err != nil {
		return nil, fmt.Errorf("failed to load reputation: %v", err)
	}

	return &trader, nil
}

func (db *Database) RecordTrade(trader *models.Trader, townID string, resourceID models.ResourceID, quantity int64, pricePerUnit models.Currency, tradeType string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.updateTrader(tx, trader); err != nil {
		return err
	}
	if err := db.upsertInventory(tx, "trader_inventory", "trader_id", trader.ID, resourceID, trader.Inventory.Quantity(resourceID)); err != nil {
		return err
	}
	if err := db.adjustTownInventory(tx, townID, resourceID, quantity, tradeType); err != nil {
		return err
	}

	totalCost := models.Currency(int64(pricePerUnit) * quantity)
	if err := db.insertTradeHistory(tx, trader.ID, trader.PlayerID, townID, resourceID, quantity, pricePerUnit, totalCost, tradeType); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *Database) GetTradeHistoryByTraderID(traderID string, limit int) ([]models.TradeHistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.Query(`
		SELECT id, trader_id, player_id, town_id, resource_id, quantity, price_per_unit, total_cost, trade_type, created_at
		FROM trade_history
		WHERE trader_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, traderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := make([]models.TradeHistoryEntry, 0, limit)
	for rows.Next() {
		var entry models.TradeHistoryEntry
		var resourceID string
		if err := rows.Scan(
			&entry.ID,
			&entry.TraderID,
			&entry.PlayerID,
			&entry.TownID,
			&resourceID,
			&entry.Quantity,
			&entry.PricePerUnit,
			&entry.TotalCost,
			&entry.TradeType,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		entry.ResourceID = models.ResourceID(resourceID)
		history = append(history, entry)
	}

	return history, rows.Err()
}

func (db *Database) LoadTowns() (map[string]*models.Town, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, x, y, prosperity, updated_at
		FROM towns
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	towns := make(map[string]*models.Town)
	for rows.Next() {
		var town models.Town
		var updatedAt time.Time
		if err := rows.Scan(&town.ID, &town.Name, &town.X, &town.Y, &town.Prosperity, &updatedAt); err != nil {
			return nil, err
		}

		town.Inventory = models.NewInventory()
		town.MarketMaker = models.MarketMaker{
			Prices:     make(map[models.ResourceID]models.MarketPrice),
			Reputation: 0,
		}
		town.Demand = make(map[models.ResourceID]int64)
		town.Supply = make(map[models.ResourceID]int64)
		town.Neighbors = []string{}
		town.Consumption = models.TownConsumption{Required: make(map[models.ResourceID]int64)}
		town.OptionalConsumption = models.TownOptionalConsumption{Optional: make(map[models.ResourceID]int64)}
		town.LastRefinement = make(map[string]time.Time)
		town.RefinementBatchesThisCycle = make(map[string]int64)
		town.UpdatedAt = updatedAt

		towns[town.ID] = &town
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := db.loadTownNeighbors(towns); err != nil {
		return nil, err
	}
	if err := db.loadTownInventory(towns); err != nil {
		return nil, err
	}
	if err := db.loadTownSupplyDemand(towns); err != nil {
		return nil, err
	}

	return towns, nil
}

func (db *Database) SeedTowns(towns map[string]*models.Town) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, town := range towns {
		if _, err := tx.Exec(`
			INSERT INTO towns (id, name, x, y, prosperity, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (id) DO NOTHING`,
			town.ID, town.Name, town.X, town.Y, town.Prosperity,
		); err != nil {
			return err
		}

		for _, neighborID := range town.Neighbors {
			if _, ok := towns[neighborID]; !ok {
				continue
			}
			if _, err := tx.Exec(`
				INSERT INTO town_neighbors (town_id, neighbor_id)
				VALUES ($1, $2)
				ON CONFLICT (town_id, neighbor_id) DO NOTHING`,
				town.ID, neighborID,
			); err != nil {
				return err
			}
		}

		for resourceID, price := range town.MarketMaker.Prices {
			if _, err := tx.Exec(`
				INSERT INTO town_supply_demand (town_id, resource_id, supply, demand, base_buy_price, base_sell_price)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (town_id, resource_id) DO NOTHING`,
				town.ID, string(resourceID), town.Supply[resourceID], town.Demand[resourceID],
				int64(price.Buy), int64(price.Sell),
			); err != nil {
				return err
			}
		}

		for resourceID, quantity := range town.Inventory.Snapshot() {
			if quantity <= 0 {
				continue
			}
			if _, err := tx.Exec(`
				INSERT INTO town_inventory (town_id, resource_id, quantity)
				VALUES ($1, $2, $3)
				ON CONFLICT (town_id, resource_id) DO NOTHING`,
				town.ID, string(resourceID), quantity,
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (db *Database) PersistTownState(town *models.Town) error {
	if town == nil {
		return fmt.Errorf("town is nil")
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE towns
		SET prosperity = $2, updated_at = NOW()
		WHERE id = $1`,
		town.ID,
		town.Prosperity,
	); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM town_inventory WHERE town_id = $1`, town.ID); err != nil {
		return err
	}

	for resourceID, quantity := range town.Inventory.Snapshot() {
		if quantity <= 0 {
			continue
		}
		if err := db.upsertInventory(tx, "town_inventory", "town_id", town.ID, resourceID, quantity); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// getTraderInventory loads a trader's inventory from the database
func (db *Database) getTraderInventory(traderID string) (models.Inventory, error) {
	query := `SELECT resource_id, quantity FROM trader_inventory WHERE trader_id = $1`

	rows, err := db.conn.Query(query, traderID)
	if err != nil {
		return models.Inventory{}, err
	}
	defer rows.Close()

	inventory := models.NewInventory()
	for rows.Next() {
		var resourceID string
		var quantity int64
		err := rows.Scan(&resourceID, &quantity)
		if err != nil {
			return models.Inventory{}, err
		}
		inventory.Items[models.ResourceID(resourceID)] = quantity
	}

	if err := rows.Err(); err != nil {
		return models.Inventory{}, err
	}

	return inventory, nil
}

func (db *Database) loadTraderReputation(trader *models.Trader) error {
	rows, err := db.conn.Query(`SELECT town_id, reputation FROM trader_reputation WHERE trader_id = $1`, trader.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var townID string
		var reputation int64
		if err := rows.Scan(&townID, &reputation); err != nil {
			return err
		}
		trader.Reputation[townID] = reputation
	}

	return rows.Err()
}

func (db *Database) loadTownNeighbors(towns map[string]*models.Town) error {
	rows, err := db.conn.Query(`SELECT town_id, neighbor_id FROM town_neighbors`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var townID, neighborID string
		if err := rows.Scan(&townID, &neighborID); err != nil {
			return err
		}
		town, ok := towns[townID]
		if !ok {
			continue
		}
		town.Neighbors = append(town.Neighbors, neighborID)
	}

	return rows.Err()
}

func (db *Database) loadTownInventory(towns map[string]*models.Town) error {
	rows, err := db.conn.Query(`SELECT town_id, resource_id, quantity FROM town_inventory`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var townID, resourceID string
		var quantity int64
		if err := rows.Scan(&townID, &resourceID, &quantity); err != nil {
			return err
		}
		town, ok := towns[townID]
		if !ok {
			continue
		}
		town.Inventory.Add(models.ResourceID(resourceID), quantity)
	}

	return rows.Err()
}

func (db *Database) loadTownSupplyDemand(towns map[string]*models.Town) error {
	rows, err := db.conn.Query(`
		SELECT town_id, resource_id, supply, demand, base_buy_price, base_sell_price
		FROM town_supply_demand`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var townID, resourceID string
		var supply, demand, baseBuyPrice, baseSellPrice int64
		if err := rows.Scan(&townID, &resourceID, &supply, &demand, &baseBuyPrice, &baseSellPrice); err != nil {
			return err
		}
		town, ok := towns[townID]
		if !ok {
			continue
		}

		resource := models.ResourceID(resourceID)
		town.Supply[resource] = supply
		town.Demand[resource] = demand
		town.MarketMaker.Prices[resource] = models.MarketPrice{
			Resource: resource,
			Buy:      models.Currency(baseBuyPrice),
			Sell:     models.Currency(baseSellPrice),
		}
	}

	return rows.Err()
}

func (db *Database) updateTrader(tx *sql.Tx, trader *models.Trader) error {
	_, err := tx.Exec(`
		UPDATE traders
		SET location = $2, balance = $3, bag_max_weight = $4, bag_max_volume = $5,
		    travel_in_transit = $6, travel_from_town = $7, travel_to_town = $8,
		    travel_method = $9, travel_started_at = $10, travel_arrives_at = $11,
		    updated_at = NOW()
		WHERE id = $1`,
		trader.ID,
		trader.Location,
		trader.Balance,
		trader.Equipment.Bag.MaxWeight,
		trader.Equipment.Bag.MaxVolume,
		trader.Travel.InTransit,
		nullIfEmpty(trader.Travel.FromTown),
		nullIfEmpty(trader.Travel.ToTown),
		nullIfEmpty(trader.Travel.Method),
		nullTime(trader.Travel.StartedAt),
		nullTime(trader.Travel.ArrivesAt),
	)
	return err
}

func (db *Database) PersistTraderState(trader *models.Trader) error {
	if trader == nil {
		return fmt.Errorf("trader is nil")
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.updateTrader(tx, trader); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *Database) upsertInventory(tx *sql.Tx, table, ownerColumn, ownerID string, resourceID models.ResourceID, quantity int64) error {
	if quantity <= 0 {
		_, err := tx.Exec(
			fmt.Sprintf(`DELETE FROM %s WHERE %s = $1 AND resource_id = $2`, table, ownerColumn),
			ownerID,
			string(resourceID),
		)
		return err
	}

	_, err := tx.Exec(
		fmt.Sprintf(`
			INSERT INTO %s (%s, resource_id, quantity, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (%s, resource_id)
			DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()`, table, ownerColumn, ownerColumn),
		ownerID,
		string(resourceID),
		quantity,
	)
	return err
}

func (db *Database) adjustTownInventory(tx *sql.Tx, townID string, resourceID models.ResourceID, quantity int64, tradeType string) error {
	currentQuantity, err := db.getTownInventoryForUpdate(tx, townID, resourceID)
	if err != nil {
		return err
	}

	newQuantity := currentQuantity
	switch tradeType {
	case "buy":
		newQuantity -= quantity
	case "sell":
		newQuantity += quantity
	default:
		return fmt.Errorf("unknown trade type: %s", tradeType)
	}

	if newQuantity < 0 {
		return fmt.Errorf("town inventory cannot become negative")
	}

	return db.upsertInventory(tx, "town_inventory", "town_id", townID, resourceID, newQuantity)
}

func (db *Database) getTownInventoryForUpdate(tx *sql.Tx, townID string, resourceID models.ResourceID) (int64, error) {
	var quantity int64
	err := tx.QueryRow(`
		SELECT quantity
		FROM town_inventory
		WHERE town_id = $1 AND resource_id = $2
		FOR UPDATE`,
		townID,
		string(resourceID),
	).Scan(&quantity)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return quantity, err
}

func (db *Database) insertTradeHistory(tx *sql.Tx, traderID, playerID, townID string, resourceID models.ResourceID, quantity int64, pricePerUnit, totalCost models.Currency, tradeType string) error {
	_, err := tx.Exec(`
		INSERT INTO trade_history (trader_id, player_id, town_id, resource_id, quantity, price_per_unit, total_cost, trade_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		traderID,
		playerID,
		townID,
		string(resourceID),
		quantity,
		pricePerUnit,
		totalCost,
		tradeType,
	)
	return err
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "23505"
}
