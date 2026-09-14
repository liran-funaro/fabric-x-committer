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
from matplotlib.lines import Line2D                                 # noqa: E402
from matplotlib.patches import Patch                                # noqa: E402
from matplotlib.ticker import FuncFormatter, MultipleLocator        # noqa: E402

SRC = sys.argv[1] if len(sys.argv) > 1 else "figures.jsonl"
OUTDIR = sys.argv[2] if len(sys.argv) > 2 else "."
# Pass "e2e" as a third argument for the arm with a real ordering service in the path. That arm reports
# one figure, the latency-throughput curve, which is the question asked of it: what the whole pipeline
# costs at a given rate. It gets no bar panels -- the paper has no end-to-end figure for them to sit
# beside, since its Section 6.2 measures ordering alone and its 6.3 the committer alone. Those two are
# still the only published bounds, so the curve carries the ordering ceiling as a line.
# "e2e" draws the shard/storage ladders, "e2e-batch" the block-size ladders. Both are
# latency-against-throughput on the same arm, but six series on one pair of axes is unreadable and the
# two answer different questions, so they get a figure each.
MODE = sys.argv[3] if len(sys.argv) > 3 else ""
E2E = MODE.startswith("e2e")
# "conflict" draws the paired double-spend ladders: what the pipeline delivers against what it was
# offered, beside the graph population that explains it. Its own mode because it is the only figure here
# whose x axis is the OFFERED rate rather than the delivered one -- a cliff is invisible on a plot that
# uses delivered throughput for x, since both axes then collapse together.
CONFLICT = MODE == "conflict"
E2E_LADDER = "shape" if MODE == "e2e" else "curve"
# Figure 7a at four parties: 280,000 tps at one shard, 414,000 at two, 430,000 at four, at about 0.6 s
# latency with 300 B transactions. This arm runs FOUR shards -- sixteen batchers, two to a machine -- so
# the comparable ceiling is the four-shard one. It was 414,000 here while the arm was believed to be two
# shards, which would have drawn the wrong line on the figure.
PAPER_ORDERING = 430_000

OURS = "#2a78d6"        # categorical slot 1
PAPER = "#eb6834"       # categorical slot 2
SMALL = "#1baf7a"       # categorical slot 3, the second block size
SMALL_DARK = "#0f7050"  # slot 3, stepped down
OURS_DARK = "#17457f"   # slot 1, stepped down: the rejected share of our own bars
PAPER_DARK = "#8f3714"  # slot 2, stepped down: the rejected share of the paper's bars
# What the latency axis shows before it starts labelling instead of drawing. The sustained medians run
# 38 ms to 438 ms and one unsustained rate reached 7.5 s, so an axis that fits everything spends nine
# tenths of its height on the points nobody would operate at, and the 40-vs-125 ms difference that is
# the whole block size result becomes two adjacent pixels.
#
# The end-to-end arm needs a taller one. There the batchers cut the blocks and
# `BatchCreationTimeout` is 500 ms, so its median floor sits ABOVE 400 ms -- at that clip every rung
# is off-scale and the plot is empty. 1,000 ms keeps the same principle (show the operating region,
# label what is past it) against a floor set half a second higher.
# It is a FLOOR, and the SLO is the ceiling. The floor exists because clipping at a fixed height cut
# the highest-throughput point off the top -- the committer's best sustained rate, 531,455 tps at a
# 407 ms median, was drawn nowhere and mentioned only in the corner text, so the curve appeared to stop
# short. The ceiling exists because the opposite is just as unreadable: a rung that delivers its rate
# out of a flat queue at a 5,333 ms median is sustained by that test, and letting it size the axis put
# every rate anyone would run in the bottom twelfth. One second is the bound every reported point meets,
# so it is where the plot stops drawing and starts naming.
LAT_FLOOR_MS = 1000 if E2E else 400
SLO_CEILING_MS = 1000
if CONFLICT:
    # The conflict ladders need a different frame. Their whole point is a regime whose median is tens of
    # seconds, so a 1,000 ms cap would push most of one series into the corner note and leave the figure
    # showing only the series that behaved. 30 s contains both; the one-second bound is drawn as a line
    # instead, so what qualifies is still visible.
    LAT_FLOOR_MS = 30_000
    SLO_CEILING_MS = 30_000
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
# Its 9a x axis is inputs/outputs per transaction, and the earlier reading of that here was wrong. It
# assumed one read-write operation was one input read plus one output written, which put the paper's
# 1/1 at our x=1. It is not: 1 in and 1 out is a **two** read-write total, so the paper's N/N sits at
# our 2N. The mistake was flattering -- it compared our 1-key point against the paper's 2-key point
# all the way along, and turned a +9%/-35% split into a uniform +23% to +28%.
#
# Consequences of the correction: only our x=2 and x=4 have a published counterpart (the paper's 1/1
# and 2/2), our x=1 and x=3 have none, and the paper's 3/3 and 4/4 -- six and eight keys -- were never
# measured here. The panel shows that rather than hiding it behind a shared tick.
PAPER_DATA = {
    "9a": {2: (474_000, None, 0.083), 4: (419_000, None, 0.083),
           6: (340_000, None, 0.093), 8: (280_000, None, 0.101)},
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
    ("9a", "Transaction size", "read-write operations per transaction",
     lambda x: f"{x}"),
    ("9b", "Invalid signatures", "invalid signatures (%)",
     lambda x: f"{x}%"),
    # Formatted through int() where integral: the conflict-nosplit rows carry x as a float, and
    # "10.0%" beside "20.0%" is wide enough that this panel's tick labels overlap each other.
    ("9c", "Double spends", "double spends (%)", lambda x: f"{x:g}%"),
]


# The reference gap the setup documents, and the only one a conflict point may be measured at. A
# back-reference is a double spend only if the key it names is already committed; at a short gap the
# referent is still in flight, so the dependency graph holds the pair and serialises it and the conflict
# never reaches the insert path at all. That is a different mechanism wearing the same x axis, and it
# reads HIGHER, so a panel that takes the best row per x silently prefers it: 9c's 5% bar drew 26,116 tps
# from a gap-1,000 run over 21,273 from the documented one. Rows are dropped at load so both the reported
# points and the collapsed ones are filtered by the same rule.
REFERENCE_GAP = 300_000


