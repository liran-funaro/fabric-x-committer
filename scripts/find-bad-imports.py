#!/usr/bin/env python3
"""
Find where unmaintained imports are used in the codebase.

This script analyzes a Go project to find:
1. Direct imports of unmaintained packages from stdin
2. Indirect imports and which direct dependency brings them in
3. Where in the code these imports are used

Usage:
    cat bad-imports.txt | python3 scripts/find-bad-imports.py <project_root>
    echo "github.com/some/package" | python3 scripts/find-bad-imports.py /path/to/project

Example:
    cat bad-imports.txt | python3 scripts/find-bad-imports.py .
    cat bad-imports.txt | python3 scripts/find-bad-imports.py /home/user/myproject
"""

import re
import subprocess
import sys
import json
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


@dataclass
class DependencyGraph:
    """Represents the dependency graphs with edge metadata.

    All edges are included in the graphs. Use edge_metadata to determine
    if an edge is direct, indirect, or tool.
    """
    graph: dict  # module -> set of all dependencies
    edge_metadata: dict  # (from, to) -> "" (direct), "indirect", or "tool"

    def find_dependency_chain(self, src_pkg: str, dst_pkg: str) -> set[tuple[str, ...]]:
        """Find all non-cyclic dependency chains from module to target package.

        Args:
            src_pkg: Source package
            dst_pkg: Target package to find chains to

        Returns:
            Set of dependency chains (lists of package names)
        """
        all_paths = find_all_paths(self.graph, src_pkg, dst_pkg)
        return dedup_all_paths(all_paths, self.edge_metadata)


@dataclass
class CodeLoc:
    file: str
    line: int
    import_path: str
    is_test: bool

    # 1. Generate a stable integer based on unique properties
    def __hash__(self):
        return hash(self._id)

    # 2. Check if two instances represent the same data
    def __eq__(self, other):
        if not isinstance(other, self.__class__):
            return False
        return self._id == other._id

    def __lt__(self, other):
        if not isinstance(other, self.__class__):
            return False
        return self._id < other._id

    @property
    def _id(self):
        return self.file, self.line


@dataclass
class ChainInfo:
    """Information about a single dependency chain with its blame point."""
    blame_point: str
    blame_to_target: set[tuple[str, ...]]  # Path(s) from blame to target
    locations: list[CodeLoc]  # locations where this blame point is imported


@dataclass
class PackageBlameInfo:
    """Information about a bad package and its blame analysis."""
    package: str
    blame_analysis: list[ChainInfo] | None
    chains: set[tuple[str, ...]]
    locations: list[CodeLoc]  # locations where this package is imported


class ModuleDependencyCache:
    """Cache for module dependencies to avoid re-parsing go.mod files."""

    def __init__(self, project_root):
        self.project_root = Path(project_root)
        self.module_name = get_module_name(self.project_root)
        self._cache = {}  # module_path -> {dependency: dependency-type}

    def get_dependencies(self, module_path):
        """ Get all dependencies (direct and indirect) for a module. """
        if module_path in self._cache:
            return self._cache[module_path]

        dependencies = self._fetch_dependencies(module_path)
        # Cache the result
        self._cache[module_path] = dependencies
        return dependencies

    def _fetch_dependencies(self, module_path: str):
        """ Fetch all dependencies (direct and indirect) for a module. """
        go_mod_path = self._get_mod_path(module_path)
        if not go_mod_path:
            # Module download failed, empty result
            return {}
        return parse_go_mod(go_mod_path)

    def _get_mod_path(self, module_path: str):
        if module_path == self.module_name:
            # Special case: if this is the root module, read go.mod directly
            return self.project_root / 'go.mod'

        # Run go mod download to ensure the module is in cache
        try:
            result = run(['go', 'mod', 'download', '-json', module_path], self.project_root)
        except subprocess.CalledProcessError as e:
            print("Module download failed: ", module_path, "-", e, file=sys.stderr)
            return None

        if not result.strip():
            print("Module download returned empty result: ", module_path, file=sys.stderr)
            return None

        mod_info = json.loads(result)
        go_mod_path = Path(mod_info.get('GoMod', ''))

        if not go_mod_path or not go_mod_path.exists():
            print("Cannot read go.mod for module: ", module_path, file=sys.stderr)
            return None

        return go_mod_path

    def dependency_type(self, module_path, dependency):
        """Check if a dependency is direct (not indirect) in a module's go mod file."""
        dependencies = self.get_dependencies(module_path)
        return dependencies.get(dependency)  # Returns direct, indirect, or tool

    def size(self):
        """Return the number of cached modules."""
        return len(self._cache)


