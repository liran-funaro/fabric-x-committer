---
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# NOTE: Marp only parses front matter when it is the very first thing in the file,
# so the license header lives here as YAML comments rather than in an HTML comment
# above it. Moving it above this block silently disables the theme below.
marp: true
paginate: true
size: 16:9
theme: default
title: Fabric-X Committer — Technical Overview
description: Technical overview of the Fabric-X Committer's major components
style: |
  @import 'default';

  :root {
    --plane:   #f9f9f7;
    --surface: #fcfcfb;
    --ink:     #0b0b0b;
    --ink2:    #52514e;
    --muted:   #898781;
    --rule:    #e1e0d9;
    --axis:    #c3c2b7;
    /* One fixed identity hue per service, used in every diagram. */
    --sidecar: #2a78d6;
    --coord:   #eb6834;
    --verif:   #1baf7a;
    --vc:      #eda100;
    --query:   #e87ba4;
    --db:      #008300;
    /* Liberation/DejaVu keep the deck sans-serif on bare Linux, where none of the
       proprietary faces exist and the generic fallback can resolve to monospace. */
    --sans: "IBM Plex Sans", "Helvetica Neue", Helvetica, Arial,
            "Liberation Sans", "DejaVu Sans", sans-serif;
    --mono: "IBM Plex Mono", "SFMono-Regular", Consolas,
            "Liberation Mono", "DejaVu Sans Mono", monospace;
  }

  section {
    background: var(--plane);
    color: var(--ink);
    font-family: var(--sans);
    font-size: 20px;
    line-height: 1.45;
    /* The bundled theme centres slide content vertically via `place-content` and pads
       78.5px. Both are pinned here so every slide's title sits at the same height —
       without this, sparse slides drift ~55px lower than dense ones. */
    display: block !important;
    place-content: start !important;
    padding: 40px 56px 48px !important;
  }

  section::after { color: var(--muted); font-size: 14px; }

  /* The bundled theme tints h1 navy via --h1-color; keep it in the deck's ink instead. */
  h1 { font-size: 40px; font-weight: 600; letter-spacing: -0.4px; margin: 0 0 12px;
       color: var(--ink); }
  h2 { font-size: 31px; font-weight: 600; letter-spacing: -0.3px; margin: 0 0 14px;
       padding-bottom: 9px; border-bottom: 2px solid var(--rule); }
  h3 { font-size: 21px; font-weight: 600; margin: 14px 0 6px; color: var(--ink); }

  .eyebrow { font-size: 13px; font-weight: 600; letter-spacing: 1.4px;
             text-transform: uppercase; color: var(--muted); margin-bottom: 6px; }

  ul, ol { margin: 6px 0; padding-left: 22px; }
  li { margin: 5px 0; }
  li::marker { color: var(--axis); }
  strong { font-weight: 600; }
  code { font-family: var(--mono); font-size: 0.86em; background: #efeee8;
         padding: 1px 5px; border-radius: 3px; color: #333230; }

  table { font-size: 16px; border-collapse: collapse; width: 100%; margin: 8px 0; }
  th { text-align: left; font-weight: 600; font-size: 14px; letter-spacing: 0.5px;
       text-transform: uppercase; color: var(--ink2); border-bottom: 2px solid var(--axis);
       padding: 6px 10px 5px; background: transparent; }
  td { padding: 6px 10px; border-bottom: 1px solid var(--rule); vertical-align: top; }

  /* Source attribution — lets a reader jump to the authoritative doc. */
  .src { position: absolute; left: 56px; bottom: 16px; font-size: 12.5px;
         color: var(--muted); font-family: var(--mono); }

  .two { display: grid; grid-template-columns: 1fr 1fr; gap: 26px; }
  .two-wide { display: grid; grid-template-columns: 1.35fr 1fr; gap: 26px; }

  .card { background: var(--surface); border: 1px solid var(--rule);
          border-left: 4px solid var(--axis); border-radius: 4px; padding: 12px 16px; }
  .card h3 { margin-top: 0; }
  .card p { margin: 4px 0; font-size: 18px; color: var(--ink2); }

  .stat-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 14px 0 4px; }
  .tile { background: var(--surface); border: 1px solid var(--rule); border-radius: 4px;
          padding: 14px 16px 16px; }
  .tile .v { font-size: 40px; font-weight: 600; letter-spacing: -1px; line-height: 1.05;
             color: var(--ink); }
  .tile .l { font-size: 14.5px; color: var(--ink2); margin-top: 5px; line-height: 1.3; }
  .tile .u { font-size: 20px; font-weight: 500; color: var(--ink2); }

  .keyline { border-left: 4px solid var(--coord); background: var(--surface);
             padding: 10px 16px; margin: 12px 0; font-size: 19px; }

  .chip { display: inline-block; font-size: 14px; font-weight: 600; color: #fff;
          padding: 2px 9px; border-radius: 11px; margin-right: 6px; }

  .lead-title { font-size: 15px; color: var(--ink2); font-weight: 500; }

  /* Contents and lookup index: dot-leader rows so a page number is easy to scan to. */
  .idx-head { font-size: 14px; font-weight: 600; letter-spacing: 0.9px;
              text-transform: uppercase; color: var(--muted);
              border-bottom: 1px solid var(--rule); padding-bottom: 6px; margin: 0 0 10px; }
  .row { display: flex; align-items: baseline; gap: 10px; font-size: 18px; margin: 9px 0; }
  .row .d { flex: 1; border-bottom: 1px dotted var(--axis); position: relative; top: -5px; }
  .row .pg { font-family: var(--mono); font-size: 16px; color: var(--ink2);
             min-width: 42px; text-align: right; }
  .row .s { font-weight: 600; }

  /* Section divider pages. The h1 here becomes a top-level PDF bookmark, with each
     page's h2 nested under it, so the PDF outline mirrors the section structure. */
  section.part { background: var(--surface); }
  section.part .num { font-size: 14px; font-weight: 600; letter-spacing: 1.6px;
                      text-transform: uppercase; color: var(--coord); }
  section.part h1 { font-size: 44px; margin: 6px 0 4px; }
  section.part .sub { font-size: 19px; color: var(--ink2); margin: 0 0 22px;
                      padding-bottom: 18px; border-bottom: 2px solid var(--rule); }

  svg { display: block; margin: 4px auto 0; }
  svg text { font-family: var(--sans); }

  /* The title slide is the one place vertical centring is wanted, so opt back into
     flex layout here (the base rule above pins every other slide to the top). */
  section.title { background: var(--surface);
                  display: flex !important; flex-direction: column;
                  justify-content: center !important; }
  section.title h1 { font-size: 52px; margin-bottom: 18px; }
  section.title .rule { width: 76px; height: 5px; background: var(--coord); margin: 0 0 22px; }
  section.title p { font-size: 21px; color: var(--ink2); margin: 3px 0; }
---

<!-- _class: title -->
<!-- _paginate: false -->

# Fabric-X Committer

<div class="rule"></div>

**A technical overview of the major components**

<p>Validation and commit for Hyperledger Fabric-X — architecture, transaction flow, component internals, and operational behaviour.</p>

<p style="font-size:17px;color:#898781;margin-top:22px">
Sourced from the <code>docs/</code> tree of <code>hyperledger/fabric-x-committer</code> · Apache-2.0
</p>

<!--
Notes: this reference assumes familiarity with Hyperledger Fabric — endorsement,
ordering, MVCC, world state. It does not cover Fabric basics. The framing throughout is what differs from the Fabric peer's validator.
-->

---

## Contents

<div class="two">
<div>

<div class="idx-head">Sections</div>

<div class="row"><span class="s">1 · Context</span><span class="d"></span><span class="pg">3</span></div>
<div class="row"><span class="s">2 · Transaction flow</span><span class="d"></span><span class="pg">9</span></div>
<div class="row"><span class="s">3 · Components</span><span class="d"></span><span class="pg">12</span></div>
<div class="row"><span class="s">4 · Cross-cutting</span><span class="d"></span><span class="pg">25</span></div>
<div class="row"><span class="s">5 · Reference</span><span class="d"></span><span class="pg">31</span></div>

<div class="keyline" style="margin-top:22px;font-size:18px">
Each section opens with a divider listing its pages. Every page cites its source doc at the
bottom left, and the PDF bookmarks mirror this structure.
</div>

</div>
<div>

<div class="idx-head">Find a topic</div>

<div class="row"><span>Dependency graph (rw / wr / ww)</span><span class="d"></span><span class="pg">16</span></div>
<div class="row"><span>MVCC validation</span><span class="d"></span><span class="pg">20</span></div>
<div class="row"><span>Database schema</span><span class="d"></span><span class="pg">21</span></div>
<div class="row"><span>Idempotency</span><span class="d"></span><span class="pg">22</span></div>
<div class="row"><span>Endorsement policies</span><span class="d"></span><span class="pg">19</span></div>
<div class="row"><span>Failure recovery</span><span class="d"></span><span class="pg">27</span></div>
<div class="row"><span>Client-facing APIs</span><span class="d"></span><span class="pg">28</span></div>
<div class="row"><span>Ports, sizing, placement</span><span class="d"></span><span class="pg">30</span></div>
<div class="row"><span>Source-code map</span><span class="d"></span><span class="pg">32</span></div>

</div>
</div>

---

<!-- _class: part -->

<div class="num">Section 1</div>

