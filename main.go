package main

import (
	"ape-trader/internal/api"
	"ape-trader/internal/db"
	"ape-trader/internal/market"
	"ape-trader/internal/models"
	"log"
	"os"
)

func main() {
	// Try to connect to database
	dbHost := os.Getenv("DB_HOST")
	var database *db.Database
	var towns map[string]*models.Town
	if dbHost != "" {
		dbPort := os.Getenv("DB_PORT")
		if dbPort == "" {
			dbPort = "5432"
		}
		dbUser := os.Getenv("DB_USER")
		dbPassword := os.Getenv("DB_PASSWORD")
		dbName := os.Getenv("DB_NAME")
		if dbName == "" {
			dbName = "apetrader_db"
		}

		var err error
		database, err = db.New(dbHost, dbPort, dbUser, dbPassword, dbName)
		if err != nil {
			log.Printf("Warning: Failed to connect to database: %v", err)
			database = nil
		} else {
			defer database.Close()
			log.Println("Connected to PostgreSQL database")
			if err := database.InitializeSchemaIfNeeded("schema.sql"); err != nil {
				log.Printf("Warning: Failed to initialize database schema: %v", err)
			}

			towns, err = database.LoadTowns()
			if err != nil {
				log.Printf("Warning: Failed to load towns from database: %v", err)
				towns = nil
			} else if len(towns) == 0 {
				log.Printf("Warning: No towns loaded from database, falling back to towns.json")
				towns = nil
			} else {
				log.Printf("Loaded %d towns from PostgreSQL", len(towns))
			}
		}
	}

	engine := market.NewMarketEngineWithTownsAndStore(towns, database)
	engine.StartMinuteTick()

	server := api.NewServer(engine, database)
	log.Println("Starting API server on :8080")
	server.Run(":8080")
}
