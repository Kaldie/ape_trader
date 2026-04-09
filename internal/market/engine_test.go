package market

import (
	"ape-trader/internal/models"
	"testing"
	"time"
)

type recordingTownStateStore struct {
	calls          int
	lastTownID     string
	lastProsperity int64
	lastInventory  map[models.ResourceID]int64
}

func (s *recordingTownStateStore) PersistTownState(town *models.Town) error {
	s.calls++
	s.lastTownID = town.ID
	s.lastProsperity = town.Prosperity
	s.lastInventory = town.Inventory.Snapshot()
	return nil
}

func testEngine() *MarketEngine {
	townInventory := models.NewInventory()
	townInventory.Add(models.ResourceWood, 10)

	player := models.NewPlayer("player_1", "Test Player", "town_1", 100)

	return &MarketEngine{
		Towns: map[string]*models.Town{
			"town_1": {
				ID:        "town_1",
				Name:      "Test Town",
				Inventory: townInventory,
				MarketMaker: models.MarketMaker{
					Prices: map[models.ResourceID]models.MarketPrice{
						models.ResourceWood: {
							Resource: models.ResourceWood,
							Buy:      10,
							Sell:     8,
						},
					},
				},
				Demand: map[models.ResourceID]int64{
					models.ResourceWood: 100,
				},
				Supply: map[models.ResourceID]int64{
					models.ResourceWood: 100,
				},
			},
		},
		Players: map[string]*models.Player{
			"player_1": &player,
		},
		BulletinBoard:  models.NewBulletinBoard(),
		TickerStopChan: make(chan bool),
		Recipes:        map[string]models.RefineRecipe{},
	}
}

func TestBuySuccess(t *testing.T) {
	engine := testEngine()
	player := engine.Players["player_1"]
	town := engine.Towns["town_1"]

	result, err := engine.Buy(player, town, models.ResourceWood, 2)
	if err != nil {
		t.Fatalf("Buy returned error: %v", err)
	}

	if result.NewBalance != 84 {
		t.Fatalf("expected balance 84, got %d", result.NewBalance)
	}
	if got := player.Inventory.Quantity(models.ResourceWood); got != 2 {
		t.Fatalf("expected player wood 2, got %d", got)
	}
	if got := town.Inventory.Quantity(models.ResourceWood); got != 8 {
		t.Fatalf("expected town wood 8, got %d", got)
	}
}

func TestBuyRejectsInsufficientTownInventory(t *testing.T) {
	engine := testEngine()
	player := engine.Players["player_1"]
	town := engine.Towns["town_1"]

	_, err := engine.Buy(player, town, models.ResourceWood, 11)
	if err != ErrInsufficientTownStock {
		t.Fatalf("expected ErrInsufficientTownStock, got %v", err)
	}
}

func TestSellSuccess(t *testing.T) {
	engine := testEngine()
	player := engine.Players["player_1"]
	town := engine.Towns["town_1"]
	player.Inventory.Add(models.ResourceWood, 3)

	result, err := engine.Sell(player, town, models.ResourceWood, 2)
	if err != nil {
		t.Fatalf("Sell returned error: %v", err)
	}

	if result.NewBalance != 120 {
		t.Fatalf("expected balance 120, got %d", result.NewBalance)
	}
	if got := player.Inventory.Quantity(models.ResourceWood); got != 1 {
		t.Fatalf("expected player wood 1, got %d", got)
	}
	if got := town.Inventory.Quantity(models.ResourceWood); got != 12 {
		t.Fatalf("expected town wood 12, got %d", got)
	}
}

func TestSellRejectsInsufficientPlayerInventory(t *testing.T) {
	engine := testEngine()
	player := engine.Players["player_1"]
	town := engine.Towns["town_1"]

	_, err := engine.Sell(player, town, models.ResourceWood, 1)
	if err != ErrInsufficientPlayerStock {
		t.Fatalf("expected ErrInsufficientPlayerStock, got %v", err)
	}
}

