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
from collections import defaultdict, deque
from dataclasses import dataclass
from pathlib import Path


@dataclass
class EdgeMetadata:
    """Metadata for a dependency edge.

    When both is_indirect and is_toolchain are False, the edge is a direct dependency.
    """
    is_indirect: bool  # True if this is an indirect dependency
    is_toolchain: bool  # True if this edge involves a toolchain package


@dataclass
class DependencyGraph:
    """Represents the dependency graphs with edge metadata.

    All edges are included in the graphs. Use edge_metadata to determine
    if an edge is direct, indirect, or involves a toolchain.
    """
    forward: dict  # module -> set of all dependencies
    reverse: dict  # module -> set of all dependents
    edge_metadata: dict  # (from, to) -> EdgeMetadata


@dataclass
class ChainInfo:
    """Information about a single dependency chain with its blame point."""
    blame_point: str
    root_to_blame: list[list[str]]
    blame_to_target: str


@dataclass
class BlameAnalysis:
    """Result of blame point analysis for dependency chains."""
    type: str  # 'single', 'convergence', 'divergence', or 'multiple'
    blame_point: str | None = None  # Single blame point (for single/convergence/divergence)
    root_to_blame: list[list[str]] | None = None  # Paths from root to blame point
    blame_to_target: str | list[str] | None = None  # Path(s) from blame to target
    chains: list[ChainInfo] | None = None  # Multiple independent chains


@dataclass
class PackageBlameInfo:
    """Information about a bad package and its blame analysis."""
    package: str
    blame_analysis: BlameAnalysis | None
    chains: list[list[str]]


class ModuleDependencyCache:
    """Cache for module dependencies to avoid re-parsing go.mod files."""

    def __init__(self, project_root):
        self.project_root = Path(project_root)
        self._cache = {}  # module_path -> {dependency: is_direct}
        self._module_name = None  # Lazy-loaded module name

    def _get_module_name(self):
        """Get the current module name (cached)."""
        if self._module_name is None:
            self._module_name = get_module_name(self.project_root)
        return self._module_name

    def get_dependencies(self, module_path):
        """
        Get all dependencies (direct and indirect) for a module.
        Returns a dict: {dependency_path: is_direct (True/False)}
        """
        if module_path in self._cache:
            return self._cache[module_path]

        # Special case: if this is the root module, read go.mod directly
        if module_path == self._get_module_name():
            go_mod_path = self.project_root / 'go.mod'
            if not go_mod_path.exists():
                self._cache[module_path] = {}
                return {}
        else:
            # Run go mod download to ensure the module is in cache
            try:
                result = run(['go', 'mod', 'download', '-json', module_path], self.project_root)
            except subprocess.CalledProcessError:
                # Module download failed, cache empty result
                self._cache[module_path] = {}
                return {}

            if not result.strip():
                # Empty result, cache empty
                self._cache[module_path] = {}
                return {}

            mod_info = json.loads(result)
            go_mod_path = Path(mod_info.get('GoMod', ''))

            if not go_mod_path or not go_mod_path.exists():
                self._cache[module_path] = {}
                return {}

        # Parse the go.mod file
        with open(go_mod_path, 'r') as f:
            content = f.read()

        dependencies = {}

        # Look for dependencies in require blocks
        require_pattern = r'require\s*\((.*?)\)'
        matches = re.findall(require_pattern, content, re.DOTALL)

        for match in matches:
            for line in match.split('\n'):
                line = line.strip()
                if not line or line.startswith('//'):
                    continue

                parts = line.split()
                if len(parts) >= 2:
                    pkg = parts[0]
                    is_indirect = '// indirect' in line
                    dependencies[pkg] = not is_indirect  # True if direct, False if indirect

        # Also check single-line requires
        single_require_pattern = r'require\s+(\S+)\s+\S+(.*)'
        for line_match in re.finditer(single_require_pattern, content):
            pkg = line_match.group(1)
            rest_of_line = line_match.group(2)
            is_indirect = '// indirect' in rest_of_line
            dependencies[pkg] = not is_indirect

        # Cache the result
        self._cache[module_path] = dependencies
        return dependencies

    def is_direct_dependency(self, module_path, dependency):
        """Check if a dependency is direct (not indirect) in a module's go.mod file."""
        dependencies = self.get_dependencies(module_path)
        return dependencies.get(dependency)  # Returns True, False, or None

    def size(self):
        """Return the number of cached modules."""
        return len(self._cache)


