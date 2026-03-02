-- +goose Up
SET @add_recharge_col = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM information_schema.COLUMNS
            WHERE TABLE_SCHEMA = DATABASE()
              AND TABLE_NAME = 'recharge_requests'
              AND COLUMN_NAME = 'resulting_balance'
        ),
        'SELECT 1',
        'ALTER TABLE recharge_requests ADD COLUMN resulting_balance BIGINT NOT NULL DEFAULT 0 AFTER payment_ref'
    )
);
PREPARE stmt_add_recharge_col FROM @add_recharge_col;
EXECUTE stmt_add_recharge_col;
DEALLOCATE PREPARE stmt_add_recharge_col;

SET @add_gift_col = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM information_schema.COLUMNS
            WHERE TABLE_SCHEMA = DATABASE()
              AND TABLE_NAME = 'gift_orders'
              AND COLUMN_NAME = 'post_balance'
        ),
        'SELECT 1',
        'ALTER TABLE gift_orders ADD COLUMN post_balance BIGINT NULL AFTER diamond_reward'
    )
);
PREPARE stmt_add_gift_col FROM @add_gift_col;
EXECUTE stmt_add_gift_col;
DEALLOCATE PREPARE stmt_add_gift_col;

-- +goose Down
SET @drop_gift_col = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM information_schema.COLUMNS
            WHERE TABLE_SCHEMA = DATABASE()
              AND TABLE_NAME = 'gift_orders'
              AND COLUMN_NAME = 'post_balance'
        ),
        'ALTER TABLE gift_orders DROP COLUMN post_balance',
        'SELECT 1'
    )
);
PREPARE stmt_drop_gift_col FROM @drop_gift_col;
EXECUTE stmt_drop_gift_col;
DEALLOCATE PREPARE stmt_drop_gift_col;

SET @drop_recharge_col = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM information_schema.COLUMNS
            WHERE TABLE_SCHEMA = DATABASE()
              AND TABLE_NAME = 'recharge_requests'
              AND COLUMN_NAME = 'resulting_balance'
        ),
        'ALTER TABLE recharge_requests DROP COLUMN resulting_balance',
        'SELECT 1'
    )
);
PREPARE stmt_drop_recharge_col FROM @drop_recharge_col;
EXECUTE stmt_drop_recharge_col;
DEALLOCATE PREPARE stmt_drop_recharge_col;