# Context

<div class="sub">What the Committer is, how it differs from the Fabric peer's validator, and the shape of the system.</div>

<div style="max-width:780px">
<div class="row"><span>Where the Committer sits</span><span class="d"></span><span class="pg">4</span></div>
<div class="row"><span>How this differs from the classic Fabric peer</span><span class="d"></span><span class="pg">5</span></div>
<div class="row"><span>Design goals</span><span class="d"></span><span class="pg">6</span></div>
<div class="row"><span>The six services</span><span class="d"></span><span class="pg">7</span></div>
<div class="row"><span>State, cardinality and scaling</span><span class="d"></span><span class="pg">8</span></div>
</div>

---

<div class="eyebrow">1 · Context</div>

## Where the Committer sits

<svg viewBox="0 0 1140 300" width="1080" height="284" role="img" aria-label="Fabric-X transaction lifecycle: execute, order, then validate and commit at the Committer">
  <defs>
    <marker id="a1" markerWidth="9" markerHeight="9" refX="7.5" refY="4.5" orient="auto">
      <path d="M0,1 L7.5,4.5 L0,8" fill="none" stroke="#52514e" stroke-width="1.6"/>
    </marker>
  </defs>
  <!-- stage 1 -->
  <rect x="8" y="74" width="238" height="104" rx="5" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="1.5"/>
  <text x="127" y="104" text-anchor="middle" font-size="19" font-weight="600" fill="#0b0b0b">1 · Execution</text>
  <text x="127" y="130" text-anchor="middle" font-size="15.5" fill="#52514e">Endorsers simulate and</text>
  <text x="127" y="150" text-anchor="middle" font-size="15.5" fill="#52514e">sign the transaction</text>
  <!-- stage 2 -->
  <rect x="286" y="74" width="238" height="104" rx="5" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="1.5"/>
  <text x="405" y="104" text-anchor="middle" font-size="19" font-weight="600" fill="#0b0b0b">2 · Ordering</text>
  <text x="405" y="130" text-anchor="middle" font-size="15.5" fill="#52514e">Ordering service fixes a</text>
  <text x="405" y="150" text-anchor="middle" font-size="15.5" fill="#52514e">total order, emits blocks</text>
  <!-- stage 3 — highlighted -->
  <rect x="564" y="56" width="568" height="140" rx="6" fill="#fff" stroke="#eb6834" stroke-width="2.5"/>
  <text x="848" y="86" text-anchor="middle" font-size="20" font-weight="600" fill="#0b0b0b">3 · Validation &amp; Commit — the Committer</text>
  <rect x="588" y="104" width="123" height="70" rx="4" fill="#fcfcfb" stroke="#e1e0d9"/>
  <text x="649.5" y="132" text-anchor="middle" font-size="14" fill="#52514e">Signature</text>
  <text x="649.5" y="150" text-anchor="middle" font-size="14" fill="#52514e">verification</text>
  <rect x="723" y="104" width="123" height="70" rx="4" fill="#fcfcfb" stroke="#e1e0d9"/>
  <text x="784.5" y="132" text-anchor="middle" font-size="14" fill="#52514e">Dependency</text>
  <text x="784.5" y="150" text-anchor="middle" font-size="14" fill="#52514e">analysis</text>
  <rect x="858" y="104" width="123" height="70" rx="4" fill="#fcfcfb" stroke="#e1e0d9"/>
  <text x="919.5" y="132" text-anchor="middle" font-size="14" fill="#52514e">MVCC</text>
  <text x="919.5" y="150" text-anchor="middle" font-size="14" fill="#52514e">validation</text>
  <rect x="993" y="104" width="115" height="70" rx="4" fill="#fcfcfb" stroke="#e1e0d9"/>
  <text x="1050.5" y="132" text-anchor="middle" font-size="14" fill="#52514e">Commit to</text>
  <text x="1050.5" y="150" text-anchor="middle" font-size="14" fill="#52514e">state DB</text>
  <line x1="248" y1="126" x2="282" y2="126" stroke="#52514e" stroke-width="1.6" marker-end="url(#a1)"/>
  <line x1="526" y1="126" x2="560" y2="126" stroke="#52514e" stroke-width="1.6" marker-end="url(#a1)"/>
  <text x="8" y="238" font-size="15.5" fill="#52514e">Out of scope</text>
  <line x1="8" y1="248" x2="524" y2="248" stroke="#c3c2b7" stroke-width="1" stroke-dasharray="4 3"/>
  <text x="564" y="238" font-size="15.5" font-weight="600" fill="#eb6834">Covered here</text>
  <line x1="564" y1="248" x2="1132" y2="248" stroke="#eb6834" stroke-width="1.5"/>
</svg>

<div class="src">Source: README.md § Background · docs/index.md</div>

<!--
Notes: the three-stage lifecycle is familiar Fabric. Scope: the Committer owns stage 3 only. Blocks arrive already ordered — it never re-orders and never executes chaincode.
-->

---

<div class="eyebrow">1 · Context</div>

## How this differs from the classic Fabric peer

| Concern | Classic Fabric peer | Fabric-X Committer |
|---|---|---|
| **Packaging** | Validation is a stage *inside* the peer process | Six independently deployable services |
| **Parallelism** | Validation largely coupled to block order | Dependency graph dispatches conflict-free transactions in parallel |
| **Scaling** | Scale the whole peer | Verifier, VC, Query and DB scale **horizontally and independently** |
| **State store** | LevelDB / CouchDB, local to the peer | Distributed SQL cluster (YugabyteDB or PostgreSQL) |
| **Commit path** | Peer writes its own state DB | VC services commit via **stored procedures**, bulk-batched |
| **Recovery** | Peer replays from its own ledger | Per-service recovery, made safe by **idempotent commits** |

<div class="keyline">
The central design move: <strong>disaggregate validation from the peer</strong>, then recover the lost throughput by scheduling non-conflicting transactions concurrently rather than serially.
</div>

<div class="src">Source: docs/architecture.md §1–3 · docs/index.md</div>

<!--
Notes: this is the "why does this exist" answer. Is it still Fabric? Yes — same lifecycle and
same endorsement semantics; the validation stage is re-implemented as a scale-out system.
-->

---

<div class="eyebrow">1 · Context</div>

## Design goals

<div class="two">
<div class="card" style="border-left-color:var(--coord)">
<h3>High throughput</h3>
<p>Pipelined stages plus parallel dispatch of conflict-free transactions. Documented at <strong>&gt;100,000 TPS</strong> on commodity hardware with YugabyteDB.</p>
</div>
<div class="card" style="border-left-color:var(--verif)">
<h3>Fault tolerance</h3>
<p>Idempotent commits let any service be restarted or replaced without data corruption or double-commit. Each service recovers independently.</p>
</div>
</div>

<div class="two" style="margin-top:16px">
<div class="card" style="border-left-color:var(--sidecar)">
<h3>Horizontal scaling</h3>
<p>Verifier, VC, Query Service and database nodes scale out. Sidecar and Coordinator scale up (single instance each).</p>
</div>
<div class="card" style="border-left-color:var(--query)">
<h3>Flexible endorsement policy</h3>
<p>Lightweight threshold rules (one public key) through to fine-grained MSP rules — AND / OR / k-of-n over organisational identities.</p>
</div>
</div>

<div class="keyline" style="border-left-color:var(--db)">
<strong>Observability</strong> — Prometheus metrics at every pipeline stage, including queue-depth gauges specifically to locate bottlenecks.
</div>

<div class="src">Source: docs/index.md § Key Capabilities</div>

<!--
Notes: the 100k TPS number is the figure documented in docs/index.md and
docs/architecture.md §3.6 for YugabyteDB on commodity hardware. It is a documented
figure, not a fresh benchmark — treat it as "what our docs claim" if pressed, and offer
to re-run the load generator for a specific hardware profile.
-->

---

<div class="eyebrow">1 · Context</div>

## The six services

