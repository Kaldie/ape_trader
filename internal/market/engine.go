package market

import (
	"ape-trader/internal/models"
	"encoding/json"
	"errors"
	"log"
	"math"
	"os"
	"strings"
	"time"
)

var (
	ErrTownNil                 = errors.New("town is nil")
	ErrPlayerNil               = errors.New("player is nil")
	ErrInvalidQuantity         = errors.New("quantity must be greater than zero")
	ErrPlayerNotAtTown         = errors.New("player is not at this town")
	ErrResourceUnavailable     = errors.New("resource not available at this market")
	ErrInsufficientFunds       = errors.New("insufficient funds")
	ErrUnknownResource         = errors.New("unknown resource")
	ErrInsufficientWeight      = errors.New("insufficient weight capacity")
	ErrInsufficientVolume      = errors.New("insufficient volume capacity")
	ErrInsufficientTownStock   = errors.New("insufficient town inventory")
	ErrInsufficientPlayerStock = errors.New("insufficient player inventory")
	ErrPlayerInTransit         = errors.New("player is in transit")
	ErrInvalidDestinationTown  = errors.New("destination town not found")
	ErrAlreadyAtDestination    = errors.New("player already at destination town")
	ErrInvalidTravelEquipment  = errors.New("invalid travel equipment")
)

type TradeResult struct {
	TradeValue   models.Currency
	NewBalance   models.Currency
	NewInventory map[models.ResourceID]int64
}

type TownStateStore interface {
	PersistTownState(town *models.Town) error
}

var travelBaseSpeedByEquipment = map[string]float64{
	"feet":  5.0,
	"cart":  10.0,
	"horse": 18.0,
}

type MarketEngine struct {
	Towns          map[string]*models.Town
	Players        map[string]*models.Player
	BulletinBoard  *models.BulletinBoard
	TickerStopChan chan bool
	Recipes        map[string]models.RefineRecipe
	TownStateStore TownStateStore
}

func NewMarketEngine() *MarketEngine {
	return NewMarketEngineWithTowns(loadTownsFromJSON("towns.json"))
}

func LoadTownsFromJSON(filePath string) map[string]*models.Town {
	return loadTownsFromJSON(filePath)
}

func NewMarketEngineWithTowns(towns map[string]*models.Town) *MarketEngine {
	return NewMarketEngineWithTownsAndStore(towns, nil)
}

func NewMarketEngineWithTownsAndStore(towns map[string]*models.Town, townStateStore TownStateStore) *MarketEngine {
	if len(towns) == 0 {
		towns = initTowns()
	}

	return &MarketEngine{
		Towns:          towns,
		Players:        initPlayers(),
		BulletinBoard:  models.NewBulletinBoard(),
		TickerStopChan: make(chan bool),
		Recipes:        models.DefaultRecipes,
		TownStateStore: townStateStore,
	}
}

func (e *MarketEngine) GetTown(id string) (*models.Town, bool) {
	town, ok := e.Towns[id]
	return town, ok
}

func (e *MarketEngine) GetPlayer(id string) (*models.Player, bool) {
	player, ok := e.Players[id]
	if ok {
		e.resolveArrival(player)
	}
	return player, ok
}

func (e *MarketEngine) ResolveArrival(player *models.Player) bool {
	return e.resolveArrival(player)
}

// StartMinuteTick starts a background goroutine that runs every minute.
// It performs resource refining and updates the bulletin board with current prices.
func (e *MarketEngine) StartMinuteTick() {
	log.Printf("market tick started interval=1m towns=%d", len(e.Towns))
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-e.TickerStopChan:
				log.Printf("market tick stopped")
				return
			case <-ticker.C:
				e.performMinuteTick()
			}
		}
	}()
}

// StopMinuteTick signals the minute tick goroutine to stop.
func (e *MarketEngine) StopMinuteTick() {
	e.TickerStopChan <- true
}

func (e *MarketEngine) SimulateMinuteTick() {
	e.performMinuteTick()
}