func TestNewMarketEngineWithTownsFallsBackWhenEmpty(t *testing.T) {
	engine := NewMarketEngineWithTowns(nil)

	if len(engine.Towns) == 0 {
		t.Fatal("expected fallback towns to be initialized")
	}
	if _, ok := engine.Towns["town_1"]; !ok {
		t.Fatal("expected fallback town_1 to exist")
	}
}

func TestNewMarketEngineWithTownsUsesProvidedTowns(t *testing.T) {
	customTowns := map[string]*models.Town{
		"db_town": {
			ID:        "db_town",
			Name:      "Database Town",
			Inventory: models.NewInventory(),
			MarketMaker: models.MarketMaker{
				Prices: map[models.ResourceID]models.MarketPrice{},
			},
			Demand: make(map[models.ResourceID]int64),
			Supply: make(map[models.ResourceID]int64),
		},
	}

	engine := NewMarketEngineWithTowns(customTowns)

	if len(engine.Towns) != 1 {
		t.Fatalf("expected 1 town, got %d", len(engine.Towns))
	}
	if _, ok := engine.Towns["db_town"]; !ok {
		t.Fatal("expected provided db_town to be used")
	}
}

func TestPerformMinuteTickPersistsTownState(t *testing.T) {
	store := &recordingTownStateStore{}
	townInventory := models.NewInventory()
	townInventory.Add(models.ResourceWood, 5)

	engine := NewMarketEngineWithTownsAndStore(map[string]*models.Town{
		"town_1": {
			ID:         "town_1",
			Name:       "Tick Town",
			Inventory:  townInventory,
			Prosperity: 10,
			MarketMaker: models.MarketMaker{
				Prices: map[models.ResourceID]models.MarketPrice{
					models.ResourceWood: {Resource: models.ResourceWood, Buy: 10, Sell: 8},
				},
			},
			Demand: map[models.ResourceID]int64{
				models.ResourceWood: 100,
			},
			Supply: map[models.ResourceID]int64{
				models.ResourceWood: 100,
			},
			Consumption: models.TownConsumption{
				CycleHours:              0,
				ProsperityIncreaseIfMet: 3,
				Required: map[models.ResourceID]int64{
					models.ResourceWood: 2,
				},
			},
		},
	}, store)
	engine.Recipes = map[string]models.RefineRecipe{}

	engine.performMinuteTick()

	if store.calls != 1 {
		t.Fatalf("expected 1 persist call, got %d", store.calls)
	}
	if store.lastTownID != "town_1" {
		t.Fatalf("expected persisted town_1, got %s", store.lastTownID)
	}
	if store.lastProsperity != 13 {
		t.Fatalf("expected prosperity 13 after tick, got %d", store.lastProsperity)
	}
	if got := store.lastInventory[models.ResourceWood]; got != 3 {
		t.Fatalf("expected persisted wood quantity 3, got %d", got)
	}
}