<svg viewBox="0 0 1140 400" width="1010" height="366" role="img" aria-label="Architecture: ordering service to sidecar to coordinator, which fans out to verifier and validator-committer; VC and query service both reach the database cluster">
  <defs>
    <marker id="a2" markerWidth="9" markerHeight="9" refX="7.5" refY="4.5" orient="auto">
      <path d="M0,1 L7.5,4.5 L0,8" fill="none" stroke="#52514e" stroke-width="1.6"/>
    </marker>
    <marker id="a2s" markerWidth="9" markerHeight="9" refX="1.5" refY="4.5" orient="auto">
      <path d="M7.5,1 L0,4.5 L7.5,8" fill="none" stroke="#52514e" stroke-width="1.6"/>
    </marker>
  </defs>
  <!-- Ordering service (external) -->
  <rect x="6" y="150" width="150" height="62" rx="5" fill="#f2f1ec" stroke="#c3c2b7" stroke-width="1.5" stroke-dasharray="5 3"/>
  <text x="81" y="176" text-anchor="middle" font-size="15" font-weight="600" fill="#52514e">Ordering</text>
  <text x="81" y="194" text-anchor="middle" font-size="15" font-weight="600" fill="#52514e">Service</text>
  <!-- Sidecar -->
  <rect x="196" y="150" width="150" height="62" rx="5" fill="#fcfcfb" stroke="#2a78d6" stroke-width="2.5"/>
  <text x="271" y="176" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Sidecar</text>
  <text x="271" y="196" text-anchor="middle" font-size="13" fill="#52514e">stateful · 1 inst.</text>
  <!-- Coordinator -->
  <rect x="386" y="150" width="150" height="62" rx="5" fill="#fcfcfb" stroke="#eb6834" stroke-width="2.5"/>
  <text x="461" y="176" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Coordinator</text>
  <text x="461" y="196" text-anchor="middle" font-size="13" fill="#52514e">stateless · 1 inst.</text>
  <!-- Verifier -->
  <rect x="600" y="40" width="168" height="62" rx="5" fill="#fcfcfb" stroke="#1baf7a" stroke-width="2.5"/>
  <text x="684" y="66" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Verifier</text>
  <text x="684" y="86" text-anchor="middle" font-size="13" fill="#52514e">stateless · 2–3 inst.</text>
  <!-- VC -->
  <rect x="600" y="150" width="168" height="62" rx="5" fill="#fcfcfb" stroke="#eda100" stroke-width="2.5"/>
  <text x="684" y="176" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Validator-Committer</text>
  <text x="684" y="196" text-anchor="middle" font-size="13" fill="#52514e">stateless · 3–6 inst.</text>
  <!-- Query -->
  <rect x="600" y="292" width="168" height="62" rx="5" fill="#fcfcfb" stroke="#e87ba4" stroke-width="2.5"/>
  <text x="684" y="318" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Query Service</text>
  <text x="684" y="338" text-anchor="middle" font-size="13" fill="#52514e">stateless · 2+ inst.</text>
  <!-- Database -->
  <rect x="856" y="150" width="150" height="204" rx="5" fill="#fcfcfb" stroke="#008300" stroke-width="2.5"/>
  <text x="931" y="222" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Database</text>
  <text x="931" y="243" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">Cluster</text>
  <text x="931" y="266" text-anchor="middle" font-size="13" fill="#52514e">stateful · 6–9 nodes</text>
  <text x="931" y="288" text-anchor="middle" font-size="12.5" fill="#898781">world state · tx status</text>
  <text x="931" y="304" text-anchor="middle" font-size="12.5" fill="#898781">policies · config</text>
  <!-- Clients -->
  <rect x="386" y="292" width="150" height="62" rx="5" fill="#f2f1ec" stroke="#c3c2b7" stroke-width="1.5" stroke-dasharray="5 3"/>
  <text x="461" y="318" text-anchor="middle" font-size="14.5" font-weight="600" fill="#52514e">Clients &amp;</text>
  <text x="461" y="336" text-anchor="middle" font-size="14.5" font-weight="600" fill="#52514e">Endorsers</text>
  <!-- flows -->
  <line x1="158" y1="181" x2="192" y2="181" stroke="#52514e" stroke-width="1.6" marker-end="url(#a2)"/>
  <text x="175" y="142" text-anchor="middle" font-size="12.5" fill="#898781">blocks</text>
  <line x1="348" y1="181" x2="382" y2="181" stroke="#52514e" stroke-width="1.6" marker-end="url(#a2)"/>
  <text x="365" y="142" text-anchor="middle" font-size="12.5" fill="#898781">blocks</text>
  <!-- coordinator to verifier (bidirectional) -->
  <path d="M536,168 C566,168 570,71 596,71" fill="none" stroke="#52514e" stroke-width="1.6"
        marker-end="url(#a2)" marker-start="url(#a2s)"/>
  <text x="576" y="112" text-anchor="middle" font-size="12.5" fill="#898781">verify</text>
  <!-- coordinator to VC (bidirectional) -->
  <line x1="540" y1="192" x2="596" y2="192" stroke="#52514e" stroke-width="1.6"
        marker-end="url(#a2)" marker-start="url(#a2s)"/>
  <text x="568" y="228" text-anchor="middle" font-size="12.5" fill="#898781">commit</text>
  <!-- VC to DB -->
  <line x1="770" y1="181" x2="852" y2="181" stroke="#52514e" stroke-width="1.6"
        marker-end="url(#a2)" marker-start="url(#a2s)"/>
  <text x="811" y="170" text-anchor="middle" font-size="12.5" fill="#898781">r/w</text>
  <!-- Query to DB -->
  <line x1="770" y1="323" x2="852" y2="323" stroke="#52514e" stroke-width="1.6" marker-end="url(#a2)"/>
  <text x="811" y="313" text-anchor="middle" font-size="12.5" fill="#898781">read</text>
  <!-- clients to query -->
  <line x1="538" y1="323" x2="596" y2="323" stroke="#52514e" stroke-width="1.6" marker-end="url(#a2)"/>
  <!-- sidecar delivers to clients -->
  <path d="M240,214 C240,266 340,266 400,290" fill="none" stroke="#2a78d6" stroke-width="1.6"
        stroke-dasharray="5 3" marker-end="url(#a2)"/>
  <text x="196" y="246" font-size="12.5" fill="#2a78d6">deliver blocks</text>
</svg>

<div class="src">Source: docs/architecture.md §2 · docs/index.md</div>

<!--
Notes: this diagram is the map for the rest of the document. Each service keeps its colour in every later diagram.

The critical structural point: the Query Service is NOT in the commit path. It reads
the database independently, so endorser query load never slows down commits.
-->

---

<div class="eyebrow">1 · Context</div>

## State, cardinality and scaling

| Service | State | Instances | Scaling |
|---|---|---|---|
| <span class="chip" style="background:var(--sidecar)">Sidecar</span> | **Stateful** — local append-only block store | 1 (critical single point) | Vertical only |
| <span class="chip" style="background:var(--coord)">Coordinator</span> | Stateless — dependency graph is in memory | 1 (critical single point) | Vertical only |
| <span class="chip" style="background:var(--verif)">Verifier</span> | Stateless — policy cache in memory | 2–3 | Horizontal + vertical |
| <span class="chip" style="background:var(--vc)">VC</span> | Stateless — all state in the database | 3–6, often co-located with DB nodes | Horizontal + vertical |
| <span class="chip" style="background:var(--query)">Query</span> | Stateless | 2+, sized to endorser count | Horizontal + vertical |
| <span class="chip" style="background:var(--db)">Database</span> | **Stateful** — source of truth | 6–9 nodes | Horizontal + vertical |

<div class="keyline">
Only <strong>two</strong> components hold persistent state: the Sidecar's block store and the database cluster. Everything else is reconstructible, which is what makes recovery cheap.
</div>

<div class="src">Source: docs/architecture.md §3 (per-service "Key Characteristics") and §4</div>

<!--
Notes: are the Sidecar and Coordinator single points of failure? Yes — both are single-instance and scale vertically only. What
mitigates it is that neither holds unrecoverable state — the Coordinator is stateless, and
the Sidecar's block store can be rebuilt from the ordering service. Restart is fast and
safe rather than being prevented. See Failure recovery, page 27.
-->

---

<!-- _class: part -->

<div class="num">Section 2</div>

# Transaction flow

<div class="sub">The path a block takes through the system, and where the throughput comes from.</div>

<div style="max-width:780px">
<div class="row"><span>The life of a block</span><span class="d"></span><span class="pg">10</span></div>
<div class="row"><span>Why the pipeline is fast</span><span class="d"></span><span class="pg">11</span></div>
</div>

---

<div class="eyebrow">2 · Transaction flow</div>

## The life of a block

<svg viewBox="0 0 1140 336" width="1050" height="316" role="img" aria-label="Seven numbered steps of block processing from ingestion through to client delivery">
  <defs>
    <marker id="a3" markerWidth="8" markerHeight="8" refX="6.8" refY="4" orient="auto">
      <path d="M0,0.8 L6.8,4 L0,7.2" fill="none" stroke="#c3c2b7" stroke-width="1.5"/>
    </marker>
  </defs>
  <line x1="30" y1="46" x2="30" y2="300" stroke="#e1e0d9" stroke-width="2"/>
  <circle cx="30" cy="46" r="15" fill="#2a78d6"/>
  <text x="30" y="52" text-anchor="middle" font-size="15" font-weight="600" fill="#fff">1</text>
  <text x="60" y="42" font-size="17.5" font-weight="600" fill="#0b0b0b">Fetch &amp; translate</text>
  <text x="60" y="63" font-size="15" fill="#52514e">Sidecar pulls the next block, decodes to <tspan font-family="IBM Plex Mono, monospace" font-size="13.5">protoblocktx.Tx</tspan>, drops malformed txs,</text>
  <text x="60" y="82" font-size="15" fill="#52514e">and marks duplicate tx IDs among in-flight transactions.</text>
  <circle cx="30" cy="118" r="15" fill="#eb6834"/>
  <text x="30" y="124" text-anchor="middle" font-size="15" font-weight="600" fill="#fff">2</text>
  <text x="60" y="114" font-size="17.5" font-weight="600" fill="#0b0b0b">Dependency analysis</text>
  <text x="60" y="135" font-size="15" fill="#52514e">Coordinator builds the DAG and selects transactions with <tspan font-weight="600" fill="#0b0b0b">out-degree zero</tspan>.</text>
  <circle cx="30" cy="171" r="15" fill="#1baf7a"/>
  <text x="30" y="177" text-anchor="middle" font-size="15" font-weight="600" fill="#fff">3</text>
  <text x="60" y="167" font-size="17.5" font-weight="600" fill="#0b0b0b">Signature verification</text>
  <text x="60" y="188" font-size="15" fill="#52514e">Verifier Manager load-balances free txs across Verifiers; failures are marked, not dropped.</text>
  <circle cx="30" cy="224" r="15" fill="#eda100"/>
  <text x="30" y="230" text-anchor="middle" font-size="15" font-weight="600" fill="#fff">4</text>
  <text x="60" y="220" font-size="17.5" font-weight="600" fill="#0b0b0b">MVCC validation &amp; commit</text>
  <text x="60" y="241" font-size="15" fill="#52514e">VC runs Prepare → Validate → Commit; writes land via stored procedures.</text>
  <circle cx="30" cy="277" r="15" fill="#52514e"/>
  <text x="30" y="283" text-anchor="middle" font-size="15" font-weight="600" fill="#fff">5</text>
  <text x="60" y="273" font-size="17.5" font-weight="600" fill="#0b0b0b">Status feedback, persist &amp; deliver</text>
  <text x="60" y="294" font-size="15" fill="#52514e">Statuses update the DAG (freeing dependents) and flow back to the Sidecar for storage and clients.</text>
