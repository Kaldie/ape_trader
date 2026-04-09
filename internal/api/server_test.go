package api

import (
	"ape-trader/internal/market"
	"ape-trader/internal/models"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func setupTestServer() (*Server, *market.MarketEngine) {
	engine := market.NewMarketEngine()
	player := engine.Players["player_1"]
	player.Location = "town_1"
	player.Balance = 100
	player.Inventory = models.NewInventory()
	player.Reputation = map[string]int64{}

	town := engine.Towns["town_1"]
	town.Inventory = models.NewInventory()
	town.Inventory.Add(models.ResourceWood, 10)
	town.Demand[models.ResourceWood] = 10
	town.Supply[models.ResourceWood] = 10

	return NewServer(engine, nil), engine
}

func performJSONRequest(t *testing.T, router http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func createTraderToken(t *testing.T, server *Server) string {
	t.Helper()
	createResp := performJSONRequest(t, server.router, http.MethodPost, "/traders", CreateTraderRequest{
		PlayerID: "player_1",
		Name:     "Test Trader",
	}, "")
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected trader creation status 201, got %d: %s", createResp.Code, createResp.Body.String())
	}
	var created CreateTraderResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create trader response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected token in create trader response")
	}
	return created.Token
}

func TestRegisterPlayerSuccessAndCanCreateTrader(t *testing.T) {
	server, _ := setupTestServer()

	registerResp := performJSONRequest(t, server.router, http.MethodPost, "/players/register", RegisterPlayerRequest{
		Name:     "Alice",
		Email:    "alice@example.com",
		Password: "super-secret-123",
	}, "")
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("expected register status 201, got %d: %s", registerResp.Code, registerResp.Body.String())
	}

	var registered RegisterPlayerResponse
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registered); err != nil {
		t.Fatalf("unmarshal register player response: %v", err)
	}
	if registered.Player == nil {
		t.Fatal("expected player payload from register response")
	}
	if registered.Player.Email != "alice@example.com" {
		t.Fatalf("expected normalized email alice@example.com, got %s", registered.Player.Email)
	}

	createTraderResp := performJSONRequest(t, server.router, http.MethodPost, "/traders", CreateTraderRequest{
		PlayerID: registered.Player.ID,
		Name:     "Alice Trader",
	}, "")
	if createTraderResp.Code != http.StatusCreated {
		t.Fatalf("expected trader creation status 201, got %d: %s", createTraderResp.Code, createTraderResp.Body.String())
	}
}

func TestRegisterPlayerDuplicateEmail(t *testing.T) {
	server, _ := setupTestServer()

	first := performJSONRequest(t, server.router, http.MethodPost, "/players/register", RegisterPlayerRequest{
		Name:     "Alice",
		Email:    "alice@example.com",
		Password: "super-secret-123",
	}, "")
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first registration status 201, got %d: %s", first.Code, first.Body.String())
	}

	duplicate := performJSONRequest(t, server.router, http.MethodPost, "/players/register", RegisterPlayerRequest{
		Name:     "Alice 2",
		Email:    "alice@example.com",
		Password: "another-secret-123",
	}, "")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expected duplicate registration status 409, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestTraderCanBuyAndSellWithoutDatabase(t *testing.T) {
	server, engine := setupTestServer()

	createResp := performJSONRequest(t, server.router, http.MethodPost, "/traders", CreateTraderRequest{
		PlayerID: "player_1",
		Name:     "Local Trader",
	}, "")
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected trader creation status 201, got %d: %s", createResp.Code, createResp.Body.String())
	}

	var created CreateTraderResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create trader response: %v", err)
	}
	if created.Token == "" {
		t.Fatal("expected token in create trader response")
	}

	buyResp := performJSONRequest(t, server.router, http.MethodPost, "/trade/buy", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 2,
	}, created.Token)
	if buyResp.Code != http.StatusOK {
		t.Fatalf("expected buy status 200, got %d: %s", buyResp.Code, buyResp.Body.String())
	}

	if got := engine.Players["player_1"].Inventory.Quantity(models.ResourceWood); got != 2 {
		t.Fatalf("expected player wood 2 after buy, got %d", got)
	}

	sellResp := performJSONRequest(t, server.router, http.MethodPost, "/trade/sell", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 1,
	}, created.Token)
	if sellResp.Code != http.StatusOK {
		t.Fatalf("expected sell status 200, got %d: %s", sellResp.Code, sellResp.Body.String())
	}

	if got := engine.Players["player_1"].Inventory.Quantity(models.ResourceWood); got != 1 {
		t.Fatalf("expected player wood 1 after sell, got %d", got)
	}
	if got := engine.Players["player_1"].Balance; got != 97 {
		t.Fatalf("expected balance 97 after buy then sell, got %d", got)
	}

	historyResp := performJSONRequest(t, server.router, http.MethodGet, "/trade/history", nil, created.Token)
	if historyResp.Code != http.StatusOK {
		t.Fatalf("expected trade history status 200, got %d: %s", historyResp.Code, historyResp.Body.String())
	}

	var history TradeHistoryResponse
	if err := json.Unmarshal(historyResp.Body.Bytes(), &history); err != nil {
		t.Fatalf("unmarshal trade history response: %v", err)
	}

	if len(history.History) != 2 {
		t.Fatalf("expected 2 trade history entries, got %d", len(history.History))
	}
	if history.History[0].TradeType != "sell" {
		t.Fatalf("expected latest history entry to be sell, got %s", history.History[0].TradeType)
	}
	if history.History[1].TradeType != "buy" {
		t.Fatalf("expected oldest history entry to be buy, got %s", history.History[1].TradeType)
	}
}