class BadImportFinder:
    def __init__(self, project_root, bad_imports: list[str]):
        self.project_root = Path(project_root)
        self.bad_imports = bad_imports

        self.import_cache = {}  # Cache of package -> locations
        self.similar_packages = defaultdict(list)  # Cache of package suffix -> full packages
        self.direct_deps, self.indirect_deps = parse_dependencies(project_root)
        self.module_name = get_module_name(project_root)
        self.dependency_graph = build_dependency_graph(project_root)

        self.direct_bad = []
        self.indirect_bad = []
        self.not_in_mod = []

        for bad_import in sorted(self.bad_imports):
            if bad_import in self.direct_deps:
                self.direct_bad.append(bad_import)
            elif bad_import in self.indirect_deps:
                self.indirect_bad.append(bad_import)
            else:
                self.not_in_mod.append(bad_import)

    def find_import_in_code(self, package):
        """Find where a package is imported in Go source files."""
        # Check cache first (but only if it's been populated)
        if package in self.import_cache:
            return self.import_cache[package]

        locations = []

        # Search for import statements in .go files
        for go_file in self.project_root.rglob("*.go"):
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

                    rel_path = go_file.relative_to(self.project_root)
                    locations.append({
                        'file': str(rel_path),
                        'line': line_num,
                        'import': full_import
                    })

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
                                rel_path = go_file.relative_to(self.project_root)
                                locations.append({
                                    'file': str(rel_path),
                                    'line': block_start + line_offset,
                                    'import': full_import
                                })

        # Cache the result
        self.import_cache[package] = locations

        # Also cache by suffix for similar package lookup
        pkg_parts = package.split('/')
        if len(pkg_parts) >= 2:
            suffix = '/'.join(pkg_parts[-2:])
            self.similar_packages[suffix].append(package)

        return locations

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

                if edge_key in self.dependency_graph.edge_metadata:
                    metadata = self.dependency_graph.edge_metadata[edge_key]
                    annotations = []

                    # Toolchain takes precedence over indirect
                    # (toolchain dependencies are inherently indirect)
                    if metadata.is_toolchain:
                        annotations.append("toolchain")
                    elif metadata.is_indirect:
                        annotations.append("indirect")

                    if annotations:
                        parts.append(f" *({', '.join(annotations)})*")

                parts.append(" -> ")

        # Remove trailing " -> " if present
        result = ''.join(parts)
        if result.endswith(" -> "):
            result = result[:-4]

        return result

    def find_dependency_chain(self, indirect_package):
        """Find which packages import an indirect package and trace back to our code.

        First tries to find paths using only direct dependencies.
        If no paths found, falls back to using all dependencies (direct + indirect).
        """
        # Check if we have the dependency graph
        if not self.dependency_graph or not self.module_name:
            return []

        # Try with direct dependencies first
        chains = self._find_chains_with_graph(
            indirect_package,
            self.dependency_graph.forward,
            self.dependency_graph.reverse
        )

        # If no chains found with direct dependencies, try with all dependencies
        if not chains:
            chains = self._find_chains_with_graph(
                indirect_package,
                self.dependency_graph.forward,
                self.dependency_graph.reverse
            )

        return chains

    def _find_chains_with_graph(self, indirect_package, graph, reverse_graph):
        """Helper method to find dependency chains using a specific graph."""

        # Find all packages that directly import the target
        # Filter out our own module from importers (these are just go.mod entries)
        direct_importers = reverse_graph.get(indirect_package, set()) - {self.module_name}

        # For each importer, check if there's a direct edge from our module to it in the graph
        # This includes both direct and indirect dependencies in go.mod
        direct_paths = []
        for importer in sorted(direct_importers):
            # Check if there's a direct edge in the graph
            if importer in graph.get(self.module_name, []):
                # Create the path: module -> importer -> target
                direct_paths.append([self.module_name, importer, indirect_package])

        # BFS to find ALL paths from module to target through each importer
        # We want to find longer paths that show the complete dependency chain
        def find_paths_through_importers(start, target, max_total_paths=50):
            in_all_paths = []
            paths_found = 0

            # For each package that imports the target (sorted for determinism)
            for in_importer in sorted(direct_importers):
                if paths_found >= max_total_paths:
                    break

                # Find paths from our module to this importer
                queue = deque([(start, [start])])
                visited = set()  # Track visited nodes, not paths
                importer_paths = []
                max_per_importer = min(10, max_total_paths - paths_found)

                while queue and len(importer_paths) < max_per_importer:
                    current, path = queue.popleft()

                    # Check if we reached the importer
                    if current == in_importer:
                        # Add the final hop to target
                        full_path = path + [target]
                        importer_paths.append(full_path)
                        continue

                    # Skip if already visited from a shorter path
                    if current in visited:
                        continue
                    visited.add(current)

                    # Don't go too deep
                    if len(path) > 6:
                        continue

                    # Explore neighbors (sorted for determinism)
                    for neighbor in sorted(graph.get(current, [])):
                        if neighbor not in path:  # Avoid cycles
                            queue.append((neighbor, path + [neighbor]))

                in_all_paths.extend(importer_paths)
                paths_found += len(importer_paths)

            return in_all_paths

        chains = []
        if self.module_name and direct_importers:
            # Start with direct paths (module -> direct_dep -> target)
            paths = direct_paths.copy()

            # Find all paths through BFS
            all_paths = find_paths_through_importers(self.module_name, indirect_package)
            paths.extend(all_paths)

            # Keep only paths with length >= 2 (module -> target or module -> dep -> target)
            # No need to validate edges since the graph already contains only direct dependencies
            valid_paths = [path for path in paths if len(path) >= 2]

            # Group chains by suffix (path from direct_dep to target)
            # Chains with the same suffix reach the target through the same intermediate packages
            chains_by_suffix: dict[tuple[str, ...], list[str]] = {}
            for path in valid_paths:
                # Suffix is everything after the module name: [direct_dep, ..., target]
                suffix = tuple(path[1:])
                if suffix not in chains_by_suffix:
                    chains_by_suffix[suffix] = path

            # Convert to DependencyChain objects (sorted for determinism)
            for suffix in sorted(chains_by_suffix.keys()):
                chains.append(chains_by_suffix[suffix])

        # Sort by path length (shortest first to show most direct paths)
        chains.sort(key=lambda x: len(x))

        return chains

    def analyze_and_group_by_blame(self) -> tuple[dict[str, list[PackageBlameInfo]], list[PackageBlameInfo]]:
        """Analyze all bad imports and group them by blame point.

        Returns:
            A tuple of (blame_groups, no_blame_packages) where:
            - blame_groups: dict mapping blame_point -> list of PackageBlameInfo
            - no_blame_packages: list of PackageBlameInfo without clear blame point
        """
        all_deps = self.direct_bad + self.indirect_bad
        blame_groups: dict[str, list[PackageBlameInfo]] = defaultdict(list)
        no_blame_packages: list[PackageBlameInfo] = []

        for pkg in sorted(all_deps):
            chains = self.find_dependency_chain(pkg)

            if chains:
                blame_analysis = find_blame_point_and_split_chains(chains)
                if blame_analysis:
                    package_info = PackageBlameInfo(
                        package=pkg,
                        blame_analysis=blame_analysis,
                        chains=chains
                    )

                    # Handle different blame analysis types
                    if blame_analysis.type == 'multiple' and blame_analysis.chains:
                        # For multiple independent chains, add the package to EACH blame point
                        # This ensures all importers are shown
                        for chain_info in blame_analysis.chains:
                            if chain_info.blame_point:
                                blame_groups[chain_info.blame_point].append(package_info)
                    else:
                        # Single blame point (convergence, divergence, or single chain)
                        primary_blame = blame_analysis.blame_point
                        if primary_blame:
                            blame_groups[primary_blame].append(package_info)
                        else:
                            no_blame_packages.append(package_info)
                else:
                    no_blame_packages.append(PackageBlameInfo(pkg, None, chains))
            else:
                no_blame_packages.append(PackageBlameInfo(pkg, None, []))

        return blame_groups, no_blame_packages

    def generate_report_grouped(self):
        """Generate a comprehensive report of bad imports grouped by blame point."""
        # Analyze and group by blame point
        blame_groups, no_blame_packages = self.analyze_and_group_by_blame()

        all_deps = self.direct_bad + self.indirect_bad

        # Separate direct bad imports (where this repo is to blame)
        # A package is a "direct bad import" if it's imported directly in THIS repo's code
        direct_bad_imports = []
        indirect_blame_groups = {}

        if all_deps:
            # Check each blame group (sorted for determinism)
            for blame_point, packages in sorted(blame_groups.items()):
                # Check if the blame point is this repo (meaning we directly import these packages)
                if blame_point == self.module_name:
                    # This repo is the blame point - these are direct bad imports
                    direct_bad_imports.extend(packages)
                else:
                    # External blame point - check if any packages are actually imported in our code
                    for info in packages:
                        # Check if this package is actually imported in our code
                        locations = self.find_import_in_code(info.package)
                        if locations:
                            # Package is imported in our code - add to direct imports
                            direct_bad_imports.append(info)

                    # Always add to indirect blame groups (even if some are also direct)
                    # This shows the complete picture of where packages come from
                    indirect_blame_groups[blame_point] = packages

        # Report direct bad imports first (THIS REPO IS TO BLAME)
        if direct_bad_imports:
            # Deduplicate direct bad imports by package name
            seen_direct = set()
            unique_direct = []
            for info in direct_bad_imports:
                if info.package not in seen_direct:
                    seen_direct.add(info.package)
                    unique_direct.append(info)

            print(f"---\n")
            print(f"## ⚠️ DIRECT UNMAINTAINED IMPORTS ({len(unique_direct)} found)\n")
            print(f"**🔴 THIS REPOSITORY IS TO BLAME**\n")
            print(f"These packages are directly imported in your code and should be removed.\n")

            for info in sorted(unique_direct, key=lambda x: x.package):
                print(f"- **📦 {info.package}**\n")

                # Find where it's imported
                locations = self.find_import_in_code(info.package)
                if locations:
                    print(f"  **Imported at:**\n")
                    for loc in locations[:3]:
                        print(f"  - `{loc['file']}:{loc['line']}`")
                    if len(locations) > 3:
                        print(f"  - ... and {len(locations) - 3} more location(s)")
                    print()
                else:
                    print(f"  *(Import location not found in code)*\n")

        # Report indirect bad imports grouped by blame point
        if indirect_blame_groups:
            # Deduplicate packages within each blame group (sorted for determinism)
            for blame_point in sorted(indirect_blame_groups.keys()):
                seen = set()
                unique_packages = []
                for info in indirect_blame_groups[blame_point]:
                    if info.package not in seen:
                        seen.add(info.package)
                        unique_packages.append(info)
                indirect_blame_groups[blame_point] = unique_packages

            # Filter out blame points that are themselves bad imports
            # A bad import cannot be a blame point for other bad imports
            filtered_blame_groups = {}
            for blame_point, packages in sorted(indirect_blame_groups.items()):
                if blame_point not in self.bad_imports:
                    filtered_blame_groups[blame_point] = packages

            indirect_blame_groups = filtered_blame_groups

            for blame_point in sorted(indirect_blame_groups.keys()):
                packages = indirect_blame_groups[blame_point]
                print(f"---\n")
                print(f"## 🎯 BLAME POINT: `{blame_point}`")
                print(f"Responsible for {len(packages)} unmaintained import(s)\n")

                # Show unmaintained imports FIRST
                print(f"- **⚠️  Unmaintained imports:**\n")

                # Show each bad package under this blame point
                for info in packages:
                    print(f"  - **📦 {info.package}**")

                    if info.blame_analysis:
                        # Show path from blame point to this bad import
                        if info.blame_analysis.type == 'single':
                            formatted_path = self.format_path_with_metadata(info.blame_analysis.blame_to_target)
                            print(f"    - Path: {formatted_path}")
                        elif info.blame_analysis.type in ['convergence', 'divergence']:
                            if isinstance(info.blame_analysis.blame_to_target, str):
                                formatted_path = self.format_path_with_metadata(info.blame_analysis.blame_to_target)
                                print(f"    - Path: {formatted_path}")
                            else:
                                print(f"    - Paths:")
                                for path in info.blame_analysis.blame_to_target[:2]:
                                    formatted_path = self.format_path_with_metadata(path)
                                    print(f"      - {formatted_path}")
                                if len(info.blame_analysis.blame_to_target) > 2:
                                    print(f"      - ... and {len(info.blame_analysis.blame_to_target) - 2} more")
                        elif info.blame_analysis.type == 'multiple' and info.blame_analysis.chains:
                            # Find chains for this blame point
                            relevant_chains = [c for c in info.blame_analysis.chains if c.blame_point == blame_point]
                            if relevant_chains:
                                formatted_path = self.format_path_with_metadata(relevant_chains[0].blame_to_target)
                                print(f"    - Path: {formatted_path}")
                    print()

                # Collect all unique paths from root to blame point
                all_paths_to_blame = set()
                all_direct_deps = set()
                for info in packages:
                    if info.blame_analysis:
                        # Handle different blame analysis types
                        if info.blame_analysis.type == 'multiple' and info.blame_analysis.chains:
                            # For multiple chains, only include paths that go through THIS blame point
                            for chain_info in info.blame_analysis.chains:
                                if chain_info.blame_point == blame_point and chain_info.root_to_blame:
                                    for path in chain_info.root_to_blame:
                                        all_paths_to_blame.add(tuple(path))
                                        if len(path) >= 2:
                                            all_direct_deps.add(path[1])
                        elif info.blame_analysis.root_to_blame:
                            # For single/convergence/divergence, use all paths
                            for path in info.blame_analysis.root_to_blame:
                                all_paths_to_blame.add(tuple(path))
                                if len(path) >= 2:
                                    all_direct_deps.add(path[1])

                # Show paths from root to blame point
                if all_paths_to_blame:
                    print(f"- **📍 Paths from your code to this blame point:**\n")
                    for path in sorted(all_paths_to_blame, key=lambda p: (len(p), p)):
                        formatted_path = self.format_path_with_metadata(path)
                        print(f"  - {formatted_path}")
                    print()

                # Show where direct dependencies are imported
                if all_direct_deps:
                    print(f"- **📝 Direct dependencies imported in your code:**\n")
                    for direct_dep in sorted(all_direct_deps):
                        locations = self.find_import_in_code(direct_dep)
                        if locations:
                            print(f"  - **✓ `{direct_dep}`** is imported at:")
                            for loc in locations[:2]:
                                print(f"    - `{loc['file']}:{loc['line']}`")
                            if len(locations) > 2:
                                print(f"    - ... and {len(locations) - 2} more location(s)")
                        else:
                            print(f"  - `{direct_dep}` (not found in code)")
                    print()

            # Show packages without clear blame point
            if no_blame_packages:
                print(f"---\n")
                print(f"## PACKAGES WITHOUT CLEAR BLAME POINT ({len(no_blame_packages)})\n")

                for info in sorted(no_blame_packages, key=lambda x: x.package):
                    print(f"**📦 Package: `{info.package}`**")
                    if info.chains:
                        print(f"⚠️  Could not determine clear blame point")
                        print(f"Found {len(info.chains)} dependency chain(s)\n")
                    else:
                        print(f"⚠️  Could not determine dependency chain\n")

        # Report on packages not in go.mod
        if self.not_in_mod:
            print(f"---\n")
            print(f"## NOT IN GO.MOD ({len(self.not_in_mod)} found)\n")

            for pkg in sorted(self.not_in_mod):
                locations = self.find_import_in_code(pkg)
                if locations:
                    print(f"- `{pkg}` - found in code at {len(locations)} location(s)")
                else:
                    print(f"- `{pkg}`")
            print()

        # Summary
        print(f"---\n")
        print(f"## SUMMARY\n")
        print(f"**Total unmaintained imports analyzed:** {len(self.bad_imports)}")
        all_deps = self.direct_bad + self.indirect_bad
        print(f"- In go.mod: {len(all_deps)} ({len(self.direct_bad)} direct, {len(self.indirect_bad)} indirect)")
        if direct_bad_imports:
            # Count unique direct unmaintained imports
            unique_direct_count = len(set(info.package for info in direct_bad_imports))
            print(f"- Direct unmaintained imports (this repo to blame): {unique_direct_count}")
        if indirect_blame_groups:
            print(f"- Indirect unmaintained imports grouped by {len(indirect_blame_groups)} external blame point(s)")
        print(f"- Not in go.mod: {len(self.not_in_mod)}")
        print()


