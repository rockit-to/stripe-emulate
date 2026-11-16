-- default_price: the id of the price the dashboard shows for a product.
ALTER TABLE products ADD COLUMN default_price TEXT NOT NULL DEFAULT '';
