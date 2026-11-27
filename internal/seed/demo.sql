-- Synthetic demo data: October and November 2026.
-- Repeatable: it empties both tables and resets IDs before inserting.
--
-- Known totals (cents):
--   November 2026: Rent 95000, Groceries 15800, Utilities 8215, Transport 6810,
--                  Dining 5900, Uncategorized 425              => 132150 (1,321.50), 11 expenses
--   October 2026:  Rent 95000, Groceries 12480, Transport 8340, Utilities 7690,
--                  Dining 5430                                 => 128940 (1,289.40), 7 expenses
-- Boundary days: 2026-10-01, 2026-10-31, 2026-11-01, 2026-11-30.

TRUNCATE expenses, categories RESTART IDENTITY;

INSERT INTO categories (name) VALUES
    ('Uncategorized'), ('Rent'), ('Groceries'), ('Transport'), ('Dining'), ('Utilities');

INSERT INTO expenses (expense_date, description, amount_cents, category_id)
SELECT v.d::date, v.descr, v.cents, c.id
FROM (VALUES
    -- October 2026
    ('2026-10-01', 'October rent',          95000, 'Rent'),
    ('2026-10-01', 'Weekly shop',            5875, 'Groceries'),
    ('2026-10-03', 'Monthly bus pass',       4500, 'Transport'),
    ('2026-10-18', 'Weekly shop',            6605, 'Groceries'),
    ('2026-10-20', 'Electricity bill',       7690, 'Utilities'),
    ('2026-10-31', 'Train to the coast',     3840, 'Transport'),
    ('2026-10-31', 'Halloween dinner',       5430, 'Dining'),
    -- November 2026
    ('2026-11-01', 'November rent',         95000, 'Rent'),
    ('2026-11-01', 'Corner shop milk',        349, 'Groceries'),
    ('2026-11-03', 'Monthly bus pass',       4500, 'Transport'),
    ('2026-11-08', 'Weekly shop',            6420, 'Groceries'),
    ('2026-11-12', 'Lunch with team',        2740, 'Dining'),
    ('2026-11-15', 'Weekly shop',            7135, 'Groceries'),
    ('2026-11-20', 'Electricity bill',       8215, 'Utilities'),
    ('2026-11-21', 'Pizza night',            3160, 'Dining'),
    ('2026-11-25', 'Online order https://shop.example.com/account/orders/2026-11-25/ORD-7F3A9C2B1E5D4A6F8B0C9D7E6F5A4B3C2D1E0F9A8B7C6D5E4F3A2B1C0D9E8F7A6B5C4D3', 425, 'Uncategorized'),
    ('2026-11-30', 'Farmers market',         1896, 'Groceries'),
    ('2026-11-30', 'Taxi home',              2310, 'Transport')
) AS v(d, descr, cents, category)
JOIN categories c ON c.name = v.category
ORDER BY v.d, v.descr;