def main():
    # Determine project root (where go.mod is)
    if len(sys.argv) < 2:
        print("Usage: python find-bad-imports.py <project_root>")
        sys.exit(1)

    project_root = Path(sys.argv[1]).resolve()

    print("# Unmaintained Imports Report\n", file=sys.stderr)
    print("Loading unmaintained imports...", file=sys.stderr)
    bad_imports = list(iter_bad_imports())

    finder = BadImportFinder(project_root, bad_imports)

    print("Analyzing imports...", file=sys.stderr)

    # Print markdown title
    print("# Unmaintained Imports Report\n")

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


def parse_tool_dependencies(project_root):
    """Parse tool dependencies from go.mod file.

    Returns a set of package paths that are tool dependencies.
    """
    go_mod_path = project_root / 'go.mod'
    if not go_mod_path.exists():
        return set()

    content = go_mod_path.read_text()
    tool_deps = set()

    # Look for tool blocks: tool ( ... )
    matches = re.findall(r'tool\s*\((.*?)\)', content, re.DOTALL | re.MULTILINE)

    for match in matches:
        for line in match.split('\n'):
            line = line.strip()
            if not line or line.startswith('//'):
                continue

            # Tool entries can be just package paths or package/cmd paths
            parts = line.split()
            if parts:
                pkg = parts[0]
                tool_deps.add(pkg)

    return tool_deps


