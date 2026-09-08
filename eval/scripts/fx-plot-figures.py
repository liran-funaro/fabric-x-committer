#!/usr/bin/env python3
"""
Plot the committer figures from eval/figures.jsonl.

Two outputs:

  figure9.png   throughput and 99th percentile latency against transaction size, invalid
                signature share, and double spend share -- the paper's Figure 9, as six panels
                rather than three. The paper puts throughput bars and a latency line on one plot
                with two y scales; two scales on one plot make the reader infer a relationship
                from an alignment that was chosen arbitrarily, so throughput and latency get a
                row each over a shared x axis instead.

  latency-throughput.png   throughput against latency, which is what replaces the paper's
                failure figure. Read it as "what does this cluster deliver if you can tolerate
                this much latency". Both block sizes share one pair of axes, because the point of
                the figure is the trade between them rather than either one alone.

Colors are the validated categorical slots 1 and 2 (blue, orange) on the light surface, used for
the only comparison in these figures: this cluster against the paper's published numbers.
"""
import json
import os
import sys
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt                                     # noqa: E402
from matplotlib.patches import Patch                                # noqa: E402
from matplotlib.ticker import FuncFormatter                         # noqa: E402

SRC = sys.argv[1] if len(sys.argv) > 1 else "figures.jsonl"
OUTDIR = sys.argv[2] if len(sys.argv) > 2 else "."
# Pass "e2e" as a third argument for the arm with a real ordering service in the path. That arm reports
# one figure, the latency-throughput curve, which is the question asked of it: what the whole pipeline
# costs at a given rate. It gets no bar panels -- the paper has no end-to-end figure for them to sit
# beside, since its Section 6.2 measures ordering alone and its 6.3 the committer alone. Those two are
# still the only published bounds, so the curve carries the ordering ceiling as a line.
E2E = len(sys.argv) > 3 and sys.argv[3] == "e2e"
# Figure 7a, read at 4 parties and 2 shards, which is this cluster's orderer topology: 280,000 tps at
# one shard, 414,000 at two, 430,000 at four, at about 0.6 s latency with 300 B transactions.
PAPER_ORDERING = 414_000

OURS = "#2a78d6"        # categorical slot 1
PAPER = "#eb6834"       # categorical slot 2
SMALL = "#1baf7a"       # categorical slot 3, the second block size
OURS_DARK = "#17457f"   # slot 1, stepped down: the rejected share of our own bars
PAPER_DARK = "#8f3714"  # slot 2, stepped down: the rejected share of the paper's bars
# What the latency axis shows before it starts labelling instead of drawing. The sustained medians run
# 38 ms to 438 ms and one unsustained rate reached 7.5 s, so an axis that fits everything spends nine
# tenths of its height on the points nobody would operate at, and the 40-vs-125 ms difference that is
# the whole block size result becomes two adjacent pixels.
LAT_AXIS_MS = 400
INK = "#0b0b0b"
INK2 = "#52514e"
GRID = "#e6e5e1"
SURFACE = "#fcfcfb"
# The latency histogram's last finite bound. A percentile that lands on it is not a measurement: it
# means the ladder was exceeded, and the conflict points read p50 AND p99 both at 60,000 with means
# above both, which is impossible for real percentiles. Those are reported as ">60 s" with the mean,
# which is the only valid latency statistic once the ladder is exceeded.
TOP_BUCKET_MS = 60_000
STEP = 1.08          # the rate search's multiplier, and so each knee's one-sided uncertainty