def documented_gap(row):
    """True if this row's conflict share was measured at the documented reference gap.

    Rows with no conflicts are unaffected -- with nothing to back-reference the gap does not apply, and
    the earliest runs predate the parameter and record it as absent.
    """
    v = row.get("vars") or {}
    if not (v.get("loadgen_key_backref_rate") or 0):
        return True
    return v.get("loadgen_tx_reference_gap") == REFERENCE_GAP


def load(path):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    dropped = [r for r in rows if not documented_gap(r)]
    if dropped:
        gaps = sorted({(r.get("vars") or {}).get("loadgen_tx_reference_gap") for r in dropped})
        print(f"  dropped {len(dropped)} conflict rows measured at gap {gaps} "
              f"(documented gap is {REFERENCE_GAP:,})")
    return [r for r in rows if documented_gap(r)]


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
        # A hold that produced no measurement did not fail the rate -- it never tested it. The
        # 4 KiB size point was recorded that way, its volume having filled before the hold could run,
        # and treating it as a failure disqualified the rate and dropped the point off the figure
        # entirely while the text still cited its probe.
        if not throughput(r) or r.get("lat_p99") is None:
            continue
        key = (config_key(r), r["limit"])
        if key not in latest or r.get("at", 0) > latest[key].get("at", 0):
            latest[key] = r
    return {key for key, r in latest.items() if not r.get("met")}


def collapsed_per_x(rows, figure):
    """The best measurement per x among points that delivered their rate but missed the latency bound.

    `best_per_x` admits only `met` rows, which is right for a headline bar but hid the double-spend
    result: at the 120-way tablet split the 5% share was measured eleven times and every attempt blew
    the bound, so the blue series was one bar at 0% and the panel looked as though nothing had been
    run. The absence of a qualifying rate IS the result there, but a reader cannot check a result that
    is drawn nowhere.

    "Delivered" is the same test the latency curve uses -- offered rate arrived within 2% -- so a point
    that merely queued does not qualify as evidence of anything.

    Among those, the one with the LOWEST tail is kept, not the highest throughput. The claim being
    evidenced is that no rate qualified, so the fair witness is the configuration's best attempt at the
    bound it failed. At 5% double spends the highest-throughput attempt was 22,000 tps with the tail
    pinned at the 60 s measurement ceiling, which invites the reply that a gentler rate would have been
    fine; the lowest-tail attempt answers it, because backing off to 8,000 tps still left a 9.9 s tail.
    """
    best = {}
    for r in rows:
        if r.get("figure") != figure or r.get("met"):
            continue
        tps, p99 = throughput(r), r.get("lat_p99")
        if not tps or not p99 or tps < r["limit"] * 0.98:
            continue
        if r["x"] not in best or p99 < best[r["x"]]["lat_p99"]:
            best[r["x"]] = r
    return best


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