func (e *MarketEngine) performMinuteTick() {
	start := time.Now()
	log.Printf("market tick begin towns=%d", len(e.Towns))
	for _, town := range e.Towns {
		beforeProsperity := town.Prosperity
		beforeUnits := totalInventoryUnits(town)

		// Reset per-cycle batch counters at the start of each tick
		if town.RefinementBatchesThisCycle == nil {
			town.RefinementBatchesThisCycle = make(map[string]int64)
		}
		for recipeID := range e.Recipes {
			town.RefinementBatchesThisCycle[recipeID] = 0
		}

		e.refineResources(town)
		e.processConsumption(town)
		e.processOptionalConsumption(town)
		e.syncTownSupplyWithInventory(town)
		e.updateBulletinBoard(town)
		e.persistTownState(town)

		afterUnits := totalInventoryUnits(town)
		if beforeProsperity != town.Prosperity || beforeUnits != afterUnits {
			log.Printf("market tick town=%s prosperity=%d->%d inventory_units=%d->%d", town.ID, beforeProsperity, town.Prosperity, beforeUnits, afterUnits)
		}
	}
	log.Printf("market tick end duration=%s", time.Since(start))
}

func (e *MarketEngine) persistTownState(town *models.Town) {
	if e.TownStateStore == nil || town == nil {
		return
	}

	if err := e.TownStateStore.PersistTownState(town); err != nil {
		log.Printf("warning: failed to persist town state for %s: %v", town.ID, err)
	}
}

func (e *MarketEngine) syncTownSupplyWithInventory(town *models.Town) {
	if town == nil {
		return
	}
	if town.Supply == nil {
		town.Supply = make(map[models.ResourceID]int64)
	}

	for resource := range models.ResourceCatalog {
		town.Supply[resource] = town.Inventory.Quantity(resource)
	}
}

// refineResources processes all available refinement recipes, consuming inputs and producing outputs.
// Uses the same batch-based logic for all recipes (calculate batches from limiting input, consume all inputs, produce output).
// Respects MaxBatchesPerCycle limit per recipe to create production bottlenecks.
func (e *MarketEngine) refineResources(town *models.Town) {
	for recipeID, recipe := range e.Recipes {
		// Calculate how many batches can be refined (minimum across all inputs).
		batches := int64(9223372036854775807) // MaxInt64 as initial max
		for inputResource, requiredPerBatch := range recipe.Inputs {
			available := town.Inventory.Quantity(inputResource)
			batchesFromThis := available / requiredPerBatch
			if batchesFromThis < batches {
				batches = batchesFromThis
			}
		}

		if batches <= 0 {
			continue
		}

		// Cap batches by the recipe's cycle limit
		batchesAvailableThisCycle := recipe.MaxBatchesPerCycle - town.RefinementBatchesThisCycle[recipeID]
		if batchesAvailableThisCycle <= 0 {
			continue // Already hit batch limit this cycle
		}
		if batches > batchesAvailableThisCycle {
			batches = batchesAvailableThisCycle
		}

		// Consume all inputs
		for inputResource, requiredPerBatch := range recipe.Inputs {
			town.Inventory.Remove(inputResource, batches*requiredPerBatch)
		}

		// Produce output
		outputProduced := batches * recipe.OutputQuantity
		town.Inventory.Add(recipe.Output, outputProduced)

		// Track refinement time and cycle batch count
		if town.LastRefinement == nil {
			town.LastRefinement = make(map[string]time.Time)
		}
		town.LastRefinement[recipeID] = time.Now()
		town.RefinementBatchesThisCycle[recipeID] += batches
	}
}

// updateBulletinBoard updates the global bulletin board with the town's current prices and inventory amounts.
// Entries are only created once and not updated on subsequent ticks.
func (e *MarketEngine) updateBulletinBoard(town *models.Town) {
	prices, err := e.CurrentPrices(town, nil)
	if err != nil {
		return
	}

	// Collect min and max amounts from the town's inventory.
	minAmount := make(map[models.ResourceID]int64)
	maxAmount := make(map[models.ResourceID]int64)

	for resource := range models.ResourceCatalog {
		qty := town.Inventory.Quantity(resource)
		minAmount[resource] = 0
		maxAmount[resource] = qty
	}

	e.BulletinBoard.Update(town.ID, prices, minAmount, maxAmount)
}