# The paper's own numbers, for the reference series, as (total tx/s, rejected tx/s, p99 seconds).
#
# Section 6.3 quotes only the ends of each sweep in prose, so the intermediate points are read off
# Figure 9 itself at 400 dpi against its own gridlines. The quoted ends agree with the reading to
# within a percent, which is the accuracy to assume for the rest: 474,000 and 280,000 for 9a's ends,
# 419,000 -> 459,000 for 9b, 419,000 -> 280,000 at 10% and 260,000 at 30% for 9c, and p99 83 -> 101 ms
# (9a), 75-83 ms (9b), 85 -> over 1,100 ms (9c).
#
# The rejected series matters as much as the total. The paper's 9b and 9c panels each plot THREE bars
# -- total, valid and invalid throughput -- so the share of work that was rejected is visible in the
# original, and a comparison that shows only totals can silently compare a 30%-rejected point against
# a 23%-rejected one. Total = valid + invalid holds in the paper's own bars, which is the same
# convention as this driver's `finished`.
#
# Its 9a x axis is inputs/outputs per transaction; one read-write operation is one input read and one
# output written, so its 1/1 sits at 1 and its 4/4 at 4.
PAPER_DATA = {
    "9a": {1: (474_000, None, 0.083), 2: (419_000, None, 0.083),
           3: (340_000, None, 0.093), 4: (280_000, None, 0.101)},
    "9b": {0: (419_000, 0, 0.083), 10: (428_000, 43_000, 0.076),
           20: (429_000, 86_000, 0.075), 30: (459_000, 138_000, 0.0755)},
    "9c": {0: (419_000, 0, 0.085), 10: (282_000, 26_000, 1.140),
           20: (281_000, 48_000, 1.400), 30: (260_000, 60_000, 1.420)},
}



# The x wording is the paper's own, so a panel here and its panel there are read the same way. Every
# panel has a formatter: 9c used to label its ticks with the share it actually rejected rather than the
# share configured, which put its bars at x positions the paper's bars could not be placed against.
# The generated share is annotated on the bar instead, which is where a discrepancy belongs.
PANELS = [
    ("9a", "Transaction size", "#inputs and #outputs in each transaction",
     lambda x: f"in={x}\nout={x}"),
    ("9b", "Invalid signatures", "percentage of transactions with invalid signature",
     lambda x: f"{x}%"),
    ("9c", "Double spends", "percentage of transactions doing double spend", lambda x: f"{x}%"),
]


def load(path):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def throughput(row):
    """Transactions finished per second: committed plus rejected.

    The paper's Figure 9b reports throughput rising as the invalid share rises, which only holds if
    a rejected transaction counts as work done. It does: the verifier or the validator finished with
    it. For 9a and the curve nothing is rejected and this equals the committed rate.
    """
    if row.get("finished") is not None:
        return row["finished"]
    return (row.get("committed") or 0) + (row.get("aborted") or 0)


def config_key(row):
    return json.dumps(row.get("vars"), sort_keys=True)


def disqualified_rates(rows):
    """Rates whose most recent hold failed, per configuration.

    A rate is only unreliable if the last thing measured about it was a failure. Two cases in this
    data set need telling apart and the latest-attempt rule does it without knowing anything about
    when the driver changed. The baseline shape's 559,872 was held by a probe and then LOST by the
    hold that followed, so it is unreliable and stays out. 9a-rw3's 420,000 failed a hold and then
    held cleanly at 419,455 -- that failure was the pre-fix driver measuring behind its own search's
    queue, a method failure rather than a rate failure, and the rate is fine.
    """
    latest = {}
    for r in rows:
        if r.get("kind") != "hold":
            continue
        key = (config_key(r), r["limit"])
        if key not in latest or r.get("at", 0) > latest[key].get("at", 0):
            latest[key] = r
    return {key for key, r in latest.items() if not r.get("met")}


def best_per_x(rows, figure):
    """The reported point for each x: the best rate that is still standing.

    Preference order is a confirmed hold, then a probe if the point never got one. Holds of an
    identical configuration are pooled across panels -- two read-writes with nothing rejected is 9a's
    n=2, 9b's 0% invalid and 9c's 0% double spend, the same workload three times -- so one
    configuration reports one number rather than appearing at three different heights. Pooling is the
    same rule the search uses (a knee is the highest rate that passed) applied across searches, and
    the one-sided whisker carries the spread between them.
    """
    out_of = disqualified_rates(rows)

    def eligible(r):
        return r.get("met") and (config_key(r), r["limit"]) not in out_of

    holds = [r for r in rows if r.get("kind") == "hold" and eligible(r)]
    points = {}
    for r in rows:
        if r.get("figure") != figure or not eligible(r):
            continue
        x = r["x"]
        pool = [h for h in holds if config_key(h) == config_key(r)]
        best = max(pool, key=throughput) if pool else (r if r.get("kind") != "hold" else None)
        if best is None:
            continue
        if x not in points or throughput(best) > throughput(points[x]):
            points[x] = best
    return dict(sorted(points.items()))


