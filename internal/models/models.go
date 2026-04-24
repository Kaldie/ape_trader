package models

import (
	"sync"
	"time"
)

type Currency int64

type WeightKg int64

type VolumeL int64

type ResourceID string

const (
	ResourceWood      ResourceID = "wood"
	ResourceStone     ResourceID = "stone"
	ResourceOre       ResourceID = "ore"
	ResourceCoal      ResourceID = "coal"
	ResourceMetal     ResourceID = "metal"
	ResourceFurniture ResourceID = "furniture"
	ResourceTools     ResourceID = "tools"
	ResourceWeapons   ResourceID = "weapons"
	ResourceArmor     ResourceID = "armor"
)

// Capacity defines physical carrying limits for weight and volume.
type Capacity struct {
	MaxWeight WeightKg `json:"max_weight_kg"`
	MaxVolume VolumeL  `json:"max_volume_l"`
}

var InitialBagCapacity = Capacity{
	MaxWeight: 50,
	MaxVolume: 40,
}

type TraderEquipment struct {
	Bag    Capacity `json:"bag"`
	Travel string   `json:"travel"`
}

// ResourceAttributes holds the physical characteristics of a resource.
type ResourceAttributes struct {
	WeightKg WeightKg `json:"weight_kg"`
	VolumeL  VolumeL  `json:"volume_l"`
}

var ResourceCatalog = map[ResourceID]ResourceAttributes{
	ResourceWood:      {WeightKg: 5, VolumeL: 10},
	ResourceStone:     {WeightKg: 20, VolumeL: 8},
	ResourceOre:       {WeightKg: 30, VolumeL: 5},
	ResourceCoal:      {WeightKg: 10, VolumeL: 10},
	ResourceMetal:     {WeightKg: 25, VolumeL: 4},
	ResourceFurniture: {WeightKg: 15, VolumeL: 15},
	ResourceTools:     {WeightKg: 8, VolumeL: 6},
	ResourceWeapons:   {WeightKg: 12, VolumeL: 5},
	ResourceArmor:     {WeightKg: 18, VolumeL: 8},
}

// Inventory tracks quantities of resources and supports concurrency-safe access.
type Inventory struct {
	mu    sync.RWMutex
	Items map[ResourceID]int64 `json:"items"`
}

func NewInventory() Inventory {
	return Inventory{
		Items: make(map[ResourceID]int64),
	}
}

func (inv *Inventory) Add(resource ResourceID, quantity int64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Items[resource] += quantity
}

func (inv *Inventory) Remove(resource ResourceID, quantity int64) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Items[resource] -= quantity
	if inv.Items[resource] <= 0 {
		delete(inv.Items, resource)
	}
}

func (inv *Inventory) Quantity(resource ResourceID) int64 {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	return inv.Items[resource]
}

func (inv *Inventory) Snapshot() map[ResourceID]int64 {
	inv.mu.RLock()
	defer inv.mu.RUnlock()

	items := make(map[ResourceID]int64, len(inv.Items))
	for resource, quantity := range inv.Items {
		items[resource] = quantity
	}

	return items
}

func (inv *Inventory) TotalWeight() WeightKg {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	var total WeightKg
	for resource, qty := range inv.Items {
		if attrs, ok := ResourceCatalog[resource]; ok {
			total += attrs.WeightKg * WeightKg(qty)
		}
	}
	return total
}

func (inv *Inventory) TotalVolume() VolumeL {
	inv.mu.RLock()
	defer inv.mu.RUnlock()
	var total VolumeL
	for resource, qty := range inv.Items {
		if attrs, ok := ResourceCatalog[resource]; ok {
			total += attrs.VolumeL * VolumeL(qty)
		}
	}
	return total
}

func (inv *Inventory) Fits(capacity Capacity) bool {
	return inv.TotalWeight() <= capacity.MaxWeight && inv.TotalVolume() <= capacity.MaxVolume
}

