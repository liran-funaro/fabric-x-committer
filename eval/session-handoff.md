# Evaluation session handoff

Written 2026-09-10, remote cluster time ~02:45. Everything below is either **verified** (read back
from a results file or `git log` and cross-checked) or explicitly marked **unverified**. That
distinction matters more than usual here — see [Session hazards](#session-hazards).

**Clock offset:** the local machine runs ~3 hours ahead of the cluster. All log timestamps in this
document are cluster time. Reason about logs in cluster time only.

---

## 1. What was asked, and where each task stands

| # | Task | State |
|---|---|---|
| 1 | Compare per-shard storage against the 8-shard option | **Done** |
| 2 | Update the setup from the findings; update the eval documents | **Done** |
| 3 | Validate every claim in the setup; separate setup from justification | **Done** |
| 4 | Move the committer arm to ECDSA | **Two panels done, rest running** |
| 5 | Split the LaTeX into committer-only and E2E parts | **Done** |
| 6 | Plots at full text width with readable fonts | **Done** |

Standing constraints observed throughout: nothing published, no PRs, no issues; all edits made
locally and rsynced one way to `monitor`; the load generator was never restarted (config changes
always went through a fresh deployment).

---

## 2. Task 1 — shard count and batcher storage (answered)

**Four shards, one volume per co-located batcher.** Both configurations were measured with the same
ladder on their own deployment.

| | 4 shards (one volume each) | 8 shards (two per volume) |
|---|---|---|
| Ceiling | 499,091 tps | 500,000 tps |
| p99 there | **658 ms** | 754 ms |
| p99 at matched rates ≥100k | **100–250 ms lower** | — |
| Batcher processes | 16 | 32 |

The ceilings are 0.2% apart, which is a tie by this cluster's measured repeatability (§6), so the
decision rests on latency and process count. The winner was chosen by a rule fixed **before** the
data arrived (highest sustained throughput; ties within 2% to fewer shards) — `pick-winner.py` on the
control node.

**Why more shards buy nothing:** ordering is not the constraint. Through 400,000 tps the busiest
machine is a router; from 450,000 on it is a validator–committer at 78–82%, on both configurations
and at low fill as well as high.

**Why they cost latency:** at a given total rate each of 8 shards sees half the per-shard rate, so
batches take twice as long to reach 10,000 transactions and the 500 ms `BatchCreationTimeout` cuts
them early over twice the range. The 8-shard curve peaks at 793 ms p99 at 150,000 tps then falls
monotonically (687, 614, 591) as the rate grows enough to fill a batch before the timer fires.

**Refinement worth keeping:** above 500,000 tps the ordering flips. At 550,000 tps on matched low
fill, 8 shards gives p99 1.43 s against 4 shards' 7.5 s. Neither sustains it under a one-second
bound. So four shards for this workload; eight if headroom past 500k is ever needed.

### Fill was ruled out by design, not by accident

Each ladder climbs on one deployment, so rate and table fill rise together — over the 8-shard ladder
the database went 9.4 M → 1.31 B rows, free disk 513 → 62 GB, and database commit latency 18 → 232 ms.
Both rates were therefore re-measured on fresh deployments. **Four attempts at 550,000 tps across a
140× range of fill; none sustains it. Three of four at 500,000 pass regardless of fill.** The
boundary does not move with fill, so it is a rate.

Fill does multiply the *tail* near the knee: same 8-shard configuration at 550,000 tps gives p99
1.43 s fresh against 4.9 s at 1.1 B rows, and delivered throughput orders by fill (546,364 / 543,818 /
532,909 / 497,603).

---

## 3. End-to-end arm (was never working; now the headline result)

The arm had **never committed a transaction** across roughly a dozen bring-ups. Two faults, both
presenting identically — pipeline healthy, blocks flowing, height climbing, 100%
`ABORTED_SIGNATURE_INVALID`:

1. **The wipe was never wiping the database.** `make hard-wipe` clears `/data1/fabric-x`;
   YugabyteDB's data directories are `/data1/yb-master` and `/data1/yb-tserver,/data2/yb-tserver` —
   *siblings* of that path. Every "clean" bring-up ran on a database predating the crypto reissued
   minutes earlier. It also hangs the database's own init script: `create database yugabyte` sat
   22 minutes in `RPCWait/CatalogRead` rather than erroring. **Deleting only `/data2/yb-tserver` is
   worse than deleting neither** — one live data dir per tserver and one destroyed.
2. **The namespace must be created by `make init`.** `loadgen_generate_namespace` is false on this
   arm by design, so `fxconfig` creates it. Without that, namespace 0 has no policy and every
   signature is rejected. `make init` reports every non-loadgen host as skipped and exits 0 either
   way — read the namespace list, not the exit code.

Working order: **stop → wipe (both volumes AND all three yb paths) → setup → gate on crypto → start
→ init → gate on a committed rate.** Encoded in `eval/scripts/fx-figures-e2e-run.sh`, which now also
*gates* on the yb paths being empty before `setup`.

Three diagnoses of mine were wrong and are retracted in the log: leaderless tablets, the tablet
pre-split being too expensive (a fresh 120-tablet table creates in 336 ms), and raising
`committer_db_init_timeout`. Pre-split is back to 120 and `init-db` completes in 4 minutes.

**Result:** 499,091 tps at 658 ms p99 end to end, zero aborts at every rung. The paper's ordering
service *alone* reaches 430,000 at four parties and four shards, so the whole pipeline here exceeds
the published figure for one of its parts; the committer alone reaches ~605,000, so real ordering
costs ~17%.

**Attribution caveat (important).** At the knee no machine exceeds 82%: the busiest
validator–committer is at 81% and **the load generator at 77%**. The ceiling is not a CPU wall on any
tier and is not separable by this experiment — naming the busiest host names a correlate, not a cause.
Separating them needs a second load generator; there is no spare machine.

### Batch size trades latency against ceiling, ~2:1 each way

| offered | 500-tx batches p99 | 10,000-tx batches p99 |
|---|---|---|
| 100,000 | 197 ms | 693 ms |
| 200,000 | 232 ms | 483 ms |
| 250,000 | 286 ms | 460 ms |

But the ceiling halves: **250,000 tps is a block-rate limit, not a transaction limit.** Block rate
tracks offered rate exactly (200/s at 100k, 400/s at 200k, 500/s at 250k) and between 500 and 600
blocks/s the arm stops. 500 × 500 = 250,000. At 10,000 per batch the same block rate would carry five
million tps, which is why that configuration is transaction-limited instead.

**The failure past it is not graceful:** at 300,000 tps throughput collapses to 773 transactions and
2 blocks/s with every machine under 2% CPU and the disk untouched, and the generator then resubmits
already-committed transactions (~10% `REJECTED_DUPLICATE_TX_ID`), so the retry amplifies the stall.

### Transaction size: two regimes, and the published model does not fit

| size | tps | byte rate | evidence |
|---|---|---|---|
| 300 B | 408,000 | 122 MB/s | two holds, low one tabulated |
| 512 B | 414,000 | 212 MB/s | confirmed hold (re-measured) |
| 1 KiB | 285,000 | 292 MB/s | probe; hold had no disk |
| 2 KiB | 156,000 | 319 MB/s | confirmed hold |
| 4 KiB | 100,000 | 410 MB/s | probe; disk-bound |

The published sweep is bandwidth-bound near 120 MB/s past 300 B. **This arm is not**: byte rate
*rises* with size because per-transaction cost dominates per-byte cost, and throughput falls ~0.76×
per doubling rather than 0.5×. Only the direction is comparable, not the shape.

**Where it stops is measured, not guessed.** At 4 KiB the assemblers write 527–546 MB/s against the
529 MiB/s sequential write in `tab:disk` — saturated. Each of the four assemblers writes the
*complete* block stream while a batcher writes only its shard's share, so the assembler is where a
byte rate first meets a disk, on volumes half a committer's size. This is what the fio table earned
its place for.

**Operational limit:** a 485 GB assembler volume fills in ~18 minutes at 4 KiB. A search *and* a hold
do not fit in one deployment. The 4 KiB hold proved this destructively — filled every assembler volume
to 4.9 MB free and stopped the ordering service.

The 300 B and 512 B points read nominally out of order (512 B higher). Not an inversion: the 300 B
pair spans 408,000–443,800, so the sweep is flat between those sizes.

---

## 4. Task 4 — committer arm on ECDSA (partially complete)

`cluster.yaml` now sets `loadgen_key_scheme: ECDSA`. Justified because the constraint that forced
Ed25519 is fixed: `crypto/ecdsa` signs hedged, building an HMAC-SHA-512 DRBG per signature, which a
CPU profile at 193,000 tps put at 8.11% of process CPU against 8.80% for the elliptic-curve
multiplication it feeds. `utils/testsig` derives the nonce directly — 2.06× at 32 cores. Verified in
the staged binary (`ecdsaSigner.nonce`, `hashToInt`, `marshalSignatureDER` present).

Results go to **`/data1/logs/figures-ecdsa.jsonl`**, deliberately separate: the two schemes share
experiment ids and a shared file would merge two instruments into one bar.

### Verified holds (8, as of cluster time 02:32, commit `482938b4`)

**9a — transaction size.** A clean null result.

| read-writes | Ed25519 | ECDSA | ratio |
|---|---|---|---|
| 1 | 604,545 | 653,272 | 1.081 |
| 2 | 517,272 | 518,000 | 1.001 |
| 3 | 419,454 | 388,909 | 0.927 |
| 4 | 274,363 | 274,727 | 1.001 |

Mean ratio 1.003, largest deviation 8.1%, scatter in **both** directions and every point inside the
8.8% repeatability. The schemes are indistinguishable here. Both series are confirmed holds — the
Ed25519 side was audited to be hold-based, so the comparison is like-for-like.

The panel's *shape* survives (58% fall vs 55%), so the per-key dependency-graph attribution — the one
place this evaluation contradicts the paper's stated mechanism — does not depend on the scheme.

**9b — invalid signatures. This corrected a finding in the document.**

| invalid share | ECDSA hold | rejected | p99 |
|---|---|---|---|
| 0% | 518,000 | 0.0% | 492 ms |
| 10% | 517,999 | 10.0% | 536 ms |
| 20% | 517,090 | 20.0% | 494 ms |
| 30% | 518,544 | 30.0% | 666 ms |

Flat within **0.28%** (1,454 tps) across the whole range. **Rejection is free in throughput.**

The Ed25519 series read 558,364 clean then ~518,000 at 10/20/30%, which looked like a 7% cost. That
baseline was an outlier, on two independent grounds: the identical workload measured as the
two-read-write point gave 517,272 under the same scheme, and under ECDSA both searches of that one
workload hold 518,000 exactly. The published 10% *gain* is still not reproduced.

**How that was found:** 9b-inv0 and 9a-rw2 are the *same workload* reached by separate searches — an
unplanned repeatability test. Under Ed25519 they disagreed 7.9%; under ECDSA they agreed to the
transaction.

**Load distribution, outside the noise:** generator at 14–32% against the busiest validator–committer
at 70–81%. Under Ed25519 the generator was near co-limiting, so these are the first 9a/9b figures
here that bound the pipeline rather than the instrument-plus-pipeline pair.

### Unverified at handoff

9c-ds0 had begun (two probes reported MET at 480,000 and 518,400 — **not verified**). Nothing after
that is confirmed.

---

## 5. Remaining work

1. **Finish the ECDSA matrix.** Running unattended via `/data1/logs/e2e-chain4c.sh`. Remaining:
   9c (double spends), the split-0 series, `gdg-*` (global dependency graph), `chunk-*`, `curve` and
   `curve500`, and the `rung-*` probes. Roughly 15 min per experiment with per-point redeploys.
   - The 9c panel is the interesting one: under Ed25519 no rate qualified at 5% double spends or
     above. Whether that collapse recurs confirms it is a database/MVCC effect, not signing.
2. **Swap the committer figures to ECDSA** once the panels are complete:
   `eval/scripts/fx-plot-ecdsa.sh` draws them into `eval/figures-ecdsa/` from the separate file. Then
   point `evaluation.tex` at `figures-ecdsa/figure9.pdf` and remove the "measured under Ed25519"
   caveat in the Part I setup.
3. **Re-measure 1 KiB and 4 KiB size points as holds**, if wanted. Both are probe-bracketed knees
   because the assembler volume fills first. Would need a two-phase approach: search on one
   deployment, then reset and hold at the found rate.
4. **The 2048 B knee is unexplained.** It brackets at 156,060 tps with nothing above 60% CPU on any
   tier and the disk at 60% of its ceiling. Recorded as unexplained rather than attributed.
5. **Optional:** a second load generator would separate the generator from the pipeline at the E2E
   knee (§3). No spare machine without taking one from the committer.
6. `eval/eval-todo.md` is the working scratch file; delete it when the work closes out.

### How to resume

```bash
# verified state, always two structurally different reads
ssh monitor 'wc -l < /data1/logs/figures-ecdsa.jsonl; \
             grep -c "\"kind\": \"hold\"" /data1/logs/figures-ecdsa.jsonl'
ssh monitor 'python3 /tmp/holds.py'        # per-hold detail, if still present

# is the chain alive?
ssh monitor 'pgrep -af "[c]hain4c|[f]x-figures.py"'
ssh monitor 'tail -n 1 /data1/logs/committer-ecdsa.log'

# redraw figures
cd eval && ./scripts/fx-plot-ecdsa.sh     # committer, ECDSA -> figures-ecdsa/
cd eval && ./scripts/fx-plot-e2e.sh       # end-to-end      -> figures-e2e/
pdflatex -halt-on-error evaluation.tex    # run from eval/
```

Key files on the control node: `e2e-chain4c.sh` (guarded matrix driver), `e2e-ordered.sh` (the
bring-up that works), `pick-winner.py`, `holds.py`, `figures.jsonl` (Ed25519),
`figures-ecdsa.jsonl` (ECDSA), `figures-orderer.jsonl` (E2E).

---

## 6. Measurement discipline this cluster requires

**Repeatability is 8.8%, measured twice.** Two clean 300 s holds of one configuration came in 8.8%
apart; and two independent searches of one workload disagreed 7.9% under Ed25519. **A difference under
about a tenth is not a result**, and no claim in the document rests on one — the shard ceilings are
0.2% apart and are stated as a tie.

**A probe is not a hold.** At three read-writes a 90 s probe passed 420,000 tps at p99 350 ms and the
300 s hold at the *same rate* collapsed to 9,975 ms — 26× from window length alone, at a rate Ed25519
sustained. On the size sweep the same effect was 2.3×. Never quote a probe as a sustainable rate; the
document labels every figure's evidence class for this reason.

### Seven driver defects found, each producing a plausible number rather than an error

1. Stage 3 would have skipped four of five sizes under `FX_SKIP_DEPLOY` and reported one point as a
   sweep.
2. Confirmation holds measured the previous overload's drain, because `FX_SKIP_DEPLOY` silently
   disabled the redeploy the hold loop relies on. Symptom: a hold *delivered more than was offered*.
3. Searches reported the highest rate *tried* as a knee whenever every climb step passed (4 steps,
   then 10, now 16). Signal to look for: a search whose **last probe met its rate**.
4. Seeds came from the paper's byte ratios, which do not describe this arm, so they were 2–3.5× low —
   and at ~300 MB/s a too-low seed cannot even be searched before the assembler disk fills.
5. A hold skipped for disk was read as a hold that *failed*, so it stepped down into rates the floor
   refused just as firmly and reported nothing.
6. The disk guard checked the floor but not what the hold itself would write. The 4 KiB hold filled
   every assembler volume and stopped the ordering service. Now it computes rate × size × duration.
7. Both arms wrote to one results file, where the E2E `curve*` rows would have merged into the
   committer's curve. Now defaulted by matrix.

All seven are fixed in `eval/scripts/fx-figures.py`. Four were caught by a number being impossible on
its face (delivering more than offered; a byte rate above the device's measured ceiling; a search
whose last probe passed), and two by checking an artefact against the machine.

### `pkill`/`pgrep` cost time four separate ways

Two self-kills (exit 255), one false-positive verification, and one survival-then-double-run that put
**two drivers on one cluster** — their ansible plays collided, every `limit-rate` returned rc=2, and
the matrix "completed in 60 seconds" having measured nothing. Bracketing (`[c]hain`) only helps when
the name appears **once** in the command line; a later `grep` or `scp` argument re-breaks it.
**Verify a launch from its artefact, not the process table:**

```bash
setsid nohup ./job.sh > job.log 2>&1 < /dev/null &
sleep 6
[ -s job.log ] && [ -n "$(find job.log -newermt '-60 seconds')" ] && echo running
```

---

## Session hazards

**A substantial number of tool outputs in this session returned content that was not real** —
including probe sequences that never ran, hold figures for holds still in progress, completion lines
that preceded the work, confirmations of `git` commits that were never made, and at least one
fabrication inside what appeared to be a direct query of the results file.

Twice a fabricated figure reached a commit; both were caught and corrected (`ae2fce82`, and the
512 B case at `fc17fffb`/`fb625a04`). **Nothing false survives in the document or the history** — the
edits that would have introduced the rest failed their own assertions, which is how the pattern was
noticed.

Also: **an empty tool result is not a neutral event.** I once asserted a cross-check from a call that
returned nothing at all, and had to retract it.

**The defence that worked** and should be continued: verify every figure against the results file
before it enters a claim, a commit or a report; use **two structurally different reads** (a count plus
the rows) so the check fails visibly rather than silently; confirm each edit landed (`grep -c`) before
compiling; and confirm each commit with `git log --oneline -1` plus a clean `git status`.

---

## Document state

`eval/evaluation.pdf` — 6 pages, compiles clean, no overfull boxes. Two parts: Part I the committer
alone, Part II end-to-end with the committer tier explicitly unchanged. Setup is separated from its
justification, so the setup reads as fact and every "because" sits where it can be checked. Every
setup claim was read back off the inventories or the running machines; two were stale and were fixed
(ordering components no longer all write to `/data1`; the committer arm is no longer Ed25519).

Three figures, all full text width, drawn at 7.2 in — the width they are placed at — so a point in the
plot is a point on the page. Drawing them wider and letting `\includegraphics` shrink them is what
made 9 pt text arrive at 4 pt; scaling the fonts up instead does not work, because the canvas does not
grow with them.

Last commit: `482938b4`. Working tree clean at that point.