class BadImportFinder:
    def __init__(self, project_root, bad_imports: list[str]):
        self.project_root = Path(project_root)
        self.bad_imports = bad_imports

        self.import_cache = {}  # Cache of package -> locations
        self.similar_packages = defaultdict(list)  # Cache of package suffix -> full packages
        self.module_name = get_module_name(project_root)
        self.dependency_graph = build_dependency_graph(project_root)

        module_deps = self.dependency_graph.graph.get(self.module_name)
        self.included_bad_imports = sorted(set(self.bad_imports).intersection(module_deps))
        self.not_in_mod = sorted(sorted(set(self.bad_imports) - set(module_deps)))

    def find_import_in_code(self, package) -> list[CodeLoc]:
        """Find where a package is imported in Go source files."""
        # Check cache first (but only if it's been populated)
        if package in self.import_cache:
            return self.import_cache[package]

        locations = sorted(set(_iter_code_locations(self.project_root, package)))

        # Cache the result. Also cache by suffix for similar package lookup
        self.import_cache[package] = locations
        pkg_parts = package.split('/')
        if len(pkg_parts) >= 2:
            suffix = '/'.join(pkg_parts[-2:])
            self.similar_packages[suffix].append(package)

        return locations

    def find_dependency_chain(self, pkg) -> set[tuple[str, ...]]:
        """Find all non-cyclic dependency chains from module to target package"""
        return self.dependency_graph.find_dependency_chain(self.module_name, pkg)

    def analyze_and_group_by_blame(self) -> dict[str, list[PackageBlameInfo]]:
        """Analyze all bad imports and group them by blame point.

        Returns:
            A tuple of (blame_groups, no_blame_packages) where:
            - blame_groups: dict mapping blame_point -> list of PackageBlameInfo
            - no_blame_packages: list of PackageBlameInfo without clear blame point
        """
        # First pass: collect all valid blame points
        package_chains = {}  # pkg -> chains

        for pkg in self.included_bad_imports:
            chains = self.find_dependency_chain(pkg)
            if chains:
                package_chains[pkg] = chains

        valid_blame = set()
        for pkg, chains in package_chains.items():
            blame_analysis = self.group_chains_by_blame_point(chains)
            for chain_info in blame_analysis:
                valid_blame.add(chain_info.blame_point)

        # Second pass: assign packages to blame points, filtering out paths through other blame points
        blame_groups: dict[str, list[PackageBlameInfo]] = defaultdict(list)

        for pkg, chains in sorted(package_chains.items()):
            # Filter out chains that pass through other blame points (except as first hop).
            # Checks if any element after the first hop is a blame point
            # (first hop is allowed to be a blame point - that's the point!)
            filtered_chains = set(filter(lambda ch: len(ch) > 1 and not any(c in valid_blame for c in ch[2:]), chains))

            blame_analysis = self.group_chains_by_blame_point(filtered_chains)
            package_info = PackageBlameInfo(
                package=pkg,
                blame_analysis=blame_analysis,
                chains=filtered_chains,
                locations=self.find_import_in_code(pkg),
            )
            for chain_info in blame_analysis:
                blame_groups[chain_info.blame_point].append(package_info)

        return blame_groups

    def group_chains_by_blame_point(self, chains: Iterable[tuple[str, ...]]) -> list[ChainInfo]:
        """
        Group chains by the blame point (the first hop, second element in path).

        A blame point must be a direct or tool dependency from the root module.
        If the first hop is indirect, the package will be handled as "no valid blame point".

        Returns: list[ChainInfo] (ChainInfo for each blame point)
        """
        # Group chains by first hop (second element in path)
        chains_by_first_hop: dict[str, set[tuple[str, ...]]] = defaultdict(set)
        for path in chains:
            if len(path) < 2:
                continue
            first_hop = path[1]
            chains_by_first_hop[first_hop].add(tuple(path[1:]))

        # Multiple valid blame points - create chain info for each
        chain_infos = []
        edge_metadata = self.dependency_graph.edge_metadata
        for blame_point, paths in sorted(chains_by_first_hop.items()):
            # Check if a blame point is valid (must be direct or tool dependency, not indirect).
            if (blame_point != self.module_name) and (edge_metadata.get((self.module_name, blame_point)) == "indirect"):
                continue
            chain_infos.append(ChainInfo(
                blame_point=blame_point,
                blame_to_target=paths,
                locations=self.find_import_in_code(blame_point),
            ))
        return chain_infos

    def generate_report_grouped(self):
        """Generate a comprehensive report of bad imports grouped by blame point."""
        # Analyze and group by blame point
        blame_groups = self.analyze_and_group_by_blame()

        # Separate direct bad imports (where this repo is to blame)
        # A package is a "direct bad import" if it's imported directly in THIS repo's code
        direct_bad_imports = blame_groups.pop(self.module_name, [])
        # Always include to indirect blame groups (even if some are also direct)
        # This shows the complete picture of where packages come from
        indirect_blame_groups = blame_groups

        # Check each blame group (sorted for determinism)
        for blame_point, packages in sorted(blame_groups.items()):
            for info in packages:
                # Check if this package is actually imported in our code
                if info.locations:
                    # Package is imported in our code - add to direct imports
                    direct_bad_imports.append(info)

        # Report direct bad imports first (THIS REPO IS TO BLAME)
        if direct_bad_imports:
            print_direct_bad_imports_section(direct_bad_imports)

        # Report indirect bad imports grouped by blame point
        if indirect_blame_groups:
            # Filter out blame points that are themselves bad imports
            # A bad import cannot be a blame point for other bad imports
            # Note: We include even indirect dependencies as blame points if they're
            # the best path we have. In the future, we may want to detect paths
            # through tool dependencies separately.
            for blame_point in list(indirect_blame_groups):
                # Skip if blame point is itself a bad import
                if blame_point in self.bad_imports:
                    del indirect_blame_groups[blame_point]

            for blame_point, packages in sorted(indirect_blame_groups.items()):
                if len(packages) == 0:
                    continue

                # Check if this is a tool dependency
                edge_type = self.dependency_graph.edge_metadata.get((self.module_name, blame_point))
                is_tool = edge_type == 'tool'

                print(f"---")
                print()

                blame_title = f"## 🎯 BLAME POINT: `{blame_point}`"
                if is_tool:
                    blame_title += " (tool)"
                print(blame_title)
                print(f"Responsible for {len(packages)} unmaintained import(s)")
                print()

                # Show unmaintained imports FIRST
                print(f"- **⚠️  Unmaintained imports:**")
                print()

                # Show each bad package under this blame point
                blame_point_locations = []
                for info in packages:
                    print(f"  - **📦 {info.package}**")

                    # Find chains for this blame point
                    relevant_chains = [c for c in info.blame_analysis if c.blame_point == blame_point]
                    if len(relevant_chains) != 1:
                        print(f"There should be exactly one chains for blame point: {blame_point} and pacakge: "
                              f"{info.package} - Actual: {len(relevant_chains)}", file=sys.stderr)
                    else:
                        self.print_multi_path("    ", relevant_chains[0].blame_to_target)
                        blame_point_locations = relevant_chains[0].locations
                    print()

                # Show where this blame point is imported in your code
                print_locations("- ", blame_point_locations)

        # Report on packages not in go.mod
        if self.not_in_mod:
            print(f"---")
            print()
            print(f"## NOT IN GO.MOD ({len(self.not_in_mod)} found)")
            print()
            for pkg in sorted(self.not_in_mod):
                print(f"- `{pkg}`")
            print()

        # Summary
        print(f"---")
        print()
        print(f"## SUMMARY")
        print()
        print(f"**Total unmaintained imports analyzed:** {len(self.bad_imports)}")
        all_deps = self.included_bad_imports
        print(f"- In go.mod: {len(all_deps)}")
        if direct_bad_imports:
            # Count unique direct unmaintained imports
            unique_direct_count = len(set(info.package for info in direct_bad_imports))
            print(f"- Direct unmaintained imports (this repo to blame): {unique_direct_count}")
        if indirect_blame_groups:
            print(f"- Indirect unmaintained imports grouped by {len(indirect_blame_groups)} external blame point(s)")
        print(f"- Not in go.mod: {len(self.not_in_mod)}")
        print()

    def print_multi_path(self, indent: str, chains: Iterable[tuple[str, ...]], max_size=2):
        chains = sorted(chains, key=lambda x: (len(x), x))
        if len(chains) == 0:
            return
        for path in chains[:2]:
            formatted_path = self.format_path_with_metadata(path)
            print(f"{indent}- {formatted_path}")
        if len(chains) > max_size:
            print(f"{indent}- ... and {len(chains) - max_size} more")

    def format_path_with_metadata(self, path_input):
        """Format a dependency path with edge metadata annotations.

        Args:
            path_input: Either a list of package names or a string with " -> " separators

        Returns:
            Formatted string with packages and edge annotations
        """
        # Convert string to list if needed
        if isinstance(path_input, str):
            path_list = path_input.split(' -> ')
        else:
            path_list = path_input

        if not path_list or len(path_list) < 2:
            return ' -> '.join(f'`{p}`' for p in path_list)

        parts = []
        for i in range(len(path_list)):
            parts.append(f'`{path_list[i]}`')

            # Add edge annotation if not the last element
            if i < len(path_list) - 1:
                from_pkg = path_list[i]
                to_pkg = path_list[i + 1]
                edge_key = (from_pkg, to_pkg)
                annotation = self.dependency_graph.edge_metadata.get(edge_key)
                if annotation:
                    parts.append(f" *({annotation})*")

                parts.append(" -> ")

        # Remove trailing " -> " if present
        result = ''.join(parts)
        if result.endswith(" -> "):
            result = result[:-4]

        return result


