-- Does detecting a primary-key conflict read once, or once per key?
--
-- eval TODO item 2d, and pre-check 1 for the insert_ns A/B. The rewrite's claim is that the EXCEPTION
-- handler's cost came from answering "which keys collided?" with `key = ANY(_keys)` over every key in
-- the batch -- a storage read per key once tablets x keys leaves YugabyteDB's batching range (~32,768;
-- at 120 tablets that is 273 keys, and the real batch carries ~355, so the real batch is just outside
-- it). If ON CONFLICT reads per key as well then the cost moved rather than went, and a null A/B
-- result would be uninterpretable.
--
-- Runs on its own table, never on ns_0: EXPLAIN ANALYZE EXECUTES the statement, so pointing it at a
-- live namespace would insert rows into a running measurement. 120 tablets to match the layout under
-- test, and the batch width is the measured one rather than a round number.
--
-- Read "Storage Read Requests" in each plan. Expected if the rewrite works: A scales with the key
-- count, B does not.
\set ON_ERROR_STOP on
\timing off
\pset pager off

DROP TABLE IF EXISTS explain_probe;
CREATE TABLE explain_probe
(
    key     BYTEA                    NOT NULL PRIMARY KEY,
    value   BYTEA  DEFAULT NULL,
    version BIGINT DEFAULT 0::BIGINT NOT NULL CHECK (version >= 0)
) SPLIT INTO 120 TABLETS;

INSERT INTO explain_probe (key, value)
SELECT decode(lpad(to_hex(i), 64, '0'), 'hex'), decode('00', 'hex')
FROM generate_series(1, 4000) AS i;

-- The real batch width: 355 keys of which 18 (5%) already exist, the share the ladders ran at. Bound
-- as a literal via \gset so both statements below receive the array the same way the real call does,
-- rather than computing it inside the plan being measured.
SELECT array_agg(decode(lpad(to_hex(i), 64, '0'), 'hex'))::text AS karr
FROM (SELECT generate_series(1, 18) AS i UNION ALL SELECT generate_series(100001, 100337)) s
\gset

\echo ''
\echo '=== A: the OLD handler''s lookup -- key = ANY(_keys) over the whole batch ==='
EXPLAIN (ANALYZE, DIST, COSTS OFF, TIMING OFF, SUMMARY ON)
SELECT key FROM explain_probe WHERE key = ANY (:'karr'::BYTEA[]);

\echo ''
\echo '=== B: the NEW form -- ON CONFLICT DO NOTHING ... RETURNING key ==='
EXPLAIN (ANALYZE, DIST, COSTS OFF, TIMING OFF, SUMMARY ON)
INSERT INTO explain_probe (key, value)
SELECT k, decode('00', 'hex') FROM unnest(:'karr'::BYTEA[]) AS t(k)
ON CONFLICT (key) DO NOTHING
RETURNING key;

\echo ''
\echo '=== C: control -- the same lookup at 18 keys, inside the batching range ==='
SELECT array_agg(decode(lpad(to_hex(i), 64, '0'), 'hex'))::text AS ksmall
FROM generate_series(1, 18) AS i
\gset
EXPLAIN (ANALYZE, DIST, COSTS OFF, TIMING OFF, SUMMARY ON)
SELECT key FROM explain_probe WHERE key = ANY (:'ksmall'::BYTEA[]);

DROP TABLE explain_probe;