</svg>

<div class="src">Source: docs/coordinator.md §4 (Steps 1–6) · docs/sidecar.md §2, §4</div>

<!--
Notes: two details that commonly surprise people.

First, invalid transactions are NOT discarded at verification. They travel all the way to
the VC service carrying a pre-set invalid status, so the VC records the final status
without re-validating. One durable status path for both outcomes — recovery logic and
clients can always query a definitive answer.

Second, step 5 is a feedback loop, not a terminus: committing a transaction can free its
dependents in the DAG, which immediately makes more work dispatchable.
-->

---

<div class="eyebrow">2 · Transaction flow</div>

## Why the pipeline is fast

<svg viewBox="0 0 1120 300" width="1010" height="272" role="img" aria-label="Timeline showing three blocks whose verify, validate and commit stages overlap in time rather than running one after another">
  <!-- axis -->
  <line x1="126" y1="232" x2="1090" y2="232" stroke="#c3c2b7" stroke-width="1.5"/>
  <text x="608" y="262" text-anchor="middle" font-size="14.5" fill="#898781">time →</text>
  <!-- legend -->
  <rect x="126" y="16" width="11" height="11" rx="2" fill="#1baf7a"/>
  <text x="144" y="26" font-size="14" fill="#52514e">verify</text>
  <rect x="212" y="16" width="11" height="11" rx="2" fill="#eda100"/>
  <text x="230" y="26" font-size="14" fill="#52514e">validate</text>
  <rect x="304" y="16" width="11" height="11" rx="2" fill="#eb6834"/>
  <text x="322" y="26" font-size="14" fill="#52514e">commit</text>
  <!-- Block N -->
  <text x="112" y="76" text-anchor="end" font-size="15.5" font-weight="600" fill="#0b0b0b">Block N</text>
  <rect x="126" y="60" width="176" height="22" rx="4" fill="#1baf7a"/>
  <rect x="306" y="60" width="176" height="22" rx="4" fill="#eda100"/>
  <rect x="486" y="60" width="176" height="22" rx="4" fill="#eb6834"/>
  <!-- Block N+1 -->
  <text x="112" y="122" text-anchor="end" font-size="15.5" font-weight="600" fill="#0b0b0b">Block N+1</text>
  <rect x="306" y="106" width="176" height="22" rx="4" fill="#1baf7a"/>
  <rect x="486" y="106" width="176" height="22" rx="4" fill="#eda100"/>
  <rect x="666" y="106" width="176" height="22" rx="4" fill="#eb6834"/>
  <!-- Block N+2 -->
  <text x="112" y="168" text-anchor="end" font-size="15.5" font-weight="600" fill="#0b0b0b">Block N+2</text>
  <rect x="486" y="152" width="176" height="22" rx="4" fill="#1baf7a"/>
  <rect x="666" y="152" width="176" height="22" rx="4" fill="#eda100"/>
  <rect x="846" y="152" width="176" height="22" rx="4" fill="#eb6834"/>
  <!-- concurrency marker -->
  <rect x="486" y="46" width="176" height="140" rx="4" fill="none" stroke="#52514e" stroke-width="1.5" stroke-dasharray="4 3"/>
  <line x1="574" y1="186" x2="574" y2="210" stroke="#52514e" stroke-width="1.2"/>
  <text x="574" y="226" text-anchor="middle" font-size="14" fill="#52514e">three stages busy at once</text>
</svg>

<div class="two" style="margin-top:2px">
<div>

**Two independent sources of concurrency**

- **Across stages** — verify, validate and commit all run simultaneously on *different* blocks
- **Within a stage** — every conflict-free transaction proceeds in parallel

</div>
<div>

**What keeps it correct**

- The DAG enforces ordering *only* between genuinely dependent transactions
- Everything else is free to reorder, so results stay deterministic

</div>
</div>

<div class="src">Source: docs/architecture.md § Component Interactions · docs/coordinator.md §4–5</div>

<!--
Notes: this diagram is illustrative of the pipelining concept — the bars are not
measured timings. For real numbers use the load generator (`make bench-*`,
docs/loadgen-artifacts.md) rather than these bar widths.

The honest summary: throughput comes from the product of these two forms of concurrency,
and the ceiling is normally the database, which is why the DB choice matters so much.
-->

---

<!-- _class: part -->

<div class="num">Section 3</div>

# Components

<div class="sub">Each of the six services: what it does, how it is configured, and how it behaves internally.</div>

<div class="two">
<div>
<div class="row"><span>Sidecar — boundary</span><span class="d"></span><span class="pg">13</span></div>
<div class="row"><span>Sidecar — internals</span><span class="d"></span><span class="pg">14</span></div>
<div class="row"><span>Coordinator — orchestrator</span><span class="d"></span><span class="pg">15</span></div>
<div class="row"><span>Dependency graph</span><span class="d"></span><span class="pg">16</span></div>
<div class="row"><span>Coordinator — wiring</span><span class="d"></span><span class="pg">17</span></div>
<div class="row"><span>Verifier</span><span class="d"></span><span class="pg">18</span></div>
</div>
<div>
<div class="row"><span>Endorsement policy</span><span class="d"></span><span class="pg">19</span></div>
<div class="row"><span>VC — pipeline</span><span class="d"></span><span class="pg">20</span></div>
<div class="row"><span>VC — schema</span><span class="d"></span><span class="pg">21</span></div>
<div class="row"><span>VC — idempotency</span><span class="d"></span><span class="pg">22</span></div>
<div class="row"><span>Query Service</span><span class="d"></span><span class="pg">23</span></div>
<div class="row"><span>Database cluster</span><span class="d"></span><span class="pg">24</span></div>
</div>
</div>

---

<div class="eyebrow">3 · Components — <span class="lead-title">Sidecar</span></div>

## Sidecar — the boundary to the ordering service

<div class="two-wide">
<div>

**Six responsibilities**

1. **Fetch** blocks sequentially from the ordering service
2. **Translate & validate** — decode to `protoblocktx.Tx`, filter malformed transactions
3. **Relay & collect** — forward to the Coordinator, receive per-transaction statuses
4. **Persist** committed blocks to a local append-only file store
5. **Deliver** committed blocks to registered clients
6. **Notify** — transaction-ID subscriptions and an all-transactions stream

</div>
<div class="card" style="border-left-color:var(--sidecar)">
<h3>At a glance</h3>
<p><strong>State</strong> — stateful (block store)</p>
<p><strong>Instances</strong> — exactly 1</p>
<p><strong>Default port</strong> — <code>:4001</code></p>
<p><strong>Config</strong> — orderer endpoints, coordinator endpoint, listen address</p>
<p><strong>Code</strong> — <code>service/sidecar/</code></p>
</div>
</div>

<div class="keyline" style="border-left-color:var(--sidecar)">
Orderer endpoints in the YAML config can be <strong>overridden by the channel's own configuration block</strong> — the channel config wins.
</div>

<div class="src">Source: docs/sidecar.md §1–3 · cmd/config/samples/sidecar.yaml</div>

<!--
Notes: the Sidecar is the only component that speaks the ordering service's
protocol, and the only one clients receive blocks from. For an integrator this is the most
important service in the deck — it is where you attach.

Note the endpoint-precedence rule: operators are caught out by editing YAML and seeing no
effect because the channel config block overrides it.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Sidecar</span></div>

## Sidecar — internals

<div class="two-wide">
<div>

**Three long-running tasks, two queues**

```
Task 1: fetch from orderer
          │  blocksToBeCommitted
          ▼
Task 2: relay to coordinator,
        collect statuses
          │  committedBlocks
          ▼
Task 3: append to block store
```

Decoupling via bounded channels means a slow disk cannot stall block fetching, and a slow orderer cannot stall persistence.

</div>
<div>

**Notification service**

A **single-threaded event loop** — no locks — selecting over three channels:

- `requestQueue` — client subscriptions
- `statusQueue` — status batches from the relay
- `timeoutQueue` — expired subscription timers

Each client stream owns a buffered `streamEventQueue` (default **100**) so one slow consumer cannot block dispatch to others.

Unmatched IDs at timeout are returned to the client as `TimeoutTxIds`.

</div>
</div>

<div class="src">Source: docs/sidecar.md §4 (Tasks 1–3), §6 (Internal Architecture) · service/sidecar/notify.go</div>

