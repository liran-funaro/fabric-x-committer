#!/usr/bin/env python3
"""
Find where unmaintained imports are pulled into the codebase.

For every unmaintained package (read from stdin) that ends up in this module's
build graph, the report answers three questions:

  1. Blame  - which of *our* direct/tool dependencies drags it in.
  2. Choke  - which node(s) every path from us to the package must pass through.
  3. Where  - the source locations that import the package (or the blame point).

Each package also gets a Mermaid graph of the paths that reach it. Small graphs
are shown in full; large ("anomalous") graphs are collapsed to a backbone of
landmark nodes (root, choke points, target) so they stay readable.

Usage:
    cat bad-imports.txt | python3 scripts/find_bad_imports.py <project_root>
    echo "github.com/some/package" | python3 scripts/find_bad_imports.py .
"""

import json
import re
import subprocess
import sys
from collections import defaultdict, deque
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

try:
    import networkx as nx
except ImportError:
    sys.exit("This script requires networkx. Install it with: pip install networkx")

# A pruned graph with more edges than this is collapsed to its landmark backbone
# instead of being drawn in full.
MAX_GRAPH_EDGES = 20


####################################################################################################
# Data types
####################################################################################################

@dataclass(frozen=True, order=True)
class CodeLoc:
    """A single import site in the codebase, identified by file and line."""
    file: str
    line: int
    import_path: str
    is_test: bool


@dataclass
class Blame:
    """A direct/tool dependency of ours that pulls a target package in."""
    dep: str
    kind: str  # "" (direct) or "tool"
    example: tuple[str, ...]  # shortest dep -> ... -> target chain
    locations: list[CodeLoc]  # where we import `dep`


@dataclass
class TargetReport:
    """Everything we report about one unmaintained package."""
    pkg: str
    is_direct: bool  # imported directly in our own source
    subgraph: "nx.DiGraph"  # edges that lie on some root -> pkg path
    blames: list[Blame]
    chokes: list[str]
    locations: list[CodeLoc]  # where we import `pkg` itself


####################################################################################################
# Analyzer
####################################################################################################