// processConsumption checks if town consumption needs are met and adjusts prosperity.
func (e *MarketEngine) processConsumption(town *models.Town) {
	// Skip if no consumption configured
	if len(town.Consumption.Required) == 0 {
		return
	}

	// Check if enough time has passed since last consumption
	cycleMinutes := time.Duration(town.Consumption.CycleHours) * 60 * time.Minute
	if !town.LastConsumption.IsZero() && time.Since(town.LastConsumption) < cycleMinutes {
		return
	}

	// Check if all required resources are available
	allRequirementsMet := true
	for resource, required := range town.Consumption.Required {
		available := town.Inventory.Quantity(resource)
		if available < required {
			allRequirementsMet = false
			break
		}
	}

	// Consume resources and adjust prosperity
	if allRequirementsMet {
		// Consume resources
		for resource, required := range town.Consumption.Required {
			town.Inventory.Remove(resource, required)
		}
		// Increase prosperity
		town.Prosperity += town.Consumption.ProsperityIncreaseIfMet
		log.Printf("consumption met town=%s prosperity_change=+%d", town.ID, town.Consumption.ProsperityIncreaseIfMet)
	} else {
		// Decrease prosperity if requirements not met
		decrease := town.Consumption.ProsperityDecreaseIfNotMet
		town.Prosperity -= town.Consumption.ProsperityDecreaseIfNotMet
		if town.Prosperity < 0 {
			town.Prosperity = 0
		}
		log.Printf("consumption unmet town=%s prosperity_change=-%d", town.ID, decrease)
	}

	town.LastConsumption = time.Now()
}

// processOptionalConsumption consumes luxury items if available and boosts prosperity slightly.
// No penalty if items are unavailable. Consumption scales with prosperity level.
func (e *MarketEngine) processOptionalConsumption(town *models.Town) {
	// Skip if no optional consumption configured
	if len(town.OptionalConsumption.Optional) == 0 {
		return
	}

	// Check if enough time has passed since last optional consumption
	cycleMinutes := time.Duration(town.OptionalConsumption.CycleHours) * 60 * time.Minute
	if !town.LastOptionalConsumption.IsZero() && time.Since(town.LastOptionalConsumption) < cycleMinutes {
		return
	}

	// Calculate needed amounts based on prosperity level
	baseAmount := town.OptionalConsumption.BaseAmount
	scaledAmount := int64(float64(town.Prosperity) * town.OptionalConsumption.ProsperityScaleFactor)
	totalNeeded := baseAmount + (scaledAmount / 100)

	totalConsumed := int64(0)

	// Try to consume optional items (consume what's available, no penalty if missing)
	for resource := range town.OptionalConsumption.Optional {
		available := town.Inventory.Quantity(resource)
		toConsume := totalNeeded
		if available < toConsume {
			toConsume = available
		}
		if toConsume > 0 {
			town.Inventory.Remove(resource, toConsume)
			totalConsumed += toConsume
		}
	}

	// Apply small prosperity boost proportional to consumption
	if totalConsumed > 0 {
		boost := totalConsumed * town.OptionalConsumption.ProsperityBoostPerUnit
		town.Prosperity += boost
		log.Printf("optional consumption town=%s consumed=%d prosperity_change=+%d", town.ID, totalConsumed, boost)
	}

	town.LastOptionalConsumption = time.Now()
}

func (e *MarketEngine) CurrentPrices(town *models.Town, player *models.Player) ([]models.MarketPrice, error) {
	if town == nil {
		return nil, ErrTownNil
	}

	var repBonus float64
	if player != nil {
		repBonus = float64(player.Reputation[town.ID]) / 100.0
	}
	repBonus = clamp(repBonus, -0.5, 0.5)

	prices := make([]models.MarketPrice, 0, len(town.MarketMaker.Prices))
	for resource, basePrice := range town.MarketMaker.Prices {
		demand := town.Demand[resource]
		supply := town.Supply[resource]
		ratio := float64(demand) / math.Max(float64(supply), 1)
		if supply <= 0 {
			ratio = 2.0
		}
		buy := calculatePrice(basePrice.Buy, ratio, repBonus, false)
		sell := calculatePrice(basePrice.Sell, ratio, repBonus, true)
		prices = append(prices, models.MarketPrice{
			Resource: resource,
			Buy:      buy,
			Sell:     sell,
		})
	}

	return prices, nil
}