<!--
Notes: the notifier design is a good illustration of a repo-wide pattern —
single-goroutine event loops preferred over mutexes. Statuses include rejections such as
REJECTED_DUPLICATE_TX_ID and MALFORMED_BAD_ENVELOPE, so a subscriber gets a definitive
answer for a transaction that never made it into the pipeline, not just a timeout.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Coordinator</span></div>

## Coordinator — the orchestrator

<div class="two-wide">
<div>

**Five responsibilities**

1. **Receive blocks** from the Sidecar
2. **Manage dependencies** — build and maintain the DAG; this is the core mechanism for safe parallelism
3. **Dispatch for verification** — load-balance across Verifiers, track in-flight work per instance
4. **Dispatch for commit** — route verified transactions to VC instances
5. **Aggregate statuses** — update the DAG, relay results to the Sidecar

</div>
<div class="card" style="border-left-color:var(--coord)">
<h3>At a glance</h3>
<p><strong>State</strong> — stateless; graph is in memory only</p>
<p><strong>Instances</strong> — exactly 1</p>
<p><strong>Default port</strong> — <code>:9001</code></p>
<p><strong>Config</strong> — verifier endpoints, VC endpoints, listen address</p>
<p><strong>Code</strong> — <code>service/coordinator/</code></p>
</div>
</div>

<div class="keyline">
Because the Coordinator tracks which transactions it sent to which instance, a Verifier or VC failure is a <strong>re-dispatch</strong>, not a lost block.
</div>

<div class="src">Source: docs/coordinator.md §1–3 · cmd/config/samples/coordinator.yaml</div>

<!--
Notes: the Coordinator is stateless but singleton — the graph it holds is derived
data, rebuildable from the blocks the Sidecar still has. That is why a restart is
inexpensive despite there being only one instance.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Coordinator</span></div>

## The dependency graph — the core idea

<div class="two-wide">
<div>

<svg viewBox="0 0 560 300" width="540" height="290" role="img" aria-label="Directed acyclic graph of five transactions; T1 and T3 have out-degree zero and are dispatched">
  <defs>
    <marker id="a4" markerWidth="8" markerHeight="8" refX="6.8" refY="4" orient="auto">
      <path d="M0,0.8 L6.8,4 L0,7.2" fill="none" stroke="#52514e" stroke-width="1.5"/>
    </marker>
  </defs>
  <!-- free nodes -->
  <circle cx="80" cy="66" r="30" fill="#fff" stroke="#1baf7a" stroke-width="3"/>
  <text x="80" y="73" text-anchor="middle" font-size="18" font-weight="600" fill="#0b0b0b">T1</text>
  <circle cx="80" cy="212" r="30" fill="#fff" stroke="#1baf7a" stroke-width="3"/>
  <text x="80" y="219" text-anchor="middle" font-size="18" font-weight="600" fill="#0b0b0b">T3</text>
  <!-- blocked nodes -->
  <circle cx="280" cy="66" r="30" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="2"/>
  <text x="280" y="73" text-anchor="middle" font-size="18" font-weight="600" fill="#52514e">T2</text>
  <circle cx="280" cy="212" r="30" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="2"/>
  <text x="280" y="219" text-anchor="middle" font-size="18" font-weight="600" fill="#52514e">T4</text>
  <circle cx="462" cy="212" r="30" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="2"/>
  <text x="462" y="219" text-anchor="middle" font-size="18" font-weight="600" fill="#52514e">T5</text>
  <!-- edges: later -> earlier -->
  <line x1="248" y1="66" x2="114" y2="66" stroke="#52514e" stroke-width="1.8" marker-end="url(#a4)"/>
  <text x="181" y="54" text-anchor="middle" font-size="14.5" font-family="IBM Plex Mono, monospace" fill="#eb6834">rw(k1)</text>
  <line x1="248" y1="212" x2="114" y2="212" stroke="#52514e" stroke-width="1.8" marker-end="url(#a4)"/>
  <text x="181" y="200" text-anchor="middle" font-size="14.5" font-family="IBM Plex Mono, monospace" fill="#eb6834">wr(k2)</text>
  <line x1="430" y1="212" x2="314" y2="212" stroke="#52514e" stroke-width="1.8" marker-end="url(#a4)"/>
  <text x="372" y="200" text-anchor="middle" font-size="14.5" font-family="IBM Plex Mono, monospace" fill="#eb6834">ww(k2)</text>
  <!-- annotation -->
  <rect x="24" y="122" width="112" height="26" rx="13" fill="#1baf7a"/>
  <text x="80" y="140" text-anchor="middle" font-size="13.5" font-weight="600" fill="#fff">dispatched now</text>
  <text x="24" y="278" font-size="14" fill="#898781">Edges point from the later transaction to the earlier one.</text>
</svg>

</div>
<div>

**Three dependency types**

`rw(k)` — T<sub>i</sub> writes `k`; later T<sub>j</sub> read the *previous* version. If T<sub>i</sub> is valid, **T<sub>j</sub> must be invalid** — it read a stale value.

`wr(k)` — T<sub>i</sub> reads `k`; later T<sub>j</sub> writes it. Enforces **commit order**, so T<sub>i</sub>'s read does not retroactively go stale.

`ww(k)` — both write `k`. Ensures T<sub>j</sub>'s write **cannot overwrite and lose** T<sub>i</sub>'s.

<div class="keyline" style="margin-top:14px">
Dispatch rule: <strong>out-degree zero</strong> — no outstanding dependencies. As transactions finalise, edges clear and dependents become dispatchable.
</div>

</div>
</div>

<div class="src">Source: docs/coordinator.md §5 (A. Dependency Types, B. Identifying Dependency-Free Transactions)</div>

<!--
Notes: this is the intellectual heart of the system, and the most likely source of questions.

Note the asymmetry that catches people out: rw is a *correctness verdict* — the dependent
transaction is doomed and will abort. wr and ww are *ordering constraints* — those
transactions can still commit perfectly well, they simply must not commit early.

The DAG is per in-flight work, not per block: dependencies span block boundaries, which is
exactly why recovery has to cope with partially-committed blocks.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Coordinator</span></div>

## Coordinator — internal wiring

<svg viewBox="0 0 1140 250" width="1030" height="228" role="img" aria-label="Five Go channels connecting the coordinator's internal components in a loop">
  <defs>
    <marker id="a5" markerWidth="8" markerHeight="8" refX="6.8" refY="4" orient="auto">
      <path d="M0,0.8 L6.8,4 L0,7.2" fill="none" stroke="#52514e" stroke-width="1.5"/>
    </marker>
  </defs>
  <rect x="6" y="70" width="150" height="58" rx="5" fill="#fcfcfb" stroke="#eb6834" stroke-width="2"/>
  <text x="81" y="95" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Coordinator</text>
  <text x="81" y="114" text-anchor="middle" font-size="13" fill="#52514e">(block intake)</text>
  <rect x="256" y="70" width="164" height="58" rx="5" fill="#fcfcfb" stroke="#eb6834" stroke-width="2"/>
  <text x="338" y="95" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Dependency</text>
  <text x="338" y="114" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Graph Manager</text>
  <rect x="520" y="70" width="164" height="58" rx="5" fill="#fcfcfb" stroke="#1baf7a" stroke-width="2"/>
  <text x="602" y="95" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Signature</text>
  <text x="602" y="114" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Verifier Manager</text>
  <rect x="784" y="70" width="164" height="58" rx="5" fill="#fcfcfb" stroke="#eda100" stroke-width="2"/>
  <text x="866" y="95" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Validator-</text>
  <text x="866" y="114" text-anchor="middle" font-size="14.5" font-weight="600" fill="#0b0b0b">Committer Manager</text>
  <rect x="1000" y="70" width="134" height="58" rx="5" fill="#f2f1ec" stroke="#c3c2b7" stroke-width="1.5" stroke-dasharray="5 3"/>
  <text x="1067" y="95" text-anchor="middle" font-size="14" font-weight="600" fill="#52514e">Sidecar</text>
  <text x="1067" y="114" text-anchor="middle" font-size="12.5" fill="#52514e">(statuses)</text>
  <!-- forward channels -->
  <line x1="158" y1="99" x2="252" y2="99" stroke="#52514e" stroke-width="1.6" marker-end="url(#a5)"/>
  <text x="205" y="60" text-anchor="middle" font-size="12" font-family="IBM Plex Mono, monospace" fill="#898781">…DepGraphTxs</text>
  <line x1="422" y1="99" x2="516" y2="99" stroke="#52514e" stroke-width="1.6" marker-end="url(#a5)"/>
  <text x="469" y="60" text-anchor="middle" font-size="12" font-family="IBM Plex Mono, monospace" fill="#898781">…FreeTxs</text>
  <line x1="686" y1="99" x2="780" y2="99" stroke="#52514e" stroke-width="1.6" marker-end="url(#a5)"/>
  <text x="733" y="60" text-anchor="middle" font-size="12" font-family="IBM Plex Mono, monospace" fill="#898781">…ValidatedTxs</text>
  <line x1="950" y1="99" x2="996" y2="99" stroke="#52514e" stroke-width="1.6" marker-end="url(#a5)"/>
  <text x="973" y="60" text-anchor="middle" font-size="12" font-family="IBM Plex Mono, monospace" fill="#898781">…TxStatus</text>
  <!-- feedback edge -->
  <path d="M866,132 C866,196 338,196 338,132" fill="none" stroke="#eb6834" stroke-width="1.8" marker-end="url(#a5)"/>
  <text x="602" y="214" text-anchor="middle" font-size="13" font-family="IBM Plex Mono, monospace" fill="#eb6834">vcServiceToDepGraphValidatedTxs — resolves dependencies</text>