def parse_dependencies(project_root):
    """Parse dependencies using go list -m -json all."""
    output = run(['go', 'list', '-m', '-json', 'all'], project_root).strip()

    # Parse JSON output - it's a stream of JSON objects, not an array
    modules = []
    decoder = json.JSONDecoder()
    idx = 0

    while idx < len(output):
        output_slice = output[idx:].lstrip()
        if not output_slice:
            break
        try:
            obj, end_idx = decoder.raw_decode(output_slice)
            modules.append(obj)
            idx += len(output[idx:]) - len(output_slice) + end_idx
        except json.JSONDecodeError as e:
            print(f"Warning: JSON decode error at position {idx}: {e}", file=sys.stderr)
            break

    indirect_deps = set()
    direct_deps = set()
    # Process modules
    for module in modules:
        path = module.get('Path')
        is_main = module.get('Main', False)
        is_indirect = module.get('Indirect', False)

        # Skip the main module
        if is_main:
            continue

        if path:
            if is_indirect:
                indirect_deps.add(path)
            else:
                direct_deps.add(path)

    return direct_deps, indirect_deps


def build_dependency_graph(project_root):
    """Build the dependency graph with edge metadata for direct/indirect/toolchain."""
    # Parse tool dependencies from go.mod
    tool_deps = parse_tool_dependencies(project_root)
    print(f"Found {len(tool_deps)} tool dependencies", file=sys.stderr)

    # Get the full graph from go mod graph and parse all edges
    all_edges = []
    for line in run(['go', 'mod', 'graph'], project_root).split('\n'):
        if not line.strip():
            continue
        parts = line.split()
        if len(parts) != 2:
            continue
        from_pkg = parts[0].split('@')[0]
        to_pkg = parts[1].split('@')[0]

        # Skip standard library packages (they don't have dots in their names)
        if '.' not in to_pkg:
            continue
        if '.' not in from_pkg:
            continue

        # Check if either package is a tool dependency or starts with 'toolchain'
        # Tool dependencies and their transitive dependencies are marked as toolchain
        from_is_tool = from_pkg in tool_deps or from_pkg.startswith('toolchain')
        to_is_tool = to_pkg in tool_deps or to_pkg.startswith('toolchain')
        is_toolchain_edge = from_is_tool or to_is_tool

        all_edges.append((from_pkg, to_pkg, is_toolchain_edge))

    print(f"Filtering {len(all_edges)} edges to separate direct and indirect dependencies...", file=sys.stderr)

    # Create cache for module dependencies
    cache = ModuleDependencyCache(project_root)

    # Build forward and reverse graphs with all edges
    graph = defaultdict(set)
    reverse_graph = defaultdict(set)
    edge_metadata = {}

    for from_pkg, to_pkg, is_toolchain_edge in all_edges:
        # Add all edges to the graph
        graph[from_pkg].add(to_pkg)
        reverse_graph[to_pkg].add(from_pkg)

        # Check if it's a direct dependency (works for all modules including root)
        is_direct = cache.is_direct_dependency(from_pkg, to_pkg)

        # Store edge metadata (will be updated for toolchain transitive deps)
        # is_indirect is True when the edge is NOT a direct dependency
        edge_metadata[(from_pkg, to_pkg)] = EdgeMetadata(
            is_indirect=not bool(is_direct),
            is_toolchain=is_toolchain_edge
        )

    # Mark all transitive dependencies of tool packages as toolchain
    # Use BFS to find all packages reachable from tool dependencies
    toolchain_packages = set(tool_deps)
    queue = list(tool_deps)
    visited = set(tool_deps)

    while queue:
        current = queue.pop(0)
        for neighbor in sorted(graph.get(current, [])):
            if neighbor not in visited:
                visited.add(neighbor)
                toolchain_packages.add(neighbor)
                queue.append(neighbor)

    print(f"Found {len(toolchain_packages)} packages in toolchain dependency tree", file=sys.stderr)

    # Update edge metadata: mark all edges involving toolchain packages
    for (from_pkg, to_pkg), metadata in edge_metadata.items():
        if from_pkg in toolchain_packages or to_pkg in toolchain_packages:
            metadata.is_toolchain = True

    # Count edge types for reporting
    direct_count = sum(1 for meta in edge_metadata.values() if not meta.is_indirect and not meta.is_toolchain)
    indirect_count = sum(1 for meta in edge_metadata.values() if meta.is_indirect)
    toolchain_count = sum(1 for meta in edge_metadata.values() if meta.is_toolchain)

    print(f"Graph contains {len(edge_metadata)} total edges:", file=sys.stderr)
    print(f"  - {direct_count} direct dependency edges", file=sys.stderr)
    print(f"  - {indirect_count} indirect dependency edges", file=sys.stderr)
    print(f"  - {toolchain_count} toolchain edges", file=sys.stderr)
    print(f"Cached {cache.size()} unique modules (avoided re-parsing)", file=sys.stderr)

    return DependencyGraph(
        forward=graph,
        reverse=reverse_graph,
        edge_metadata=edge_metadata
    )