func (e *MarketEngine) Buy(player *models.Player, town *models.Town, resource models.ResourceID, quantity int64) (*TradeResult, error) {
	if player == nil {
		return nil, ErrPlayerNil
	}
	if town == nil {
		return nil, ErrTownNil
	}
	e.resolveArrival(player)
	if player.Travel.InTransit {
		return nil, ErrPlayerInTransit
	}

	if quantity <= 0 {
		return nil, ErrInvalidQuantity
	}
	if player.Location != town.ID {
		return nil, ErrPlayerNotAtTown
	}
	if _, ok := models.ResourceCatalog[resource]; !ok {
		return nil, ErrUnknownResource
	}
	if town.Inventory.Quantity(resource) < quantity {
		return nil, ErrInsufficientTownStock
	}

	price, err := e.priceForResource(town, player, resource, false)
	if err != nil {
		return nil, err
	}

	totalCost := models.Currency(int64(price) * quantity)
	if player.Balance < totalCost {
		return nil, ErrInsufficientFunds
	}
	if err := validateCapacity(player, resource, quantity); err != nil {
		return nil, err
	}

	player.Balance -= totalCost
	player.Inventory.Add(resource, quantity)
	town.Inventory.Remove(resource, quantity)
	e.syncTownSupplyWithInventory(town)
	town.UpdatedAt = time.Now()

	return &TradeResult{
		TradeValue:   totalCost,
		NewBalance:   player.Balance,
		NewInventory: player.Inventory.Snapshot(),
	}, nil
}

func (e *MarketEngine) Sell(player *models.Player, town *models.Town, resource models.ResourceID, quantity int64) (*TradeResult, error) {
	if player == nil {
		return nil, ErrPlayerNil
	}
	if town == nil {
		return nil, ErrTownNil
	}
	e.resolveArrival(player)
	if player.Travel.InTransit {
		return nil, ErrPlayerInTransit
	}

	if quantity <= 0 {
		return nil, ErrInvalidQuantity
	}
	if player.Location != town.ID {
		return nil, ErrPlayerNotAtTown
	}
	if _, ok := models.ResourceCatalog[resource]; !ok {
		return nil, ErrUnknownResource
	}
	if player.Inventory.Quantity(resource) < quantity {
		return nil, ErrInsufficientPlayerStock
	}

	price, err := e.priceForResource(town, player, resource, true)
	if err != nil {
		return nil, err
	}

	totalValue := models.Currency(int64(price) * quantity)
	player.Inventory.Remove(resource, quantity)
	player.Balance += totalValue
	town.Inventory.Add(resource, quantity)
	e.syncTownSupplyWithInventory(town)
	town.UpdatedAt = time.Now()

	return &TradeResult{
		TradeValue:   totalValue,
		NewBalance:   player.Balance,
		NewInventory: player.Inventory.Snapshot(),
	}, nil
}

func (e *MarketEngine) StartTravel(player *models.Player, destinationTownID, equipment string) error {
	if player == nil {
		return ErrPlayerNil
	}
	e.resolveArrival(player)
	if player.Travel.InTransit {
		return ErrPlayerInTransit
	}

	destinationTown, ok := e.Towns[destinationTownID]
	if !ok {
		return ErrInvalidDestinationTown
	}
	currentTown, ok := e.Towns[player.Location]
	if !ok {
		return ErrTownNil
	}
	if player.Location == destinationTown.ID {
		return ErrAlreadyAtDestination
	}

	equipment = strings.ToLower(strings.TrimSpace(equipment))
	if equipment == "" {
		equipment = "feet"
	}
	baseSpeed, ok := travelBaseSpeedByEquipment[equipment]
	if !ok {
		return ErrInvalidTravelEquipment
	}

	weightPenalty := 1.0 + (float64(player.Inventory.TotalWeight()) / 300.0)
	distance := math.Hypot(destinationTown.X-currentTown.X, destinationTown.Y-currentTown.Y)
	if distance < 0.1 {
		distance = 0.1
	}

	travelHours := (distance * weightPenalty) / baseSpeed
	travelDuration := time.Duration(travelHours * float64(time.Hour))
	if travelDuration < time.Minute {
		travelDuration = time.Minute
	}

	now := time.Now()
	player.Travel = models.TravelState{
		InTransit: true,
		FromTown:  player.Location,
		ToTown:    destinationTownID,
		Equipment: equipment,
		StartedAt: now,
		ArrivesAt: now.Add(travelDuration),
	}
	return nil
}