def save(fig, path):
    """Write the figure as both PDF and PNG.

    PDF is what the evaluation section includes: it is vector, so it survives being scaled into a column
    and its text stays selectable and searchable. PNG is what `README.md` embeds, because GitHub will not
    render a PDF inline in Markdown. Same figure, two containers -- so neither reader is served a
    resampled plot.
    """
    stem = os.path.splitext(path)[0]
    for ext in (".pdf", ".png"):
        fig.savefig(stem + ext, dpi=160, facecolor=SURFACE)
        print("wrote", stem + ext)


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
    """Three panels in a row, each with its latency overlaid on its bars.

    The paper's own Figure 9 puts throughput and latency in one panel per condition, and doing the
    same here halves the height: two stacked rows of three spent most of a page restating the same x
    axis three times over. Latency goes on a twin right axis, so a bar and the mark above it are the
    same measurement -- which is also why the marks carry each series' own colour rather than a
    separate latency colour.
    """
    fig, axes = plt.subplots(1, 3, figsize=(7.2, 2.9))
    fig.patch.set_facecolor(SURFACE)

    drew_collapsed = False
    drew_nosplit = False
    for col, (figure, title, xlabel, xfmt) in enumerate(PANELS):
        data = best_per_x(rows, figure)
        paper = PAPER_DATA[figure]
        # The union, not just what was measured. A panel drawn over its own x values only hides the
        # published points it has nothing to compare against -- which for 9c is three of the four,
        # and those three are the paper's whole result.
        # The double-spend panel carries a second measured series. At the 120-way tablet split every
        # conflict point collapses and no rate qualifies, so the panel would otherwise be one bar and
        # three absences; at the default split the same shapes hold. Two series make the panel say what
        # the measurement actually found, which is that the split is the difference.
        # The default-split series is gone from this panel. It was here to show that the same
        # conflict shapes hold at the other tablet split, but the paper used the same 120-way split
        # this deployment does, so the split was never the variable those runs treated it as -- and
        # every one of them was measured with the reference gap that made the workload a dependency
        # convoy rather than a double spend. What belongs here comes from the conflict analysis.
        # Shares that were measured at this split and collapsed. Drawn, because "no rate qualified" is
        # a claim the reader should be able to see the evidence for.
        weak = {x: r for x, r in collapsed_per_x(rows, figure).items()
                if x not in data} if figure == "9c" else {}
        # The second measured series, and the panel's actual finding: the same conflict shapes with
        # pre-splitting disabled. It is a separate series rather than more rows under figure="9c"
        # because best_per_x keeps the maximum throughput per x -- pooled, the no-split bar would
        # silently REPLACE the collapsed 120-way bar at the same share and the panel would show the
        # good number with the collapse erased. Both have to be visible; the contrast is the result.
        # Restricted to the shares the paper also ran. 5% exists only because the tablet sweep used
        # it, so it has no bar to be compared against and adding a fifth x squeezes the panel while
        # putting a 95k bar on an axis the paper's 600k sets -- the finding becomes invisible exactly
        # where the data is best. The 5% ladder is quoted in the text instead.
        nosplit = {x: r for x, r in best_per_x(rows, "conflict-nosplit").items()
                   if x in paper} if figure == "9c" else {}
        drew_nosplit = drew_nosplit or bool(nosplit)
        xs = sorted(set(data) | set(paper) | set(weak) | set(nosplit))
        pos = list(range(len(xs)))
        labels = [xfmt(x) for x in xs]

        top = axes[col]
        # The latency twin carries no grid of its own: two grids over one panel read as a moire.
        bottom = top.twinx()
        style(top)
        bottom.grid(False)
        bottom.set_facecolor("none")
        for side in ("top", "left"):
            bottom.spines[side].set_visible(False)
        bottom.spines["right"].set_color(GRID)
        bottom.tick_params(colors=INK2, labelsize=8, length=0)
        top.set_title(f"({'abc'[col]}) {title.lower()}", color=INK, fontsize=10, pad=6, loc="left")

        # Three slots only where a third series exists, so the other two panels keep the wider bars.
        # Order is the categorical order: slot 1 ours, slot 3 no-split, slot 2 the paper -- which also
        # puts this cluster's two configurations next to each other, since that pair is the comparison.
        if nosplit:
            width, gap = 0.26, 0.012
            offsets = (-width - gap, 0.0, width + gap)
        else:
            width, gap = 0.38, 0.012
            offsets = (-width / 2 - gap, None, width / 2 + gap)

        def draw(xp, total, rejected, base, dark, dy=3, value=False):
            """One bar, with the rejected part of it drawn inside it.

            The paper breaks each of its 9b and 9c bars into total, valid and invalid; this draws the
            total and overlays the invalid share on the same footing, so the two sit at the same x
            and the reject shares can be read against each other directly.

            The share is written beside the segment in 9c only. In 9b it is the x value, so labelling
            it put the same number -- "10%" -- beside two segments of visibly different height, one a
            tenth of 517,455 and the other a tenth of 428,000. The label read as a claim that the
            geometry then denied. 9c's two labels differ from each other and from x, so each is read
            against its own segment.
            """
            top.bar(xp, total, width, color=base, zorder=3)
            # The no-split slot sits at 2.74:1 against the surface, under the 3:1 floor, so it carries
            # a direct value label as its relief rather than relying on the fill being distinguishable.
            if value:
                top.annotate(f"{total / 1000:,.0f}k", (xp, total), textcoords="offset points",
                             xytext=(0, 2), ha="center", fontsize=6, color=INK, zorder=7)
            if rejected:
                top.bar(xp, rejected, width, color=dark, zorder=4, edgecolor=SURFACE,
                        linewidth=0.8, hatch="///")
                # zorder above the bars: at the default a neighbouring bar painted over the label,
                # which is what clipped "10%" to "10(".
                if figure == "9c":
                    top.annotate(f"{100 * rejected / total:.0f}%",
                                 (xp, rejected), textcoords="offset points", xytext=(0, dy),
                                 ha="center", fontsize=6, color=INK, zorder=7)

        for pp, x in zip(pos, xs):
            if x in data:
                r = data[x]
                total = throughput(r)
                # A knee is the highest rate that passed, resolved to the search's 8% step, so it is
                # a lower bound: the sustainable rate lies between the bar and one step above it. The
                # whisker is that step, one-sided for the same reason.
                top.errorbar(pp + offsets[0], total, yerr=[[0], [total * (STEP - 1)]],
                             ecolor=INK2, elinewidth=1, capsize=3, capthick=1, fmt="none", zorder=5)
                draw(pp + offsets[0], total, r.get("aborted") or 0, OURS, OURS_DARK)
            elif x in weak:
                # Measured and collapsed: an outline, because it is not a rate anyone would run, with
                # the tail that disqualified it named beside it. A filled bar here would read as a
                # result of the same standing as the others.
                r = weak[x]
                total = throughput(r)
                drew_collapsed = True
                top.bar(pp + offsets[0], total, width, facecolor="none", edgecolor=OURS,
                        linewidth=1.2, linestyle=":", zorder=3)
                tail = r.get("lat_p99") or 0
                # Stacked rather than run together: a one-line label here is wider than the panel's
                # own tick spacing and was clipped by the axis.
                top.annotate(f"{total / 1000:,.0f}k\n{tail:,.1f} s" if tail < 10 else
                             f"{total / 1000:,.0f}k\n{tail:,.0f} s",
                             (pp + offsets[0], total), textcoords="offset points", xytext=(0, 2),
                             ha="center", fontsize=5.5, color=INK2)
            elif figure == "9c":
                # Nothing to draw in this cluster's 120-way slot, and the two reasons are different
                # claims. "not run" means never attempted -- the higher shares were only run at the
                # other split, so saying "none" would assert a measurement that does not exist.
                # "no rate qualified" means attempted and never passed at any rate, which is the 5%
                # case: nineteen rows, none of which met the bound, and none of which even delivered
                # its offered rate, so collapsed_per_x rightly refuses to draw one as a bar.
                attempted = any(r.get("figure") == figure and r.get("x") == x for r in rows)
                # Anchored to whichever bar is present at this x, since a share the paper never ran
                # has no paper bar to sit above.
                anchor = (paper[x][0] if x in paper else
                          throughput(nosplit[x]) if x in nosplit else 0)
                top.annotate("no rate qualified" if attempted else "not run",
                             (pp + offsets[0], anchor * 1.06),
                             ha="center", va="bottom", fontsize=6.5, color=INK2, rotation=90)
            if x in nosplit:
                r = nosplit[x]
                total = throughput(r)
                # No whisker: these are fixed-rate holds, not a search, so the rate is what was offered
                # rather than a knee resolved to a step -- there is no bracketing interval to draw.
                draw(pp + offsets[1], total, r.get("aborted") or 0, SMALL, SMALL_DARK, value=True)
            if x in paper:
                total, rejected, _ = paper[x]
                draw(pp + offsets[2], total, rejected, PAPER, PAPER_DARK, dy=11)

        top.yaxis.set_major_formatter(FuncFormatter(thousands))
        # A tick every 100k on all three panels, so a bar in one is read against a bar in another
        # without counting gridlines: matplotlib's own choice was 200k here and 250k there.
        top.yaxis.set_major_locator(MultipleLocator(100_000))
        top.set_xticks(pos)
        top.set_xticklabels(labels)
        top.set_xlabel(xlabel, color=INK2, fontsize=8.5)
        # Room above the tallest bar for its value label and for the latency marks to clear it.
        top.set_ylim(top=1.22 * max([throughput(r) for r in data.values()] +
                                    [paper[x][0] for x in paper] +
                                    [throughput(r) for r in nosplit.values()] +
                                    [throughput(r) for r in weak.values()]))
        if col == 0:
            top.set_ylabel("throughput (tx/s)", color=INK2, fontsize=9)

        ours = [(pp, latency_ms(data[x])) for pp, x in zip(pos, xs)
                if x in data and latency_ms(data[x])]
        if ours:
            bottom.plot([pp for pp, _ in ours], [v for _, v in ours], color=OURS, linewidth=1.6,
                        marker="o", markersize=6, markerfacecolor=SURFACE, markeredgewidth=1.6,
                        linestyle="-", zorder=6)
        for pp, x in zip(pos, xs):
            if x in data and latency_ms(data[x]) is None:
                bottom.annotate(f"p99 >60 s\nmean {(data[x].get('lat_mean') or 0):,.0f} s",
                                (pp, 0), xytext=(0, 14), textcoords="offset points", ha="center",
                                fontsize=6, color=INK2)
        ns_lat = [(pp, latency_ms(nosplit[x])) for pp, x in zip(pos, xs)
                  if x in nosplit and latency_ms(nosplit[x])]
        if ns_lat:
            bottom.plot([pp for pp, _ in ns_lat], [v for _, v in ns_lat], color=SMALL, linewidth=1.6,
                        marker="^", markersize=6, markerfacecolor=SURFACE, markeredgewidth=1.6,
                        linestyle="-", zorder=6)
        lat = [(pp, paper[x][2] * 1000) for pp, x in zip(pos, xs)
               if x in paper and paper[x][2] is not None]
        if lat:
            bottom.plot([pp for pp, _ in lat], [v for _, v in lat], color=PAPER, linewidth=1.6,
                        marker="s", markersize=6, markerfacecolor=SURFACE, markeredgewidth=1.6,
                        linestyle="--", zorder=6)
        bottom.set_ylim(bottom=0)
        if col == len(PANELS) - 1:
            bottom.set_ylabel("p99 latency (ms)", color=INK2, fontsize=9)

    # The dotted outline is listed only when a panel drew one, for the same reason the curve figures
    # list their hollow marker conditionally: a legend row for a mark that appears nowhere sends the
    # reader hunting for it. A condition that collapsed without even delivering its offered rate is not
    # drawn at all, so this row comes and goes with the data.
    legend = [Patch(color=OURS, label="this cluster")]
    if drew_collapsed:
        legend.append(Patch(facecolor="none", edgecolor=OURS, linestyle=":",
                            label="measured, missed the latency bound"))
    if drew_nosplit:
        legend.append(Patch(color=SMALL, label="this cluster, pre-splitting off"))
    legend += [Patch(facecolor=OURS_DARK, hatch="///", edgecolor=SURFACE,
                    label="of which rejected"),
               Patch(color=PAPER, label="SIGMOD'26 paper"),
               Patch(facecolor=PAPER_DARK, hatch="///", edgecolor=SURFACE,
                     label="of which rejected"),
               Line2D([], [], color=INK2, marker="o", markersize=6, markerfacecolor=SURFACE,
                      markeredgewidth=1.6, linewidth=1.6,
                      label="p99 latency (right axis)")]
    fig.legend(handles=legend, frameon=False, fontsize=6.5, labelcolor=INK2,
               loc="upper center", bbox_to_anchor=(0.5, 1.005), ncol=3,
               columnspacing=1.2, handlelength=1.6, handletextpad=0.5)
    fig.tight_layout(rect=(0, 0, 1, 0.89), w_pad=2.4)
    save(fig, path)


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