class Analyzer:
    def __init__(self, project_root: str, bad_imports: Iterable[str]):
        self.root = Path(project_root).resolve()
        self.module = get_module_name(self.root)
        self.bad_imports = sorted(set(bad_imports))

        self.graph = build_graph(self.root, self.module, set(self.bad_imports))
        self.unreachable: list[str] = []  # bad imports not pulled into our build
        self._loc_cache: dict[str, list[CodeLoc]] = {}

    def locations(self, package: str) -> list[CodeLoc]:
        """Where `package` is imported in our source (cached)."""
        if package not in self._loc_cache:
            self._loc_cache[package] = sorted(set(_iter_code_locations(self.root, package)))
        return self._loc_cache[package]

    def analyze(self) -> list[TargetReport]:
        """Analyze every bad import reachable from our module.

        Reachability (not go.mod membership) is the right filter: a package can
        be pulled into the build purely transitively without being pinned in our
        require block, and those are exactly the ones we want to blame.
        """
        reports = []
        self.unreachable = []
        for pkg in self.bad_imports:
            sub = relevant_subgraph(self.graph, self.module, pkg)
            if sub is None:
                self.unreachable.append(pkg)
                continue
            cut = choke_points(sub, self.module, pkg)
            chokes = sorted({extend_choke(sub, self.module, c) for c in cut})
            reports.append(TargetReport(
                pkg=pkg,
                is_direct=bool(self.locations(pkg)),
                subgraph=sub,
                blames=self._blames(sub, pkg),
                chokes=chokes,
                locations=self.locations(pkg),
            ))
        return reports

    def _blames(self, sub: "nx.DiGraph", pkg: str) -> list[Blame]:
        """Direct/tool dependencies of ours (first hops) that reach `pkg`."""
        blames = []
        for dep in sorted(sub.successors(self.module)):
            kind = self.graph[self.module][dep].get("kind", "")
            if kind == "indirect":
                continue  # a blame point must be a direct or tool dependency of ours
            chain = example_chain(sub, dep, pkg)
            if chain is None:
                continue
            blames.append(Blame(dep=dep, kind=kind, example=chain, locations=self.locations(dep)))
        return blames

    ####################################################################
    # Report
    ####################################################################

    def report(self):
        reports = self.analyze()
        groups = group_by_identical_analysis(reports)
        print("# Unmaintained Imports Report")
        print()
        print("*Graph nodes omit the `github.com/` and `hyperledger/` prefixes; "
              "blue nodes are Hyperledger packages, green nodes are build tools.*")
        print()

        # Directly-imported groups first (this repo is to blame).
        for group in sorted(groups, key=lambda g: (not any(r.is_direct for r in g), g[0].pkg)):
            self._print_group(group)

        print_unreachable(self.unreachable)
        self._print_summary(reports, groups)

    def _print_group(self, group: list[TargetReport]):
        """Print one section for a group of packages with identical analysis.

        Most groups are a single package. When several packages share the same
        choke points and the same blame set (e.g. go-spew and go-difflib both
        pulled in solely through testify), they are reported together under one
        title with a single analysis.
        """
        rep = group[0]  # analysis is identical across the group
        pkgs = [r.pkg for r in group]
        if len(group) == 1:
            tag = "direct" if rep.is_direct else "indirect"
            print(f"## 📦 `{rep.pkg}` — {tag}")
        else:
            print(f"## 📦 {', '.join(f'`{p}`' for p in pkgs)}")
            print()
            print(f"*{len(group)} packages pulled in identically:*")
        print()

        # Choke points first: they are the headline (what to fix), and each one
        # names the direct import that drags it in.
        self._print_chokes(rep)

        composed = compose_subgraphs([r.subgraph for r in group])
        print(render_mermaid(composed, self.module, pkgs, rep.chokes))
        print()

        for r in group:
            if r.is_direct:
                print(f"**📝 `{r.pkg}` imported in our code:** {format_locations(r.locations)}")
                print()

        # Full blame list is verbose; fold it so the doc stays glanceable. For a
        # merged group, paths stop at the shared choke (past it they only differ
        # by which grouped package they end at, and those are named in the title).
        self._print_blame(rep, truncate_at=set(rep.chokes) if len(group) > 1 else set())
        print("---")
        print()

    def _print_chokes(self, rep: TargetReport):
        if not rep.chokes:
            return
        print("**🚧 Choke points** — every path funnels through these:")
        print()
        for choke in rep.chokes:
            print(f"- `{choke}`")
            path = example_chain(rep.subgraph, self.module, choke)
            if not path or len(path) < 2:
                continue
            if len(path) == 2:
                # The choke is one of our own direct dependencies.
                print(f"  - {format_locations(self.locations(choke))}")
            else:
                importer = path[1]  # path[0] is our module; path[1] is the direct import
                print(f"  - reached via {format_path(path[1:])}")
                print(f"  - `{importer}` {format_locations(self.locations(importer))}")
        print()

    def _print_blame(self, rep: TargetReport, truncate_at: set[str]):
        if not rep.blames:
            return
        print("<details>")
        print(f"<summary>🎯 Blame — {len(rep.blames)} of our dependencies pull it in</summary>")
        print()
        for b in rep.blames:
            label = f"`{b.dep}`" + (" *(tool)*" if b.kind == "tool" else "")
            print(f"- {label}")
            print(f"  - path: {format_path(truncate_path(b.example, truncate_at))}")
            print(f"  - {format_locations(b.locations)}")
        print()
        print("</details>")
        print()

    def _print_summary(self, reports: list[TargetReport], groups: list[list[TargetReport]]):
        blame_points = {b.dep for r in reports for b in r.blames} - set(self.bad_imports)
        print("## Summary")
        print()
        print(f"- Total unmaintained imports checked: {len(self.bad_imports)}")
        print(f"- Pulled into our build: {len(reports)} (in {len(groups)} source group(s))")
        print(f"- Distinct blame points: {len(blame_points)}")
        print(f"- Not in the dependency graph: {len(self.unreachable)}")
        print()


####################################################################################################
# Graph analysis
####################################################################################################

def relevant_subgraph(graph: "nx.DiGraph", root: str, target: str) -> "nx.DiGraph | None":
    """Subgraph of edges that lie on *some* root -> target path.

    A node belongs to such a path iff root can reach it and it can reach target.
    This replaces enumerating every path (which is exponential) with two linear
    reachability passes.
    """
    if root not in graph or target not in graph:
        return None
    downstream = nx.descendants(graph, root) | {root}
    upstream = nx.ancestors(graph, target) | {target}
    keep = downstream & upstream
    if root not in keep or target not in keep:
        return None  # target is unreachable from root
    return graph.subgraph(keep).copy()


