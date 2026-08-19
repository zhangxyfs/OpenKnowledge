<p align="center">
  <img src="docs/assets/logo.svg" alt="OpenKnowledge" width="580">
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <b>English</b> · <a href="docs/ARCHITECTURE.md">Architecture</a> · <a href="docs/changelogs/">Changelogs</a>
</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-2.19.0-2563eb">
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

## Get started in three minutes

### 1. Install

| Platform | How |
|----------|-----|
| Windows | Run `OpenKnowledgeSetup-<version>.exe` (no admin rights; installs to `%LOCALAPPDATA%\Programs\OpenKnowledge`; uninstall keeps knowledge-base data by default) |
| Linux | Extract `openknowledge_<version>_linux_amd64.tar.gz` and run `./ok setup`, or `sudo dpkg -i openknowledge_<version>_amd64.deb` |

> The installer is ~50MB and bundles the llama.cpp CPU runtime (for on-device embeddings); models are downloaded on first activation.

### 2. Open the Web GUI and finish the guided setup

**Double-click `ok.exe` (or run `ok gui`)** — the browser opens the admin UI at `http://127.0.0.1:17888`. On first use you land on the **Guide tab**; work through the cards in order:

1. **Hooks** — writes integrations for every detected AI assistant on your machine: Kimi Code, Pi, ZCode, Reasonix, opencode, Claude Code/CodePilot, Codex, Qoder CN (CLI + IDE), DeepSeek Harness
2. **Skills** — installs the six skills `openknowledge-init / on / off / propose / capture / wiki` into each agent's skills directory
3. **Embedding** — pick one of three semantic-retrieval forms: hosted OpenAI-compatible service / local Ollama (key-free) / built-in on-device model (fully offline, knowledge never leaves the machine); skip it to use keyword-only retrieval — you can configure it later anytime

<p align="center">
  <img src="docs/assets/gui-guide.png" alt="Guide tab" width="860"><br>
  <sub>Guide tab: one-click hooks, skills and embedding setup</sub>
</p>

When done you land on the Manage tab, where day-to-day entry maintenance is all point-and-click:

<p align="center">
  <img src="docs/assets/gui-manage.png" alt="Manage tab" width="860"><br>
  <sub>Manage tab: project / entry list, search preview and the global switch</sub>
</p>

| Tab | What it offers |
|-----|----------------|
| **Manage** | Create/edit/delete projects and entries, search preview, global switch; drafts carry a badge and can be promoted with one click |
| **Guide** | Hooks, skills and embedding in one place (the graphical equivalent of `ok setup`), plus an uninstall card |
| **Misc** | Data export/import (zip backup & restore), changelogs, delete a project's knowledge base (triple confirmation) |
| **Logs** | Live tails of ok / daemon / embedding logs with multi-select filters |

The GUI is hosted by a single system-wide resident daemon — closing the page doesn't stop it; `ok daemon stop` does. Prefer the terminal? `ok setup` is the CLI equivalent of the Guide tab (idempotent, safe to re-run), and `ok doctor` health-checks config and hooks.

### 3. Register the project and generate its wiki

Open your AI assistant and **start a new session inside the project directory** (hooks load at session start), then use two skills in turn:

1. **Initialize** — type `/openknowledge-init` (or just say "initialize the knowledge base"). The skill registers the current project, taking the directory name automatically — no parameters needed
2. **Generate the wiki** — then say **"generate the project wiki"**. The wiki skill scans the code structure and git history and distills the project into a set of reference entries (index and vectors are built automatically). Later, when a feature or module is finalized, say **"update the wiki"** to refresh it incrementally