def find_blame_point_and_split_chains(chains: list[list[str]]) -> BlameAnalysis | None:
    """
    Find the common blame point in chains and split them into:
    1. Paths from root to blame point
    2. Paths from blame point to target

    The blame point can be:
    - Single chain: the first dependency after root
    - Convergence point: where chains converge (common suffix)
    - Divergence point: where chains diverge (common prefix)
    - Multiple independent: each chain has its own blame point

    Returns: BlameAnalysis or None if no analysis possible
    """
    if not chains:
        return None

    # Extract all paths as lists from DependencyChain objects
    all_paths = []
    for chain in chains:
        # Use the path_list field directly from the DependencyChain dataclass
        all_paths.append(chain)

    # Handle single chain case
    if len(chains) == 1:
        path = all_paths[0]
        if len(path) >= 2:
            # Blame point is the first dependency after root
            blame_point = path[1]
            root_to_blame = path[:2]  # root -> blame
            blame_to_target = ' -> '.join(path[1:])  # blame -> ... -> target

            return BlameAnalysis(
                type='single',
                blame_point=blame_point,
                root_to_blame=[root_to_blame],
                blame_to_target=blame_to_target
            )
        return None

    min_len = min(len(p) for p in all_paths)

    # Try to find common suffix (convergence point)
    common_suffix_len = 0
    for i in range(1, min_len):
        elements_at_pos = [p[-i] for p in all_paths]
        if len(set(elements_at_pos)) == 1:
            common_suffix_len = i
        else:
            break

    if common_suffix_len >= 2:
        # Found convergence point - blame point is first element of common suffix
        blame_point = all_paths[0][-common_suffix_len]

        # Split chains into root->blame and blame->target
        root_to_blame_paths: set[tuple] = set()
        blame_to_target_path: list | None = None

        for path in all_paths:
            blame_idx = path.index(blame_point)
            root_to_blame = path[:blame_idx + 1]
            root_to_blame_paths.add(tuple(root_to_blame))

            if blame_to_target_path is None:
                blame_to_target_path = path[blame_idx:]

        if blame_to_target_path is None:
            return None

        return BlameAnalysis(
            type='convergence',
            blame_point=blame_point,
            root_to_blame=[list(p) for p in sorted(root_to_blame_paths, key=len)],
            blame_to_target=' -> '.join(blame_to_target_path)
        )

    # Try to find common prefix (divergence point)
    common_prefix_len = 0
    for i in range(min_len):
        elements_at_pos = [p[i] for p in all_paths]
        if len(set(elements_at_pos)) == 1:
            common_prefix_len = i + 1
        else:
            break

    if common_prefix_len >= 2:
        # Found divergence point - blame point is last element of common prefix
        blame_point = all_paths[0][common_prefix_len - 1]

        # For divergence, root->blame is the same for all paths (the common prefix)
        root_to_blame = all_paths[0][:common_prefix_len]

        # blame->target paths are different for each chain
        blame_to_target_paths = []
        for path in all_paths:
            blame_to_target = path[common_prefix_len - 1:]
            blame_to_target_paths.append(' -> '.join(blame_to_target))

        return BlameAnalysis(
            type='divergence',
            blame_point=blame_point,
            root_to_blame=[root_to_blame],
            blame_to_target=blame_to_target_paths
        )

    # Try to find partial convergence - where most (but not all) chains share a common package
    # Count how many times each package appears in the chains (excluding root and target)
    package_counts = defaultdict(list)  # package -> list of (path_index, position_in_path)
    for path_idx, path in enumerate(all_paths):
        for pos, pkg in enumerate(path[1:-1], 1):  # Skip root and target
            package_counts[pkg].append((path_idx, pos))

    # Find packages that appear in most chains (>= 50% threshold)
    threshold = len(all_paths) * 0.5
    common_packages = [(pkg, occurrences) for pkg, occurrences in package_counts.items()
                       if len(occurrences) >= threshold]

    # Sort by frequency (most common first), then by average position (earlier in path preferred)
    common_packages.sort(key=lambda x: (-len(x[1]), sum(pos for _, pos in x[1]) / len(x[1])))

    if common_packages:
        # Use the most common package as the primary blame point
        primary_blame, primary_occurrences = common_packages[0]
        paths_with_primary = {path_idx for path_idx, _ in primary_occurrences}
        paths_without_primary = set(range(len(all_paths))) - paths_with_primary

        # Group chains by whether they go through the primary blame point
        primary_chains = []
        other_chains = []

        for path_idx, path in enumerate(all_paths):
            if path_idx in paths_with_primary and primary_blame in path:
                # Find where the primary blame point appears
                blame_idx = path.index(primary_blame)
                primary_chains.append(ChainInfo(
                    blame_point=primary_blame,
                    root_to_blame=[path[:blame_idx + 1]],
                    blame_to_target=' -> '.join(path[blame_idx:])
                ))
            else:
                # This chain doesn't go through the primary blame point
                if len(path) >= 2:
                    other_chains.append(ChainInfo(
                        blame_point=path[1],
                        root_to_blame=[path[:2]],
                        blame_to_target=' -> '.join(path[1:])
                    ))

        # If we have a significant primary group, return it with others as secondary
        if len(primary_chains) >= 2:
            all_chain_infos = primary_chains + other_chains
            return BlameAnalysis(
                type='multiple',
                chains=all_chain_infos
            )

    # No meaningful common point found - treat each chain independently
    # Each chain gets its own blame point (first dependency after root)
    chain_infos = []
    for path in all_paths:
        if len(path) >= 2:
            chain_infos.append(ChainInfo(
                blame_point=path[1],
                root_to_blame=[path[:2]],
                blame_to_target=' -> '.join(path[1:])
            ))

    if chain_infos:
        return BlameAnalysis(
            type='multiple',
            chains=chain_infos
        )

    return None


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
