/* template for the querying rows for each namespace. */
SELECT key, value, version FROM ns_%d WHERE key = ANY($1);
