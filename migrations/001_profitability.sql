PRAGMA foreign_keys = ON;

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    display_name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE allegro_integrations (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    allegro_account_id TEXT NOT NULL,
    access_token_ciphertext BLOB,
    refresh_token_ciphertext BLOB,
    token_expires_at TEXT,
    last_order_event_id TEXT,
    last_synced_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, allegro_account_id)
);

CREATE TABLE products (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sku TEXT NOT NULL,
    name TEXT NOT NULL,
    ean TEXT,
    current_price_minor INTEGER NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'PLN' CHECK(length(currency) = 3),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, sku)
);

CREATE TABLE allegro_offers (
    id INTEGER PRIMARY KEY,
    integration_id INTEGER NOT NULL REFERENCES allegro_integrations(id) ON DELETE CASCADE,
    product_id INTEGER REFERENCES products(id) ON DELETE SET NULL,
    allegro_offer_id TEXT NOT NULL,
    external_sku TEXT,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    current_price_minor INTEGER,
    currency TEXT CHECK(currency IS NULL OR length(currency) = 3),
    source_updated_at TEXT,
    synced_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(integration_id, allegro_offer_id)
);
CREATE INDEX idx_offers_product ON allegro_offers(product_id);
CREATE INDEX idx_offers_external_sku ON allegro_offers(integration_id, external_sku);

CREATE TABLE allegro_orders (
    id INTEGER PRIMARY KEY,
    integration_id INTEGER NOT NULL REFERENCES allegro_integrations(id) ON DELETE CASCADE,
    allegro_order_id TEXT NOT NULL,
    status TEXT NOT NULL,
    fulfillment_status TEXT,
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    buyer_delivery_minor INTEGER NOT NULL DEFAULT 0,
    paid_amount_minor INTEGER,
    total_to_pay_minor INTEGER,
    seller_shipping_cost_minor INTEGER,
    seller_shipping_source TEXT,
    bought_at TEXT NOT NULL,
    source_updated_at TEXT NOT NULL,
    synced_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(integration_id, allegro_order_id)
);
CREATE INDEX idx_orders_period ON allegro_orders(integration_id, bought_at);
CREATE INDEX idx_orders_updated ON allegro_orders(integration_id, source_updated_at);

CREATE TABLE order_items (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES allegro_orders(id) ON DELETE CASCADE,
    offer_id INTEGER REFERENCES allegro_offers(id) ON DELETE SET NULL,
    allegro_line_item_id TEXT NOT NULL,
    allegro_offer_id TEXT NOT NULL,
    external_sku TEXT,
    name TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK(quantity > 0),
    unit_price_minor INTEGER NOT NULL,
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    reconciliation_minor INTEGER NOT NULL DEFAULT 0,
    bought_at TEXT NOT NULL,
    discounts_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(order_id, allegro_line_item_id)
);
CREATE INDEX idx_order_items_offer ON order_items(offer_id);

CREATE TABLE product_costs (
    id INTEGER PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    unit_cost_minor INTEGER NOT NULL CHECK(unit_cost_minor >= 0),
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    valid_from TEXT NOT NULL,
    source TEXT NOT NULL CHECK(source IN ('manual', 'import')),
    source_key TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(product_id, valid_from, currency)
);
CREATE INDEX idx_product_costs_lookup ON product_costs(product_id, valid_from DESC);

CREATE TABLE allegro_fees (
    id INTEGER PRIMARY KEY,
    integration_id INTEGER NOT NULL REFERENCES allegro_integrations(id) ON DELETE CASCADE,
    order_id INTEGER REFERENCES allegro_orders(id) ON DELETE SET NULL,
    offer_id INTEGER REFERENCES allegro_offers(id) ON DELETE SET NULL,
    allegro_fee_id TEXT NOT NULL,
    type_id TEXT NOT NULL,
    type_name TEXT NOT NULL,
    value_minor INTEGER NOT NULL,
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    occurred_at TEXT NOT NULL,
    synced_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(integration_id, allegro_fee_id)
);
CREATE INDEX idx_fees_order ON allegro_fees(order_id, occurred_at);
CREATE INDEX idx_fees_offer ON allegro_fees(offer_id, occurred_at);

CREATE TABLE order_adjustments (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES allegro_orders(id) ON DELETE CASCADE,
    external_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('surcharge', 'reconciliation', 'revenue_correction', 'cost_correction')),
    value_minor INTEGER NOT NULL,
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    status TEXT NOT NULL DEFAULT 'finalized',
    source TEXT NOT NULL,
    reason TEXT,
    occurred_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(order_id, external_key, kind)
);

CREATE TABLE profitability_results (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES allegro_orders(id) ON DELETE CASCADE,
    calculation_version INTEGER NOT NULL,
    currency TEXT NOT NULL CHECK(length(currency) = 3),
    product_revenue_minor INTEGER NOT NULL,
    delivery_revenue_minor INTEGER NOT NULL,
    surcharge_revenue_minor INTEGER NOT NULL,
    reconciliation_revenue_minor INTEGER NOT NULL,
    adjustment_revenue_minor INTEGER NOT NULL,
    allegro_cost_minor INTEGER NOT NULL,
    purchase_cost_minor INTEGER,
    shipping_cost_minor INTEGER,
    adjustment_cost_minor INTEGER NOT NULL,
    profit_minor INTEGER,
    margin_basis_points INTEGER,
    completeness TEXT NOT NULL CHECK(completeness IN ('complete', 'missing_product_cost', 'missing_shipping_cost', 'missing_multiple_costs')),
    inputs_updated_at TEXT NOT NULL,
    calculated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(order_id, calculation_version)
);
CREATE INDEX idx_profitability_period ON profitability_results(calculated_at);