def series_name(row):
    """What series a row belongs to.

    The figure name, except that both shard configurations record figure="shape" -- they are the
    same kind of measurement on different deployments -- so those are told apart by experiment id.
    """
    name = row.get("figure") or ""
    # The conflict ladders are two runs of one figure that differ only in a configured limit, so like the
    # shard ladders they are told apart by their label rather than by their figure name.
    if name not in ("shape", "conflict-ladder"):
        return name
    # Keyed on the label, not the experiment id, so a ladder extended with higher rungs under a new
    # id joins the same curve instead of drawing a second one. The label names the configuration,
    # which is what the series actually is.
    return row.get("label") or row.get("experiment") or name


def latency_axis_ms(rows, floor=None):
    """How tall the latency axis is: at least the floor, at most one second.

    The floor keeps the operating region legible -- at 400 ms the 40-vs-125 ms difference that is the
    whole block-size result stays readable. The ceiling is the SLO. A rung can deliver its offered rate
    out of a flat queue and still take seconds to do it, and letting one of those size the axis is what
    made this plot unreadable: a 5,333 ms median gave a 5,866 ms axis, on which every rate anyone would
    operate at sat in the bottom twelfth. Past a second is past the bound every reported point meets, so
    those rungs are named in the corner note instead of drawn.

    The 99th-percentile envelope may still leave the top, which is expected and not a defect. It is
    not monotonic in throughput (482 -> 1,351 -> 591 ms across the top three rungs), so fitting it
    would give an axis governed by one spike.
    """
    floor = LAT_FLOOR_MS if floor is None else floor
    medians = [(r.get("lat_p50") or 0) * 1000 for r in rows if sustained(r) and throughput(r)]
    return min(SLO_CEILING_MS, max(floor, 1.1 * max(medians, default=0)))


BATCH_SERIES = [("4 shards, one volume each", "10,000-transaction batches"),
                ("curve500", "500-transaction batches")]


def curve_series_names(rows):
    """The series this plot will draw, so the axis can be sized from them alone.

    Needed because `rows` is the whole results file: the 9a/9b/9c panels carry deliberate multi-second
    points -- a 30% double-spend rung sits at tens of seconds -- and sizing the axis over all of them
    produced a 60,000 ms axis with every curve flat against zero.
    """
    if not E2E:
        return {"curve", "curve500"}
    # Mirror the selection latency_curve() makes, or the axis is sized from a series that is not
    # drawn: including the 500-transaction batch ladder put a 91 ms point in the calculation and sent
    # the off-scale note to the wrong corner.
    if E2E_LADDER == "shape":
        return {series_name(r) for r in rows
                if r.get("kind") == "curve" and r.get("figure") == "shape"
                and not str(r.get("experiment") or "").startswith("e2e-fresh")}
    return {name for name, _ in BATCH_SERIES}


