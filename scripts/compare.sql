-- Run against the original and the restored database; the outputs must match.
SELECT 'expenses' AS table_name, count(*) AS row_count FROM expenses
UNION ALL SELECT 'categories', count(*) FROM categories
UNION ALL SELECT 'schema_migrations', count(*) FROM schema_migrations;

SELECT to_char(e.expense_date, 'YYYY-MM') AS month,
       c.name AS category,
       count(*) AS expenses,
       sum(e.amount_cents) AS total_cents
FROM expenses e
JOIN categories c ON c.id = e.category_id
GROUP BY 1, 2
ORDER BY 1, 4 DESC, 2;

SELECT to_char(expense_date, 'YYYY-MM') AS month, count(*) AS expenses, sum(amount_cents) AS total_cents
FROM expenses GROUP BY 1 ORDER BY 1;
