-- 001: expenses with a calendar date, description and positive integer-cent amount.
CREATE TABLE expenses (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    expense_date DATE   NOT NULL,
    description  TEXT   NOT NULL CHECK (btrim(description) <> ''),
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0)
);
