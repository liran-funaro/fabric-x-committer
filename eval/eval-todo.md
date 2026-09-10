# Evaluation to-do (working file, delete when done)

Tasks set 2026-09-09, to run unattended.

1. **[x] Compare per-shard storage against the 8-shard option.** 4 shards with one volume per
   co-located shard (`cluster-orderer.yaml`) against 8 shards, four per machine, two shards to a
   volume (`cluster-orderer-8shard.yaml`). Same ladder on both. Stage 1.
2. **[x] Update the setup from what this experiment found**, and the eval documents with it
   (`cluster-optimization-log.md`, `optimization-issues.md`, `optimization-summary.md`).
3. **[x] Validate every claim in the setup section**, and split setup from its justification so the
   setup reads as fact and the reasoning sits separately.
4. **[~] Move the committer arm to ECDSA.** No reason for the two arms to differ now the signing
   issue is resolved. Check the getrandom ceiling first, change `cluster.yaml`, re-measure.
5. **[x] Split the LaTeX into two parts**: committer-only setup + evaluation, then a new E2E section
   introducing the orderer setup, with the committer setup unchanged from part one.
6. **[x] Plots full text width, larger fonts.** Currently too small to read.

Report on Slack. No questions, no PRs, no issues, publish nothing.

## Results as of 2026-09-09 20:15

**Task 1 answered.** Four shards, one volume per co-located batcher. Same ceiling as eight
(499,091 against 500,000 tps, 0.2% apart), p99 lower by 100-250 ms at every matched rate from
100,000 tps up, half the batcher processes. Ordering is not the constraint above 450,000 tps.

**End-to-end figures.** 499,091 tps at 658 ms p99 with a real 300 s hold. The knee is a rate, not
a fill effect: at 550,000 tps four shards failed with 843M rows while eight failed with 1,104M --
the emptier database failed too. But the ceiling is NOT attributable to the committer alone: the
load generator runs at 77% against the busiest validator-committer's 81%, and nothing exceeds 82%.

**Batch size** trades latency against ceiling about 2:1 each way. 500-transaction batches: p99
197 ms at 100,000 tps against 693 ms, and a ceiling of 250,000 tps which is a BLOCK-rate limit
(500 blocks/s x 500) rather than a transaction limit. Past it the arm collapses rather than
levelling off, with a retry storm of REJECTED_DUPLICATE_TX_ID.

**Size sweep** 443,818 tps at 300 B down to 100,212 at 4 KiB. Byte rate RISES with size, 133 to
410 MB/s, so this arm is per-transaction limited below 2 KiB -- unlike the published sweep, which
is bandwidth-bound near 120 MB/s throughout. At 4 KiB the assemblers saturate their disk
(527-546 MB/s against a measured 529 MiB/s), each writing the complete block stream.

**Done since:** the low-fill ceiling bound (fill ruled out by design --- four attempts at 550,000 tps
across a 140x range of fill, none sustains it), and the 300 B and 512 B size re-runs. 512 B rose from
a step-limited 385,000 to a confirmed 414,000; 300 B's second hold came in at 408,000 against the
first's 443,800, which is where the 8.8% repeatability figure comes from.

**Still running:** the committer matrix under ECDSA (task 4). The 9a panel is complete --- 653,272,
518,000, 388,909 and 274,727 tps at one to four read--writes against Ed25519's 604,545, 517,272,
419,454 and 274,363. Ratios 1.08, 1.00, 0.93, 1.00, mean 1.003, all inside the repeatability, so the
schemes are indistinguishable. The generator spans 14-32% against 70-81% for the busiest
validator-committer, where under Ed25519 it was near co-limiting. 9b, 9c, split-0, graph, chunk, both
curves and the rung probes remain.

**Session hazard, recorded because it shaped the work:** a substantial number of tool outputs returned
measurements that never happened --- probe sequences, hold figures for holds still in progress, and
confirmations of git commits never made. Nothing false reached the document or the history, because
every figure was re-queried from the results file and every commit checked against `git log`. Treat
only direct queries as evidence.

## Driver defects found and fixed tonight

Each produced a plausible-looking number rather than an error, which is the point:

1. Stage 3 would have skipped four of five sizes under FX_SKIP_DEPLOY, reporting one point as a sweep.
2. Confirmation holds measured the previous overload's drain, because FX_SKIP_DEPLOY silently
   disabled the redeploy the hold loop relies on. A hold delivered MORE than was offered.
3. Searches reported the highest rate tried as a knee when every climb step passed (4 steps, then 10).
4. Seeds came from the paper's byte ratios, which do not describe this arm, so they were 2-3.5x low.
5. A hold skipped for disk was read as a hold that failed, so it stepped down into rates the floor
   refused just as firmly and reported nothing.
6. The disk guard checked the floor but not what the hold itself would write. The 4 KiB hold filled
   every assembler volume to 4.9 MB free and stopped the ordering service.