def main():
    # Determine project root (where go.mod is)
    if len(sys.argv) < 2:
        print("Usage: python find-bad-imports.py <project_root>")
        sys.exit(1)

    project_root = Path(sys.argv[1]).resolve()

    print("Loading unmaintained imports...", file=sys.stderr)
    bad_imports = list(iter_bad_imports())

    finder = BadImportFinder(project_root, bad_imports)

    print("Analyzing imports...", file=sys.stderr)

    # Print Markdown title
    print("# Unmaintained Imports Report")
    print()

    finder.generate_report_grouped()


def iter_bad_imports():
    """Load the list of bad imports from stdin."""
    if sys.stdin.isatty():
        print("Error: No input provided. Please pipe input to this script.")
        print("Usage: cat bad-imports.txt | python3 scripts/find-bad-imports.py")
        sys.exit(1)

    lines = sys.stdin.readlines()

    for line in lines:
        line = line.strip()
        # Skip empty lines and comments
        if line and not line.startswith('#'):
            # Handle URLs like https://github.com/...
            if line.startswith('https://'):
                line = line.replace('https://', '')
            yield line


def get_module_name(project_root):
    """Get the module name using go list -m."""
    return run(['go', 'list', '-m'], project_root).strip()


def parse_go_mod(go_mod_path: Path) -> dict[str, str]:
    if not go_mod_path.exists():
        print(f"Warning: go.mod not found at {go_mod_path}", file=sys.stderr)
        return {}

    content = go_mod_path.read_text()
    tool_deps = set(_iter_tools(content))
    dependencies = {}

    for pkg, is_indirect in _iter_dep(content):
        tag = ""
        if pkg in tool_deps:
            tag = "tool"
        elif is_indirect:
            tag = "indirect"
        dependencies[pkg] = tag

    return dependencies