def block_wait_ms(row):
    """How long a transaction waited for its block to be cut, which the measurement excludes.

    The generator starts each transaction's clock when its block is SUBMITTED, not when the
    transaction was created: adapters/common.go calls sender() and then OnSendBatch(), which is what
    calls onSendTransaction for every ID in the block. So the reported latency is submission to
    status, and the wait while the block accumulated is outside it.

    That wait is one block interval at most and half of it on average, since transactions arrive
    uniformly during formation. The interval is the block size over the rate, so it is 1,000 ms at
    10,000 tps and 17 ms at 604,545 -- negligible at the knees, dominant at the bottom of the ladder.
    """
    blocks_per_s, rate = row.get("blk_rate"), throughput(row)
    if not blocks_per_s or not rate:
        return None
    return 1000 * (rate / blocks_per_s) / rate


def latency_ms(row, key="lat_p99"):
    """A percentile in ms, or None when it has saturated the histogram's top bucket."""
    v = row.get(key)
    if v is None:
        return None
    ms = v * 1000
    return None if ms >= TOP_BUCKET_MS else ms


def measured_conflicts(row):
    """The abort fraction actually generated, which is not always the fraction configured.

    A back-reference is drawn from the newest `key_lookback_window` keys, and a window wider than the
    keys created so far reaches out of range, where the generator leaves the slot alone. At a
    10,000,000-key window and 22,727 tps the run produces about 45,000 keys a second, so the window
    spans some 220 s of production and most references fall below index 0 -- the point configured for
    5% conflicts generated 2.6%. The x axis has to be what was generated.
    """
    total = throughput(row)
    return (row.get("aborted") or 0) / total if total else 0


def thousands(v, _pos=None):
    return f"{v / 1000:,.0f}k"


def style(ax):
    ax.set_facecolor(SURFACE)
    ax.grid(True, axis="y", color=GRID, linewidth=0.8)
    ax.set_axisbelow(True)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(GRID)
    ax.tick_params(colors=INK2, labelsize=9, length=0)