</svg>

<div class="keyline">
Internal components communicate <strong>only</strong> through bounded Go channels. Each stage applies backpressure to the one before it, so no stage can outrun the database.
</div>

<div class="src">Source: docs/coordinator.md §4 (Inter-component Communication Channels)</div>

<!--
Notes: the orange feedback edge is the important part. Committing a
transaction is what unblocks its dependents, so the graph manager sits in a closed loop
with the VC manager. Throughput is self-regulating: if the database slows, channels fill,
backpressure propagates to block intake.

Channel names are abbreviated here for legibility; full names are in coordinator.md §4.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Verifier</span></div>

## Verifier — parallel signature verification

<div class="two-wide">
<div>

**Four components**

- **Server** — implements the gRPC `VerifierServer`, manages bidirectional streams
- **Verifier** — holds the namespace→policy map, performs validation
- **Parallel Executor** — worker-goroutine pool, size set by `parallelism`
- **Policy Manager** — parses updates from config transactions and namespace policies

**Batched responses**

- **Size-based** — flush at `batch-size-cutoff`
- **Time-based** — flush at `batch-time-cutoff` regardless

</div>
<div class="card" style="border-left-color:var(--verif)">
<h3>At a glance</h3>
<p><strong>State</strong> — stateless; policies cached in memory, DB is authoritative</p>
<p><strong>Instances</strong> — 2–3</p>
<p><strong>Default port</strong> — <code>:5001</code></p>
<p><strong>Code</strong> — <code>service/verifier/</code></p>
</div>
</div>

<div class="keyline" style="border-left-color:var(--verif)">
The policy map is swapped through an <strong>atomic pointer</strong> — policy updates never block in-flight verification. Note that <em>structural</em> transaction validation happens in the Sidecar, not here.
</div>

<div class="src">Source: docs/verification-service.md §2–4 · docs/architecture.md §3.3</div>

<!--
Notes: a common misconception is that this service does structural validation. It
does not — the Sidecar filters malformed transactions during translation. This service is
signatures against policies, nothing more.

Outcome is binary per transaction: all signatures valid → COMMITTED, otherwise
ABORTED_SIGNATURE_INVALID.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Verifier</span></div>

## Endorsement policy — two rule types

<div class="two">
<div class="card" style="border-left-color:var(--verif)">
<h3>Threshold rules</h3>
<p>Lightweight. The public key is <strong>embedded in the policy</strong>.</p>
<p>ASN.1 DER-encode the namespace data, compute <code>SHA256</code> explicitly, verify against the configured key.</p>
<p>The endorsement's <code>Identity</code> field is <strong>ignored</strong> — only the signature bytes are used.</p>
</div>
<div class="card" style="border-left-color:var(--verif)">
<h3>MSP rules</h3>
<p>Fine-grained: AND / OR / k-of-n over organisational identities.</p>
<p>Identities are deserialised via the MSP's <code>IdentityDeserializer</code>, paired with signatures, and the <code>SignaturePolicyEnvelope</code> tree is evaluated.</p>
<p>Raw ASN.1 bytes are passed to <code>identity.Verify</code>, which hashes internally.</p>
</div>
</div>

<div class="keyline" style="border-left-color:var(--coord)">
<strong>Signatures are consumed in order.</strong> A single endorser cannot satisfy two distinct principal requirements, even if its identity matches both — each <code>SignedBy</code> leaf consumes one signature.
</div>

<h3 style="margin-top:10px">Policy propagation</h3>

When a committed transaction creates or updates a namespace, the new policy is
**piggybacked onto the next validation request** to each Verifier rather than pushed separately — the verifier applies it before processing the accompanying transactions.

<div class="src">Source: docs/verification-service.md §8 · docs/namespace-policy.md · docs/coordinator.md §4 Step 6</div>

<!--
Notes: the signature-consumption rule is a real, spec-level gotcha — worth
mentioning to anyone writing policies, because a k-of-n policy cannot be satisfied by one
party signing n times.

The piggyback mechanism is a throughput optimisation: no extra round trip per policy
change, and policy updates are naturally ordered with respect to the transactions that
depend on them.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Validator-Committer</span></div>

## VC — the three-stage pipeline

<svg viewBox="0 0 1140 232" width="1030" height="212" role="img" aria-label="Prepare, validate, commit pipeline with valid transactions applying writes and invalid transactions recording status only">
  <defs>
    <marker id="a6" markerWidth="8" markerHeight="8" refX="6.8" refY="4" orient="auto">
      <path d="M0,0.8 L6.8,4 L0,7.2" fill="none" stroke="#52514e" stroke-width="1.5"/>
    </marker>
  </defs>
  <rect x="6" y="76" width="150" height="60" rx="5" fill="#f2f1ec" stroke="#c3c2b7" stroke-width="1.5" stroke-dasharray="5 3"/>
  <text x="81" y="101" text-anchor="middle" font-size="14" fill="#52514e">Verified tx</text>
  <text x="81" y="120" text-anchor="middle" font-size="14" fill="#52514e">batch</text>
  <rect x="200" y="76" width="164" height="60" rx="5" fill="#fcfcfb" stroke="#eda100" stroke-width="2.5"/>
  <text x="282" y="100" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">1 · Prepare</text>
  <text x="282" y="122" text-anchor="middle" font-size="13" fill="#52514e">structure read/write sets</text>
  <rect x="408" y="76" width="164" height="60" rx="5" fill="#fcfcfb" stroke="#eda100" stroke-width="2.5"/>
  <text x="490" y="100" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">2 · Validate</text>
  <text x="490" y="122" text-anchor="middle" font-size="13" fill="#52514e">MVCC read-version check</text>
  <rect x="640" y="26" width="164" height="58" rx="5" fill="#fcfcfb" stroke="#eda100" stroke-width="2.5"/>
  <text x="722" y="50" text-anchor="middle" font-size="16" font-weight="600" fill="#0b0b0b">3 · Commit</text>
  <text x="722" y="71" text-anchor="middle" font-size="13" fill="#52514e">apply writes</text>
  <rect x="640" y="130" width="164" height="58" rx="5" fill="#fcfcfb" stroke="#c3c2b7" stroke-width="2"/>
  <text x="722" y="154" text-anchor="middle" font-size="15" font-weight="600" fill="#52514e">Status only</text>
  <text x="722" y="175" text-anchor="middle" font-size="13" fill="#52514e">skip state writes</text>
  <rect x="892" y="76" width="164" height="60" rx="5" fill="#fcfcfb" stroke="#008300" stroke-width="2.5"/>
  <text x="974" y="100" text-anchor="middle" font-size="15.5" font-weight="600" fill="#0b0b0b">State database</text>
  <text x="974" y="121" text-anchor="middle" font-size="13" fill="#52514e">+ final status</text>
  <line x1="158" y1="106" x2="196" y2="106" stroke="#52514e" stroke-width="1.6" marker-end="url(#a6)"/>
  <line x1="366" y1="106" x2="404" y2="106" stroke="#52514e" stroke-width="1.6" marker-end="url(#a6)"/>
  <path d="M574,96 C606,96 610,55 636,55" fill="none" stroke="#008300" stroke-width="1.8" marker-end="url(#a6)"/>
  <text x="592" y="34" font-size="13" font-weight="600" fill="#008300">valid</text>
  <path d="M574,118 C606,118 610,159 636,159" fill="none" stroke="#898781" stroke-width="1.8" marker-end="url(#a6)"/>
  <text x="588" y="192" font-size="13" font-weight="600" fill="#898781">invalid</text>
  <path d="M806,55 C852,55 848,96 888,96" fill="none" stroke="#52514e" stroke-width="1.6" marker-end="url(#a6)"/>
  <path d="M806,159 C852,159 848,118 888,118" fill="none" stroke="#52514e" stroke-width="1.6" marker-end="url(#a6)"/>
</svg>

<div class="keyline" style="border-left-color:var(--vc)">
<strong>One durable status path for both outcomes.</strong> Invalid transactions skip application-state writes but still get a final, queryable status — which is what makes recovery and client notification unambiguous.
</div>

<div class="src">Source: docs/validator-committer.md §1–2, §5 (Tasks 1–3)</div>

<!--
Notes: batches arriving here are already internally conflict-free — the Coordinator
guaranteed that. So the VC performs optimistic concurrency control per transaction without
worrying about its batch peers.

Transactions already marked invalid upstream (bad signature) arrive with that status
pre-set; the VC skips its own validation and just records the verdict.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Validator-Committer</span></div>

## VC — schema and stored procedures

<div class="two">
<div>

**System tables**

| Table | Holds |
|---|---|
| `tx_status` | Final status of every tx; `height` = block number + index, order-preserving |
| `ns__meta` | Per-namespace endorsement policy + version |
| `ns__config` | The config transaction (single row, key `_config`) |
| `metadata` | Internal K/V — currently last committed block number |

**User namespace tables** — one table per namespace:
`key` (PK), `value`, `version` for MVCC.