def _iter_dep(content: str):
    # Look for dependencies in require blocks
    for match in re.findall(r'require\s*\((.*?)\)', content, re.DOTALL):
        for line in match.split('\n'):
            line = line.strip()
            if not line or line.startswith('//'):
                continue

            parts = line.split()
            if len(parts) >= 2:
                pkg = parts[0]
                is_indirect = '// indirect' in line
                yield pkg, is_indirect

    # Also check single-line requires
    for line_match in re.finditer(r'require\s+(\S+)\s+\S+(.*)', content):
        pkg = line_match.group(1)
        is_indirect = '// indirect' in line_match.group(2)
        yield pkg, is_indirect


def _iter_tools(content: str):
    # Look for tool blocks: tool ( ... )
    for m in re.findall(r'tool\s*\((.*?)\)', content, re.DOTALL | re.MULTILINE):
        for line in m.split('\n'):
            line = line.strip()
            if not line or line.startswith('//'):
                continue

            # Tool entries can be just package paths or package/cmd paths
            parts = line.split()
            if parts:
                pkg = parts[0]
                yield pkg


def build_dependency_graph(project_root) -> DependencyGraph:
    """Build the dependency graph with edge metadata for direct/indirect/toolchain."""
    # Get the full graph from go mod graph and parse all edges
    all_edges = list(_iter_project_graph_edges(project_root))
    print(f"Filtering {len(all_edges)} edges to separate direct and indirect dependencies...", file=sys.stderr)

    # Create cache for module dependencies
    cache = ModuleDependencyCache(project_root)

    # Build forward and reverse graphs with all edges
    graph = defaultdict(set)
    edge_metadata = {}

    for from_pkg, to_pkg in all_edges:
        # Add all edges to the graph
        graph[from_pkg].add(to_pkg)

        # Get dependency type for all edges: "" (direct), "indirect", or "tool"
        edge_metadata[(from_pkg, to_pkg)] = cache.dependency_type(from_pkg, to_pkg)

    # Count edge types for reporting
    direct_count = sum(1 for meta in edge_metadata.values() if meta == "")
    indirect_count = sum(1 for meta in edge_metadata.values() if meta == "indirect")
    toolchain_count = sum(1 for meta in edge_metadata.values() if meta == "tool")
    print(f"Graph contains {len(edge_metadata)} total edges:", file=sys.stderr)
    print(f"  - {direct_count} direct dependency edges", file=sys.stderr)
    print(f"  - {indirect_count} indirect dependency edges", file=sys.stderr)
    print(f"  - {toolchain_count} toolchain edges", file=sys.stderr)
    print(f"Cached {cache.size()} unique modules (avoided re-parsing)", file=sys.stderr)

    return DependencyGraph(
        graph=graph,
        edge_metadata=edge_metadata
    )


