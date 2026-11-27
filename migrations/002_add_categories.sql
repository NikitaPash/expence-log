-- 002: categories. Existing expenses are backfilled to 'Uncategorized'
-- before the column becomes NOT NULL and gains its foreign key.
CREATE TABLE categories (
    id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE CHECK (btrim(name) <> '')
);

INSERT INTO categories (name) VALUES ('Uncategorized');

ALTER TABLE expenses ADD COLUMN category_id BIGINT;

UPDATE expenses
SET category_id = (SELECT id FROM categories WHERE name = 'Uncategorized')
WHERE category_id IS NULL;

ALTER TABLE expenses ALTER COLUMN category_id SET NOT NULL;

ALTER TABLE expenses
    ADD CONSTRAINT expenses_category_id_fkey
    FOREIGN KEY (category_id) REFERENCES categories (id);

-- Supports the month list: a range scan on expense_date, with category_id
-- available in the index for the optional category filter.
CREATE INDEX expenses_date_category_idx ON expenses (expense_date, category_id);
