package api

import (
	"ape-trader/internal/auth"
	"ape-trader/internal/db"
	"ape-trader/internal/market"
	"ape-trader/internal/models"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type TradeRequest struct {
	TownID   string            `json:"town_id" binding:"required"`
	Resource models.ResourceID `json:"resource" binding:"required"`
	Quantity int64             `json:"quantity" binding:"required"`
}

type TradeResponse struct {
	Success      bool                        `json:"success"`
	Message      string                      `json:"message"`
	NewBalance   models.Currency             `json:"new_balance,omitempty"`
	NewInventory map[models.ResourceID]int64 `json:"new_inventory,omitempty"`
	TotalCost    models.Currency             `json:"total_cost,omitempty"`
}

type CreateTraderRequest struct {
	PlayerID string `json:"player_id" binding:"required"`
	Name     string `json:"name" binding:"required,min=1,max=100"`
}

type CreateTraderResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Trader  models.Trader `json:"trader,omitempty"`
	Token   string        `json:"token,omitempty"`
}

type RegisterPlayerRequest struct {
	Name     string `json:"name" binding:"required,min=1,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type RegisterPlayerResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Player  *RegisteredPlayer `json:"player,omitempty"`
}

type RegisteredPlayer struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Email            string          `json:"email"`
	Location         string          `json:"location"`
	Balance          models.Currency `json:"balance"`
	IdentityProvider string          `json:"identity_provider"`
}

type TradeHistoryResponse struct {
	Success bool                       `json:"success"`
	Message string                     `json:"message,omitempty"`
	History []models.TradeHistoryEntry `json:"history,omitempty"`
}

type TravelRequest struct {
	DestinationTownID string `json:"destination_town_id" binding:"required"`
	Equipment         string `json:"equipment"`
}

type TravelResponse struct {
	Success  bool                `json:"success"`
	Message  string              `json:"message,omitempty"`
	PlayerID string              `json:"player_id,omitempty"`
	Location string              `json:"location,omitempty"`
	Travel   *models.TravelState `json:"travel,omitempty"`
}

type TownResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Town    *TownViewModel `json:"town,omitempty"`
}

type TownViewModel struct {
	ID                  string                         `json:"id"`
	Name                string                         `json:"name"`
	Prosperity          int64                          `json:"prosperity"`
	Neighbors           []string                       `json:"neighbors"`
	Inventory           map[models.ResourceID]int64    `json:"inventory"`
	Prices              []models.MarketPrice           `json:"prices"`
	Consumption         models.TownConsumption         `json:"consumption"`
	OptionalConsumption models.TownOptionalConsumption `json:"optional_consumption"`
	Production          []models.RefineRecipe          `json:"production"`
	UpdatedAt           time.Time                      `json:"updated_at"`
}

type Server struct {
	engine             *market.MarketEngine
	router             *gin.Engine
	db                 *db.Database
	inMemoryTraders    map[string]*models.Trader
	inMemoryHistory    map[string][]models.TradeHistoryEntry
	inMemoryIdentities map[string]localIdentity
	passwordHasher     *auth.PasswordHasher
	tradersMu          sync.RWMutex
	historyMu          sync.RWMutex
	inMemoryIdentityMu sync.RWMutex
}

type localIdentity struct {
	PlayerID     string
	Email        string
	PasswordHash string
	PasswordSalt string
}

func NewServer(engine *market.MarketEngine, database *db.Database) *Server {
	r := gin.Default()
	pepper := os.Getenv("AUTH_PASSWORD_PEPPER")
	if pepper == "" {
		pepper = "dev-pepper-change-me"
		log.Printf("warning: AUTH_PASSWORD_PEPPER is not set, using development default pepper")
	}
	s := &Server{
		engine:             engine,
		router:             r,
		db:                 database,
		inMemoryTraders:    make(map[string]*models.Trader),
		inMemoryHistory:    make(map[string][]models.TradeHistoryEntry),
		inMemoryIdentities: make(map[string]localIdentity),
		passwordHasher:     auth.NewPasswordHasher(pepper),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Public routes
	s.router.POST("/players/register", s.handleRegisterPlayer)
	s.router.POST("/traders", s.handleCreateTrader)

	// Protected routes (require trader authentication)
	authorized := s.router.Group("/")
	authorized.Use(s.traderAuthMiddleware())
	{
		authorized.GET("/market/:town_id", s.handleGetMarket)
		authorized.GET("/town/:town_id", s.handleGetTown)
		authorized.GET("/bulletin/:town_id", s.handleGetBulletin)
		authorized.GET("/trade/history", s.handleGetTradeHistory)
		authorized.GET("/travel/status", s.handleTravelStatus)
		authorized.POST("/travel/start", s.handleTravelStart)
		authorized.POST("/trade/buy", s.handleBuyTrade)
		authorized.POST("/trade/sell", s.handleSellTrade)
	}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *Server) handleGetMarket(c *gin.Context) {
	townID := c.Param("town_id")

	town, ok := s.engine.GetTown(townID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "town not found"})
		return
	}

	trader, player, ok := s.authenticatedTraderAndPlayer(c)
	if !ok {
		return
	}
	if !s.ensurePlayerPositionForTown(c, player, townID) {
		return
	}

	prices, err := s.engine.CurrentPrices(town, player)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"town_id":   town.ID,
		"player_id": trader.PlayerID,
		"prices":    prices,
	})
}

func (s *Server) handleGetBulletin(c *gin.Context) {
	townID := c.Param("town_id")
	if _, ok := s.engine.GetTown(townID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "town not found"})
		return
	}

	_, player, ok := s.authenticatedTraderAndPlayer(c)
	if !ok {
		return
	}
	if !s.ensurePlayerPositionForTown(c, player, townID) {
		return
	}

	entry, ok := s.engine.BulletinBoard.GetEntry(townID)
	if !ok {
		entry, ok = s.createBulletinEntry(townID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "bulletin board entry not found"})
			return
		}
	}

	if entry.IsExpired() {
		entry, ok = s.createBulletinEntry(townID)
		if !ok || entry.IsExpired() {
			c.JSON(http.StatusGone, gin.H{"error": "bulletin board entry has expired"})
			return
		}
	}

	c.JSON(http.StatusOK, entry)
}

func (s *Server) handleGetTown(c *gin.Context) {
	townID := c.Param("town_id")

	town, ok := s.engine.GetTown(townID)
	if !ok {
		c.JSON(http.StatusNotFound, TownResponse{
			Success: false,
			Message: "Town not found",
		})
		return
	}

	_, player, ok := s.authenticatedTraderAndPlayer(c)
	if !ok {
		return
	}
	if !s.ensurePlayerPositionForTown(c, player, townID) {
		return
	}

	prices, err := s.engine.CurrentPrices(town, player)
	if err != nil {
		c.JSON(http.StatusInternalServerError, TownResponse{
			Success: false,
			Message: "Failed to calculate prices",
		})
		return
	}

	c.JSON(http.StatusOK, TownResponse{
		Success: true,
		Town: &TownViewModel{
			ID:                  town.ID,
			Name:                town.Name,
			Prosperity:          town.Prosperity,
			Neighbors:           append([]string(nil), town.Neighbors...),
			Inventory:           town.Inventory.Snapshot(),
			Prices:              prices,
			Consumption:         town.Consumption,
			OptionalConsumption: town.OptionalConsumption,
			Production:          s.townProduction(),
			UpdatedAt:           town.UpdatedAt,
		},
	})
}

func (s *Server) handleBuyTrade(c *gin.Context) {
	var req TradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, TradeResponse{
			Success: false,
			Message: "Invalid request: " + err.Error(),
		})
		return
	}

	// Get trader from context
	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, TradeResponse{
			Success: false,
			Message: "No trader authentication",
		})
		return
	}
	player, err := s.getPlayerByID(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusNotFound, TradeResponse{
			Success: false,
			Message: "Player not found",
		})
		return
	}

	// Validate: Town exists
	town, ok := s.engine.GetTown(req.TownID)
	if !ok {
		c.JSON(http.StatusNotFound, TradeResponse{
			Success: false,
			Message: "Town not found",
		})
		return
	}

	result, err := s.engine.Buy(player, town, req.Resource, req.Quantity)
	if err != nil {
		log.Printf("buy trade rejected player=%s town=%s resource=%s quantity=%d err=%v", player.ID, town.ID, req.Resource, req.Quantity, err)
		c.JSON(tradeErrorStatus(err), TradeResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	pricePerUnit := models.Currency(int64(result.TradeValue) / req.Quantity)
	if err := s.recordTradeAndRollbackOnFailure(player, town, req.Resource, req.Quantity, pricePerUnit, "buy", result.TradeValue); err != nil {
		log.Printf("buy trade persist failed player=%s town=%s resource=%s quantity=%d err=%v", player.ID, town.ID, req.Resource, req.Quantity, err)
		c.JSON(http.StatusInternalServerError, TradeResponse{
			Success: false,
			Message: "Failed to persist trade",
		})
		return
	}
	log.Printf("buy trade completed player=%s town=%s resource=%s quantity=%d total=%d", player.ID, town.ID, req.Resource, req.Quantity, result.TradeValue)

	c.JSON(http.StatusOK, TradeResponse{
		Success:      true,
		Message:      "Buy trade completed successfully",
		NewBalance:   result.NewBalance,
		NewInventory: result.NewInventory,
		TotalCost:    result.TradeValue,
	})
}

func (s *Server) handleSellTrade(c *gin.Context) {
	var req TradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, TradeResponse{
			Success: false,
			Message: "Invalid request: " + err.Error(),
		})
		return
	}

	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, TradeResponse{
			Success: false,
			Message: "No trader authentication",
		})
		return
	}

	player, err := s.getPlayerByID(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusNotFound, TradeResponse{
			Success: false,
			Message: "Player not found",
		})
		return
	}

	town, ok := s.engine.GetTown(req.TownID)
	if !ok {
		c.JSON(http.StatusNotFound, TradeResponse{
			Success: false,
			Message: "Town not found",
		})
		return
	}

	result, err := s.engine.Sell(player, town, req.Resource, req.Quantity)
	if err != nil {
		log.Printf("sell trade rejected player=%s town=%s resource=%s quantity=%d err=%v", player.ID, town.ID, req.Resource, req.Quantity, err)
		c.JSON(tradeErrorStatus(err), TradeResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	pricePerUnit := models.Currency(int64(result.TradeValue) / req.Quantity)
	if err := s.recordTradeAndRollbackOnFailure(player, town, req.Resource, req.Quantity, pricePerUnit, "sell", result.TradeValue); err != nil {
		log.Printf("sell trade persist failed player=%s town=%s resource=%s quantity=%d err=%v", player.ID, town.ID, req.Resource, req.Quantity, err)
		c.JSON(http.StatusInternalServerError, TradeResponse{
			Success: false,
			Message: "Failed to persist trade",
		})
		return
	}
	log.Printf("sell trade completed player=%s town=%s resource=%s quantity=%d total=%d", player.ID, town.ID, req.Resource, req.Quantity, result.TradeValue)

	c.JSON(http.StatusOK, TradeResponse{
		Success:      true,
		Message:      "Sell trade completed successfully",
		NewBalance:   result.NewBalance,
		NewInventory: result.NewInventory,
		TotalCost:    result.TradeValue,
	})
}

func (s *Server) handleGetTradeHistory(c *gin.Context) {
	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, TradeHistoryResponse{
			Success: false,
			Message: "No trader authentication",
		})
		return
	}

	history, err := s.getTradeHistory(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, TradeHistoryResponse{
			Success: false,
			Message: "Failed to load trade history",
		})
		return
	}

	c.JSON(http.StatusOK, TradeHistoryResponse{
		Success: true,
		History: history,
	})
}

func (s *Server) handleTravelStatus(c *gin.Context) {
	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, TravelResponse{Success: false, Message: "No trader authentication"})
		return
	}
	player, err := s.getPlayerByID(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusNotFound, TravelResponse{Success: false, Message: "Player not found"})
		return
	}

	arrived := s.engine.ResolveArrival(player)
	if arrived && s.db != nil {
		if err := s.db.PersistPlayerState(player); err != nil {
			log.Printf("travel status persist failed player=%s err=%v", player.ID, err)
			c.JSON(http.StatusInternalServerError, TravelResponse{Success: false, Message: "Failed to persist player travel state"})
			return
		}
		log.Printf("travel arrival resolved player=%s location=%s", player.ID, player.Location)
	}

	// In local mode, prefer the canonical in-engine player if it exists.
	if s.db == nil {
		if enginePlayer, ok := s.engine.GetPlayer(player.ID); ok {
			player = enginePlayer
		}
	}

	c.JSON(http.StatusOK, TravelResponse{
		Success:  true,
		PlayerID: player.ID,
		Location: player.Location,
		Travel:   &player.Travel,
	})
}

func (s *Server) handleTravelStart(c *gin.Context) {
	var req TravelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, TravelResponse{Success: false, Message: "Invalid request: " + err.Error()})
		return
	}

	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, TravelResponse{Success: false, Message: "No trader authentication"})
		return
	}
	player, err := s.getPlayerByID(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusNotFound, TravelResponse{Success: false, Message: "Player not found"})
		return
	}

	if err := s.engine.StartTravel(player, req.DestinationTownID, req.Equipment); err != nil {
		log.Printf("travel start rejected player=%s from=%s to=%s equipment=%s err=%v", player.ID, player.Location, req.DestinationTownID, req.Equipment, err)
		status := http.StatusBadRequest
		if err == market.ErrTownNil || err == market.ErrInvalidDestinationTown {
			status = http.StatusNotFound
		}
		c.JSON(status, TravelResponse{Success: false, Message: err.Error()})
		return
	}

	if s.db != nil {
		if err := s.db.PersistPlayerState(player); err != nil {
			log.Printf("travel start persist failed player=%s to=%s err=%v", player.ID, req.DestinationTownID, err)
			c.JSON(http.StatusInternalServerError, TravelResponse{Success: false, Message: "Failed to persist player travel state"})
			return
		}
	}
	log.Printf("travel started player=%s from=%s to=%s equipment=%s arrives_at=%s", player.ID, player.Travel.FromTown, player.Travel.ToTown, player.Travel.Equipment, player.Travel.ArrivesAt.UTC().Format(time.RFC3339))

	c.JSON(http.StatusOK, TravelResponse{
		Success:  true,
		Message:  "Travel started",
		PlayerID: player.ID,
		Location: player.Location,
		Travel:   &player.Travel,
	})
}

func (s *Server) handleRegisterPlayer(c *gin.Context) {
	var req RegisterPlayerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("register player rejected invalid payload err=%v", err)
		c.JSON(http.StatusBadRequest, RegisterPlayerResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, RegisterPlayerResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, RegisterPlayerResponse{
			Success: false,
			Message: "name is required",
		})
		return
	}
	password := strings.TrimSpace(req.Password)
	if len(password) < 8 {
		c.JSON(http.StatusBadRequest, RegisterPlayerResponse{
			Success: false,
			Message: "password must be at least 8 characters",
		})
		return
	}

	passwordHash, passwordSalt, err := s.passwordHasher.HashWithNewSalt(password)
	if err != nil {
		log.Printf("register player hash failed email=%s err=%v", email, err)
		c.JSON(http.StatusInternalServerError, RegisterPlayerResponse{
			Success: false,
			Message: "failed to hash password",
		})
		return
	}

	startTownID, err := s.defaultStartingTownID()
	if err != nil {
		log.Printf("register player failed no towns available err=%v", err)
		c.JSON(http.StatusInternalServerError, RegisterPlayerResponse{
			Success: false,
			Message: "no starting town configured",
		})
		return
	}

	player := models.NewPlayer(generatePlayerID(), name, startTownID, 100)

	if s.db != nil {
		err = s.db.CreatePlayerWithLocalIdentity(&player, email, passwordHash, passwordSalt)
		if err == db.ErrEmailAlreadyExists {
			c.JSON(http.StatusConflict, RegisterPlayerResponse{
				Success: false,
				Message: "email already registered",
			})
			return
		}
		if err != nil {
			log.Printf("register player persist failed email=%s err=%v", email, err)
			c.JSON(http.StatusInternalServerError, RegisterPlayerResponse{
				Success: false,
				Message: "failed to create player",
			})
			return
		}
	} else {
		if err := s.registerPlayerInMemory(&player, email, passwordHash, passwordSalt); err != nil {
			c.JSON(http.StatusConflict, RegisterPlayerResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}
	}

	log.Printf("player registered player=%s email=%s provider=%s", player.ID, email, auth.LocalIdentityProvider)
	c.JSON(http.StatusCreated, RegisterPlayerResponse{
		Success: true,
		Message: "Player registered successfully",
		Player: &RegisteredPlayer{
			ID:               player.ID,
			Name:             player.Name,
			Email:            email,
			Location:         player.Location,
			Balance:          player.Balance,
			IdentityProvider: auth.LocalIdentityProvider,
		},
	})
}

func (s *Server) handleCreateTrader(c *gin.Context) {
	var req CreateTraderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("create trader rejected invalid payload err=%v", err)
		c.JSON(http.StatusBadRequest, CreateTraderResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid request: %v", err),
		})
		return
	}

	// Validate player exists
	if s.db != nil {
		_, err := s.db.GetPlayerByID(req.PlayerID)
		if err != nil {
			log.Printf("create trader rejected player not found player=%s", req.PlayerID)
			c.JSON(http.StatusNotFound, CreateTraderResponse{
				Success: false,
				Message: "Player not found",
			})
			return
		}
	} else {
		_, exists := s.engine.GetPlayer(req.PlayerID)
		if !exists {
			log.Printf("create trader rejected player not found player=%s", req.PlayerID)
			c.JSON(http.StatusNotFound, CreateTraderResponse{
				Success: false,
				Message: "Player not found",
			})
			return
		}
	}

	// Generate unique trader ID and token
	traderID := generateTraderID()
	token := generateTraderToken()

	// Hash the token for storage (we'll store the hash, return the plain token)
	tokenHash := hashToken(token)

	// Create trader
	trader := models.NewTrader(traderID, req.PlayerID, req.Name, tokenHash)

	s.storeInMemoryTrader(tokenHash, &trader)

	// Persist trader to database if available
	if s.db != nil {
		err := s.db.CreateTrader(&trader)
		if err != nil {
			log.Printf("create trader persist failed player=%s err=%v", req.PlayerID, err)
			c.JSON(http.StatusInternalServerError, CreateTraderResponse{
				Success: false,
				Message: fmt.Sprintf("Failed to create trader: %v", err),
			})
			return
		}
	}

	c.JSON(http.StatusCreated, CreateTraderResponse{
		Success: true,
		Message: "Trader created successfully",
		Trader:  trader,
		Token:   token,
	})
	log.Printf("trader created trader=%s player=%s name=%s", trader.ID, trader.PlayerID, trader.Name)
}

func (s *Server) getAuthenticatedTrader(c *gin.Context) (*models.Trader, bool) {
	traderInterface, exists := c.Get("trader")
	if !exists {
		return nil, false
	}

	trader, ok := traderInterface.(*models.Trader)
	if !ok {
		return nil, false
	}

	return trader, true
}

func (s *Server) authenticatedTraderAndPlayer(c *gin.Context) (*models.Trader, *models.Player, bool) {
	trader, ok := s.getAuthenticatedTrader(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No trader authentication"})
		return nil, nil, false
	}

	player, err := s.getPlayerByID(trader.PlayerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
		return nil, nil, false
	}

	return trader, player, true
}

func (s *Server) ensurePlayerPositionForTown(c *gin.Context, player *models.Player, townID string) bool {
	if strings.TrimSpace(player.Location) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "player has no position"})
		return false
	}
	if player.Travel.InTransit {
		c.JSON(http.StatusForbidden, gin.H{"error": "player is in transit and cannot access market prices"})
		return false
	}
	if player.Location != townID {
		c.JSON(http.StatusForbidden, gin.H{"error": "player is not in this town"})
		return false
	}
	return true
}

func (s *Server) createBulletinEntry(townID string) (models.BulletinBoardEntry, bool) {
	town, ok := s.engine.GetTown(townID)
	if !ok {
		return models.BulletinBoardEntry{}, false
	}

	prices, err := s.engine.CurrentPrices(town, nil)
	if err != nil {
		return models.BulletinBoardEntry{}, false
	}

	minAmount := make(map[models.ResourceID]int64)
	maxAmount := make(map[models.ResourceID]int64)
	for resource := range models.ResourceCatalog {
		minAmount[resource] = 0
		maxAmount[resource] = town.Inventory.Quantity(resource)
	}

	s.engine.BulletinBoard.Update(townID, prices, minAmount, maxAmount)
	entry, ok := s.engine.BulletinBoard.GetEntry(townID)
	return entry, ok
}

func (s *Server) getPlayerByID(playerID string) (*models.Player, error) {
	if s.db != nil {
		return s.db.GetPlayerByID(playerID)
	}

	if player, ok := s.engine.GetPlayer(playerID); ok {
		return player, nil
	}

	return nil, fmt.Errorf("player not found")
}

func (s *Server) registerPlayerInMemory(player *models.Player, email, passwordHash, passwordSalt string) error {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	s.inMemoryIdentityMu.Lock()
	defer s.inMemoryIdentityMu.Unlock()

	if _, exists := s.inMemoryIdentities[normalizedEmail]; exists {
		return fmt.Errorf("email already registered")
	}

	s.engine.Players[player.ID] = player
	s.inMemoryIdentities[normalizedEmail] = localIdentity{
		PlayerID:     player.ID,
		Email:        normalizedEmail,
		PasswordHash: passwordHash,
		PasswordSalt: passwordSalt,
	}
	return nil
}

func (s *Server) storeInMemoryTrader(tokenHash string, trader *models.Trader) {
	s.tradersMu.Lock()
	defer s.tradersMu.Unlock()
	s.inMemoryTraders[tokenHash] = trader
}

func (s *Server) getInMemoryTrader(tokenHash string) (*models.Trader, bool) {
	s.tradersMu.RLock()
	defer s.tradersMu.RUnlock()
	trader, ok := s.inMemoryTraders[tokenHash]
	return trader, ok
}

func (s *Server) townProduction() []models.RefineRecipe {
	production := make([]models.RefineRecipe, 0, len(s.engine.Recipes))
	for _, recipe := range s.engine.Recipes {
		production = append(production, recipe)
	}
	return production
}

func (s *Server) appendInMemoryTrade(entry models.TradeHistoryEntry) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.inMemoryHistory[entry.PlayerID] = append([]models.TradeHistoryEntry{entry}, s.inMemoryHistory[entry.PlayerID]...)
}

func (s *Server) getTradeHistory(playerID string) ([]models.TradeHistoryEntry, error) {
	if s.db != nil {
		return s.db.GetTradeHistoryByPlayerID(playerID, 50)
	}

	s.historyMu.RLock()
	defer s.historyMu.RUnlock()
	history := s.inMemoryHistory[playerID]
	copied := make([]models.TradeHistoryEntry, len(history))
	copy(copied, history)
	return copied, nil
}

func (s *Server) recordTradeAndRollbackOnFailure(player *models.Player, town *models.Town, resource models.ResourceID, quantity int64, pricePerUnit models.Currency, tradeType string, totalValue models.Currency) error {
	if s.db != nil {
		if err := s.db.RecordTrade(player, town.ID, resource, quantity, pricePerUnit, tradeType); err != nil {
			rollbackTrade(player, town, resource, quantity, totalValue, tradeType)
			return err
		}
	}

	s.appendInMemoryTrade(models.TradeHistoryEntry{
		PlayerID:     player.ID,
		TownID:       town.ID,
		ResourceID:   resource,
		Quantity:     quantity,
		PricePerUnit: pricePerUnit,
		TotalCost:    totalValue,
		TradeType:    tradeType,
		CreatedAt:    nowUTC(),
	})

	return nil
}

func (s *Server) defaultStartingTownID() (string, error) {
	if _, ok := s.engine.Towns["town_1"]; ok {
		return "town_1", nil
	}
	for townID := range s.engine.Towns {
		return townID, nil
	}
	return "", fmt.Errorf("no towns available")
}

func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address == "" {
		return "", fmt.Errorf("invalid email address")
	}
	if parsed.Address != normalized {
		return "", fmt.Errorf("invalid email address")
	}
	return normalized, nil
}

func rollbackTrade(player *models.Player, town *models.Town, resource models.ResourceID, quantity int64, totalValue models.Currency, tradeType string) {
	switch tradeType {
	case "buy":
		player.Balance += totalValue
		player.Inventory.Remove(resource, quantity)
		town.Inventory.Add(resource, quantity)
	case "sell":
		player.Balance -= totalValue
		player.Inventory.Add(resource, quantity)
		town.Inventory.Remove(resource, quantity)
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

// generateTraderID creates a unique ID for a trader
func generateTraderID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return "trader_" + hex.EncodeToString(bytes)[:16]
}

func generatePlayerID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return "player_" + hex.EncodeToString(bytes)[:16]
}

// generateTraderToken creates a secure bearer token
func generateTraderToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return "trader_" + hex.EncodeToString(bytes)
}

// hashToken creates a SHA-256 hash of the token for secure storage
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func tradeErrorStatus(err error) int {
	switch err {
	case market.ErrTownNil, market.ErrPlayerNil:
		return http.StatusNotFound
	case market.ErrInvalidQuantity, market.ErrPlayerNotAtTown, market.ErrResourceUnavailable,
		market.ErrInsufficientFunds, market.ErrUnknownResource, market.ErrInsufficientWeight,
		market.ErrInsufficientVolume, market.ErrInsufficientTownStock, market.ErrInsufficientPlayerStock,
		market.ErrPlayerInTransit:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// traderAuthMiddleware validates trader bearer tokens
func (s *Server) traderAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Printf("auth rejected missing authorization header path=%s", c.FullPath())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Extract token from "Bearer <token>" format
		const bearerPrefix = "Bearer "
		if len(authHeader) <= len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
			log.Printf("auth rejected invalid bearer format path=%s", c.FullPath())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format. Use 'Bearer <token>'"})
			c.Abort()
			return
		}

		token := authHeader[len(bearerPrefix):]
		if token == "" {
			log.Printf("auth rejected empty token path=%s", c.FullPath())
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token is required"})
			c.Abort()
			return
		}

		// Validate token
		if s.db != nil {
			// Validate token against database
			tokenHash := hashToken(token)
			trader, err := s.db.GetTraderByTokenHash(tokenHash)
			if err != nil {
				log.Printf("auth rejected invalid token path=%s", c.FullPath())
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
				c.Abort()
				return
			}
			// Set trader info in context for handlers to use
			c.Set("trader", trader)
		} else {
			if !strings.HasPrefix(token, "trader_") {
				log.Printf("auth rejected invalid token prefix path=%s", c.FullPath())
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token format"})
				c.Abort()
				return
			}

			tokenHash := hashToken(token)
			trader, ok := s.getInMemoryTrader(tokenHash)
			if !ok {
				log.Printf("auth rejected unknown token path=%s", c.FullPath())
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
				c.Abort()
				return
			}

			c.Set("trader", trader)
		}

		// Set trader token in context for handlers to use
		c.Set("trader_token", token)
		c.Next()
	}
}