def _iter_project_graph_edges(project_root):
    for line in run(['go', 'mod', 'graph'], project_root).split('\n'):
        if not line.strip():
            continue
        parts = line.split()
        if len(parts) != 2:
            continue
        from_pkg, to_pkg = [p.split('@')[0] for p in parts]

        # Skip standard library packages (they don't have dots in their names)
        if '.' not in to_pkg or '.' not in from_pkg:
            continue

        yield from_pkg, to_pkg


def find_all_paths(graph, start, target, max_depth=10) -> set[tuple[str, ...]]:
    # DFS to find ALL non-cyclic paths from start to target
    all_paths = set()

    def dfs(current, path, visited):
        # Found target
        if current == target:
            all_paths.add(tuple(path))
            return

        # Don't go too deep
        if len(path) >= max_depth:
            return

        # Explore neighbors (sorted for determinism)
        for neighbor in sorted(graph.get(current, [])):
            if neighbor in visited:
                continue
            visited.add(neighbor)
            path.append(neighbor)
            dfs(neighbor, path, visited)
            path.pop()
            visited.remove(neighbor)

    # Start DFS
    dfs(start, [start], {start})
    return all_paths


def dedup_all_paths(all_paths: set[tuple[str, ...]], edge_metadata: dict[tuple[str, str], str]) -> set[tuple[str, ...]]:
    shortcuts: dict[tuple[str, str], tuple[str, ...]] = {}
    for path in all_paths:
        if len(path) < 2:
            continue
        for i in range(len(path)):
            for j in range(i + 1, len(path)):
                if edge_metadata.get((path[j - 1], path[j])):
                    # Only follow direct path
                    break
                k = path[i], path[j]
                if k not in shortcuts:
                    shortcuts[k] = tuple(path[i:j + 1])

    new_all_paths = set()
    for path in all_paths:
        if len(path) < 2:
            new_all_paths.add(path)
            continue
        for i in reversed(range(len(path) - 1)):
            k = path[i], path[i + 1]
            if not edge_metadata.get(k):
                continue
            short_path = shortcuts.get(k)
            if not short_path:
                continue
            path = tuple(path[:i]) + tuple(short_path) + tuple(path[i + 2:])

        new_all_paths.add(path)

    return new_all_paths


