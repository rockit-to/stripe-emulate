-- customer_email: the address Checkout fills in for a guest (a session without a customer).
ALTER TABLE checkout_sessions ADD COLUMN customer_email TEXT NOT NULL DEFAULT '';