def series(rows, ax, figure, color, label, axis_ms):
    """One block size\'s curve: median as the line, the tail as an envelope above it.

    The median is the shape and the tail is an envelope around it. That is not a stylistic choice:
    across the top three rungs p50 rises monotonically 272 -> 351 -> 438 ms while p99 goes
    482 -> 1,351 -> 591, so the 99th percentile is not even ordered and a line through it draws a
    spike where the distribution has none.

    Returns the points that ran off the top of the axis, for the caller to label.
    """
    every = sorted([r for r in rows if series_name(r) == figure and throughput(r)],
                   key=lambda r: r["limit"])
    # One configuration per curve. A ladder is drawn as a single line across several experiment ids --
    # curve500, curve500hi and curve500top are one series -- so a configuration change that lands in only
    # some of them would otherwise be drawn as one curve made of two different systems. It happened with
    # `fast-block-prepare`: the rungs above 380,000 tps were re-measured with it while the ones below
    # were not, and nothing in the plot would have said so. Keep the newest configuration and say what
    # was dropped, so a half-finished re-measurement is visible rather than silent.
    configs = {}
    for r in every:
        configs.setdefault(config_key(r), []).append(r)
    if len(configs) > 1:
        newest = max(configs.values(), key=lambda g: max(r.get("at") or 0 for r in g))
        stale = sum(len(g) for g in configs.values()) - len(newest)
        print(f"  {figure}: {len(configs)} configurations; drawing the newest {len(newest)} rung(s) "
              f"and dropping {stale} measured under another")
        every = sorted(newest, key=lambda r: r["limit"])
    sustained_pts = [r for r in every if sustained(r)]
    missed = [r for r in every if not sustained(r)]
    # A sustained rung whose median is past the axis is named in the corner note, not drawn. Drawing it
    # means matplotlib clips the segment leading to it, which puts a vertical line up the top of the
    # plot at the highest throughput -- exactly where a reader looks for the ceiling.
    points = [r for r in sustained_pts if (r.get("lat_p50") or 0) * 1000 <= axis_ms]
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
    return [r for r in sustained_pts + missed if (r.get("lat_p50") or 0) * 1000 > axis_ms]


