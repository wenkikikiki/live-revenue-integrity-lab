-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    user_id BIGINT UNSIGNED PRIMARY KEY,
    age_years TINYINT UNSIGNED NOT NULL,
    region_code CHAR(2) NOT NULL,
    account_standing ENUM('GOOD','RESTRICTED','BANNED') NOT NULL,
    can_go_live BOOLEAN NOT NULL,
    live_gifts_enabled BOOLEAN NOT NULL,
    account_type ENUM('NORMAL','GOVERNMENT','POLITICIAN','POLITICAL_PARTY','PUBLIC_INTEREST') NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS live_sessions (
    live_session_id BIGINT UNSIGNED PRIMARY KEY,
    creator_id BIGINT UNSIGNED NOT NULL,
    campaign_id BIGINT UNSIGNED NULL,
    status ENUM('OPEN','CLOSED') NOT NULL,
    started_at DATETIME(6) NOT NULL,
    closed_at DATETIME(6) NULL,
    CONSTRAINT fk_live_session_creator FOREIGN KEY (creator_id) REFERENCES users(user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS live_matches (
    match_id BIGINT UNSIGNED PRIMARY KEY,
    live_session_id BIGINT UNSIGNED NOT NULL,
    mode ENUM('ALL_GIFTS','SPECIFIC_GIFT') NOT NULL,
    specific_gift_id VARCHAR(32) NULL,
    status ENUM('OPEN','CLOSED') NOT NULL,
    CONSTRAINT fk_live_match_session FOREIGN KEY (live_session_id) REFERENCES live_sessions(live_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gift_catalog (
    gift_id VARCHAR(32) PRIMARY KEY,
    display_name VARCHAR(64) NOT NULL,
    coin_price INT UNSIGNED NOT NULL,
    match_points INT UNSIGNED NOT NULL,
    diamond_reward INT UNSIGNED NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wallet_accounts (
    user_id BIGINT UNSIGNED NOT NULL,
    currency ENUM('COIN') NOT NULL,
    available_balance BIGINT NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id, currency),
    CONSTRAINT fk_wallet_account_user FOREIGN KEY (user_id) REFERENCES users(user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wallet_transactions (
    tx_id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    tx_type ENUM('RECHARGE','GIFT_DEBIT') NOT NULL,
    actor_user_id BIGINT UNSIGNED NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    body_hash BINARY(32) NULL,
    created_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_wallet_tx (actor_user_id, tx_type, request_id),
    CONSTRAINT fk_wallet_tx_user FOREIGN KEY (actor_user_id) REFERENCES users(user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS wallet_entries (
    tx_id BIGINT UNSIGNED NOT NULL,
    account_code VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    PRIMARY KEY (tx_id, account_code),
    CONSTRAINT fk_wallet_entry_tx FOREIGN KEY (tx_id) REFERENCES wallet_transactions(tx_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS recharge_requests (
    viewer_id BIGINT UNSIGNED NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    body_hash BINARY(32) NOT NULL,
    coins INT UNSIGNED NOT NULL,
    payment_ref VARCHAR(128) NOT NULL,
    resulting_balance BIGINT NOT NULL,
    tx_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (viewer_id, request_id),
    CONSTRAINT fk_recharge_tx FOREIGN KEY (tx_id) REFERENCES wallet_transactions(tx_id),
    CONSTRAINT fk_recharge_user FOREIGN KEY (viewer_id) REFERENCES users(user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gift_orders (
    gift_order_id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    viewer_id BIGINT UNSIGNED NOT NULL,
    creator_id BIGINT UNSIGNED NOT NULL,
    live_session_id BIGINT UNSIGNED NOT NULL,
    match_id BIGINT UNSIGNED NULL,
    gift_id VARCHAR(32) NOT NULL,
    quantity SMALLINT UNSIGNED NOT NULL,
    charged_coins INT UNSIGNED NOT NULL,
    match_points_added INT UNSIGNED NOT NULL,
    diamond_reward INT UNSIGNED NOT NULL,
    post_balance BIGINT NULL,
    status ENUM('ACCEPTED','REJECTED') NOT NULL,
    reject_code VARCHAR(64) NULL,
    request_id VARCHAR(64) NOT NULL,
    body_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_gift_req (viewer_id, request_id),
    INDEX idx_gift_orders_live (live_session_id, status),
    INDEX idx_gift_orders_match (match_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS outbox_events (
    event_id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    topic VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    payload_json JSON NOT NULL,
    published_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL,
    INDEX idx_outbox_publish (published_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS consumer_dedupe (
    consumer_name VARCHAR(64) NOT NULL,
    event_id BIGINT UNSIGNED NOT NULL,
    applied_at DATETIME(6) NOT NULL,
    PRIMARY KEY (consumer_name, event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS fan_point_ledger (
    event_id BIGINT UNSIGNED PRIMARY KEY,
    viewer_id BIGINT UNSIGNED NOT NULL,
    creator_id BIGINT UNSIGNED NOT NULL,
    live_session_id BIGINT UNSIGNED NOT NULL,
    points INT NOT NULL,
    reason ENUM('GIFT','COMMENT','WATCH_MINUTE') NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_fan_points_lookup (viewer_id, live_session_id, reason),
    CONSTRAINT fk_fan_points_viewer FOREIGN KEY (viewer_id) REFERENCES users(user_id),
    CONSTRAINT fk_fan_points_creator FOREIGN KEY (creator_id) REFERENCES users(user_id),
    CONSTRAINT fk_fan_points_live FOREIGN KEY (live_session_id) REFERENCES live_sessions(live_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS campaign_point_ledger (
    event_id BIGINT UNSIGNED PRIMARY KEY,
    campaign_id BIGINT UNSIGNED NOT NULL,
    creator_id BIGINT UNSIGNED NOT NULL,
    points INT NOT NULL,
    reason ENUM('GIFT') NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_campaign_points_lookup (campaign_id, creator_id),
    CONSTRAINT fk_campaign_points_creator FOREIGN KEY (creator_id) REFERENCES users(user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS stream_settlements (
    live_session_id BIGINT UNSIGNED PRIMARY KEY,
    gross_coin_spend BIGINT NOT NULL,
    accepted_gift_count BIGINT NOT NULL,
    diamond_reward_total BIGINT NOT NULL,
    generated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_settlement_live FOREIGN KEY (live_session_id) REFERENCES live_sessions(live_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS reconciliation_results (
    live_session_id BIGINT UNSIGNED PRIMARY KEY,
    status ENUM('PASS','FAIL') NOT NULL,
    wallet_gift_debit_total BIGINT NOT NULL,
    gift_order_coin_total BIGINT NOT NULL,
    mismatch_count INT NOT NULL,
    details_json JSON NOT NULL,
    generated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_reconciliation_live FOREIGN KEY (live_session_id) REFERENCES live_sessions(live_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS reconciliation_results;
DROP TABLE IF EXISTS stream_settlements;
DROP TABLE IF EXISTS campaign_point_ledger;
DROP TABLE IF EXISTS fan_point_ledger;
DROP TABLE IF EXISTS consumer_dedupe;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS gift_orders;
DROP TABLE IF EXISTS recharge_requests;
DROP TABLE IF EXISTS wallet_entries;
DROP TABLE IF EXISTS wallet_transactions;
DROP TABLE IF EXISTS wallet_accounts;
DROP TABLE IF EXISTS gift_catalog;
DROP TABLE IF EXISTS live_matches;
DROP TABLE IF EXISTS live_sessions;
DROP TABLE IF EXISTS users;
