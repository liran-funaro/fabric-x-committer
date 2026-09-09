# Evaluation to-do (working file, delete when done)

Tasks set 2026-09-09, to run unattended.

1. **[~] Compare per-shard storage against the 8-shard option.** 4 shards with one volume per
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