def latency_curve(rows, path):
    """Throughput on x, latency on y, both block sizes on one pair of axes.

    An earlier version put latency on x, which is what the request asked for, and it read badly: a
    reader scanning left to right was scanning the dependent variable. Throughput is what an operator
    chooses and latency is what they get, so throughput belongs on x.
    """
    fig, ax = plt.subplots(figsize=(7.2, 3.6))
    fig.patch.set_facecolor(SURFACE)
    style(ax)
    ax.grid(True, axis="x", color=GRID, linewidth=0.8)
    # Computed once from the series on this plot, so the tallest sustained median sets the height and
    # no curve is cut short of the throughput it reached.
    drawn = curve_series_names(rows)
    axis_ms = latency_axis_ms([r for r in rows if series_name(r) in drawn])


    off = []
    if E2E:
        # On this arm the batchers cut the blocks, so a ladder's series is whatever
        # `armageddon_batch_max_message_count` it ran at -- there is no fixed pair of block sizes to
        # hard-code, and the driver may add ladders as configurations are tried. Take the series from the
        # data and their names from the rows, so a new ladder appears without editing this file.
        # Only the shard/storage ladders belong on this figure -- that is what its caption compares.
        # The block-size ladders (500-transaction batches) are a different configuration and are
        # reported in prose; they used to be picked up here by accident and then dropped just as
        # silently, because zip() against a four-colour tuple truncated the fifth series.
        ladders = []
        for r in rows:
            # Every ladder point records kind="curve", whichever variable the ladder sweeps -- the
            # shard/storage comparison and the block-size comparison are both
            # latency-against-throughput series measured identically, and both belong on this plot.
            # Matching on the name instead is what broke: a series called "e2e-shape-4s" starts
            # with neither "curve" nor "shape".
            if r.get("kind") != "curve" or r.get("figure") != "shape":
                continue
            if str(r.get("experiment") or "").startswith("e2e-fresh"):
                continue
            name = series_name(r)
            if name not in [n for n, _ in ladders]:
                ladders.append((name, r.get("label") or name))
        if E2E_LADDER != "shape":
            ladders = [(n, l) for n, l in BATCH_SERIES
                       if any(series_name(r) == n for r in rows)]
        for (name, label), colour in zip(sorted(ladders), (OURS, SMALL, PAPER_DARK, OURS_DARK)):
            off += series(rows, ax, name, colour, label, axis_ms)
    elif MODE == "conflict":
        # Two ladders of the same conflict workload at two admission limits. Ordered so the larger limit
        # is drawn first and in the primary colour: it is the claim, and the other is the control.
        names = sorted({series_name(r) for r in rows if r.get("figure") == "conflict-ladder"},
                       key=lambda n: "5M" not in n)
        for name, colour in zip(names, (OURS, SMALL)):
            off += series(rows, ax, name, colour, name, axis_ms)
    else:
        off += series(rows, ax, "curve", OURS, "10,000-tx blocks", axis_ms)
        off += series(rows, ax, "curve500", SMALL, "500-tx blocks", axis_ms)

    total, rejected, p99 = PAPER_DATA["9b"][0]
    ax.plot([total], [p99 * 1000], color=PAPER, marker="s", markersize=9, linestyle="none", zorder=4,
            label=(f"paper, committer only: {total:,} tx/s at {p99 * 1000:,.0f} ms" if E2E else
                   f"paper: {total:,} tx/s at {p99 * 1000:,.0f} ms"))
    if E2E:
        ax.axvline(PAPER_ORDERING, color=INK2, linewidth=1, linestyle=":", zorder=2)
        ax.annotate(f"paper: ordering alone at this topology, {PAPER_ORDERING / 1000:,.0f}k tx/s",
                    (PAPER_ORDERING, 0), xytext=(-4, 6), textcoords="offset points", ha="right",
                    va="bottom", fontsize=7.5, color=INK2, rotation=90)

    # Every mark on the plot gets a legend row. The thin line and the dashed line carry as much of
    # the result as the median does -- the tail and the block wait -- and an unlabelled line is a
    # line a reader has to guess at.
    ax.plot([], [], color=INK2, linewidth=1, alpha=0.55,
            label="99th percentile (shaded to median)")
    ax.plot([], [], color=INK2, linewidth=1, linestyle="--",
            label="median + wait for the block to be cut")
    # Only when one is actually on the plot. A rate that was not sustained is almost always far above
    # a latency axis sized to the sustained region, so it lands in the corner note instead -- and a
    # legend row for a mark that appears nowhere sends the reader hunting for it.
    if any(not sustained(r) and throughput(r) and 0 < (r.get("lat_p50") or 0) * 1000 <= axis_ms
           for r in rows if series_name(r) in drawn):
        ax.plot([], [], marker="o", markersize=8, markerfacecolor=SURFACE, markeredgecolor=INK2,
                markeredgewidth=2, linestyle="none", label="offered but not sustained")

    ax.set_ylim(0, axis_ms)
    ax.set_xlim(left=0)
    if CONFLICT:
        # What a reported point has to meet, drawn because this figure's axis reaches far past it.
        ax.axhline(1000, color=INK2, linewidth=1, linestyle=":", zorder=2)
        ax.annotate("one-second bound", (0.01, 1000), xycoords=("axes fraction", "data"),
                    textcoords="offset points", xytext=(0, 4), fontsize=7, color=INK2)
    # What the axis cuts off is named rather than drawn, which is the point of cutting it: the region
    # worth reading is 40-400 ms and these points would own the plot if the axis reached them. One
    # block of text, not a label per point -- the off-scale rungs are within a few percent of each
    # other in throughput, so labels at their own x positions land on top of one another.
    if off:
        # Two kinds of point sit above the axis and the note must not conflate them: rates that were
        # offered and not delivered, and rates that were delivered out of a flat queue but took longer
        # than the one-second bound to do it. Naming them by what they have in common -- a median past
        # the axis -- is accurate for both. Per-point detail is in figures-table.md.
        #
        # The rates as OFFERED, not as delivered. A rung that collapsed delivered almost nothing --
        # 773 tps at one point -- so a range built from delivered throughput read "1k-533k tx/s" for
        # rates that were 300k and 560k.
        lo, hi = min(r["limit"] for r in off), max(r["limit"] for r in off)
        note = (f"above this axis: {len(off)} offered rate{'s' if len(off) > 1 else ''} with a median "
                f"past {axis_ms / 1000:,.0f} s, {lo / 1000:,.0f}k-{hi / 1000:,.0f}k tx/s")
        # Which bottom corner is free depends on the plot, so measure rather than hardcode. The
        # published reference marker sits at a fixed throughput in the right half of both plots, and
        # the committer's small-block curve runs along the bottom left, so neither corner is reliably
        # empty: on the committer arm the left is occupied by a curve at 40 ms, and end to end the
        # right is occupied by a marker at 83 ms on a 1,000 ms axis.
        drawn_pts = [r for r in rows if series_name(r) in drawn and sustained(r) and throughput(r)]
        mid = max(throughput(r) for r in drawn_pts) / 2
        # Clearance in each bottom corner, as a fraction of the axis. The published marker sits in the
        # right half at a fixed latency, so it occupies that corner whenever the axis is tall enough to
        # push it down -- which is why the batch-size figure has neither bottom corner free: a curve at
        # 90 ms on the left and the marker at 83 ms on the right.
        def clearance(half):
            lows = [(r.get("lat_p50") or 0) * 1000 for r in drawn_pts
                    if (throughput(r) < mid) == (half == "left")]
            return min(lows, default=axis_ms) / axis_ms
        # Two different obstacles with two different thresholds. A curve has to clear the note's whole
        # height, but the marker only blocks it by sitting inside it: the note occupies roughly the
        # bottom eighth, so a marker at 0.17 of the axis passes above it while one at 0.08 does not.
        marker_frac = (PAPER_DATA["9b"][0][2] * 1000) / axis_ms
        # One line at 6.5pt sits between about 0.05 and 0.09 of the axis, so an obstacle needs to clear
        # roughly 0.13 -- not 0.18, which rejected a corner the note fits under with room to spare.
        if clearance("left") > 0.13:
            xy, ha, va = (0.015, 0.05), "left", "bottom"
        elif clearance("right") > 0.13 and marker_frac > 0.14:
            xy, ha, va = (0.985, 0.05), "right", "bottom"
        else:
            # Both corners taken; go above the curves, which are at their lowest on the right.
            xy, ha, va = (0.985, 0.97), "right", "top"
        ax.text(xy[0], xy[1], note, transform=ax.transAxes, ha=ha, va=va,
                fontsize=6.5, color=INK2)

    ax.xaxis.set_major_formatter(FuncFormatter(thousands))
    ax.set_xlabel("throughput (tx/s)", color=INK2, fontsize=9)
    ax.set_ylabel("latency (ms)", color=INK2, fontsize=9)
    ax.legend(frameon=False, fontsize=7, labelcolor=INK2, loc="upper center",
              bbox_to_anchor=(0.5, -0.16), ncol=3, columnspacing=1.2, handlelength=1.6,
              handletextpad=0.5)
    fig.tight_layout()
    save(fig, path)


# The published ordering-service line, Figure 7b at four parties and TWO shards, read off the plot at
# 400 dpi: bytes -> tps. Every point is within 10% of 120 MB/s, so past 300 bytes that service is
# bandwidth-bound and the rate is simply bytes per second divided by transaction size.
#
# This arm runs four shards, so the absolute values are not comparable and only the SHAPE is -- whether
# throughput falls as 1/size, meaning a fixed byte rate, or falls more slowly. The figure says so, because
# a figure gets read on its own.
PAPER_7B = {128: 596_000, 256: 496_000, 300: 413_000, 512: 245_000,
            1024: 123_000, 2048: 55_000, 3500: 35_000, 4096: 27_000}


