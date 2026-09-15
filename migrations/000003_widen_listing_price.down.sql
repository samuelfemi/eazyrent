-- Roll back to NUMERIC(10,2). Fails if any price exceeds 99,999,999.99.
ALTER TABLE listings ALTER COLUMN price TYPE NUMERIC(10,2);
