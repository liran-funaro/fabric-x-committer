<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
| figure | condition | rate limit | finished tx/s | of which aborted | mean ms | p50 ms | p99 ms | db commit ms | fill Mtx | verifier cpu | coord cpu | gen cpu | busiest host |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 9a | in=1
out=1 | 653,033 | 653,273 | 0 | 624 | 624 | 813 | 219.7 | 245 | 49% | 23% | 32% | commit6 |
| 9a | in=2
out=2 | 518,400 | 518,000 | 0 | 345 | 341 | 457 | 135.7 | 194 | 40% | 18% | 25% | commit6 |
| 9a | in=3
out=3 | 420,000 | 420,727 | 0 | 265 | 264 | 350 | 85.6 | 70 | 32% | 16% | 21% | commit5 |