func TestGetTownReturnsTownDetails(t *testing.T) {
	server, engine := setupTestServer()
	engine.Towns["town_1"].Neighbors = []string{"town_2"}
	engine.Towns["town_1"].Consumption.Required = map[models.ResourceID]int64{
		models.ResourceWood: 2,
	}
	engine.Towns["town_1"].OptionalConsumption.Optional = map[models.ResourceID]int64{
		models.ResourceFurniture: 1,
	}

	createResp := performJSONRequest(t, server.router, http.MethodPost, "/traders", CreateTraderRequest{
		PlayerID: "player_1",
		Name:     "Town Viewer",
	}, "")
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected trader creation status 201, got %d: %s", createResp.Code, createResp.Body.String())
	}

	var created CreateTraderResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create trader response: %v", err)
	}

	townResp := performJSONRequest(t, server.router, http.MethodGet, "/town/town_1", nil, created.Token)
	if townResp.Code != http.StatusOK {
		t.Fatalf("expected town status 200, got %d: %s", townResp.Code, townResp.Body.String())
	}

	var response TownResponse
	if err := json.Unmarshal(townResp.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal town response: %v", err)
	}

	if !response.Success {
		t.Fatal("expected successful town response")
	}
	if response.Town == nil {
		t.Fatal("expected town payload")
	}
	if response.Town.ID != "town_1" {
		t.Fatalf("expected town_1, got %s", response.Town.ID)
	}
	if response.Town.Inventory[models.ResourceWood] != 10 {
		t.Fatalf("expected wood inventory 10, got %d", response.Town.Inventory[models.ResourceWood])
	}
	if len(response.Town.Production) == 0 {
		t.Fatal("expected production recipes to be present")
	}
	if response.Town.Consumption.Required[models.ResourceWood] != 2 {
		t.Fatalf("expected required wood consumption 2, got %d", response.Town.Consumption.Required[models.ResourceWood])
	}
	if response.Town.OptionalConsumption.Optional[models.ResourceFurniture] != 1 {
		t.Fatalf("expected optional furniture consumption 1, got %d", response.Town.OptionalConsumption.Optional[models.ResourceFurniture])
	}
}