> Advanced: behind these skills are `ok init` and the wiki CLI — skills are the in-session wrapper around the commands. Use natural language day to day; the CLI is for scripting and troubleshooting (see [Common commands](#common-commands)).

### 4. Daily use: skills inside AI sessions

Once set up there is barely anything to memorize — natural language triggers the right skill:

| You say | What happens |
|---------|--------------|
| "initialize the knowledge base" | The skill runs `ok init` to register the current project |
| "generate / update the project wiki" | The wiki skill distills the project structure, fully or incrementally |
| "enable / disable the knowledge base" | The skill runs `ok on` / `ok off` to flip the global switch |
| "enable auto capture" / "change capture frequency" | The skill runs `ok capture` to switch capture modes |
| (a pitfall is solved, a requirement is finalized) | The AI offers "capture this into the knowledge base?", records a draft via `ok propose` once you agree, and it takes effect after your approval |

Meanwhile the hooks silently do two things in the background — no commands needed:

- **Injection on every question**: each prompt is matched by hybrid keyword + vector retrieval and the relevant knowledge is injected into the AI's context; the first question of a session also carries the full text of mandatory rules (ask "what are the git commit conventions" and the answer quotes the knowledge base)
- **No code change without a changelog**: if the AI changed code but wrote no changelog, the turn ends with a blocking reminder (at most once per rule per session)

Manual entry maintenance works too (equivalent to the GUI Manage tab):

```bash
ok add --title "Git commit conventions" --type note --tags git --file git.md
ok search commit conventions    # preview retrieval from the CLI
```

<details>
<summary>More screenshots (Misc tab)</summary>

<p align="center">
  <img src="docs/assets/gui-misc.png" alt="Misc tab" width="860"><br>
  <sub>Misc tab: data export / import, changelogs and project deletion</sub>
</p>

</details>

---

## Knowledge entries

Each entry is a Markdown file with frontmatter (created via `ok add`, or handwritten), stored centrally in `~/.openknowledge/` — project repositories stay clean:

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

**Draft flow (AI proposes, human approves)**: drafts recorded via `ok propose` (`draft: true`) are excluded from retrieval and injection, visible only in `ok list` and the GUI; promote them with `ok approve <file>` or the GUI's "Approve" button. Switch capture modes with `ok capture propose|auto`: `propose` means the AI volunteers drafts (default); `auto` forces self-reflection every N turns, configured via `ok capture interval <n>`.

<p align="center">
  <img src="docs/assets/ai-session.png" alt="Knowledge capture in a real AI session" width="860"><br>
  <sub>In a real session: after finishing a release, the AI captures wiki entries on its own and asks whether to record the pitfall separately (propose mode)</sub>
</p>

## Common commands

| Command | Purpose |
|---------|---------|
| `ok setup` | First-run wizard: hooks + skills + embedding configuration |
| `ok gui` | Launch the web admin UI (same as double-clicking the exe) |
| `ok init [name]` | Register the current project; also idempotently writes/updates hook configuration |
| `ok add` / `ok list` | Create an entry / list projects and entries |
| `ok propose` / `ok approve <file>` | AI proposes a draft / promote it |
| `ok capture [propose\|auto\|interval <n>]` | Show/switch capture mode and the turn interval |
| `ok search <term>` | Preview retrieval results from the CLI |
| `ok index` | Sync index & vectors (run after hand-editing entries) |
| `ok doctor` | Health check: config, embedding connectivity, hook status |
| `ok on` / `ok off` | Global switch |
| `ok daemon [stop]` | Resident process management (auto-starts at login; rarely needs manual action) |

<details>
<summary>More commands: wiki cursor / branch-provenance backfill</summary>

| Command | Purpose |
|---------|---------|
| `ok wiki status` / `mark` / `base` / `diff` | Wiki status (JSON) / record cursor / view or set the base branch / emit branch-delta material (used internally by the wiki skill) |
| `ok backfill-born` | Backfill the born branch provenance tag on existing entries (writes after preview confirmation; never overwrites) |

</details>

<details>
<summary>Configuration: two-layer TOML and embedding profiles</summary>

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

</details>

<details>
<summary>Deep dive: retrieval algorithm and how it works</summary>

**Retrieval**: hybrid retrieval on SQLite + FTS5 — one `kb.db` file, zero infrastructure.

- Keyword channel: FTS5 + BM25 with Chinese bigram tokenization; semantic channel: embeddings + cosine similarity
- Admission separated from ranking: per-channel admission, RRF fusion by default (rank-based, model-agnostic, no tuning); better none than noise — `top_n` is never force-filled
- ~30ms per query over 10k entries; when the embedding service is down it degrades to keyword-only and injection never goes missing

**How it works** (Kimi Code as the example; other agents trigger the equivalents through their own adapters):

| Hook | Effect |
|------|--------|
| `UserPromptSubmit` | First question: mandatory entries + index; every question: retrieval injection |
| `PostToolUse` (Write/Edit) | Records the files the AI touched into session state |
| `Stop` | Code changed without changelog → exit 2 block (at most once per rule per session) |

All hook paths are fail-open: any internal error is only logged (`~/.openknowledge/ok.log`) and never disrupts the session. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) (Chinese).

</details>

## Development

```bash
go test ./...    # all 25 packages (no network; includes end-to-end)
go vet ./...
```

```bash
# Maintainer release builds
bash scripts/build-dist.sh        # dist/ok.exe + dist/web/
bash scripts/build-installer.sh   # Windows installer
bash scripts/build-linux.sh       # Linux tar.gz / .deb
```

| Document | Location |
|----------|----------|
| Architecture | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) (Chinese) |
| Changelogs | [docs/changelogs/](docs/changelogs/) (Chinese) |