// MarketPrice contains buy and sell prices for one resource.
type MarketPrice struct {
	Resource ResourceID `json:"resource"`
	Buy      Currency   `json:"buy"`
	Sell     Currency   `json:"sell"`
}

// MarketMaker contains local pricing logic and reputation modifiers.
type MarketMaker struct {
	Prices     map[ResourceID]MarketPrice `json:"prices"`
	Reputation int64                      `json:"reputation"`
}

// Town is a simulation node with inventory, prosperity, and market behavior.
type Town struct {
	ID                         string                  `json:"id"`
	Name                       string                  `json:"name"`
	X                          float64                 `json:"x"`
	Y                          float64                 `json:"y"`
	Inventory                  Inventory               `json:"inventory"`
	Prosperity                 int64                   `json:"prosperity"`
	MarketMaker                MarketMaker             `json:"market_maker"`
	Demand                     map[ResourceID]int64    `json:"demand"`
	Supply                     map[ResourceID]int64    `json:"supply"`
	Neighbors                  []string                `json:"neighbors"`
	Consumption                TownConsumption         `json:"consumption"`
	OptionalConsumption        TownOptionalConsumption `json:"optional_consumption"`
	LastConsumption            time.Time               `json:"last_consumption"`
	LastOptionalConsumption    time.Time               `json:"last_optional_consumption"`
	LastRefinement             map[string]time.Time    `json:"last_refinement"`
	RefinementBatchesThisCycle map[string]int64        `json:"refinement_batches_this_cycle"`
	UpdatedAt                  time.Time               `json:"updated_at"`
}

// TownConsumption defines what resources a town needs and prosperity impacts.
type TownConsumption struct {
	CycleHours                 int64                `json:"cycle_hours"`
	ProsperityIncreaseIfMet    int64                `json:"prosperity_increase_if_met"`
	ProsperityDecreaseIfNotMet int64                `json:"prosperity_decrease_if_not_met"`
	Required                   map[ResourceID]int64 `json:"required"`
}

// TownOptionalConsumption defines optional luxury items that boost prosperity if available (no penalty if missing).
type TownOptionalConsumption struct {
	CycleHours             int64                `json:"cycle_hours"`
	ProsperityBoostPerUnit int64                `json:"prosperity_boost_per_unit"`
	BaseAmount             int64                `json:"base_amount"`
	ProsperityScaleFactor  float64              `json:"prosperity_scale_factor"`
	Optional               map[ResourceID]int64 `json:"optional"`
}