def size_curve(rows, path):
    """Throughput against transaction size, ours against the published ordering-only line.

    Plotted as bytes per second on a second axis as well as transactions per second, because the
    published line is flat in the former and steep in the latter -- and which of those this arm is flat in
    is the actual result.
    """
    data = best_per_x(rows, "size")
    if not data:
        print("no transaction-size data yet")
        return
    fig, ax = plt.subplots(figsize=(7.2, 3.4))
    fig.patch.set_facecolor(SURFACE)
    style(ax)
    ax.grid(True, axis="x", color=GRID, linewidth=0.8)

    px = sorted(PAPER_7B)
    ax.plot(px, [PAPER_7B[x] for x in px], color=PAPER, linewidth=2, marker="s", markersize=7,
            linestyle="--", label="paper, ordering only (Fig. 7b, 4 parties, two shards)")
    xs = sorted(data)
    ax.plot(xs, [throughput(data[x]) for x in xs], color=OURS, linewidth=2, marker="o", markersize=8,
            label="this cluster, end to end")
    for x in xs:
        r = data[x]
        ax.annotate(f"{throughput(r) / 1000:,.0f}k\n{throughput(r) * x / 1e6:,.0f} MB/s",
                    (x, throughput(r)), textcoords="offset points", xytext=(0, 10), ha="center",
                    fontsize=7.5, color=INK2)

    ax.set_xscale("log", base=2)
    ticks = [128, 300, 512, 1024, 2048, 4096]
    ax.set_xticks(ticks)
    ax.set_xticklabels([f"{t}" for t in ticks])
    ax.minorticks_off()
    ax.yaxis.set_major_formatter(FuncFormatter(thousands))
    ax.set_xlabel("transaction size (bytes, log scale)", color=INK2, fontsize=9)
    ax.set_ylabel("throughput (tx/s)", color=INK2, fontsize=9)
    ax.legend(frameon=False, fontsize=8, labelcolor=INK2, loc="upper right")
    fig.tight_layout()
    save(fig, path)


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


# The measured median width of a database insert batch, over 14,682 samples of the collapse: 152
# transactions, which at two read-writes is ~304 keys. Used only for points measured before the driver
# recorded the width per row, and those are drawn hollow so an assumed x is never mistaken for a measured
# one. The width matters because it is half of the product that crosses YugabyteDB's threshold, and it is
# not the coordinator's chunk size -- the validator-committer re-batches, so a 500-transaction chunk
# arrives as ~150.
ASSUMED_KEYS_PER_INSERT = 304
BATCHING_THRESHOLD = 32_768


def conflict_threshold(rows, path):
    """Throughput and per-transaction CPU against tablets x keys per insert.

    The panel that makes the mechanism visible without prose. YugabyteDB batches a multi-key lookup per
    tablet only while tablets x keys stays under about 32,768, and issues one storage read per key above
    it. If that threshold is what the conflict workload runs into, throughput falls and CPU per transaction
    rises as the product crosses it -- and the crossing is a property of the product, not of the tablet
    count, which is why x is the product. Plotting against tablets alone hid this: at ~304 keys an insert,
    8, 16, 32 and 64 tablets all sit on the same side of the threshold.

    CPU per transaction is the discriminator between a cost and a queue. A queue cannot make a transaction
    cost twenty times the CPU; only work can.
    """
    # One variable. Every experiment that also moved the graph limit, the chunk size, the sidecar window or
    # the manager sits at the same x as the plain 120-tablet point, and drawing them together would put a
    # column of unrelated configurations where the tablet comparison belongs -- their spread would read as
    # scatter in the tablet effect. Those runs answered other questions and are reported elsewhere.
    CONFOUNDS = ("committer_coordinator_dep_graph_wait_tx_limit",
                 "committer_coordinator_dep_graph_chunk_size",
                 "committer_coordinator_dep_graph_use_simple_manager",
                 "committer_sidecar_waiting_txs_limit")
    pts = []
    for r in rows:
        v = r.get("vars") or {}
        if not v.get("loadgen_key_backref_rate"):
            continue
        if any(k in v for k in CONFOUNDS):
            continue
        tps = throughput(r)
        if not tps:
            continue
        tablets = v.get("committer_database_table_pre_split_tablets", 120)
        measured = r.get("tx_per_insert")
        keys = 2 * measured if measured else ASSUMED_KEYS_PER_INSERT
        cpu_us = (r.get("cpu_max") or 0) * 64 / tps * 1e6
        pts.append(dict(x=tablets * keys, tps=tps, cpu=cpu_us, tablets=tablets,
                        measured=bool(measured)))
    if not pts:
        print("no conflict rows with a tablet count yet")
        return

    fig, ax = plt.subplots(figsize=(7.2, 3.4))
    fig.patch.set_facecolor(SURFACE)
    style(ax)
    bx = ax.twinx()
    bx.grid(False)
    bx.set_facecolor("none")
    for side in ("top", "left"):
        bx.spines[side].set_visible(False)
    bx.spines["right"].set_color(GRID)
    bx.tick_params(colors=INK2, labelsize=8, length=0)

    ax.axvline(BATCHING_THRESHOLD, color=INK2, linewidth=1, linestyle=":", zorder=2)
    ax.annotate(f"batching threshold, {BATCHING_THRESHOLD:,} keys x tablets",
                (BATCHING_THRESHOLD, 0.97), xycoords=("data", "axes fraction"),
                textcoords="offset points", xytext=(-5, 0), rotation=90, ha="right", va="top",
                fontsize=7, color=INK2)

    pts.sort(key=lambda p: p["x"])
    for p in pts:
        face = OURS if p["measured"] else SURFACE
        ax.plot([p["x"]], [p["tps"]], marker="o", markersize=9, color=OURS, markerfacecolor=face,
                markeredgewidth=2, linestyle="none", zorder=4)
        bx.plot([p["x"]], [p["cpu"]], marker="s", markersize=8, color=PAPER,
                markerfacecolor=face if p["measured"] else SURFACE, markeredgewidth=2,
                linestyle="none", zorder=4)
        ax.annotate(f"{p['tablets']:g}", (p["x"], p["tps"]), textcoords="offset points",
                    xytext=(0, 11), ha="center", fontsize=7, color=INK2)
    ax.plot([p["x"] for p in pts], [p["tps"] for p in pts], color=OURS, linewidth=1.5, zorder=3)
    bx.plot([p["x"] for p in pts], [p["cpu"] for p in pts], color=PAPER, linewidth=1.5,
            linestyle="--", zorder=3)

    ax.set_xscale("log")
    ax.set_yscale("log")
    bx.set_yscale("log")
    ax.set_xlabel("tablets x keys per insert (log scale)", color=INK2, fontsize=9)
    ax.set_ylabel("throughput (tx/s)", color=INK2, fontsize=9)
    bx.set_ylabel("CPU per transaction (us)", color=INK2, fontsize=9)
    ax.yaxis.set_major_formatter(FuncFormatter(thousands))
    legend = [Line2D([], [], color=OURS, marker="o", markersize=9, linewidth=1.5,
                     label="throughput (left)"),
              Line2D([], [], color=PAPER, marker="s", markersize=8, linewidth=1.5, linestyle="--",
                     label="CPU per transaction (right)"),
              Line2D([], [], color=INK2, marker="o", markersize=9, markerfacecolor=SURFACE,
                     markeredgewidth=2, linestyle="none",
                     label=f"hollow: insert width assumed at {ASSUMED_KEYS_PER_INSERT} keys, not recorded")]
    fig.legend(handles=legend, frameon=False, fontsize=7.5, labelcolor=INK2, loc="upper center",
               bbox_to_anchor=(0.5, 1.03), ncol=2)
    fig.tight_layout(rect=(0, 0, 1, 0.87))
    save(fig, path)


