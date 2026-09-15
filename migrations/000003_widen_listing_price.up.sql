-- NUMERIC(10,2) tops out at 99,999,999.99, which real Naira rents exceed.
-- Widen to NUMERIC(14,2) (max 999,999,999,999.99).
ALTER TABLE listings ALTER COLUMN price TYPE NUMERIC(14,2);