func (e *MarketEngine) resolveArrival(player *models.Player) bool {
	if player == nil || !player.Travel.InTransit {
		return false
	}
	if time.Now().Before(player.Travel.ArrivesAt) {
		return false
	}
	player.Location = player.Travel.ToTown
	player.Travel = models.TravelState{}
	return true
}

func (e *MarketEngine) priceForResource(town *models.Town, player *models.Player, resource models.ResourceID, useBuyPrice bool) (models.Currency, error) {
	prices, err := e.CurrentPrices(town, player)
	if err != nil {
		return 0, err
	}

	for _, price := range prices {
		if price.Resource != resource {
			continue
		}
		if useBuyPrice {
			return price.Buy, nil
		}
		return price.Sell, nil
	}

	return 0, ErrResourceUnavailable
}

func validateCapacity(player *models.Player, resource models.ResourceID, quantity int64) error {
	resourceAttrs, ok := models.ResourceCatalog[resource]
	if !ok {
		return ErrUnknownResource
	}

	itemWeight := resourceAttrs.WeightKg * models.WeightKg(quantity)
	itemVolume := resourceAttrs.VolumeL * models.VolumeL(quantity)

	currentWeight := player.Inventory.TotalWeight()
	currentVolume := player.Inventory.TotalVolume()

	if currentWeight+itemWeight > player.Pants.MaxWeight {
		return ErrInsufficientWeight
	}
	if currentVolume+itemVolume > player.Pants.MaxVolume {
		return ErrInsufficientVolume
	}

	return nil
}

func calculatePrice(base models.Currency, ratio, repBonus float64, isSell bool) models.Currency {
	price := float64(base) * ratio
	if isSell {
		price = price * (1 + repBonus)
	} else {
		price = price * (1 - repBonus)
	}
	return models.Currency(math.Max(1, math.Round(price)))
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func totalInventoryUnits(town *models.Town) int64 {
	if town == nil {
		return 0
	}
	var total int64
	for _, quantity := range town.Inventory.Snapshot() {
		total += quantity
	}
	return total
}

func initTowns() map[string]*models.Town {
	return map[string]*models.Town{
		"town_1": {
			ID:         "town_1",
			Name:       "Apeville",
			X:          0,
			Y:          0,
			Inventory:  models.NewInventory(),
			Prosperity: 120,
			MarketMaker: models.MarketMaker{
				Prices: map[models.ResourceID]models.MarketPrice{
					models.ResourceWood:  {Resource: models.ResourceWood, Buy: 10, Sell: 8},
					models.ResourceStone: {Resource: models.ResourceStone, Buy: 25, Sell: 20},
					models.ResourceOre:   {Resource: models.ResourceOre, Buy: 40, Sell: 35},
					models.ResourceCoal:  {Resource: models.ResourceCoal, Buy: 15, Sell: 12},
					models.ResourceMetal: {Resource: models.ResourceMetal, Buy: 50, Sell: 45},
				},
				Reputation: 0,
			},
			Demand: map[models.ResourceID]int64{
				models.ResourceWood:  120,
				models.ResourceStone: 80,
				models.ResourceOre:   60,
				models.ResourceCoal:  90,
				models.ResourceMetal: 40,
			},
			Supply: map[models.ResourceID]int64{
				models.ResourceWood:  100,
				models.ResourceStone: 120,
				models.ResourceOre:   50,
				models.ResourceCoal:  100,
				models.ResourceMetal: 10,
			},
			Neighbors: []string{"town_2"},
			UpdatedAt: time.Now(),
		},
	}
}

type TownJSON struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	X            float64          `json:"x"`
	Y            float64          `json:"y"`
	Prosperity   int64            `json:"prosperity"`
	Neighbors    []string         `json:"neighbors"`
	Inventory    map[string]int64 `json:"inventory"`
	SupplyDemand map[string]struct {
		Supply        int64 `json:"supply"`
		Demand        int64 `json:"demand"`
		BaseBuyPrice  int64 `json:"base_buy_price"`
		BaseSellPrice int64 `json:"base_sell_price"`
	} `json:"supply_demand"`
	Consumption struct {
		CycleHours                 int64            `json:"cycle_hours"`
		ProsperityIncreaseIfMet    int64            `json:"prosperity_increase_if_met"`
		ProsperityDecreaseIfNotMet int64            `json:"prosperity_decrease_if_not_met"`
		Required                   map[string]int64 `json:"required"`
	} `json:"consumption"`
	OptionalConsumption struct {
		CycleHours             int64            `json:"cycle_hours"`
		ProsperityBoostPerUnit int64            `json:"prosperity_boost_per_unit"`
		BaseAmount             int64            `json:"base_amount"`
		ProsperityScaleFactor  float64          `json:"prosperity_scale_factor"`
		Optional               map[string]int64 `json:"optional"`
	} `json:"optional_consumption"`
	RefinementBatchesThisCycle map[string]int64 `json:"refinement_batches_this_cycle"`
}