def choke_points(sub: "nx.DiGraph", root: str, target: str) -> set[str]:
    """Smallest set of nodes whose removal disconnects root from target.

    These are the bottlenecks every dependency path is forced through. If root
    depends on target directly, no intermediate node can be a choke.
    """
    if sub.has_edge(root, target):
        return set()
    try:
        return nx.minimum_node_cut(sub, s=root, t=target)
    except (nx.NetworkXNoPath, nx.NetworkXError):
        return set()


def extend_choke(sub: "nx.DiGraph", root: str, choke: str) -> str:
    """Walk a choke back up its single-predecessor chain to the topmost node.

    `minimum_node_cut` returns the choke closest to the target (the last node in
    a line), but the interesting one is the highest node that everything still
    funnels through — the real dependency that is the *reason* the whole chain is
    present. We walk backward while each node has exactly one predecessor,
    stopping at a fan-in or our own module. For gojsonpointer this turns the
    reported choke from `gojsonreference` into `gavv/httpexpect/v2`, so the whole
    xeipuuv chain is attributed to the dependency we actually import.
    """
    cur = choke
    seen = {cur}
    while True:
        preds = list(sub.predecessors(cur))
        if len(preds) != 1 or preds[0] == root or preds[0] in seen:
            return cur
        cur = preds[0]
        seen.add(cur)


def example_chain(sub: "nx.DiGraph", src: str, dst: str) -> tuple[str, ...] | None:
    """A shortest src -> dst chain, preferring direct edges over indirect ones."""
    if src == dst:
        return (src,)
    try:
        path = nx.shortest_path(sub, src, dst, weight=_edge_weight)
    except nx.NetworkXNoPath:
        return None
    return tuple(path)


def _edge_weight(_u, _v, data) -> int:
    return 5 if data.get("kind") == "indirect" else 1


####################################################################################################
# Mermaid rendering
####################################################################################################

def render_mermaid(sub: "nx.DiGraph", root: str, targets: list[str], chokes: list[str]) -> str:
    if sub.number_of_edges() <= MAX_GRAPH_EDGES:
        edges = [(u, v, d.get("kind", "")) for u, v, d in sub.edges(data=True)]
    else:
        edges = _contract_to_backbone(sub, root, targets, chokes)

    # Nodes drop noisy prefixes (see report header); colour marks their role.
    targets_set, chokes_set = set(targets), set(chokes)
    tools = {v for u, v, kind in sub.edges(data="kind") if u == root and kind == "tool"}
    node_names = {n for u, v, _ in edges for n in (u, v)}

    lines = ["```mermaid", "graph TD"]
    tool_edges = []
    for index, (u, v, label) in enumerate(sorted((_short(u), _short(v), label) for u, v, label in edges)):
        arrow = f"-->|{label}|" if label else "-->"
        lines.append(f"    {u} {arrow} {v}")
        if label == "tool":
            tool_edges.append(index)  # linkStyle indexes edges in declaration order
    for node in sorted(node_names):
        style = _node_style(node, node in targets_set, node in chokes_set, node in tools)
        if style:
            lines.append(f"    style {_short(node)} {style}")
    if tool_edges:
        idx = ",".join(str(i) for i in tool_edges)
        lines.append(f"    linkStyle {idx} stroke:{_TOOL_GREEN},stroke-width:2px,"
                     f"color:#fff,background-color:{_TOOL_GREEN}")
    lines.append("```")
    return "\n".join(lines)


_HYPERLEDGER_PREFIX = "github.com/hyperledger/"
_IBM_BLUE = "#0f62fe"  # Carbon "Blue 60" — the IBM brand blue
_TOOL_GREEN = "#0e6027"  # Carbon "Green 80" — dark green for build tools


def _short(name: str) -> str:
    """Node label used in graphs — the `github.com/` and `hyperledger/` prefixes
    are dropped (Hyperledger packages are instead identified by colour)."""
    return name.removeprefix("github.com/").removeprefix("hyperledger/")


