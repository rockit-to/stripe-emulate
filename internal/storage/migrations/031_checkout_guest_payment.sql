-- A payment-mode checkout session may have no customer (Stripe's customer_creation if_required):
-- customer_id becomes nullable. SQLite rebuilds the table to change a column.
CREATE TABLE checkout_sessions_new (
	id TEXT PRIMARY KEY,
	customer_id TEXT REFERENCES customers(id),
	mode TEXT NOT NULL,
	line_items TEXT NOT NULL,
	success_url TEXT NOT NULL DEFAULT '',
	cancel_url TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	payment_status TEXT NOT NULL,
	subscription_id TEXT,
	invoice_id TEXT,
	payment_intent_id TEXT,
	created_at TEXT NOT NULL,
	completed_at TEXT,
	allow_promotion_codes INTEGER NOT NULL DEFAULT 0,
	trial_period_days INTEGER NOT NULL DEFAULT 0,
	discounts TEXT NOT NULL DEFAULT '[]',
	automatic_tax INTEGER NOT NULL DEFAULT 0,
	tax_id_collection INTEGER NOT NULL DEFAULT 0,
	tax_percent REAL NOT NULL DEFAULT 0,
	default_tax_rates TEXT NOT NULL DEFAULT '[]',
	client_reference_id TEXT NOT NULL DEFAULT '',
	payment_intent_data TEXT NOT NULL DEFAULT '{}',
	metadata TEXT NOT NULL DEFAULT '{}',
	subscription_metadata TEXT NOT NULL DEFAULT '{}'
);

INSERT INTO checkout_sessions_new (
	id, customer_id, mode, line_items, success_url, cancel_url, status, payment_status, subscription_id, invoice_id,
	payment_intent_id, created_at, completed_at, allow_promotion_codes, trial_period_days, discounts, automatic_tax,
	tax_id_collection, tax_percent, default_tax_rates, client_reference_id, payment_intent_data, metadata, subscription_metadata
)
SELECT
	id, customer_id, mode, line_items, success_url, cancel_url, status, payment_status, subscription_id, invoice_id,
	payment_intent_id, created_at, completed_at, allow_promotion_codes, trial_period_days, discounts, automatic_tax,
	tax_id_collection, tax_percent, default_tax_rates, client_reference_id, payment_intent_data, metadata, subscription_metadata
FROM checkout_sessions;

DROP TABLE checkout_sessions;

ALTER TABLE checkout_sessions_new RENAME TO checkout_sessions;

CREATE INDEX IF NOT EXISTS idx_checkout_sessions_customer ON checkout_sessions(customer_id);