// Player represents the account owner who can control multiple traders.
type Player struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TravelStatus struct {
	InTransit bool      `json:"in_transit"`
	FromTown  string    `json:"from_town,omitempty"`
	ToTown    string    `json:"to_town,omitempty"`
	Method    string    `json:"method,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	ArrivesAt time.Time `json:"arrives_at,omitempty"`
}

func NewPlayer(id, name string) Player {
	return Player{
		ID:   id,
		Name: name,
	}
}

// Trader represents a playable trading unit owned by a player account.
type Trader struct {
	ID         string           `json:"id"`
	PlayerID   string           `json:"player_id"`
	Name       string           `json:"name"`
	Location   string           `json:"location"`
	Balance    Currency         `json:"balance"`
	Inventory  Inventory        `json:"inventory"`
	Reputation map[string]int64 `json:"reputation"`
	Equipment  TraderEquipment  `json:"equipment"`
	Travel     TravelStatus     `json:"travel"`
	Token      string           `json:"-"` // Bearer token, not exposed in JSON
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

func NewTrader(id, playerID, name, token, location string, balance Currency) Trader {
	now := time.Now()
	return Trader{
		ID:         id,
		PlayerID:   playerID,
		Name:       name,
		Location:   location,
		Balance:    balance,
		Inventory:  NewInventory(),
		Reputation: make(map[string]int64),
		Equipment: TraderEquipment{
			Bag:    InitialBagCapacity,
			Travel: "feet",
		},
		Travel:    TravelStatus{},
		Token:     token,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

type TradeHistoryEntry struct {
	ID           int64      `json:"id"`
	TraderID     string     `json:"trader_id"`
	PlayerID     string     `json:"player_id"`
	TownID       string     `json:"town_id"`
	ResourceID   ResourceID `json:"resource_id"`
	Quantity     int64      `json:"quantity"`
	PricePerUnit Currency   `json:"price_per_unit"`
	TotalCost    Currency   `json:"total_cost"`
	TradeType    string     `json:"trade_type"`
	CreatedAt    time.Time  `json:"created_at"`
}

// BulletinBoardEntry represents snapshot of a town's prices at a point in time.
// It includes the minimum and maximum amounts of resources available and is not updated each tick.
// Entries expire after a configured duration.
type BulletinBoardEntry struct {
	TownID    string               `json:"town_id"`
	Prices    []MarketPrice        `json:"prices"`
	MinAmount map[ResourceID]int64 `json:"min_amount"`
	MaxAmount map[ResourceID]int64 `json:"max_amount"`
	Timestamp time.Time            `json:"timestamp"`
	ExpiresAt time.Time            `json:"expires_at"`
}

// IsExpired returns true if the entry has passed its expiration time.
func (e *BulletinBoardEntry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

// BulletinBoard tracks stale price data as a global state updated every tick.
type BulletinBoard struct {
	mu             sync.RWMutex
	Entries        map[string]BulletinBoardEntry `json:"entries"`
	ExpirationTime time.Duration
}

func NewBulletinBoard() *BulletinBoard {
	return &BulletinBoard{
		Entries:        make(map[string]BulletinBoardEntry),
		ExpirationTime: 24 * time.Hour,
	}
}

func (bb *BulletinBoard) Update(townID string, prices []MarketPrice, minAmount, maxAmount map[ResourceID]int64) {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	// Only create the entry if it doesn't exist yet; do not update existing entries.
	if _, exists := bb.Entries[townID]; exists {
		return
	}

	now := time.Now()
	bb.Entries[townID] = BulletinBoardEntry{
		TownID:    townID,
		Prices:    prices,
		MinAmount: minAmount,
		MaxAmount: maxAmount,
		Timestamp: now,
		ExpiresAt: now.Add(bb.ExpirationTime),
	}
}

func (bb *BulletinBoard) GetEntry(townID string) (BulletinBoardEntry, bool) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()
	entry, ok := bb.Entries[townID]
	return entry, ok
}

// RefineRecipe defines a general refinement recipe: consume inputs, produce output.
type RefineRecipe struct {
	RecipeID           string               `json:"recipe_id"`
	Name               string               `json:"name"`
	Inputs             map[ResourceID]int64 `json:"inputs"`
	Output             ResourceID           `json:"output"`
	OutputQuantity     int64                `json:"output_quantity"`
	MaxBatchesPerCycle int64                `json:"max_batches_per_cycle"`
	CycleHours         int64                `json:"cycle_hours"`
}

// DefaultRecipes defines the built-in refinement recipes.
var DefaultRecipes = map[string]RefineRecipe{
	"ore_coal_to_metal": {
		RecipeID:           "ore_coal_to_metal",
		Name:               "Ore + Coal → Metal",
		Inputs:             map[ResourceID]int64{ResourceOre: 2, ResourceCoal: 1},
		Output:             ResourceMetal,
		OutputQuantity:     3,
		MaxBatchesPerCycle: 10,
		CycleHours:         0,
	},
	"wood_to_furniture": {
		RecipeID:           "wood_to_furniture",
		Name:               "Wood → Furniture",
		Inputs:             map[ResourceID]int64{ResourceWood: 4},
		Output:             ResourceFurniture,
		OutputQuantity:     2,
		MaxBatchesPerCycle: 15,
		CycleHours:         0,
	},
}
