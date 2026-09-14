/*
 * Copyright IBM Corp. All Rights Reserved.
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/*
This SQL file is a template for creating a new namespace.
We use strings.ReplaceAll(sqlTemplate, "${NAMESPACE_ID}", namespaceID) to fill in the the namespace ID.

For each new namespace ID, we create a table ns_<namespace-ID> and three methods:
- insert_ns_<namespace-ID>: Inserts new keys with version 0. Returns the keys that already existed,
  leaving the rest inserted, so the caller must abort the transaction when the result is non-empty.
- update_ns_<namespace-ID>: Update existing keys. Fails for keys that doesn't exist.
- validate_reads_ns_<namespace-ID>: Validate the key's version.
*/

CREATE TABLE IF NOT EXISTS ns_${NAMESPACE_ID}
(
    key     BYTEA                    NOT NULL PRIMARY KEY,
    value   BYTEA  DEFAULT NULL,
    version BIGINT DEFAULT 0::BIGINT NOT NULL CHECK (version >= 0)
)${SPLIT_INTO_TABLETS};

-- Returns the keys that already existed, so the caller can invalidate their transactions. The
-- non-conflicting rows of a partially-conflicting batch ARE inserted by this, which is safe only
-- because the caller aborts the whole database transaction whenever the result is non-empty: a
-- transaction with two new keys of which one collides would otherwise commit the other, leaving a
-- partially-applied transaction. See database.commit, which returns before its own COMMIT.
--
-- ON CONFLICT rather than an EXCEPTION handler for two reasons. The handler had to answer "which
-- keys collided?" after the fact, which it could only do with `key = ANY(_keys)` over EVERY key in
-- the batch -- a storage read per key once tablets x keys leaves the batching range, measured at
-- 1.2-2.6 s per attempt against 23 ms for a whole clean commit. And a plpgsql EXCEPTION block opens
-- an implicit subtransaction on every call, including the conflict-free ones, which are the common
-- case at ~3,400 batches a second.
--
-- The two mentions of `key` below are different things, which is easy to misread. `ON CONFLICT (key)`
-- is the conflict TARGET -- it names the unique index that defines what counts as a conflict, and
-- returns nothing. `RETURNING key` yields one row per row the statement actually AFFECTED, and a row
-- skipped by DO NOTHING was never inserted, so it contributes no row: the result is the keys that
-- WERE inserted, not the ones that collided. The collided set is therefore requested minus returned,
-- which is what the ARRAY(...) below computes.
--
-- EXCEPT ALL, not EXCEPT: two transactions in one batch may create the same key. DO NOTHING skips the
-- second occurrence rather than erroring (unlike DO UPDATE, which refuses to touch a row twice in one
-- command), so the multiset difference reports that key once -- matching the old handler, which
-- flagged it and let the caller invalidate both transactions.
CREATE OR REPLACE FUNCTION insert_ns_${NAMESPACE_ID}(
    IN _keys BYTEA[],
    IN _values BYTEA[]
) RETURNS BYTEA[]
AS
$$
DECLARE
    inserted BYTEA[];
BEGIN
    WITH ins AS (
        INSERT INTO ns_${NAMESPACE_ID} (key, value)
        SELECT k, v
        FROM unnest(_keys, _values) AS t(k, v)
        ON CONFLICT (key) DO NOTHING
        RETURNING key
    )
    -- `inserted`, not `violating`: see the note above on RETURNING's semantics.
    SELECT COALESCE(array_agg(key), '{}') INTO inserted FROM ins;

    IF cardinality(inserted) = cardinality(_keys) THEN
        RETURN '{}';
    END IF;

    RETURN ARRAY(SELECT k FROM unnest(_keys) AS k
                 EXCEPT ALL
                 SELECT i FROM unnest(inserted) AS i);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_ns_${NAMESPACE_ID}(
    IN _keys BYTEA[],
    IN _values BYTEA[],
    IN _versions BIGINT[]
)
    RETURNS VOID
AS
$$
BEGIN
    UPDATE ns_${NAMESPACE_ID}
    SET value   = t.value,
        version = t.version
    FROM (SELECT *
          FROM unnest(_keys, _values, _versions) AS t(key, value, version)) AS t
    WHERE ns_${NAMESPACE_ID}.key = t.key;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION validate_reads_ns_${NAMESPACE_ID}(
    keys BYTEA[],
    versions BIGINT[]
) RETURNS INTEGER[]
AS
$$
DECLARE
    bad_indices INTEGER[];
BEGIN
    SELECT array_agg(expected.idx)
    INTO bad_indices
    FROM unnest(keys, versions) WITH ORDINALITY AS expected(key, version, idx)
             LEFT JOIN
         ns_${NAMESPACE_ID} actual ON actual.key = expected.key
    WHERE -- Followed are mismatch detected
       -- The key does not exist in the committed state but expected version is not null
        (actual.key IS NULL AND expected.version IS NOT NULL)
       OR -- The key exists in the committed state but expected version is null
        (actual.key is NOT NULL AND expected.version IS NULL)
       OR -- The committed version of a key is different from the expected version
        (actual.version IS DISTINCT FROM expected.version);

    RETURN COALESCE(bad_indices, '{}');
END;
$$ LANGUAGE plpgsql;