def _node_style(name: str, is_target: bool, is_choke: bool, is_tool: bool) -> str | None:
    """Mermaid style for a node, or None when it needs no highlight.

    Hyperledger packages are filled IBM blue and build tools green, both with
    white text (so dropping the `hyperledger/` prefix, and telling tools apart,
    does not depend on the label); a thicker border marks a node that is also a
    choke. The unmaintained target keeps its dark fill, and any other choke keeps
    its grey fill.
    """
    if is_target:
        return "fill:#555,stroke:#333,stroke-width:2px,color:#fff"
    border = "4px" if is_choke else "2px"
    if name.startswith(_HYPERLEDGER_PREFIX):
        return f"fill:{_IBM_BLUE},stroke:#002d9c,stroke-width:{border},color:#fff"
    if is_tool:
        return f"fill:{_TOOL_GREEN},stroke:#044317,stroke-width:{border},color:#fff"
    if is_choke:
        return "fill:#777"
    return None


def _contract_to_backbone(
        sub: "nx.DiGraph", root: str, targets: list[str], chokes: list[str],
) -> list[tuple[str, str, str]]:
    """Collapse the subgraph to edges between landmark nodes.

    Landmarks are the structural nodes: the root, the choke points every path is
    forced through, and the target(s). Chains of uninteresting intermediate nodes
    are contracted into a single edge labelled with the hop count. Blame points
    are deliberately excluded here (they are already listed in full below); for
    an anomalous graph this keeps the picture to the choke funnel.

    The one node we never hide inside a hop count is the root's own first hop: a
    direct dependency of the repository is the actionable node, so we promote it
    to a landmark. `root -->|3 hops| X` becomes `root --> dep -->|2 hops| X`.
    """
    targets = set(targets)
    landmarks = {root} | set(chokes) | targets
    # Promote the root's own first hops until no `root -->|N hops|` edge remains.
    # Each pass surfaces one more hidden direct dependency; it converges because
    # every pass adds at least one landmark. Deps that only reach a landmark the
    # root already reaches in one hop (e.g. testify) are never promoted, so a
    # tight funnel like go-spew stays tight.
    while True:
        promoted = _hidden_root_first_hops(sub, root, landmarks)
        if not promoted:
            break
        landmarks |= promoted
    edges: dict[tuple[str, str], str] = {}

    # Targets only ever consume edges; starting a BFS there would follow
    # dependency cycles forward and invent misleading target -> landmark edges.
    for start in landmarks - targets:
        # BFS from `start`, stopping whenever we reach another landmark.
        dist = {start: 0}
        queue = deque([start])
        while queue:
            node = queue.popleft()
            for nxt in sub.successors(node):
                if nxt in dist:
                    continue
                dist[nxt] = dist[node] + 1
                if nxt in landmarks:
                    label = sub[start][nxt].get("kind", "") if dist[nxt] == 1 else f"{dist[nxt]} hops"
                    edges.setdefault((start, nxt), label)
                else:
                    queue.append(nxt)

    return [(u, v, label) for (u, v), label in edges.items()]


def _hidden_root_first_hops(sub: "nx.DiGraph", root: str, landmarks: set[str]) -> set[str]:
    """Root's direct successors that begin a >1-hop path to a landmark.

    BFS from the root, stopping at landmarks (so we only see landmarks the root
    reaches without an intervening landmark). Any such landmark sitting more than
    one hop away was reached through a hidden direct dependency; we return that
    first hop so the caller can promote it. Returns empty once every landmark the
    root reaches is either one hop away or shielded by another landmark.
    """
    parent: dict[str, str] = {root: ""}
    dist = {root: 0}
    queue = deque([root])
    first_hops = set()
    while queue:
        node = queue.popleft()
        for nxt in sub.successors(node):
            if nxt in dist:
                continue
            dist[nxt] = dist[node] + 1
            parent[nxt] = node
            if nxt in landmarks:
                if dist[nxt] > 1:
                    hop = nxt
                    while parent[hop] != root:
                        hop = parent[hop]
                    first_hops.add(hop)
                # Stop at landmarks: do not expand past them.
            else:
                queue.append(nxt)
    return first_hops


def compose_subgraphs(subgraphs: list["nx.DiGraph"]) -> "nx.DiGraph":
    """Union of relevant subgraphs (for a merged group); identity for one graph."""
    return subgraphs[0] if len(subgraphs) == 1 else nx.compose_all(subgraphs)