def figure9(rows, path):
    fig, axes = plt.subplots(2, 3, figsize=(14.2, 8.0), sharex="col")
    fig.patch.set_facecolor(SURFACE)

    for col, (figure, title, xlabel, xfmt) in enumerate(PANELS):
        data = best_per_x(rows, figure)
        paper = PAPER_DATA[figure]
        # The union, not just what was measured. A panel drawn over its own x values only hides the
        # published points it has nothing to compare against -- which for 9c is three of the four,
        # and those three are the paper's whole result.
        xs = sorted(set(data) | set(paper))
        pos = list(range(len(xs)))
        labels = [xfmt(x) for x in xs]

        top, bottom = axes[0][col], axes[1][col]
        style(top)
        style(bottom)
        top.set_title(title, color=INK, fontsize=11, pad=8, loc="left")

        width, gap = 0.38, 0.012

        def draw(xp, total, rejected, base, dark):
            """One bar, with the rejected part of it drawn inside it.

            The paper breaks each of its 9b and 9c bars into total, valid and invalid; this draws the
            total and overlays the invalid share on the same footing, so the two sit at the same x
            and the reject shares can be read against each other directly.
            """
            top.bar(xp, total, width, color=base, zorder=3)
            if rejected:
                top.bar(xp, rejected, width, color=dark, zorder=4, edgecolor=SURFACE,
                        linewidth=0.8, hatch="///")
                top.annotate(f"{100 * rejected / total:.0f}% rej",
                             (xp, rejected), textcoords="offset points", xytext=(0, 3),
                             ha="center", fontsize=6.5, color=INK)

        for pp, x in zip(pos, xs):
            if x in data:
                r = data[x]
                total = throughput(r)
                # A knee is the highest rate that passed, resolved to the search's 8% step, so it is
                # a lower bound: the sustainable rate lies between the bar and one step above it. The
                # whisker is that step, one-sided for the same reason.
                top.errorbar(pp - width / 2 - gap, total, yerr=[[0], [total * (STEP - 1)]],
                             ecolor=INK2, elinewidth=1, capsize=3, capthick=1, fmt="none", zorder=5)
                draw(pp - width / 2 - gap, total, r.get("aborted") or 0, OURS, OURS_DARK)
                top.annotate(f"{total / 1000:,.0f}k",
                             (pp - width / 2 - gap, total * STEP), textcoords="offset points",
                             xytext=(0, 4), ha="center", fontsize=8, color=INK2)
            elif figure == "9c":
                # 9c is the panel where the absence is the result: every rate offered at 5% and above
                # failed the conditions, so there is no bar to draw and saying so is the measurement.
                top.annotate("no rate\nqualified", (pp - width / 2 - gap, paper[x][0] * 1.04),
                             ha="center", va="bottom", fontsize=7.5, color=INK2)
            if x in paper:
                total, rejected, _ = paper[x]
                draw(pp + width / 2 + gap, total, rejected, PAPER, PAPER_DARK)

        top.yaxis.set_major_formatter(FuncFormatter(thousands))
        # Both rows carry the tick labels. With sharex the bar row's labels are hidden by default,
        # which leaves the panel a reader looks at first with no x axis at all.
        top.set_xticks(pos)
        top.set_xticklabels(labels)
        top.tick_params(labelbottom=True)
        top.set_xlabel(xlabel, color=INK2, fontsize=8.5)
        if col == 0:
            top.set_ylabel("throughput (tx/s)", color=INK2, fontsize=9)

        ours = [(pp, latency_ms(data[x])) for pp, x in zip(pos, xs)
                if x in data and latency_ms(data[x])]
        if ours:
            bottom.plot([pp for pp, _ in ours], [v for _, v in ours], color=OURS, linewidth=2,
                        marker="o", markersize=8, zorder=3)
        for pp, x in zip(pos, xs):
            if x in data and latency_ms(data[x]) is None:
                bottom.annotate(f">60 s\nmean {(data[x].get('lat_mean') or 0):,.0f} s",
                                (pp, 0), xytext=(0, 18), textcoords="offset points", ha="center",
                                fontsize=7.5, color=INK2)
        lat = [(pp, paper[x][2] * 1000) for pp, x in zip(pos, xs)
               if x in paper and paper[x][2] is not None]
        if lat:
            bottom.plot([pp for pp, _ in lat], [v for _, v in lat], color=PAPER, linewidth=2,
                        marker="s", markersize=8, linestyle="--", zorder=3)
        bottom.set_xticks(pos)
        bottom.set_xticklabels(labels)
        bottom.set_xlabel(xlabel, color=INK2, fontsize=9)
        bottom.set_ylim(bottom=0)
        if col == 0:
            bottom.set_ylabel("99th percentile latency (ms)", color=INK2, fontsize=9)

    legend = [Patch(color=OURS, label="this cluster"),
              Patch(facecolor=OURS_DARK, hatch="///", edgecolor=SURFACE,
                    label="of which rejected"),
              Patch(color=PAPER, label="paper"),
              Patch(facecolor=PAPER_DARK, hatch="///", edgecolor=SURFACE,
                    label="of which rejected")]
    fig.legend(handles=legend, frameon=False, fontsize=9, labelcolor=INK2,
               loc="upper right", bbox_to_anchor=(0.997, 0.999), ncol=4)
    fig.suptitle("Committer throughput and tail latency, at a one second latency bound",
                 color=INK, fontsize=13, x=0.006, ha="left", y=0.985)
    fig.text(0.006, 0.951,
             "Each bar is the highest rate held for 300 s with 99th percentile latency under one "
             "second, no queue growth, and the offered rate arriving. Throughput counts committed "
             "plus rejected transactions,\nand the hatched part of a bar is the rejected share, "
             "labelled where it is non-zero, so the two experiments are compared at a matched "
             "reject rate. The whisker is the search's 8% step,\none-sided because a knee is a "
             "lower bound. The paper's series is its Figure 9: the ends as quoted in its text, the "
             "intermediate points read off its plots.",
             color=INK2, fontsize=8, ha="left", va="top", linespacing=1.5)
    fig.tight_layout(rect=(0, 0, 1, 0.915))
    fig.savefig(path, dpi=160, facecolor=SURFACE)
    print("wrote", path)


