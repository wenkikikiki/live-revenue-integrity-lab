-- +goose Up
INSERT INTO users (user_id, age_years, region_code, account_standing, can_go_live, live_gifts_enabled, account_type)
VALUES
    (1001, 25, 'US', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (1002, 27, 'US', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (2001, 30, 'US', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (2002, 28, 'US', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (3001, 17, 'US', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (3002, 20, 'KR', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (3003, 19, 'KR', 'GOOD', TRUE, TRUE, 'NORMAL'),
    (4001, 42, 'US', 'GOOD', TRUE, TRUE, 'GOVERNMENT')
ON DUPLICATE KEY UPDATE user_id = VALUES(user_id);

INSERT INTO wallet_accounts (user_id, currency, available_balance)
VALUES
    (1001, 'COIN', 50000),
    (1002, 'COIN', 50000),
    (2001, 'COIN', 1000),
    (2002, 'COIN', 1000)
ON DUPLICATE KEY UPDATE available_balance = VALUES(available_balance);

INSERT INTO gift_catalog (gift_id, display_name, coin_price, match_points, diamond_reward, enabled)
VALUES
    ('ROSE', 'Rose', 1, 1, 1, TRUE),
    ('HEART', 'Heart', 5, 5, 5, TRUE),
    ('GG', 'GG', 10, 10, 10, TRUE),
    ('DONUT', 'Donut', 30, 30, 30, TRUE),
    ('COFFEE', 'Coffee', 50, 50, 50, TRUE),
    ('CLOUD', 'Cloud', 99, 99, 99, TRUE),
    ('PERFUME', 'Perfume', 199, 199, 199, TRUE),
    ('RING', 'Ring', 299, 299, 299, TRUE),
    ('FIREWORK', 'Firework', 399, 399, 399, TRUE),
    ('DRUM', 'Drum', 499, 499, 499, TRUE),
    ('CASTLE', 'Castle', 999, 999, 999, TRUE),
    ('LION', 'Lion', 1999, 1999, 1999, TRUE)
ON DUPLICATE KEY UPDATE gift_id = VALUES(gift_id);

INSERT INTO live_sessions (live_session_id, creator_id, campaign_id, status, started_at, closed_at)
VALUES
    (9001, 1001, 7001, 'OPEN', UTC_TIMESTAMP(6), NULL),
    (9002, 1002, NULL, 'OPEN', UTC_TIMESTAMP(6), NULL)
ON DUPLICATE KEY UPDATE status = VALUES(status), closed_at = VALUES(closed_at);

INSERT INTO live_matches (match_id, live_session_id, mode, specific_gift_id, status)
VALUES
    (8001, 9001, 'ALL_GIFTS', NULL, 'OPEN'),
    (8002, 9002, 'SPECIFIC_GIFT', 'ROSE', 'OPEN')
ON DUPLICATE KEY UPDATE status = VALUES(status), mode = VALUES(mode), specific_gift_id = VALUES(specific_gift_id);

-- +goose Down
DELETE FROM live_matches WHERE match_id IN (8001, 8002);
DELETE FROM live_sessions WHERE live_session_id IN (9001, 9002);
DELETE FROM gift_catalog WHERE gift_id IN ('ROSE','HEART','GG','DONUT','COFFEE','CLOUD','PERFUME','RING','FIREWORK','DRUM','CASTLE','LION');
DELETE FROM wallet_accounts WHERE user_id IN (1001,1002,2001,2002) AND currency = 'COIN';
DELETE FROM users WHERE user_id IN (1001,1002,2001,2002,3001,3002,3003,4001);