def conflict_ladders(rows, path):
    """The two double-spend ladders: delivered against offered, and the graph population beside it.

    Two panels because the result is a claim about cause. The left panel shows what the pipeline delivers
    as the offered rate rises; the right shows how many transactions the dependency graph is holding at
    the same rungs. A cliff appears as the left curve leaving the diagonal exactly where the right curve
    reaches its limit, and the paired ladders differ only in that limit -- so the two panels together say
    "throughput stopped because admission stopped", which neither says alone.

    The graph population is read from the sidecar's waiting-transaction gauge, which tracks it one for one:
    both count transactions handed to the coordinator and not yet finished, and a cross-tier sample showed
    them equal to within 500 out of 500,000.
    """
    ladders = {}
    for r in rows:
        if r.get("figure") != "conflict-ladder" or r.get("kind") != "curve":
            continue
        ladders.setdefault(r.get("label") or r.get("experiment"), []).append(r)
    if not ladders:
        print("no conflict-ladder data yet")
        return

    fig, (ax, bx) = plt.subplots(1, 2, figsize=(7.2, 3.2))
    fig.patch.set_facecolor(SURFACE)
    for a in (ax, bx):
        style(a)
        a.grid(True, axis="x", color=GRID, linewidth=0.8)

    # The diagonal is what a pipeline that keeps up looks like, so the eye needs no legend entry to read
    # the gap: every point below it is work that was offered and not delivered.
    top = max(r["limit"] for rs in ladders.values() for r in rs)
    ax.plot([0, top], [0, top], color=INK2, linewidth=1, linestyle=":", zorder=1)
    ax.annotate("delivered = offered", (top, top), textcoords="offset points", xytext=(-4, -12),
                ha="right", fontsize=7, color=INK2)

    for (label, rs), colour in zip(sorted(ladders.items()), (OURS, SMALL, PAPER_DARK)):
        rs = sorted(rs, key=lambda r: r["limit"])
        xs = [r["limit"] for r in rs]
        # The committers' own counter, not the generator's rate. Behind a queue this deep the two disagree
        # by a third: the generator measures status arrivals in a sixty-second window with millions of
        # transactions queued ahead of them, so its variance is the queue's while its mean is sound. Both
        # are drawn -- the committers' solid, the generator's faint -- because the gap between them is
        # itself the reason the figure quotes one and not the other.
        ax.plot(xs, [r.get("vc_commit") or throughput(r) or 0 for r in rs], color=colour, linewidth=2,
                marker="o", markersize=7, zorder=3, label=label)
        # The generator's own rate is NOT drawn beside it. Against an axis that reaches the offered rate,
        # 20,300 and the generator's 18,000-25,273 are the same pixel, so the line added clutter rather
        # than the contrast it was meant to show. That belongs in prose, where the numbers can be read.
        held = [(r["limit"], r.get("sc_waiting")) for r in rs if r.get("sc_waiting")]
        if held:
            # No label: the figure legend collects handles from both axes, and labelling the same series
            # twice listed every ladder twice.
            bx.plot([x for x, _ in held], [h for _, h in held], color=colour, linewidth=2,
                    marker="o", markersize=7, zorder=3)

    ax.set_xlabel("offered rate (tx/s)", color=INK2, fontsize=9)
    ax.set_ylabel("committed (tx/s)", color=INK2, fontsize=9)
    bx.set_xlabel("offered rate (tx/s)", color=INK2, fontsize=9)
    bx.set_ylabel("transactions held by the graph", color=INK2, fontsize=9)
    for a in (ax, bx):
        a.xaxis.set_major_formatter(FuncFormatter(thousands))
        a.yaxis.set_major_formatter(FuncFormatter(thousands))
        a.set_xlim(left=0)
        a.set_ylim(bottom=0)
    # The limit each ladder was given, drawn where it bites. A horizontal line is the whole explanation of
    # the left panel, so it belongs on the right one rather than in prose.
    for lim, colour in ((500_000, OURS), (5_000_000, SMALL)):
        if any(abs((r.get("vars") or {}).get("committer_coordinator_dep_graph_wait_tx_limit", 500_000)
                   - lim) < 1 for rs in ladders.values() for r in rs):
            bx.axhline(lim, color=colour, linewidth=1, linestyle="--", zorder=2)
    fig.legend(frameon=False, fontsize=8, labelcolor=INK2, loc="upper center",
               bbox_to_anchor=(0.5, 1.02), ncol=2)
    fig.tight_layout(rect=(0, 0, 1, 0.9))
    save(fig, path)


def main():
    rows = load(SRC)
    print(f"{len(rows)} rows from {SRC}")
    print(summary(rows))
    if CONFLICT:
        # Two figures, because the ladders answer two questions and one plot cannot hold both. The paired
        # latency curve is the familiar view -- what it costs to run at a rate -- and the cliff panels are
        # the causal one: delivered against offered, beside the graph population that explains the gap.
        conflict_ladders(rows, os.path.join(OUTDIR, "conflict-ladders.png"))
        conflict_threshold(rows, os.path.join(OUTDIR, "conflict-threshold.png"))
        latency_curve(rows, os.path.join(OUTDIR, "conflict-latency.png"))
        table(rows, os.path.join(OUTDIR, "figures-table.md"))
        return
    if not E2E:
        figure9(rows, os.path.join(OUTDIR, "figure9.png"))
    latency_curve(rows, os.path.join(OUTDIR, "latency-throughput.png"))
    if E2E:
        size_curve(rows, os.path.join(OUTDIR, "size-throughput.png"))
    table(rows, os.path.join(OUTDIR, "figures-table.md"))


if __name__ == "__main__":
    main()