def _iter_code_locations(project_root: Path, package: str) -> Iterable[CodeLoc]:
    # Search for import statements in .go files
    for go_file in project_root.rglob("*.go"):
        # Skip vendor and hidden directories
        if any(part.startswith('.') or part == 'vendor' for part in go_file.parts):
            continue

        with open(go_file, 'r', encoding='utf-8') as f:
            content = f.read()

        # Look for import statements
        # Match both single imports and import blocks
        import_patterns = [
            rf'import\s+"({re.escape(package)}[^"]*)"',
            rf'import\s+\w+\s+"({re.escape(package)}[^"]*)"',
        ]

        for pattern in import_patterns:
            for match in re.finditer(pattern, content):
                # Find line number
                line_num = content[:match.start()].count('\n') + 1
                full_import = match.group(1)

                rel_path = go_file.relative_to(project_root)
                yield CodeLoc(
                    file=str(rel_path),
                    line=line_num,
                    import_path=full_import,
                    is_test=str(rel_path).endswith("_test.go")
                )

        # Also check import blocks
        import_block_pattern = r'import\s*\((.*?)\)'
        for block_match in re.finditer(import_block_pattern, content, re.DOTALL):
            block_content = block_match.group(1)
            block_start = content[:block_match.start()].count('\n') + 1

            for line_offset, line in enumerate(block_content.split('\n')):
                if package in line:
                    # Extract the full import path
                    import_match = re.search(r'"([^"]+)"', line)
                    if import_match:
                        full_import = import_match.group(1)
                        if package in full_import:
                            rel_path = go_file.relative_to(project_root)
                            yield CodeLoc(
                                file=str(rel_path),
                                line=block_start + line_offset,
                                import_path=full_import,
                                is_test=str(rel_path).endswith("_test.go")
                            )


def print_locations(indent: str, locations, max_size=3):
    locations = sorted(locations, key=lambda x: (x.file, x.line))
    if not locations:
        print(f"{indent}*(Import location not found in code)*")
        print()
        return

    print(f"{indent}**📝 Imported in your code at:**")
    print()
    for loc in locations[:max_size]:
        print(f"  - `{loc.file}:{loc.line}`")
    if len(locations) > max_size:
        print(f"  - ... and {len(locations) - max_size} more location(s)")
    print()


def print_direct_bad_imports_section(direct_bad_imports: list[PackageBlameInfo]):
    print(f"---")
    print()
    print(f"## ⚠️ DIRECT UNMAINTAINED IMPORTS ({len(direct_bad_imports)} found)")
    print()

    for info in sorted(direct_bad_imports, key=lambda x: x.package):
        print(f"- **📦 {info.package}**")
        print()
        print_locations("  ", info.locations)


def run(cmd: list[str], project_root):
    return subprocess.run(
        cmd,
        cwd=str(project_root),
        capture_output=True,
        text=True,
        timeout=30,
        check=True,
    ).stdout


if __name__ == "__main__":
    main()