def truncate_path(path: tuple[str, ...], stops: set[str]) -> tuple[str, ...]:
    """Cut `path` at the first node in `stops` (used to end blame paths at the
    shared choke of a merged group). Returns `path` unchanged if none is hit."""
    for i, node in enumerate(path):
        if node in stops:
            return path[:i + 1]
    return path


def group_by_identical_analysis(reports: list[TargetReport]) -> list[list[TargetReport]]:
    """Group packages whose analysis is identical: same choke set and same blame
    set. Such packages are pulled in the exact same way and share one section."""
    buckets: dict[tuple, list[TargetReport]] = defaultdict(list)
    for r in reports:
        key = (frozenset(r.chokes), frozenset(b.dep for b in r.blames))
        buckets[key].append(r)
    return [sorted(group, key=lambda r: r.pkg) for group in buckets.values()]


####################################################################################################
# Building the dependency graph
####################################################################################################

def build_graph(project_root: Path, module: str, bad_imports: set[str]) -> "nx.DiGraph":
    """Build the module dependency graph with a `kind` label on every edge.

    Edges come from two sources that detect dependencies differently, so we need
    both:
      - `go mod graph` gives the module *require* graph. Under Go's module-graph
        pruning it can omit edges (e.g. an ancient dependency whose go.mod has no
        `require` block never advertises what it imports).
      - `go mod why <pkg>` reports the actual *package* import chain, which
        surfaces those missing edges (it is how we learn, for instance, that
        gojsonreference imports gojsonpointer even though gojsonreference's
        go.mod is empty).

    Each edge is labelled "" (direct), "indirect", or "tool" from the *source*
    module's go.mod.
    """
    edges = set(_iter_project_graph_edges(project_root))
    print(f"Loaded {len(edges)} edges from 'go mod graph'.", file=sys.stderr)

    cache = ModuleDependencyCache(project_root, module)
    indirect_deps = [p for p, kind in cache.get_dependencies(module).items() if kind == "indirect"]
    for dep in indirect_deps:
        chain = list(_iter_go_mod_why(project_root, dep, module))
        edges.update(zip(chain, chain[1:]))

    graph = nx.DiGraph()
    for src, dst in edges:
        graph.add_edge(src, dst, kind=cache.dependency_type(src, dst) or "")

    removed = prune_indirect_shortcuts(graph, bad_imports)
    kinds = defaultdict(int)
    for _, _, kind in graph.edges(data="kind"):
        kinds[kind or "direct"] += 1
    print(f"Graph: {graph.number_of_edges()} edges ({dict(kinds)}); "
          f"pruned {removed} redundant indirect edges; cached {cache.size()} modules.",
          file=sys.stderr)
    return graph


def prune_indirect_shortcuts(graph: "nx.DiGraph", bad_imports: set[str]) -> int:
    """Drop `indirect` edges that are go.mod pinning artifacts.

    go.mod lists indirect requirements at the top level for version pinning, so
    `go mod graph` contains a shortcut edge (u -> v) alongside the real path
    (u -> ... -> v). Removing a shortcut whenever v is still reachable from u
    without it makes every path follow the real dependency structure, which in
    turn restores meaningful choke points and keeps graphs small.

    The alternate path may not run *through* another unmaintained package: those
    are the very packages we report, so an edge into one must not be dissolved
    just because a route exists via a different one. Otherwise a chain of
    unmaintained packages (gojsonschema -> gojsonreference -> gojsonpointer)
    would delete the real gojsonschema -> gojsonpointer edge and collapse the
    graph into a misleading straight line. The edge's own endpoint is fine as
    the destination; only pass-through is blocked.
    """
    indirect_edges = [(u, v) for u, v, kind in graph.edges(data="kind") if kind == "indirect"]
    removed = 0
    for u, v in indirect_edges:
        graph.remove_edge(u, v)
        if _reachable_without_passing_through(graph, u, v, bad_imports):
            removed += 1  # redundant: a real path remains, keep it removed
        else:
            graph.add_edge(u, v, kind="indirect")  # a genuine sole path, restore it
    return removed