func TestPerformMinuteTickUpdatesPricesFromInventoryChanges(t *testing.T) {
	engine := NewMarketEngineWithTowns(map[string]*models.Town{
		"town_1": {
			ID:         "town_1",
			Name:       "Price Town",
			Inventory:  models.NewInventory(),
			Prosperity: 10,
			MarketMaker: models.MarketMaker{
				Prices: map[models.ResourceID]models.MarketPrice{
					models.ResourceWood: {Resource: models.ResourceWood, Buy: 10, Sell: 8},
				},
			},
			Demand: map[models.ResourceID]int64{
				models.ResourceWood: 100,
			},
			Supply: map[models.ResourceID]int64{
				models.ResourceWood: 10,
			},
			Consumption: models.TownConsumption{
				CycleHours:              0,
				ProsperityIncreaseIfMet: 1,
				Required: map[models.ResourceID]int64{
					models.ResourceWood: 7,
				},
			},
		},
	})
	engine.Recipes = map[string]models.RefineRecipe{}
	engine.Towns["town_1"].Inventory.Add(models.ResourceWood, 10)

	before, err := engine.CurrentPrices(engine.Towns["town_1"], nil)
	if err != nil {
		t.Fatalf("CurrentPrices before tick returned error: %v", err)
	}

	engine.performMinuteTick()

	after, err := engine.CurrentPrices(engine.Towns["town_1"], nil)
	if err != nil {
		t.Fatalf("CurrentPrices after tick returned error: %v", err)
	}

	if got := engine.Towns["town_1"].Supply[models.ResourceWood]; got != 3 {
		t.Fatalf("expected synced supply 3, got %d", got)
	}
	if before[0].Sell == after[0].Sell {
		t.Fatalf("expected sell price to change after inventory consumption, stayed at %d", before[0].Sell)
	}
	if after[0].Sell <= before[0].Sell {
		t.Fatalf("expected sell price to increase as supply dropped, before=%d after=%d", before[0].Sell, after[0].Sell)
	}
}

func TestRefineResourcesHonorsBatchCap(t *testing.T) {
	engine := NewMarketEngineWithTowns(map[string]*models.Town{
		"town_1": {
			ID:        "town_1",
			Name:      "Refine Town",
			Inventory: models.NewInventory(),
			MarketMaker: models.MarketMaker{
				Prices: map[models.ResourceID]models.MarketPrice{},
			},
			Demand:                     map[models.ResourceID]int64{},
			Supply:                     map[models.ResourceID]int64{},
			RefinementBatchesThisCycle: map[string]int64{},
		},
	})
	engine.Recipes = map[string]models.RefineRecipe{
		"ore_coal_to_metal": {
			RecipeID:           "ore_coal_to_metal",
			Inputs:             map[models.ResourceID]int64{models.ResourceOre: 2, models.ResourceCoal: 1},
			Output:             models.ResourceMetal,
			OutputQuantity:     3,
			MaxBatchesPerCycle: 2,
		},
	}
	town := engine.Towns["town_1"]
	town.Inventory.Add(models.ResourceOre, 20)
	town.Inventory.Add(models.ResourceCoal, 20)

	engine.refineResources(town)

	if got := town.RefinementBatchesThisCycle["ore_coal_to_metal"]; got != 2 {
		t.Fatalf("expected capped batches 2, got %d", got)
	}
	if got := town.Inventory.Quantity(models.ResourceMetal); got != 6 {
		t.Fatalf("expected metal output 6, got %d", got)
	}
}

func TestProcessConsumptionProsperityAndInventory(t *testing.T) {
	town := &models.Town{
		Inventory:  models.NewInventory(),
		Prosperity: 20,
		Consumption: models.TownConsumption{
			CycleHours:                 0,
			ProsperityIncreaseIfMet:    5,
			ProsperityDecreaseIfNotMet: 7,
			Required: map[models.ResourceID]int64{
				models.ResourceWood: 3,
			},
		},
	}
	engine := NewMarketEngineWithTowns(map[string]*models.Town{"town_1": town})

	town.Inventory.Add(models.ResourceWood, 3)
	engine.processConsumption(town)
	if got := town.Prosperity; got != 25 {
		t.Fatalf("expected prosperity increase to 25, got %d", got)
	}
	if got := town.Inventory.Quantity(models.ResourceWood); got != 0 {
		t.Fatalf("expected required resource consumed, got %d", got)
	}

	engine.processConsumption(town)
	if got := town.Prosperity; got != 18 {
		t.Fatalf("expected prosperity decrease to 18, got %d", got)
	}
}

