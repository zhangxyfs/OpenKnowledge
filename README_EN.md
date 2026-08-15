<p align="center">
  <img src="docs/assets/logo.svg" alt="OpenKnowledge" width="580">
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <b>English</b> · <a href="docs/ARCHITECTURE.md">Architecture</a> · <a href="docs/changelogs/">Changelogs</a>
</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-2.16.0-2563eb">
  <img alt="go" src="https://img.shields.io/badge/go-%3E%3D1.25-00ADD8">
  <img alt="platform" src="https://img.shields.io/badge/platform-windows%20%7C%20linux-0078d6">
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-green"></a>
</p>

<p align="center">
  A <b>project knowledge base</b> for AI coding assistants — knowledge is isolated per project and injected into the AI's context<br>
  through each assistant's hooks/extensions, and it can enforce workflow rules like "no code change without a changelog entry".<br>
  Single-binary Go CLI (<code>ok</code>) with zero runtime dependencies.
</p>

---

## Contents

- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Web GUI](#web-gui)
- [Knowledge entries](#knowledge-entries)
- [Common commands](#common-commands)
- [Configuration](#configuration)
- [Retrieval algorithm](#retrieval-algorithm)
- [How it works](#how-it-works)
- [Development](#development)

## Features

| Feature | Description |
|---------|-------------|
| **Base injection** | On the first question of each session, the project's mandatory entries (full text) plus the knowledge index are sent to the AI |
| **Retrieval injection** | Every question is matched by hybrid keyword + vector-semantic retrieval, injecting the most relevant entries (e.g. "git commit conventions") |
| **Three semantic-retrieval forms** | Hosted OpenAI-compatible / local Ollama / built-in llama.cpp on-device model (works offline, knowledge never leaves the machine), managed in one GUI dialog |
| **Enforcement** | Tracks files the AI modified; if code changed but no changelog was written by the end of the turn, the turn is blocked until it's fixed (at most once per rule per session) |
| **Multi-agent support** | Kimi Code, Pi, ZCode, Reasonix, opencode, Claude Code/CodePilot, Codex, Qoder CN and DeepSeek Harness share the same knowledge base (extensible adapter architecture) — kimi via TOML hook marker blocks, pi via a TypeScript extension, zcode via the Claude JSON protocol, reasonix via an Extension Protocol sidecar, opencode via a TypeScript plugin, claude via a merged hooks write to ~/.claude/settings.json (shared by Claude Code, CodePilot and other compatible hosts), codex via a merged hooks write to ~/.codex/hooks.json (Claude-compatible hook contract; zero skill adaptation via the shared ~/.agents/skills), qoder via a merged hooks write to ~/.qoder-cn/settings.json (Claude-compatible hook contract plus the hooksConfig.enabled switch; zero skill adaptation via ~/.qoder-cn/skills; covers the terminal CLI), qoder-ide via a merged hooks write to ~/.lingma/settings.json (Qoder CN IDE Lingma core: injection and touch tracking work, Stop is not blockable so enforcement degrades; skills go to ~/.lingma/skills; an IDE restart is required), dsh via a local JS plugin (mounted by absolute path through the home-level cordis.patch.yml; skills shared via ~/.agents/skills) |
| **One-step setup** | `ok setup` configures hooks, installs skills, and sets up embeddings |
| **Switch anytime** | `ok off` disables globally, `ok on` re-enables |
| **Web GUI & resident daemon** | Double-click the exe or run `ok gui` to open the admin UI; a single resident `ok.exe daemon` process (auto-starts at login) serves both the GUI and every agent's hook requests — millisecond forwarding, no more process-per-session |

## Installation

Installer packages are ~50MB — they bundle the llama.cpp CPU runtime (used by the built-in on-device embedding model); models are not bundled and are downloaded on first activation.

### Option A: Windows installer (recommended for end users)

Just run the released `OpenKnowledgeSetup-<version>.exe` — no Go toolchain, no build steps. Installs to `%LOCALAPPDATA%\Programs\OpenKnowledge` by default (no admin rights needed), with optional desktop shortcut / PATH entry / first-run wizard. Uninstall keeps knowledge-base data by default (interactive uninstall offers deletion; silent uninstall always keeps it).

<details>
<summary>Maintainers: build the installer yourself</summary>

```bash
bash scripts/build-installer.sh   # produces installer/output/OpenKnowledgeSetup-<version>.exe
```

</details>

### Option B: Linux (amd64)

Two formats (both dependency-free, statically compiled):

| Format | How to install |
|--------|----------------|
| `openknowledge_<version>_linux_amd64.tar.gz` | Extract, then `cd openknowledge_* && ./ok setup` (hooks/skills + login autostart; the autostart Exec points at the extracted directory — uninstall or re-run setup before deleting it) |
| `openknowledge_<version>_amd64.deb` | `sudo dpkg -i` installs to `/usr/lib/openknowledge/` (`ok` lands in PATH), then run `ok setup` |

<details>
<summary>Maintainers: build the Linux packages</summary>

```bash
bash scripts/build-linux.sh   # .deb requires: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
```

</details>

### Option C: Manual build

```bash
# 1. Build (Go ≥ 1.25)
go build -o ok.exe ./cmd/ok        # Windows
go build -o ok ./cmd/ok            # Linux/macOS

# 2. First-run wizard (idempotent, safe to re-run)
./ok.exe setup
```

`ok setup` performs three steps, in order:

1. **Writes hook configurations** for every detected AI assistant — for kimi that's 3 hook marker blocks in `~/.kimi-code/config.toml` (idempotent, backs up the original; existing ok hooks are detected and overwritten with the current exe path, never duplicated); pi gets a TypeScript extension; zcode gets a merged `config.json` write; opencode gets a TypeScript plugin in `~/.config/opencode/plugins/`; claude gets a merged hooks write to `~/.claude/settings.json`; codex gets a merged hooks write to `~/.codex/hooks.json` (ok auto-enables the feature flag and writes trust records — exe migrations no longer break trust; verified working on desktop app 26.707 and CLI 0.147+); qoder gets a merged hooks write to `~/.qoder-cn/settings.json` (ok auto-enables the hooksConfig.enabled switch — hooks are silently not dispatched otherwise; Windows commands go through .cmd wrappers to dodge the cmd /s quote-stripping); qoder-ide gets a merged hooks write to `~/.lingma/settings.json` (Qoder CN IDE Lingma core: Stop is not blockable so enforcement degrades; an IDE restart is required); dsh gets a local JS plugin at `<dsh home>/plugins/openknowledge/index.js` mounted via a marker block in the home-level `cordis.patch.yml`. Use `ok setup --agent <id>` to target one agent only
2. **Installs the six skills** `openknowledge-init / on / off / propose / capture / wiki` into each agent's skills directory (kimi/pi/opencode/codex/dsh share `~/.agents/skills/`; zcode uses `~/.zcode/skills`; claude uses `~/.claude/skills`; qoder uses `~/.qoder-cn/skills`; qoder-ide uses `~/.lingma/skills`)
3. **Configures embeddings** — pick one of three forms: hosted OpenAI-compatible service (base_url / model / API key, key optional), local Ollama (key-free, model list auto-detected), or the built-in on-device model (auto-download, fully offline; press Enter to skip and use keyword-only retrieval), writes the global config and verifies connectivity on the spot

## Quick start

```bash
# Inside the project that should use the knowledge base
cd /your/project
ok init                      # register the project (name defaults to the directory name)
ok add --title "Changelog enforcement rule" --type rule --mandatory --file rule.md
ok add --title "Git commit conventions" --type note --tags git --file git.md
ok index                     # sync index & vectors once embedding is configured
```

Then **start a new AI assistant session** (hooks load at session start) and it just works:

- Ask "what are the git commit conventions" → the AI answers quoting the knowledge base
- AI changed code without a changelog → the turn ends with a blocking reminder

You can also say "initialize the knowledge base" or "disable knowledge-base hooks" in a session — the corresponding skills run automatically.

## Web GUI

**Double-click `ok.exe` (no arguments) or run `ok gui`** to launch the web admin UI: the browser opens `http://127.0.0.1:17888` (the fixed port doubles as the single-instance lock; the access token is injected into the served page and never appears in the URL). The GUI is hosted by the daemon — closing the page or window does not stop the process; use `ok daemon stop` to stop the resident service.

<p align="center">
  <img src="docs/assets/gui-manage.png" alt="Manage tab" width="860"><br>
  <sub>Manage tab: project / entry list, search preview and the global switch</sub>
</p>

<p align="center">
  <img src="docs/assets/gui-guide.png" alt="Guide tab" width="860"><br>
  <sub>Guide tab: one-click hooks, skills and embedding setup</sub>
</p>

<p align="center">
  <img src="docs/assets/gui-misc.png" alt="Misc tab" width="860"><br>
  <sub>Misc tab: data export / import, changelogs and project deletion</sub>
</p>

| Tab | What it offers |
|-----|----------------|
| **Manage** | Project/entry list — create, edit, delete entries, search preview, global switch. The project dropdown is sorted by most recent knowledge update; entry rows carry ⎇born-branch / ⇢scope-branch dual badges plus a branch filter; 12 entries per page; the summary column clamps to two lines with a hover tooltip showing the full text; the "Refresh" button re-fetches everything (projects + entries). The tab is hidden automatically on first use (hooks not installed) |
| **Guide** | One-click hooks/skills/embedding setup (the graphical equivalent of `ok setup`), a configurable hook timeout, the three-mode "enforcement" card (reasonix only), and an "Uninstall" card that removes all integrations (knowledge data is kept). Leads into the Manage tab when done |
| **Misc** | Data export/import (knowledge-base zip backup & restore, with same-name overwrite and index rebuild), changelog and user-guide entries, **Delete project knowledge base** (triple confirmation: impact summary + pre-delete zip backup checked by default + ticking "I understand" and typing the full project name to unlock), version and project count |

The GUI needs the `web/` directory next to `ok.exe` (or in the current directory). The release build script produces both:

```bash
bash scripts/build-dist.sh   # produces dist/ok.exe + dist/web/
```

## Knowledge entries

Each entry is a Markdown file with frontmatter (created via `ok add`, or handwritten):

```markdown
---
title: Changelog enforcement rule
type: rule              # rule | pitfall | note | reference
tags: [changelog, workflow]
mandatory: true         # true = full-text injection on the first question of every session
summary: Every code change must be accompanied by a changelog entry
---

Body (free-form Markdown)
```

Data lives in `~/.openknowledge/` (centralized storage — project repositories stay clean).

**Draft flow (AI proposes, human approves)**: the AI can record session learnings as **draft entries** via `ok propose` (frontmatter `draft: true`) — drafts are excluded from retrieval and injection, and only appear in `ok list` and the GUI Manage tab (with a "draft" badge). A human promotes them with `ok approve <file>` or the GUI's "Approve" button.

The capture mode is switched with `ok capture propose|auto`: `propose` means the AI volunteers drafts (default, no turn limit); `auto` makes the Stop hook force self-reflection every N turns, with the interval configured via `ok capture interval <n>` (only effective in auto mode).

<p align="center">
  <img src="docs/assets/ai-session.png" alt="Knowledge capture in a real AI session" width="860"><br>
  <sub>In a real session: after finishing a release, the AI captures wiki entries on its own and asks whether to record the pitfall separately (propose mode)</sub>
</p>

## Common commands

| Command | Purpose |
|---------|---------|
| `ok setup` | First-run wizard: hooks + skills + embedding configuration |
| `ok gui` | Launch the web admin UI (same as double-clicking the exe) |
| `ok daemon [stop]` | Resident process hosting the GUI and hook forwarding (auto-starts at login; rarely needs manual action) |
| `ok init [name]` | Register the current project (name defaults to the directory base name); also idempotently writes/updates hook configuration |
| `ok add` | Create a knowledge entry (auto-rebuilds index & vectors) |
| `ok propose` | AI proposes a draft entry (excluded from retrieval until approved) |
| `ok approve <file>` | Promote a draft (syncs index & vectors) |
| `ok backfill-born` | Backfill the born branch provenance tag on existing entries (writes after preview confirmation; never overwrites) |
| `ok capture [propose\|auto\|interval <n>]` | Show/switch capture mode; configure the turn interval |
| `ok wiki status` / `mark` / `base` / `diff` | Wiki status (JSON) / record cursor / view or set the base branch / emit branch-delta material |
| `ok search <term>` | Preview retrieval results from the CLI |
| `ok index` | Sync index & vectors (run after hand-editing entries; with kb.db deleted it is a full rebuild) |
| `ok list` | List projects and entries |
| `ok doctor` | Health check: config, embedding connectivity, hook status |
| `ok on` / `ok off` | Global switch (default on) |

## Configuration

Effective config = built-in defaults ← global `~/.openknowledge/config.toml` ← per-project `~/.openknowledge/projects/<name>/config.toml` (each layer overrides the previous).

```toml
# Global config (ok setup can write this interactively; the GUI guide-tab dialog manages multiple profiles)
[embedding]
active = "default"                 # active profile name; empty = keyword-only retrieval

[[embedding.profiles]]             # form 1: hosted/self-hosted OpenAI-compatible service
name = "default"
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = "sk-..."                 # or use api_key_env to point at an environment variable; may be empty for key-less local services
model = "text-embedding-3-small"

# [[embedding.profiles]]           # form 2: local/LAN Ollama (key-free)
# name = "ollama"
# type = "ollama"
# base_url = "http://127.0.0.1:11434"
# model = "bge-m3"

# [[embedding.profiles]]           # form 3: built-in llama.cpp on-device model (offline, installer builds only)
# name = "builtin"
# type = "builtin"
# model = "qwen3-emb-0.6b-q8"      # one of 4 manifest tiers; download via GUI/CLI, then activate
# mirror = "hf-mirror"

# Project config: enforcement rule example
[[enforce]]
type = "changelog_required"
code_globs = ["**/*.go"]                  # touching these = changed code
changelog_glob = "docs/changelogs/**"     # touching these = wrote a changelog
message = "Code was changed this session without a changelog update; please add one first."
```

## Retrieval algorithm

**What**: hybrid retrieval on SQLite + FTS5 —

- **Keyword channel**: FTS5 full-text index + BM25 scoring (rare terms weigh more, long docs get no advantage), weighted across title/tags/summary/body; Chinese uses bigram tokenization, zero dependencies
- **Semantic channel**: embeddings + cosine similarity, recalling entries that "ask differently but mean the same"; three forms to choose from — hosted OpenAI-compatible / local Ollama / built-in llama.cpp on-device model (works offline, knowledge never leaves the machine; switching models triggers an automatic vector rebuild on `ok index`)
- The two scores are normalized and blended (α/β tunable), top-2 injected (`top_n` configurable)
- **Draft entries (from `ok propose`) stay out of both retrieval channels**: excluded from FTS and vectors until approved

**Why**: keyword and semantic signals complement each other — that's the baseline of retrieval quality. SQLite (pure-Go port, no CGO) carries the index instead of a vector DB / search service to preserve the "single binary, zero infrastructure" deployment shape — one `kb.db` file is everything.

**What you get**: ~30ms per query over 10k entries (~36ms on the hot path including index sync); when the embedding service is down it degrades to keyword-only retrieval and injection never goes missing. See [ARCHITECTURE §17](docs/ARCHITECTURE.md#17-检索算法实现深度) (Chinese).

## How it works

Using Kimi Code as the example, the assistant calls `ok` at three moments (other agents trigger the equivalents through their own adapters):

| Hook | When it runs | ok's internal preconditions | Effect |
|------|--------------|------------------------------|--------|
| `UserPromptSubmit` | Every user message, before the model call | global switch on + directory registered | First question: mandatory entries + index; every question: retrieval injection |
| `PostToolUse` (matcher `Write\|Edit`) | After the AI successfully writes/edits a file (not on failure) | switch + registered + project-relative path parseable | Records the touched file into session state |
| `Stop` | When the AI's turn is about to end (not on Esc interruption) | switch + registered + `[[enforce]]` configured + rule conditions met | Code changed without changelog → exit 2 block (at most once per rule per session) |

All hook paths are fail-open: any internal error is only logged (`~/.openknowledge/ok.log`) and never disrupts the session. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) (Chinese).

## Development

```bash
go test ./...    # all 25 packages (no network; includes end-to-end)
go vet ./...
```

| Document | Location |
|----------|----------|
| Architecture | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) (Chinese) |
| Design doc | `docs/superpowers/specs/2026-07-22-openknowledge-design.md` (Chinese) |
| Implementation plan | `docs/superpowers/plans/2026-07-22-openknowledge.md` (Chinese) |
| Changelogs | [docs/changelogs/](docs/changelogs/) (Chinese) |