def _reachable_without_passing_through(graph: "nx.DiGraph", src: str, dst: str, blocked: set[str]) -> bool:
    """Is `dst` reachable from `src` without routing through a `blocked` node?

    `dst` itself is always an allowed destination even if it is blocked; only
    intermediate blocked nodes stop the search.
    """
    seen = {src}
    queue = deque([src])
    while queue:
        node = queue.popleft()
        for nxt in graph.successors(node):
            if nxt == dst:
                return True
            if nxt in seen or nxt in blocked:
                continue
            seen.add(nxt)
            queue.append(nxt)
    return False


class ModuleDependencyCache:
    """Caches each module's go.mod dependency table to avoid re-parsing."""

    def __init__(self, project_root: Path, module: str):
        self.project_root = Path(project_root)
        self.module_name = module
        self._cache: dict[str, dict[str, str]] = {}

    def get_dependencies(self, module_path: str) -> dict[str, str]:
        """All dependencies of `module_path` -> kind ("", "indirect", "tool")."""
        if module_path not in self._cache:
            self._cache[module_path] = self._fetch_dependencies(module_path)
        return self._cache[module_path]

    def dependency_type(self, module_path: str, dependency: str) -> str | None:
        return self.get_dependencies(module_path).get(dependency)

    def size(self) -> int:
        return len(self._cache)

    def _fetch_dependencies(self, module_path: str) -> dict[str, str]:
        go_mod_path = self._mod_path(module_path)
        return parse_go_mod(go_mod_path) if go_mod_path else {}

    def _mod_path(self, module_path: str) -> Path | None:
        if module_path == self.module_name:
            return self.project_root / "go.mod"

        # Ensure the module is downloaded so we can read its go.mod.
        try:
            result = run(["go", "mod", "download", "-json", module_path], self.project_root)
        except subprocess.CalledProcessError as e:
            print(f"Module download failed: {module_path} - {e}", file=sys.stderr)
            return None
        if not result.strip():
            print(f"Module download returned empty result: {module_path}", file=sys.stderr)
            return None

        go_mod_path = Path(json.loads(result).get("GoMod", ""))
        if not go_mod_path.exists():
            print(f"Cannot read go.mod for module: {module_path}", file=sys.stderr)
            return None
        return go_mod_path


####################################################################################################
# Parsers
####################################################################################################

def iter_bad_imports() -> Iterable[str]:
    """Load the list of unmaintained imports from stdin."""
    if sys.stdin.isatty():
        sys.exit("Error: no input. Usage: cat bad-imports.txt | python3 scripts/find_bad_imports.py .")
    for line in sys.stdin:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        yield line.removeprefix("https://")


def get_module_name(project_root: Path) -> str:
    return run(["go", "list", "-m"], project_root).strip()


def parse_go_mod(go_mod_path: Path) -> dict[str, str]:
    """Parse a go.mod into {package: kind} where kind is "", "indirect", or "tool"."""
    if not go_mod_path.exists():
        print(f"Warning: go.mod not found at {go_mod_path}", file=sys.stderr)
        return {}

    content = go_mod_path.read_text()
    tools = set(_iter_tools(content))
    dependencies = {}
    for pkg, is_indirect in _iter_deps(content):
        pkg = pkg.strip('"')
        if not is_indirect:
            dependencies[pkg] = ""
        elif pkg in tools:
            dependencies[pkg] = "tool"
        else:
            dependencies[pkg] = "indirect"
    return dependencies


def _iter_deps(content: str) -> Iterable[tuple[str, bool]]:
    for block in re.findall(r"require\s*\((.*?)\)", content, re.DOTALL):
        for line in block.splitlines():
            line = line.strip()
            if line and not line.startswith("//"):
                parts = line.split()
                if len(parts) >= 2:
                    yield parts[0], "// indirect" in line
    for match in re.finditer(r"require\s+([^\s(]+)\s+(\S+.*)", content):
        yield match.group(1), "// indirect" in match.group(2)


def _iter_tools(content: str) -> Iterable[str]:
    for block in re.findall(r"tool\s*\((.*?)\)", content, re.DOTALL | re.MULTILINE):
        for line in block.splitlines():
            line = line.strip()
            if not line or line.startswith("//"):
                continue
            pkg = line.split()[0]
            yield pkg
            # A tool may be a "<module>/cmd/<tool>" path; the module owns the edge.
            base = pkg.split("/cmd/")[0]
            if base != pkg:
                yield base