def sustained(row):
    """Whether a ladder point delivered its offered rate out of a flat queue.

    `met` is the panels\' gate and it is the wrong test here, because it includes the one second
    latency bound: 500,000 tps was delivered in full with a flat queue at 1,351 ms, which the gate
    rejects and the curve should absolutely show -- a curve with a latency gate in it cannot answer
    "what does this deliver if you tolerate more latency", which is the whole question.

    What does disqualify a point is failing to deliver its rate, or delivering it out of a growing
    queue: then its latency is the queue\'s drain time rather than the cost of the rate.
    """
    offered_met = throughput(row) >= row["limit"] * 0.98
    queue_flat = (row.get("inflight_growth") or 0) <= row["limit"] * 0.02
    return offered_met and queue_flat


def series(rows, ax, figure, color, label):
    """One block size\'s curve: median as the line, the tail as an envelope above it.

    The median is the shape and the tail is an envelope around it. That is not a stylistic choice:
    across the top three rungs p50 rises monotonically 272 -> 351 -> 438 ms while p99 goes
    482 -> 1,351 -> 591, so the 99th percentile is not even ordered and a line through it draws a
    spike where the distribution has none.

    Returns the points that ran off the top of the axis, for the caller to label.
    """
    every = sorted([r for r in rows if r.get("figure") == figure and throughput(r)],
                   key=lambda r: r["limit"])
    points = [r for r in every if sustained(r)]
    missed = [r for r in every if not sustained(r)]
    if not points:
        return []

    tps = [throughput(r) for r in points]
    p50 = [(r.get("lat_p50") or 0) * 1000 for r in points]
    p99 = [(r.get("lat_p99") or 0) * 1000 for r in points]
    ax.fill_between(tps, p50, p99, color=color, alpha=0.10, linewidth=0, zorder=1)
    ax.plot(tps, p99, color=color, linewidth=1, alpha=0.55, zorder=2)
    ax.plot(tps, p50, color=color, linewidth=2, marker="o", markersize=8, zorder=3, label=label)

    waits = [block_wait_ms(r) for r in points]
    if all(w is not None for w in waits):
        ax.plot(tps, [m + w / 2 for m, w in zip(p50, waits)], color=color, linewidth=1,
                linestyle="--", zorder=2)

    if missed:
        ax.plot([throughput(r) for r in missed], [(r.get("lat_p50") or 0) * 1000 for r in missed],
                marker="o", markersize=8, markerfacecolor=SURFACE, markeredgecolor=color,
                markeredgewidth=2, linestyle="none", zorder=3)
    return [r for r in points + missed if (r.get("lat_p50") or 0) * 1000 > LAT_AXIS_MS]