</div>
<div>

**Stored procedures** — minimise round trips

| Procedure | Purpose |
|---|---|
| `insert_tx_status` | Bulk status insert; returns violating `tx_id` on PK conflict |
| `validate_reads_ns_${NS}` | MVCC check for a batch; returns indices whose committed version differs |
| `update_ns_${NS}` | Update existing keys with new values + versions |
| `insert_ns_${NS}` | Insert new pairs; returns keys that violated the PK |

</div>
</div>

<div class="keyline" style="border-left-color:var(--vc)">
<code>${NS}</code> is substituted at runtime — each namespace is a separate data partition with its own procedures, so namespaces validate and commit independently.
</div>

<div class="src">Source: docs/validator-committer.md §4 (a, b, c)</div>

<!--
Notes: pushing validation into stored procedures is the key database-side
optimisation — one call validates a whole batch of reads instead of a round trip per key.
That is what makes 100k TPS plausible at all.

Note that the PK-violation returns are not merely error handling; they are how idempotency
is detected — see page 22.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Validator-Committer</span></div>

## VC — idempotency, the linchpin of recovery

<div class="keyline" style="border-left-color:var(--vc);font-size:20px">
Before storing a transaction's status, the VC checks whether a record matching
<strong>(<code>txID</code>, block number, transaction index)</strong> already exists. If it does, the service <strong>reuses the existing committed status</strong> instead of reprocessing.
</div>

<div class="two" style="margin-top:16px">
<div>

**Why this matters**

- The Coordinator re-dispatches work from a failed VC to a healthy replica — possibly work that already committed
- After a Coordinator restart, blocks may be re-fetched even though some transactions already committed
- Transactions can commit **across block boundaries**, so a re-fetched block is often partially committed

</div>
<div>

**What it buys**

- Re-dispatch is always safe — no double-commit, no corruption
- Recovery needs no distributed coordination or two-phase protocol
- Services can restart independently and converge
- Even simultaneous multi-service failure follows the same per-service procedure

</div>
</div>

<div class="src">Source: docs/architecture.md §5 (VC / Coordinator failure) · docs/validator-committer.md §7</div>

<!--
Notes: this is the single most important design idea in the system.
Idempotent commit is what turns a hard distributed-consensus problem into a simple
"retry until it sticks" model. Every recovery story in the next section reduces to it.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Query Service</span></div>

## Query Service — reads off the commit path

<div class="two-wide">
<div>

**View-based model** — begin a view, run many queries inside it, end the view. A view is a read-only database transaction.

| Isolation level | Consistent reads |
|---|---|
| Read Uncommitted | no |
| Read Committed | no |
| **Repeatable Read** | **yes** |
| **Serializable** | **yes** |

Views are additionally **deferred** or **non-deferred** — deferred checks conflicts at commit time, trading early error detection for performance.

</div>
<div class="card" style="border-left-color:var(--query)">
<h3>At a glance</h3>
<p><strong>State</strong> — stateless</p>
<p><strong>Instances</strong> — 2+, sized to endorser count</p>
<p><strong>Default port</strong> — <code>:7001</code></p>
<p><strong>API</strong> — <code>BeginView</code>, <code>EndView</code>, <code>GetRows</code>,
<code>GetNamespacePolicies</code>, <code>GetConfigTransaction</code></p>
<p><strong>Code</strong> — <code>service/query/</code></p>
</div>
</div>

<h3 style="margin-top:6px">Three optimisations</h3>

**View aggregation** — many client views collapse into one DB transaction ·
**Request aggregation** — batched by namespace and timing ·
**Lock-free** — concurrent maps, atomics, channel signalling

<div class="src">Source: docs/query-service.md §1–4</div>

<!--
Notes: "off the commit path" is the key property — this service is deliberately not co-located
with database nodes, precisely so query load does not perturb commit performance.

The honest caveat: only Repeatable Read and Serializable give consistent access. If an
endorser needs a stable snapshot it must ask for one of those; the weaker levels exist for
throughput when consistency is not required.
-->

---

<div class="eyebrow">3 · Components — <span class="lead-title">Database</span></div>

## Database cluster — the source of truth

<div class="two">
<div class="card" style="border-left-color:var(--db)">
<h3>YugabyteDB — recommended</h3>
<p>Distributed SQL built on PostgreSQL, designed for cloud-native deployment.</p>
<p>Automatic sharding and a distributed architecture deliver <strong>&gt;100,000 TPS</strong> on standard server hardware.</p>
<p>Consistency via <strong>Raft</strong>; shard replication survives node loss.</p>
</div>
<div class="card" style="border-left-color:var(--axis)">
<h3>PostgreSQL — supported</h3>
<p>Production-supported, but needs <strong>high-performance flash storage</strong> to be comparable.</p>
<p>Without flash it cannot match 100k+ TPS because of I/O limits.</p>
<p>Sensible where flash and PostgreSQL operational expertise already exist.</p>
</div>
</div>

<div class="keyline" style="border-left-color:var(--db)">
<strong>Stores:</strong> world state per namespace · final transaction statuses · namespace endorsement policies · system configuration. This makes the database the authoritative source of truth for the whole system.
</div>

<p style="font-size:18px;color:#52514e;margin-top:8px">
Typically <strong>6–9 nodes</strong>. VC instances connect to <em>all</em> nodes and perform client-side load balancing.
</p>

<div class="src">Source: docs/architecture.md §3.6 · docs/deployment-guide.md</div>

<!--
Notes: the database is almost always the throughput ceiling, so this is where sizing
conversations should focus. The PostgreSQL caveat is important to state plainly — it is
supported, but the 100k figure does not transfer without flash storage.

Catastrophic loss of the entire state database means rebuilding by reprocessing all blocks
from the ordering service. Shard replication covers single-node failure.
-->

---

<!-- _class: part -->

<div class="num">Section 4</div>

# Cross-cutting

<div class="sub">Concerns that span the services: durability, failure, the client-facing surface, and operations.</div>

<div style="max-width:780px">
<div class="row"><span>State management</span><span class="d"></span><span class="pg">26</span></div>
<div class="row"><span>Failure recovery — service by service</span><span class="d"></span><span class="pg">27</span></div>
<div class="row"><span>Integration surfaces</span><span class="d"></span><span class="pg">28</span></div>
<div class="row"><span>Observability</span><span class="d"></span><span class="pg">29</span></div>
<div class="row"><span>Deployment shape</span><span class="d"></span><span class="pg">30</span></div>
</div>

---

<div class="eyebrow">4 · Cross-cutting</div>

## State management

<div class="two">
<div>

<h3 style="color:var(--db)">Persistent — survives restart</h3>

**Sidecar block store** (local filesystem) Durable record of received blocks. Reconstructible from the ordering service, but kept local for fast queries and to reduce orderer load.

**Database cluster** World state, transaction statuses, namespace policies, system configuration. The authoritative source of truth.

</div>
<div>

<h3 style="color:var(--muted)">Transient — rebuilt on restart</h3>

**Coordinator** — dependency graph for in-flight blocks, plus pending-transaction tracking per Verifier and VC. Rebuilt as new blocks arrive.

**Verifier** — namespace policy cache; reloaded from the database.

**VC** — in-memory Prepare / Validate / Commit buffers; drained and refilled continuously in normal operation.

</div>
</div>

<div class="keyline">
Minimising persistent state is a deliberate choice: the less durable state exists, the less there is to reconcile after a failure.
</div>

<div class="src">Source: docs/architecture.md §4</div>

<!--
Notes: the design rule is that anything derivable is not persisted. The dependency
graph is the clearest case — it looks expensive to lose, but it is a pure function of
blocks the Sidecar still holds and statuses already in the database.
-->

---

<div class="eyebrow">4 · Cross-cutting</div>

## Failure recovery — service by service

| Failure | Recovery mechanism |
|---|---|
| <span class="chip" style="background:var(--verif)">Verifier</span> | Coordinator tracks per-instance in-flight txs and resubmits to a replica. Late responses for reassigned txs are **ignored**, preventing duplicate processing. |
| <span class="chip" style="background:var(--vc)">VC</span> | Coordinator resubmits pending txs to another replica. Safe because commit is **idempotent** — existing status is reused, never reprocessed. |
| <span class="chip" style="background:var(--coord)">Coordinator</span> | Sidecar periodically persists the last committed block number to the DB. On restart the Coordinator reads it and tells the Sidecar where to resume. Partially-committed blocks are handled by idempotency. |
| <span class="chip" style="background:var(--sidecar)">Sidecar</span> | On restart, gets the next expected block from the Coordinator and compares with its local store. Gaps are refilled from the orderer, statuses from the state DB. |
| <span class="chip" style="background:var(--query)">Query</span> | Stateless — reconnect and resume. Clients see a brief interruption and can retry another instance. |
| <span class="chip" style="background:var(--db)">Database</span> | Shard replication covers node loss. Total loss requires rebuilding state by reprocessing all blocks from the orderer. |

<div class="keyline">
<strong>Multiple simultaneous failures</strong> need no special handling — each service follows its own procedure and the system converges as instances return.
</div>

<div class="src">Source: docs/architecture.md §5</div>

<!--
Notes: the table above answers most resilience questions. Every row reduces to either "it was stateless" or
"idempotency made the retry safe". No two-phase commit anywhere in the system.
-->

---

