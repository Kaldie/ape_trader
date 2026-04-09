-- Resources enum
CREATE TABLE resources (
    id VARCHAR(50) PRIMARY KEY,
    weight_kg INT NOT NULL,
    volume_l INT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO resources (id, weight_kg, volume_l) VALUES
    ('wood', 5, 10),
    ('stone', 20, 8),
    ('ore', 30, 5),
    ('coal', 10, 10),
    ('metal', 25, 4),
    ('furniture', 15, 15),
    ('tools', 8, 6),
    ('weapons', 12, 5),
    ('armor', 18, 8);

-- Towns
CREATE TABLE towns (
    id VARCHAR(100) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    x DOUBLE PRECISION NOT NULL DEFAULT 0,
    y DOUBLE PRECISION NOT NULL DEFAULT 0,
    prosperity BIGINT NOT NULL DEFAULT 100,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Town neighbors
CREATE TABLE town_neighbors (
    town_id VARCHAR(100) NOT NULL,
    neighbor_id VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (town_id, neighbor_id),
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE,
    FOREIGN KEY (neighbor_id) REFERENCES towns(id) ON DELETE CASCADE
);

-- Town inventory
CREATE TABLE town_inventory (
    id SERIAL PRIMARY KEY,
    town_id VARCHAR(100) NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(town_id, resource_id),
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Town supply and demand
CREATE TABLE town_supply_demand (
    id SERIAL PRIMARY KEY,
    town_id VARCHAR(100) NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    supply BIGINT NOT NULL DEFAULT 0,
    demand BIGINT NOT NULL DEFAULT 0,
    base_buy_price BIGINT NOT NULL DEFAULT 10,
    base_sell_price BIGINT NOT NULL DEFAULT 8,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(town_id, resource_id),
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Players
CREATE TABLE players (
    id VARCHAR(100) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    location VARCHAR(100) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 100,
    pants_max_weight INT NOT NULL DEFAULT 50,
    pants_max_volume INT NOT NULL DEFAULT 40,
    travel_in_transit BOOLEAN NOT NULL DEFAULT FALSE,
    travel_from_town VARCHAR(100),
    travel_to_town VARCHAR(100),
    travel_equipment VARCHAR(20),
    travel_started_at TIMESTAMP,
    travel_arrives_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (location) REFERENCES towns(id) ON DELETE RESTRICT
);

-- Player identities (local credentials now, extensible to external IdP subjects later)
CREATE TABLE player_identities (
    id SERIAL PRIMARY KEY,
    player_id VARCHAR(100) NOT NULL,
    provider VARCHAR(50) NOT NULL, -- e.g. 'local', 'google'
    subject VARCHAR(255) NOT NULL, -- email for local, provider subject for IdP
    password_hash VARCHAR(255),
    password_salt VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, subject),
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

-- Player inventory
CREATE TABLE player_inventory (
    id SERIAL PRIMARY KEY,
    player_id VARCHAR(100) NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(player_id, resource_id),
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Player reputation by town
CREATE TABLE player_reputation (
    id SERIAL PRIMARY KEY,
    player_id VARCHAR(100) NOT NULL,
    town_id VARCHAR(100) NOT NULL,
    reputation BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(player_id, town_id),
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE,
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE
);

-- Bulletin board entries
CREATE TABLE bulletin_board_entries (
    id SERIAL PRIMARY KEY,
    town_id VARCHAR(100) NOT NULL UNIQUE,
    timestamp TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE
);

-- Bulletin board prices (snapshot)
CREATE TABLE bulletin_board_prices (
    id SERIAL PRIMARY KEY,
    entry_id SERIAL NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    buy_price BIGINT NOT NULL,
    sell_price BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (entry_id) REFERENCES bulletin_board_entries(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Bulletin board amounts (min/max)
CREATE TABLE bulletin_board_amounts (
    id SERIAL PRIMARY KEY,
    entry_id SERIAL NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    min_amount BIGINT NOT NULL DEFAULT 0,
    max_amount BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (entry_id) REFERENCES bulletin_board_entries(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Trade history (audit log)
CREATE TABLE trade_history (
    id SERIAL PRIMARY KEY,
    player_id VARCHAR(100) NOT NULL,
    town_id VARCHAR(100) NOT NULL,
    resource_id VARCHAR(50) NOT NULL,
    quantity BIGINT NOT NULL,
    price_per_unit BIGINT NOT NULL,
    total_cost BIGINT NOT NULL,
    trade_type VARCHAR(10) NOT NULL, -- 'buy' or 'sell'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE,
    FOREIGN KEY (town_id) REFERENCES towns(id) ON DELETE CASCADE,
    FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
);

-- Traders (AI trading agents)
CREATE TABLE traders (
    id VARCHAR(100) PRIMARY KEY,
    player_id VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    token_hash VARCHAR(255) NOT NULL UNIQUE, -- Hashed token for security
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (player_id) REFERENCES players(id) ON DELETE CASCADE
);

-- Indexes for performance
CREATE INDEX idx_player_location ON players(location);
CREATE INDEX idx_player_identities_player ON player_identities(player_id);
CREATE INDEX idx_player_inventory_player ON player_inventory(player_id);
CREATE INDEX idx_player_inventory_resource ON player_inventory(resource_id);
CREATE INDEX idx_town_inventory_town ON town_inventory(town_id);
CREATE INDEX idx_town_inventory_resource ON town_inventory(resource_id);
CREATE INDEX idx_player_reputation_player ON player_reputation(player_id);
CREATE INDEX idx_player_reputation_town ON player_reputation(town_id);
CREATE INDEX idx_trade_history_player ON trade_history(player_id);
CREATE INDEX idx_trade_history_town ON trade_history(town_id);
CREATE INDEX idx_trade_history_created ON trade_history(created_at);
CREATE INDEX idx_bulletin_expires ON bulletin_board_entries(expires_at);