func loadTownsFromJSON(filePath string) map[string]*models.Town {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return initTowns()
	}

	var townsJSON []TownJSON
	err = json.Unmarshal(data, &townsJSON)
	if err != nil {
		return initTowns()
	}

	towns := make(map[string]*models.Town)
	for _, t := range townsJSON {
		inventory := models.NewInventory()
		for resource, qty := range t.Inventory {
			inventory.Add(models.ResourceID(resource), qty)
		}

		prices := make(map[models.ResourceID]models.MarketPrice)
		demands := make(map[models.ResourceID]int64)
		supplies := make(map[models.ResourceID]int64)

		for resource, sd := range t.SupplyDemand {
			rid := models.ResourceID(resource)
			prices[rid] = models.MarketPrice{
				Resource: rid,
				Buy:      models.Currency(sd.BaseBuyPrice),
				Sell:     models.Currency(sd.BaseSellPrice),
			}
			demands[rid] = sd.Demand
			supplies[rid] = sd.Supply
		}

		// Convert consumption required map
		consumptionRequired := make(map[models.ResourceID]int64)
		for resource, qty := range t.Consumption.Required {
			consumptionRequired[models.ResourceID(resource)] = qty
		}

		// Convert optional consumption map
		optionalConsumptionMap := make(map[models.ResourceID]int64)
		for resource, qty := range t.OptionalConsumption.Optional {
			optionalConsumptionMap[models.ResourceID(resource)] = qty
		}

		// Initialize refinement batches this cycle (always start at 0)
		refinementBatchesThisCycle := make(map[string]int64)
		if t.RefinementBatchesThisCycle != nil {
			for recipeID := range t.RefinementBatchesThisCycle {
				refinementBatchesThisCycle[recipeID] = 0
			}
		}

		towns[t.ID] = &models.Town{
			ID:         t.ID,
			Name:       t.Name,
			X:          t.X,
			Y:          t.Y,
			Inventory:  inventory,
			Prosperity: t.Prosperity,
			MarketMaker: models.MarketMaker{
				Prices:     prices,
				Reputation: 0,
			},
			Demand:    demands,
			Supply:    supplies,
			Neighbors: t.Neighbors,
			Consumption: models.TownConsumption{
				CycleHours:                 t.Consumption.CycleHours,
				ProsperityIncreaseIfMet:    t.Consumption.ProsperityIncreaseIfMet,
				ProsperityDecreaseIfNotMet: t.Consumption.ProsperityDecreaseIfNotMet,
				Required:                   consumptionRequired,
			},
			OptionalConsumption: models.TownOptionalConsumption{
				CycleHours:             t.OptionalConsumption.CycleHours,
				ProsperityBoostPerUnit: t.OptionalConsumption.ProsperityBoostPerUnit,
				BaseAmount:             t.OptionalConsumption.BaseAmount,
				ProsperityScaleFactor:  t.OptionalConsumption.ProsperityScaleFactor,
				Optional:               optionalConsumptionMap,
			},
			LastConsumption:            time.Time{},
			LastOptionalConsumption:    time.Time{},
			LastRefinement:             make(map[string]time.Time),
			RefinementBatchesThisCycle: refinementBatchesThisCycle,
			UpdatedAt:                  time.Now(),
		}
	}

	return towns
}

func initPlayers() map[string]*models.Player {
	player := models.NewPlayer("player_1", "Ape Explorer", "town_1", 100)
	player.Reputation["town_1"] = 15
	return map[string]*models.Player{
		"player_1": &player,
	}
}