func TestProcessOptionalConsumptionBoostsProsperityWithoutPenalty(t *testing.T) {
	town := &models.Town{
		Inventory:  models.NewInventory(),
		Prosperity: 100,
		OptionalConsumption: models.TownOptionalConsumption{
			CycleHours:             0,
			ProsperityBoostPerUnit: 2,
			BaseAmount:             2,
			ProsperityScaleFactor:  0,
			Optional: map[models.ResourceID]int64{
				models.ResourceTools: 1,
			},
		},
	}
	engine := NewMarketEngineWithTowns(map[string]*models.Town{"town_1": town})

	engine.processOptionalConsumption(town)
	if got := town.Prosperity; got != 100 {
		t.Fatalf("expected no penalty when optional goods absent, got %d", got)
	}

	town.Inventory.Add(models.ResourceTools, 2)
	engine.processOptionalConsumption(town)
	if got := town.Prosperity; got != 104 {
		t.Fatalf("expected prosperity boost from optional consumption, got %d", got)
	}
}

func TestPerformMinuteTickCreatesBulletinEntryAndMutatesInventory(t *testing.T) {
	town := &models.Town{
		ID:        "town_1",
		Name:      "Bulletin Town",
		Inventory: models.NewInventory(),
		MarketMaker: models.MarketMaker{
			Prices: map[models.ResourceID]models.MarketPrice{
				models.ResourceWood: {Resource: models.ResourceWood, Buy: 10, Sell: 8},
			},
		},
		Demand: map[models.ResourceID]int64{
			models.ResourceWood: 10,
		},
		Supply: map[models.ResourceID]int64{
			models.ResourceWood: 10,
		},
		Consumption: models.TownConsumption{
			CycleHours:              0,
			ProsperityIncreaseIfMet: 1,
			Required: map[models.ResourceID]int64{
				models.ResourceWood: 2,
			},
		},
	}
	town.Inventory.Add(models.ResourceWood, 10)
	engine := NewMarketEngineWithTowns(map[string]*models.Town{"town_1": town})
	engine.Recipes = map[string]models.RefineRecipe{}

	engine.performMinuteTick()

	entry, ok := engine.BulletinBoard.GetEntry("town_1")
	if !ok {
		t.Fatal("expected bulletin entry after minute tick")
	}
	if entry.IsExpired() {
		t.Fatal("did not expect fresh bulletin entry to be expired")
	}
	if got := town.Inventory.Quantity(models.ResourceWood); got != 8 {
		t.Fatalf("expected inventory mutation from consumption, got %d", got)
	}
	if got := town.Supply[models.ResourceWood]; got != 8 {
		t.Fatalf("expected synced supply to 8, got %d", got)
	}
}

func TestStartTravelAndArrivalResolution(t *testing.T) {
	engine := NewMarketEngineWithTowns(map[string]*models.Town{
		"town_1": {ID: "town_1", Name: "Start", X: 0, Y: 0, Inventory: models.NewInventory(), MarketMaker: models.MarketMaker{Prices: map[models.ResourceID]models.MarketPrice{}}, Demand: map[models.ResourceID]int64{}, Supply: map[models.ResourceID]int64{}},
		"town_2": {ID: "town_2", Name: "End", X: 10, Y: 0, Inventory: models.NewInventory(), MarketMaker: models.MarketMaker{Prices: map[models.ResourceID]models.MarketPrice{}}, Demand: map[models.ResourceID]int64{}, Supply: map[models.ResourceID]int64{}},
	})
	player := engine.Players["player_1"]
	player.Location = "town_1"

	if err := engine.StartTravel(player, "town_2", "horse"); err != nil {
		t.Fatalf("StartTravel returned error: %v", err)
	}
	if !player.Travel.InTransit {
		t.Fatal("expected player to be in transit")
	}

	player.Travel.ArrivesAt = time.Now().Add(-time.Second)
	engine.resolveArrival(player)
	if player.Travel.InTransit {
		t.Fatal("expected transit to resolve after arrival time")
	}
	if player.Location != "town_2" {
		t.Fatalf("expected player location town_2 after arrival, got %s", player.Location)
	}
}