def _iter_project_graph_edges(project_root: Path) -> Iterable[tuple[str, str]]:
    for line in run(["go", "mod", "graph"], project_root).splitlines():
        parts = line.split()
        if len(parts) != 2:
            continue
        src, dst = (p.split("@")[0] for p in parts)
        # Skip standard-library packages (no dot in the path).
        if "." in src and "." in dst:
            yield src, dst


def _iter_go_mod_why(project_root: Path, pkg: str, module: str) -> Iterable[str]:
    """The module chain `go mod why` reports for `pkg`, mapped to module paths."""
    seen = set()
    for line in run(["go", "mod", "why", pkg], project_root).splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "main module does not need" in line:
            continue
        if line.endswith(".test"):
            continue  # synthetic test-binary package; not a resolvable module path
        if line == module or line.startswith(module + "/"):
            node = module
        else:
            try:
                node = run(["go", "list", "-f", "{{.Module.Path}}", line], project_root).strip()
            except subprocess.CalledProcessError as e:
                print(e, file=sys.stderr)
                continue
        if node and node not in seen:
            seen.add(node)
            yield node


# Go import declarations: a single `import "path"` (optional alias) and blocks.
_IMPORT_SINGLE = re.compile(r'import\s+(?:[\w.]+\s+)?"([^"]+)"')
_IMPORT_BLOCK = re.compile(r"import\s*\((.*?)\)", re.DOTALL)
_QUOTED = re.compile(r'"([^"]+)"')


def _iter_code_locations(project_root: Path, package: str) -> Iterable[CodeLoc]:
    def matches(path: str) -> bool:
        return path == package or path.startswith(package + "/")

    def line_of(text: str, index: int) -> int:
        return text.count("\n", 0, index) + 1

    for go_file in project_root.rglob("*.go"):
        if any(part.startswith(".") or part == "vendor" for part in go_file.parts):
            continue
        try:
            content = go_file.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        if package not in content:
            continue

        rel_path = str(go_file.relative_to(project_root))
        is_test = rel_path.endswith("_test.go")

        for match in _IMPORT_SINGLE.finditer(content):
            if matches(match.group(1)):
                yield CodeLoc(rel_path, line_of(content, match.start(1)), match.group(1), is_test)
        for block in _IMPORT_BLOCK.finditer(content):
            base = block.start(1)
            for quoted in _QUOTED.finditer(block.group(1)):
                if matches(quoted.group(1)):
                    yield CodeLoc(rel_path, line_of(content, base + quoted.start(1)),
                                  quoted.group(1), is_test)


####################################################################################################
# Formatting helpers
####################################################################################################

def format_path(path: tuple[str, ...]) -> str:
    return " → ".join(f"`{p}`" for p in path)


def format_locations(locations: list[CodeLoc], max_shown: int = 3) -> str:
    if not locations:
        return "*(not imported directly)*"
    locations = sorted(locations)
    shown = ", ".join(f"`{loc.file}:{loc.line}`" for loc in locations[:max_shown])
    extra = len(locations) - max_shown
    return f"imported at {shown}" + (f" (+{extra} more)" if extra > 0 else "")


def print_unreachable(unreachable: list[str]):
    if not unreachable:
        return
    print(f"## Not in the dependency graph ({len(unreachable)} found)")
    print()
    print("These unmaintained packages are not reachable from this module — "
          "they are not pulled into the build.")
    print()
    for pkg in sorted(unreachable):
        print(f"- `{pkg}`")
    print()
    print("---")
    print()


def run(cmd: list[str], project_root) -> str:
    return subprocess.run(
        cmd, cwd=str(project_root), capture_output=True, text=True, timeout=30, check=True,
    ).stdout


####################################################################################################
# Main
####################################################################################################

def main():
    if len(sys.argv) < 2:
        sys.exit("Usage: cat bad-imports.txt | python3 scripts/find_bad_imports.py <project_root>")

    print("Loading unmaintained imports...", file=sys.stderr)
    bad_imports = list(iter_bad_imports())

    print("Analyzing imports...", file=sys.stderr)
    Analyzer(sys.argv[1], bad_imports).report()


if __name__ == "__main__":
    main()