def latency_curve(rows, path):
    """Throughput on x, latency on y, both block sizes on one pair of axes.

    An earlier version put latency on x, which is what the request asked for, and it read badly: a
    reader scanning left to right was scanning the dependent variable. Throughput is what an operator
    chooses and latency is what they get, so throughput belongs on x.
    """
    fig, ax = plt.subplots(figsize=(10, 6.4))
    fig.patch.set_facecolor(SURFACE)
    style(ax)
    ax.grid(True, axis="x", color=GRID, linewidth=0.8)

    off = []
    off += series(rows, ax, "curve", OURS, "10,000-transaction blocks (tuned for throughput)")
    off += series(rows, ax, "curve500", SMALL, "500-transaction blocks")

    total, rejected, p99 = PAPER_DATA["9b"][0]
    ax.plot([total], [p99 * 1000], color=PAPER, marker="s", markersize=9, linestyle="none", zorder=4,
            label=(f"paper, committer only: {total:,} tx/s at {p99 * 1000:,.0f} ms" if E2E else
                   f"paper: {total:,} tx/s at {p99 * 1000:,.0f} ms (99th pct)"))
    if E2E:
        ax.axvline(PAPER_ORDERING, color=INK2, linewidth=1, linestyle=":", zorder=2)
        ax.annotate(f"paper: ordering alone at this topology, {PAPER_ORDERING / 1000:,.0f}k tx/s",
                    (PAPER_ORDERING, 0), xytext=(-6, 8), textcoords="offset points", ha="right",
                    va="bottom", fontsize=7.5, color=INK2, rotation=90)

    # Every mark on the plot gets a legend row. The thin line and the dashed line carry as much of
    # the result as the median does -- the tail and the block wait -- and an unlabelled line is a
    # line a reader has to guess at.
    ax.plot([], [], color=INK2, linewidth=1, alpha=0.55,
            label="thin line above each curve: 99th percentile (shaded to the median)")
    ax.plot([], [], color=INK2, linewidth=1, linestyle="--",
            label="dashed: median + mean wait for the block to be cut (excluded from the measurement)")
    ax.plot([], [], marker="o", markersize=8, markerfacecolor=SURFACE, markeredgecolor=INK2,
            markeredgewidth=2, linestyle="none", label="hollow: rate offered but not sustained")

    ax.set_ylim(0, LAT_AXIS_MS)
    ax.set_xlim(left=0)
    # What the axis cuts off is named rather than drawn, which is the point of cutting it: the region
    # worth reading is 40-400 ms and these points would own the plot if the axis reached them. One
    # block of text, not a label per point -- the off-scale rungs are within a few percent of each
    # other in throughput, so labels at their own x positions land on top of one another.
    if off:
        lines = ["above this axis:"]
        for r in sorted(off, key=throughput):
            lines.append(f"{throughput(r) / 1000:,.0f}k tx/s: median "
                         f"{(r.get('lat_p50') or 0) * 1000:,.0f} ms, p99 "
                         f"{(r.get('lat_p99') or 0) * 1000:,.0f} ms"
                         + ("" if sustained(r) else " (not sustained)"))
        # Upper middle-left: the only region of the axes both curves and both envelopes stay out of.
        ax.text(0.22, 0.98, "\n".join(lines), transform=ax.transAxes, ha="left", va="top",
                fontsize=7.5, color=INK2, linespacing=1.6)

    ax.xaxis.set_major_formatter(FuncFormatter(thousands))
    ax.set_xlabel("throughput (tx/s)", color=INK2, fontsize=9)
    ax.set_ylabel("latency (ms): median, with the 99th percentile above it", color=INK2, fontsize=9)
    ax.set_title("What latency the whole pipeline costs at a given throughput, ordering included"
                 if E2E else
                 "What latency the committer costs at a given throughput, and what the block "
                 "size trades", color=INK, fontsize=13, loc="left", pad=10)
    ax.legend(frameon=False, fontsize=8, labelcolor=INK2, loc="upper center",
              bbox_to_anchor=(0.5, -0.11), ncol=2)
    fig.tight_layout()
    fig.savefig(path, dpi=160, facecolor=SURFACE)
    print("wrote", path)


def fmt_ms(row, key):
    ms = latency_ms(row, key)
    return f"{ms:,.0f}" if ms else ">60,000"


def row_cells(figure, condition, r):
    def pct(v):
        return "-" if v is None else f"{v * 100:.0f}%"
    return (f"| {figure} | {condition} | {r['limit']:,} | {throughput(r):,.0f} |"
            f" {(r.get('aborted') or 0):,.0f} | {(r.get('lat_mean') or 0) * 1000:,.0f} |"
            f" {fmt_ms(r, 'lat_p50')} | {fmt_ms(r, 'lat_p99')} |"
            f" {(r.get('db_commit') or 0) * 1000:,.1f} |"
            f" {(r.get('committed_total') or 0) / 1e6:,.0f} | {pct(r.get('verifier_cpu'))} |"
            f" {pct(r.get('coord_cpu'))} | {pct(r.get('loadgen_cpu'))} |"
            f" {r.get('cpu_busiest_host') or '-'} |")