<div class="eyebrow">4 · Cross-cutting</div>

## Integration surfaces

<div class="two">
<div class="card" style="border-left-color:var(--sidecar)">
<h3>Block delivery — Sidecar</h3>
<p>Registered clients receive committed blocks as a stream. Also serves historical blocks and transactions from the local block store.</p>
</div>
<div class="card" style="border-left-color:var(--sidecar)">
<h3>Notifications — Sidecar</h3>
<p><strong>By transaction ID</strong> — subscribe and be told when specific txs commit, abort or are rejected, with per-request timeouts.</p>
<p><strong>All-transactions stream</strong> — every committed tx in block order, with optional filtering.</p>
</div>
</div>

<div class="two" style="margin-top:16px">
<div class="card" style="border-left-color:var(--query)">
<h3>State queries — Query Service</h3>
<p>Key/value reads per namespace under an explicit isolation level, plus namespace policies and the config transaction. The path endorsers use.</p>
</div>
<div class="card" style="border-left-color:var(--db)">
<h3>Operations</h3>
<p>Prometheus metrics and <code>pprof</code> per service (e.g. Coordinator <code>:2119</code>, VC <code>:2116</code>, Verifier <code>:2115</code>). YAML config per service; TLS configurable throughout.</p>
</div>
</div>

<div class="keyline">
Rejection statuses are explicit and queryable — e.g. <code>REJECTED_DUPLICATE_TX_ID</code>,
<code>MALFORMED_BAD_ENVELOPE</code> — so a client always learns the outcome, even for a transaction that never entered the pipeline.
</div>

<div class="src">Source: docs/notification-service.md · docs/query-service.md §3 · docs/sidecar.md §5–6 · cmd/config/samples/</div>

<!--
Notes: this is the page an integrating team cares about most — it is the API
surface. Three things to build against: block delivery, notifications, state queries.
Everything else in the deck is internal detail they can ignore.
-->

---

<div class="eyebrow">4 · Cross-cutting</div>

## Observability

<div class="two-wide">
<div>

**Every pipeline stage is instrumented**

- Prometheus metrics exposed per service on a dedicated monitoring endpoint
- `pprof` profiling served alongside at `/debug/pprof/`
- **Queue-depth gauges** on the internal channels — included specifically to locate bottlenecks

**Reading the queue depths**

Because stages are joined by bounded channels, a full queue points at the *next* stage as the constraint. Walking the depths from block intake toward the database finds the limiting stage directly.

</div>
<div class="card" style="border-left-color:var(--db)">
<h3>Where to look</h3>
<p><strong>Metrics reference</strong><br><code>docs/metrics_reference.md</code><br>
regenerate with <code>make generate-metrics-doc</code></p>
<p><strong>Logging</strong><br><code>docs/logging.md</code></p>
<p><strong>Tuning</strong><br><code>docs/performance-tuning.md</code></p>
<p><strong>Load generation</strong><br><code>./bin/loadgen</code> · <code>docs/loadgen-artifacts.md</code></p>
</div>
</div>

<div class="src">Source: docs/index.md § Key Capabilities · docs/metrics_reference.md · cmd/config/samples/</div>

<!--
Notes: the queue-depth gauges are the practically useful part. They were added as a
diagnostic affordance, not an afterthought — with a pipeline this deep, "which stage is
slow" is otherwise genuinely hard to answer.

The load generator is a first-class tool in the repo, not a test fixture — it is how
performance claims get validated on specific hardware.
-->

---

<div class="eyebrow">4 · Cross-cutting</div>

## Deployment shape

<div class="two-wide">
<div>

| Component | Instances | Placement and sizing notes |
|---|---|---|
| Sidecar | 1 | 32 cores, 8 GB RAM, **NVMe SSD** — block store I/O is on the critical path |
| Coordinator | 1 | Stateless; 8 GB RAM is sufficient (in-flight state only) |
| Verifier | 2–3 | CPU-bound; scale on signature load |
| VC | 3–6 | **Co-located with DB nodes** — cuts MVCC round trips from ms to µs |
| Query Service | 2+ | Deliberately *not* co-located, to keep read load off the commit path |
| Database | 6–9 | 32 GB RAM for state caching; NVMe SSD |

Each service is a separate process with its own YAML config; the committer, load generator and mocks ship as separate binaries under `cmd/`.

</div>
<div class="card" style="border-left-color:var(--coord)">
<h3>Default ports</h3>
<p><code>:4001</code> Sidecar<br>
<code>:9001</code> Coordinator<br>
<code>:5001</code> Verifier<br>
<code>:6001</code> VC<br>
<code>:7001</code> Query</p>
<p style="margin-top:10px"><strong>Startup order</strong>, hardware sizing and reference topology:
<code>docs/deployment-guide.md</code></p>
</div>
</div>

<div class="src">Source: docs/architecture.md §3 · docs/deployment-guide.md · cmd/config/samples/*.yaml</div>

<!--
Notes: the two placement decisions are deliberate and belong together. VC
goes next to the database because it is chatty on the commit path. Query Service goes
elsewhere for exactly the same reason — to keep read load off the machines doing commits.

Ports listed are the sample-config defaults. Note that architecture.md's prose mentions
7055/7056 as illustrative examples; the sample configs are what a real deployment starts
from.

Operational gotcha relevant to dynamic scaling: the Coordinator's
connections to Verifier/VC instances retry with exponential backoff bounded by
`reconnect.max-elapsed-time`, default 15 minutes — after which it gives up. Deployments
that stop and start instances at runtime should set `reconnect.max-elapsed-time: 0s` to
retry indefinitely. See docs/deployment-guide.md §3.
-->

---

<!-- _class: part -->

<div class="num">Section 5</div>

# Reference

<div class="sub">Where to look in the source tree, the headline figures, and the authoritative docs.</div>

<div style="max-width:780px">
<div class="row"><span>Code map</span><span class="d"></span><span class="pg">32</span></div>
<div class="row"><span>Key figures and where to read more</span><span class="d"></span><span class="pg">33</span></div>
</div>

---

<div class="eyebrow">5 · Reference</div>

## Code map

<div class="two">
<div>

| Path | Contents |
|---|---|
| `service/sidecar/` | Block fetch, relay, block store, notifications |
| `service/coordinator/` | Orchestration, managers, policy manager |
| `service/coordinator/dependencygraph/` | **The DAG** |
| `service/verifier/` | Signature verification, parallel executor |
| `service/vc/` | Preparer, validator, committer, DB access |
| `service/query/` | View management, query batching |

</div>
<div>

| Path | Contents |
|---|---|
| `api/` | Protocol Buffer definitions (`make proto`) |
| `cmd/committer/` | Main service binary |
| `cmd/loadgen/` | Load generator |
| `cmd/mock/` | Mock services for testing |
| `cmd/config/samples/` | Reference YAML for every service |
| `loadgen/` | Load-generation framework |
| `integration/` | Integration tests |
| `docs/` | The documentation this overview is drawn from |

</div>
</div>

<div class="keyline">
Useful entry points: <code>service/coordinator/dependencygraph/</code> for the scheduling core ·
<code>service/vc/validator.go</code> and <code>committer.go</code> for the MVCC path ·
<code>service/sidecar/notify.go</code> for the lock-free event loop.
</div>

<div class="src">Source: repository layout · CLAUDE.md § Project Structure</div>

<!--
Notes: if someone wants to read exactly one package to understand what is novel
here, it is service/coordinator/dependencygraph. Everything else is competent
engineering; that package is the idea.
-->

---

<div class="eyebrow">5 · Reference</div>

## Key figures and where to read more

<div class="stat-row">
<div class="tile">
<div class="v">6</div>
<div class="l">Core services, gRPC-connected</div>
</div>
<div class="tile">
<div class="v">&gt;100<span class="u">k</span></div>
<div class="l">TPS documented on commodity hardware with YugabyteDB</div>
</div>
<div class="tile">
<div class="v">3</div>
<div class="l">Dependency types tracked: rw, wr, ww</div>
</div>
<div class="tile">
<div class="v">2</div>
<div class="l">Components holding persistent state</div>
</div>
</div>

<div class="two" style="margin-top:18px">
<div>

**Architecture & design**

- `docs/architecture.md` — components, state, recovery
- `docs/coordinator.md` — dependency graph, pipeline
- `docs/validator-committer.md` — MVCC, schema
- `docs/verification-service.md` — policies, parallelism
- `docs/sidecar.md` · `docs/query-service.md`

</div>
<div>

**Operations**

- `docs/deployment-guide.md` — sizing, topology, startup
- `docs/performance-tuning.md` — configuration parameters
- `docs/metrics_reference.md` — every metric
- `docs/tls-configurations.md` · `docs/logging.md`
- `docs/setup.md` — prerequisites, quick start

</div>
</div>

<p style="font-size:16px;color:#898781;margin-top:14px">
Per-service block diagrams (<code>docs/*-block-diagram.md</code>) go deeper than this overview for each component.
</p>

<div class="src">Source: docs/index.md · docs/architecture.md</div>

<!--
Notes: the >100k TPS figure is what our documentation states for YugabyteDB on
commodity hardware — quote it as a documented figure, and offer a load-generator run
against a specific hardware profile if someone wants a number they can hold us to.

For anything this overview does not cover, the per-service block-diagram docs are the next
level of detail, and they are considerably more thorough.
-->
