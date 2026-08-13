<!--
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
-->
# Presentation decks

Slide decks authored as [Marp](https://marp.app/) Markdown, so they live beside the
documentation they are drawn from and can be regenerated as the code evolves.

| Deck | Audience | Contents |
|------|----------|----------|
| [committer-technical-overview.md](committer-technical-overview.md) | Engineers already familiar with Hyperledger Fabric | 33 pages in five sections: context and how it differs from the classic Fabric peer, transaction flow, all six components, cross-cutting concerns, reference |

It is written as a **reference document** rather than a talk: a contents page with a topic
index, a divider opening each section, and every page self-contained enough to answer a
question on its own.

## Building

```bash
./build.sh                                    # every deck in this directory
./build.sh committer-technical-overview.md    # one deck
```

Output goes to `out/` (git-ignored). On first run the script installs Marp into
`.marp/` (also git-ignored); afterwards builds take a few seconds.

HTML export needs only Node.js. **PDF export additionally needs a Chromium-family
browser** — without one, the script still writes the HTML and tells you how to get the
PDF. To enable it:

```bash
sudo dnf install -y chromium-headless    # RHEL/Fedora (EPEL)
sudo apt-get install -y chromium         # Debian/Ubuntu
```

Alternatively, open the HTML in a browser and print to PDF — the HTML is self-contained.

Set `MARP_BIN` to use a Marp binary you already have.

## Finding things

- **Page 2** is the contents: five sections with their starting pages, plus a
  "find a topic" index for the questions that come up most (dependency graph, MVCC,
  schema, idempotency, policies, recovery, APIs, ports, code map)
- **Each section opens with a divider** listing every page in that section
- **PDF bookmarks** mirror the structure — section titles at the top level, page
  titles nested beneath, so the sidebar is a full table of contents
- **Every page cites its source doc** in the bottom-left, so any statement can be traced
  back to the authoritative documentation
- **Running labels** in the top-left of each page name the section and component

Most pages also carry additional notes — the reasoning behind a design choice, common
questions, and caveats worth knowing. These are HTML comments in the Markdown source; in
the HTML build they are visible via `?view=presenter`.

## Authoring notes

Three Marp-specific traps, each of which fails silently:

1. **Front matter must be the very first thing in the file.** A license header in an
   HTML comment above it stops Marp parsing the front matter, and the whole theme is
   rendered as body text instead of applied. The license therefore lives inside the
   front matter as YAML comments.
2. **No blank lines inside an inline `<svg>` block.** A blank line ends the Markdown
   HTML block, so the rest of the SVG leaks out as body text.
3. **No HTML elements inside SVG `<text>`.** Tags such as `<strong>` are on the HTML
   parser's foreign-content breakout list and terminate the SVG. Use
   `<tspan font-weight="600">` instead.

Marp also renders single newlines as `<br>`, so a wrapped paragraph in the source
breaks at exactly those points. Keep each prose paragraph on one line and let it wrap.