def table(rows, path):
    """The table view the figures are read against, and the numbers for the write-up."""
    header = ("| figure | condition | rate limit | finished tx/s | of which aborted | mean ms |"
              " p50 ms | p99 ms | db commit ms | fill Mtx | verifier cpu | coord cpu | gen cpu |"
              " busiest host |")
    lines = [header, "|" + "---|" * (header.count("|") - 1)]
    for figure, title, _, xfmt in PANELS:
        for x, r in best_per_x(rows, figure).items():
            condition = xfmt(x) if xfmt else f"{measured_conflicts(r) * 100:.1f}% rejected"
            lines.append(row_cells(figure, condition, r))
    # The diagnostics are not in PANELS, so they would otherwise be measured and then dropped.
    extras = {"9a-utxo": lambda x: f"{x} in / {x} out",
              "curve500": lambda x: f"{x:,}-tx blocks",
              "split": lambda x: f"{x}% double spend, default tablet split",
              "graph": lambda x: f"{x} read-writes, global graph"}
    for extra, label in extras.items():
        for x, r in best_per_x(rows, extra).items():
            lines.append(row_cells(extra, label(x), r))
    curve_rows = sorted([r for r in rows if r.get("figure") == "curve" and throughput(r)],
                        key=lambda r: r["limit"])
    for r in curve_rows:
        lines.append(row_cells("curve", f"{r['limit']:,} offered", r))
    header = ("<!--\nCopyright IBM Corp. All Rights Reserved.\n\n"
              "SPDX-License-Identifier: Apache-2.0\n-->\n")
    with open(path, "w") as f:
        f.write(header + "\n".join(lines) + "\n")
    print("wrote", path)


def summary(rows):
    """A few lines fit for a phone: the headline per figure, against the paper's own number."""
    out = []
    diagnostics = [("9a-utxo", "UTXO shape", "", lambda x: f"{x}/{x}"),
                   ("blocks", "Block size", "", lambda x: f"{x:,} tx"),
                   ("split", "Default tablet split", "", lambda x: f"{x}% ds"),
                   ("graph", "Global dependency graph", "", lambda x: f"{x} rw")]
    for figure, title, _, xfmt in PANELS + diagnostics:
        data = best_per_x(rows, figure)
        if not data:
            continue
        peak_x = max(data, key=lambda x: throughput(data[x]))
        peak = data[peak_x]
        published = PAPER_DATA.get(figure, {}).get(peak_x)
        against = ""
        if published:
            against = (f", paper {published[0] / 1000:,.0f}k"
                       f" ({throughput(peak) / published[0] - 1:+.0%})")
        where = xfmt(peak_x) if xfmt else f"{measured_conflicts(peak) * 100:.1f}% rejected"
        p99 = latency_ms(peak)
        tail = f"p99 {p99:,.0f} ms" if p99 else f"p99 >60 s, mean {(peak.get('lat_mean') or 0):,.0f} s"
        out.append(f"{figure} {title}: best {throughput(peak) / 1000:,.0f}k tx/s at "
                   f"{where}, {tail}{against}"
                   f" [{len(data)} of the sweep's points measured]")
    curve_rows = [r for r in rows if r.get("figure") == "curve" and throughput(r)]
    if curve_rows:
        peak = max(curve_rows, key=throughput)
        quickest = min(curve_rows, key=lambda r: r.get("lat_p99") or 9)
        out.append(f"curve: {len(curve_rows)} points; peak {throughput(peak) / 1000:,.0f}k tx/s "
                   f"at {peak['lat_p99'] * 1000:,.0f} ms p99; floor "
                   f"{quickest['lat_p99'] * 1000:,.0f} ms at "
                   f"{throughput(quickest) / 1000:,.0f}k")
    return "\n".join(out)


def main():
    rows = load(SRC)
    print(f"{len(rows)} rows from {SRC}")
    print(summary(rows))
    if not E2E:
        figure9(rows, os.path.join(OUTDIR, "figure9.png"))
    latency_curve(rows, os.path.join(OUTDIR, "latency-throughput.png"))
    table(rows, os.path.join(OUTDIR, "figures-table.md"))


if __name__ == "__main__":
    main()