func TestTradeFailurePaths(t *testing.T) {
	server, engine := setupTestServer()
	token := createTraderToken(t, server)
	player := engine.Players["player_1"]
	town := engine.Towns["town_1"]

	player.Balance = 1
	resp := performJSONRequest(t, server.router, http.MethodPost, "/trade/buy", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 2,
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected insufficient funds status 400, got %d: %s", resp.Code, resp.Body.String())
	}

	player.Balance = 1000
	town.Inventory = models.NewInventory()
	town.Inventory.Add(models.ResourceWood, 1)
	resp = performJSONRequest(t, server.router, http.MethodPost, "/trade/buy", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 5,
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected insufficient town stock status 400, got %d: %s", resp.Code, resp.Body.String())
	}

	town.Inventory = models.NewInventory()
	town.Inventory.Add(models.ResourceWood, 100)
	player.Pants.MaxWeight = 10
	resp = performJSONRequest(t, server.router, http.MethodPost, "/trade/buy", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 3,
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected capacity overflow status 400, got %d: %s", resp.Code, resp.Body.String())
	}

	player.Pants = models.InitialPantsCapacity
	player.Inventory = models.NewInventory()
	resp = performJSONRequest(t, server.router, http.MethodPost, "/trade/sell", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 1,
	}, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected insufficient player stock status 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAuthFailurePaths(t *testing.T) {
	server, _ := setupTestServer()

	req := httptest.NewRequest(http.MethodGet, "/market/town_1", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth status 401, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/market/town_1", nil)
	req.Header.Set("Authorization", "Token not-bearer")
	w = httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid bearer format status 401, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/market/town_1", nil)
	req.Header.Set("Authorization", "Bearer trader_invalid")
	w = httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid token status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTravelBlocksTradingAndStatusWorks(t *testing.T) {
	server, engine := setupTestServer()
	token := createTraderToken(t, server)
	engine.Towns["town_2"] = &models.Town{
		ID:        "town_2",
		Name:      "Far Town",
		X:         10,
		Y:         0,
		Inventory: models.NewInventory(),
		MarketMaker: models.MarketMaker{
			Prices: map[models.ResourceID]models.MarketPrice{},
		},
		Demand: make(map[models.ResourceID]int64),
		Supply: make(map[models.ResourceID]int64),
	}

	startResp := performJSONRequest(t, server.router, http.MethodPost, "/travel/start", TravelRequest{
		DestinationTownID: "town_2",
		Equipment:         "feet",
	}, token)
	if startResp.Code != http.StatusOK {
		t.Fatalf("expected travel start status 200, got %d: %s", startResp.Code, startResp.Body.String())
	}

	buyResp := performJSONRequest(t, server.router, http.MethodPost, "/trade/buy", TradeRequest{
		TownID:   "town_1",
		Resource: models.ResourceWood,
		Quantity: 1,
	}, token)
	if buyResp.Code != http.StatusBadRequest {
		t.Fatalf("expected in-transit buy block status 400, got %d: %s", buyResp.Code, buyResp.Body.String())
	}

	player := engine.Players["player_1"]
	player.Travel.ArrivesAt = time.Now().Add(-time.Minute)
	statusResp := performJSONRequest(t, server.router, http.MethodGet, "/travel/status", nil, token)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("expected travel status 200, got %d: %s", statusResp.Code, statusResp.Body.String())
	}

	var status TravelResponse
	if err := json.Unmarshal(statusResp.Body.Bytes(), &status); err != nil {
		t.Fatalf("unmarshal travel status response: %v", err)
	}
	if status.Travel != nil && status.Travel.InTransit {
		t.Fatal("expected transit to auto-resolve after arrival")
	}
}

func TestPriceVisibilityRequiresTraderPositionInTown(t *testing.T) {
	server, engine := setupTestServer()
	token := createTraderToken(t, server)

	if _, ok := engine.Towns["town_2"]; !ok {
		engine.Towns["town_2"] = &models.Town{
			ID:          "town_2",
			Name:        "Town 2",
			Inventory:   models.NewInventory(),
			Prosperity:  100,
			MarketMaker: models.MarketMaker{Prices: map[models.ResourceID]models.MarketPrice{}},
			Demand:      map[models.ResourceID]int64{models.ResourceWood: 10},
			Supply:      map[models.ResourceID]int64{models.ResourceWood: 10},
		}
	}

	resp := performJSONRequest(t, server.router, http.MethodGet, "/market/town_2", nil, token)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected market visibility status 403, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = performJSONRequest(t, server.router, http.MethodGet, "/town/town_2", nil, token)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected town visibility status 403, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = performJSONRequest(t, server.router, http.MethodGet, "/bulletin/town_2", nil, token)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected bulletin visibility status 403, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = performJSONRequest(t, server.router, http.MethodGet, "/bulletin/town_1", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected local bulletin status 200, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestPriceVisibilityRequiresKnownPlayerLocation(t *testing.T) {
	server, engine := setupTestServer()
	token := createTraderToken(t, server)

	engine.Players["player_1"].Location = ""
	resp := performJSONRequest(t, server.router, http.MethodGet, "/market/town_1", nil, token)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected missing position status 400, got %d: %s", resp.Code, resp.Body.String())
	}
}
