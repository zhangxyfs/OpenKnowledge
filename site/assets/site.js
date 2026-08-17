/* OpenKnowledge 官网交互运行时：主题切换 + 中英文切换。
   中文为页面原始 DOM（无需字典）；英文字典集中在本文件 EN 中。
   首次切到英文时缓存每个 [data-i18n] 元素的中文 innerHTML，切回中文时还原。
   注意：i.hint 内含 Release 直链版本号，发版时由 scripts/sync-version.sh 同步本文件。 */
(function () {
  'use strict';

  var EN = {
    /* ── 通用 ── */
    'nav.home': 'Home',
    'nav.features': 'Features',
    'nav.shots': 'Screenshots',
    'nav.install': 'Install',
    'nav.docs': 'Docs',
    'nav.changelog': 'Changelog',
    't.new': 'New',
    't.fix': 'Fixed',
    't.improve': 'Improved',
    't.note': 'Notes',

    /* ── 首页 ── */
    'meta.title.i': 'OpenKnowledge — Project knowledge base for AI coding assistants',
    'i.tagline': 'A <b>project knowledge base</b> for AI coding assistants — knowledge is isolated per project and injected into the AI&#39;s context through each assistant&#39;s hooks/extensions, and it can enforce workflow rules like "no code change without a changelog entry".',
    'i.src': 'Source',
    'i.hint': 'Auto-detects your OS · <a href="https://github.com/zhangxyfs/OpenKnowledge/releases/download/v2.18.0/OpenKnowledgeSetup-2.18.0.exe">Windows</a> / <a href="https://github.com/zhangxyfs/OpenKnowledge/releases/download/v2.18.0/openknowledge_2.18.0_amd64.deb">Linux .deb</a> / <a href="https://github.com/zhangxyfs/OpenKnowledge/releases/download/v2.18.0/openknowledge_2.18.0_linux_amd64.tar.gz">Linux .tar.gz</a> · <a href="https://github.com/zhangxyfs/OpenKnowledge/releases" target="_blank" rel="noopener">All releases</a>',
    'i.feat.h': 'Features',
    'i.feat.sub': 'Single-binary Go CLI (ok) with zero runtime dependencies',
    'i.f1t': 'Base injection',
    'i.f1p': 'On the first question of each session, the project&#39;s mandatory entries (full text) plus the knowledge index are sent to the AI.',
    'i.f2t': 'Retrieval injection',
    'i.f2p': 'Every question is matched by hybrid keyword + vector-semantic retrieval, injecting the most relevant entries — e.g. your git commit conventions.',
    'i.f3t': 'Enforcement',
    'i.f3p': 'Tracks files the AI modified; if code changed but no changelog was written by the end of the turn, the turn is blocked until it&#39;s fixed.',
    'i.f4t': 'Multi-agent support',
    'i.f4p': 'Kimi Code, Pi, ZCode, Reasonix, opencode, Claude Code/CodePilot, Codex and Qoder CN (CLI &amp; IDE) share the same knowledge base through an extensible adapter architecture.',
    'i.f5t': 'One-step setup',
    'i.f5p': '<code>ok setup</code> configures hooks, skills and embeddings; <code>ok off</code> / <code>ok on</code> toggles everything anytime.',
    'i.f6t': 'Web GUI & daemon',
    'i.f6p': 'Double-click the exe or run <code>ok gui</code> to open the admin UI; a single resident process serves the GUI and every agent&#39;s hook requests with millisecond forwarding.',
    'i.shots.h': 'Screenshots',
    'i.shots.sub': 'The four GUI tabs + knowledge capture in a real session',
    'i.s1c': 'Manage tab: project / entry list, search preview and the global switch',
    'i.s2c': 'Guide tab: one-click hooks, skills and embedding setup',
    'i.s3c': 'In a real session: after finishing a release, the AI captures wiki entries on its own (propose mode)',
    'i.inst.h': 'Install',
    'i.inst.sub': 'Windows installer / Linux packages / manual build — pick one',
    'i.w1h': 'Windows installer',
    'i.w1tag': 'Recommended',
    'i.w1p': 'No Go toolchain needed; installs to <code>%LOCALAPPDATA%\\Programs\\OpenKnowledge</code> (no admin rights). Uninstall keeps your knowledge base by default.',
    'i.w1pre': '<code><span class="c"># Download from Releases, then run</span>\nOpenKnowledgeSetup-2.18.0.exe</code>',
    'i.lxh': 'Linux (amd64)',
    'i.lxp': 'Statically compiled, zero dependencies.',
    'i.lxpre': '<code><span class="c"># tar.gz</span>\ntar xzf openknowledge_*_linux_amd64.tar.gz\ncd openknowledge_* &amp;&amp; ./ok setup\n\n<span class="c"># or .deb</span>\nsudo dpkg -i openknowledge_*_amd64.deb\nok setup</code>',
    'i.mbh': 'Manual build',
    'i.mbp': 'Requires Go ≥ 1.25.',
    'i.mbpre': '<code>go build -o ok.exe ./cmd/ok   <span class="c"># Windows</span>\ngo build -o ok ./cmd/ok       <span class="c"># Linux/macOS</span>\n./ok setup                    <span class="c"># first-run wizard (idempotent)</span></code>',
    'i.foot': '<a href="https://github.com/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>· <a href="https://github.com/zhangxyfs/OpenKnowledge/releases" target="_blank" rel="noopener">Releases</a>· <a href="https://github.com/zhangxyfs/OpenKnowledge/blob/master/docs/ARCHITECTURE.md" target="_blank" rel="noopener">Architecture</a>',

    /* ── 文档页 ── */
    'meta.title.d': 'Docs — OpenKnowledge',
    'd.g1': 'Getting started',
    'd.g1a': 'Install & first-run setup',
    'd.g1b': 'Quick start',
    'd.g2': 'Daily use',
    'd.g2a': 'Knowledge entries',
    'd.g2b': 'Draft flow',
    'd.g2c': 'Common commands',
    'd.g3': 'Understand',
    'd.g3a': 'Configuration',
    'd.g3b': 'Retrieval algorithm',
    'd.g3c': 'How it works',
    'd.g4': 'More',
    'd.g4a': 'Architecture (repo)',
    'd.h1': 'Documentation',
    'd.lede': 'OpenKnowledge is a project knowledge base for AI coding assistants: knowledge is isolated per project, injected into the AI&#39;s context via hooks/extensions, and can enforce workflow rules.',
    'd.e1b': 'Five-minute start',
    'd.e1p': 'From install to the first retrieval injection',
    'd.e2b': 'Knowledge capture',
    'd.e2p': 'The draft flow: AI proposes, humans approve',
    'd.e3b': 'Under the hood',
    'd.e3p': 'Hybrid retrieval and hook timing',
    'd.s1h': 'Install & first-run setup',
    'd.s1p': 'Pick any of the three install options on the <a href="index.html#install">home page</a>, then run the first-run wizard (idempotent, safe to re-run):',
    'd.s1pre': '<code>ok setup            <span class="c"># or one agent only: ok setup --agent zcode</span></code>',
    'd.s1lead': 'performs three steps, in order:',
    'd.s1l1': '<b>Writes hook configurations</b> — covering every detected AI assistant: Kimi Code gets 3 hook marker blocks in <code>~/.kimi-code/config.toml</code> (backup + idempotent overwrite), Pi gets a TypeScript extension, ZCode gets a merged <code>config.json</code> write, Reasonix gets an Extension Protocol plugin package, opencode gets a TypeScript plugin in <code>~/.config/opencode/plugins/</code>, claude gets a merged hooks write to <code>~/.claude/settings.json</code> (shared by Claude Code, CodePilot and other compatible hosts), codex gets a merged hooks write to <code>~/.codex/hooks.json</code> (Claude-compatible hook contract; zero skill adaptation via the shared skills directory; ok auto-enables the feature flag and writes trust records; verified working on desktop app 26.707 and CLI 0.147+), qoder gets a merged hooks write to <code>~/.qoder-cn/settings.json</code> (Qoder CN terminal CLI; Claude-compatible contract; ok auto-enables the hooksConfig.enabled switch — off by default, hooks silently never dispatch otherwise), qoder-ide gets a merged hooks write to <code>~/.lingma/settings.json</code> (Qoder CN IDE Lingma core: knowledge injection and touch tracking work, Stop is not blockable so enforcement degrades)',
    'd.s1l2': '<b>Installs six skills</b> — <code>openknowledge-init / on / off / propose / capture / wiki</code>, written into each agent&#39;s skills directory',
    'd.s1l3': '<b>Configures embeddings</b> — prompts for base_url / model / API key, writes the global config and verifies connectivity; press Enter to skip and use keyword-only retrieval',
    'd.s1note': '<b>Note</b>Hooks load at session start: after installing or changing configuration, <b>start a new AI assistant session</b> for them to take effect.',
    'd.s2h': 'Quick start',
    'd.s2pre': '<code><span class="c"># Inside the project that should use the knowledge base</span>\ncd /your/project\nok init                      <span class="c"># register the project (name defaults to the directory)</span>\nok add --title "Changelog enforcement rule" --type rule --mandatory --file rule.md\nok add --title "Git commit conventions" --type note --tags git --file git.md\nok index                     <span class="c"># sync index &amp; vectors once embedding is configured</span></code>',
    'd.s2p': 'Then start a new AI assistant session and it just works:',
    'd.s2l1': 'Ask "what are the git commit conventions" → the AI answers quoting the knowledge base',
    'd.s2l2': 'AI changed code without a changelog → the turn ends with a blocking reminder',
    'd.s2tail': 'You can also say "initialize the knowledge base" or "disable knowledge-base hooks" in a session — the corresponding skills run automatically.',
    'd.s3h': 'Knowledge entries',
    'd.s3p': 'Each entry is a Markdown file with frontmatter (created via <code>ok add</code>, or handwritten):',
    'd.s3pre': '<code>---\ntitle: Changelog enforcement rule\ntype: rule              <span class="c"># rule | pitfall | note | reference</span>\ntags: [changelog, workflow]\nmandatory: true         <span class="c"># true = full-text injection on the first question of every session</span>\nsummary: Every code change must be accompanied by a changelog entry\n---\n\nBody (free-form Markdown)</code>',
    'd.s3tail': 'Data lives centrally in <code>~/.openknowledge/</code> — project repositories stay clean. Four entry types: <code>rule</code>, <code>pitfall</code>, <code>note</code>, <code>reference</code>.',
    'd.s4h': 'Draft flow (AI proposes, human approves)',
    'd.s4p': 'The AI can record session learnings as <b>draft entries</b> via <code>ok propose</code> (frontmatter <code>draft: true</code>) — drafts are excluded from retrieval and injection, and only appear in <code>ok list</code> and the GUI Manage tab (with a "draft" badge). A human promotes them with <code>ok approve &lt;file&gt;</code> or the GUI&#39;s "Approve" button.',
    'd.s4lead': 'The capture mode is switched with <code>ok capture propose|auto</code>:',
    'd.s4l1': '<code>propose</code> (default): the AI volunteers drafts on its own judgment, no turn limit',
    'd.s4l2': '<code>auto</code>: the Stop hook forces self-reflection every N turns; the interval is set via <code>ok capture interval &lt;n&gt;</code>',
    'd.s5h': 'Common commands',
    'd.s5tbl': '<table class="doc"><tr><th>Command</th><th>Purpose</th></tr><tr><td><code>ok setup</code></td><td>First-run wizard: hooks + skills + embedding configuration</td></tr><tr><td><code>ok gui</code></td><td>Launch the web admin UI (same as double-clicking the exe)</td></tr><tr><td><code>ok init [name]</code></td><td>Register the current project; also idempotently writes/updates hook configuration</td></tr><tr><td><code>ok add</code></td><td>Create a knowledge entry (auto-rebuilds index &amp; vectors)</td></tr><tr><td><code>ok propose</code> / <code>ok approve</code></td><td>AI proposes a draft / promote it</td></tr><tr><td><code>ok capture [...]</code></td><td>Show/switch capture mode; configure the turn interval</td></tr><tr><td><code>ok wiki status|mark|base|diff</code></td><td>Wiki status / record cursor / base branch / branch-delta material</td></tr><tr><td><code>ok search &lt;term&gt;</code></td><td>Preview retrieval results from the CLI</td></tr><tr><td><code>ok index</code></td><td>Sync index &amp; vectors (with kb.db deleted it is a full rebuild)</td></tr><tr><td><code>ok doctor</code></td><td>Health check: config, embedding connectivity, hook status</td></tr><tr><td><code>ok on</code> / <code>ok off</code></td><td>Global switch (default on)</td></tr></table>',
    'd.s6p': '<b>Double-click <code>ok.exe</code> (no arguments) or run <code>ok gui</code></b> to launch the web admin UI; the browser opens <code>http://127.0.0.1:17888</code>. The GUI is hosted by the resident daemon — closing the page does not stop it; use <code>ok daemon stop</code> to stop the service. Four tabs:',
    'd.s6l1': '<b>Manage</b>: project/entry list, create/edit/delete, search preview, global switch, branch filter',
    'd.s6l2': '<b>Guide</b>: a graphical <code>ok setup</code>, plus hook timeout, enforcement mode (reasonix) and uninstall cards',
    'd.s6l3': '<b>Misc</b>: data export/import, changelogs, user guide, delete project knowledge base (triple confirmation)',
    'd.s6l4': '<b>Logs</b>: live ok / daemon / embedding-sidecar logs, multi-select source chips + a "semantic only" toggle + a free-text filter',
    'd.s7h': 'Configuration',
    'd.s7p': 'Effective config = built-in defaults ← global <code>~/.openknowledge/config.toml</code> ← per-project <code>~/.openknowledge/projects/&lt;name&gt;/config.toml</code> (each layer overrides the previous).',
    'd.s7pre': '<code><span class="c"># Global config (ok setup can write this interactively)</span>\n[embedding]\nbase_url = "https://api.openai.com/v1"   <span class="c"># any OpenAI-compatible service</span>\napi_key = "sk-..."                        <span class="c"># or use api_key_env for an environment variable</span>\nmodel = "text-embedding-3-small"\n\n<span class="c"># Project config: enforcement rule example</span>\n[[enforce]]\ntype = "changelog_required"\ncode_globs = ["**/*.go"]                  <span class="c"># touching these = changed code</span>\nchangelog_glob = "docs/changelogs/**"     <span class="c"># touching these = wrote a changelog</span>\nmessage = "Code was changed this session without a changelog update; please add one first."</code>',
    'd.s8h': 'Retrieval algorithm',
    'd.s8p': 'Hybrid retrieval on SQLite + FTS5 — the total score only ranks, <b>admission is decided per channel</b>: better none than noise, top_n is never force-filled:',
    'd.s8l1': '<b>Keyword channel</b>: FTS5 full-text index + BM25 scoring, weighted across title/tags/summary/body; Chinese uses bigram tokenization, zero dependencies; admission requires normalized BM25 ≥ <code>min_score</code> (default 0.5, scaled by corpus size)',
    'd.s8l2': '<b>Semantic channel</b>: OpenAI-compatible embeddings + cosine similarity, recalling entries that "ask differently but mean the same"; admission goes through a model-agnostic gate (judged against the cosine distribution of the current query — active only when the head separates from the median significantly; <code>min_gap</code> default 0.25, configurable)',
    'd.s8l3': '<b>Drafts stay out of both channels</b>: excluded from FTS and vectors until approved',
    'd.s8tail': '~30ms per query over 10k entries; when the embedding service is down it degrades to keyword-only retrieval and injection never goes missing. Implementation details in <a href="https://github.com/zhangxyfs/OpenKnowledge/blob/master/docs/ARCHITECTURE.md" target="_blank" rel="noopener">ARCHITECTURE §17</a>.',
    'd.s9h': 'How it works',
    'd.s9p': 'Using Kimi Code as the example, the assistant calls <code>ok</code> at three moments (other agents trigger the equivalents through their own adapters):',
    'd.s9tbl': '<table class="doc"><tr><th>Hook</th><th>When it runs</th><th>Effect</th></tr><tr><td><code>UserPromptSubmit</code></td><td>Every user message, before the model call</td><td>First question: mandatory entries + index; every question: retrieval injection</td></tr><tr><td><code>PostToolUse</code></td><td>After the AI successfully writes/edits a file</td><td>Records the touched file into session state</td></tr><tr><td><code>Stop</code></td><td>When the AI&#39;s turn is about to end</td><td>Code changed without changelog → exit 2 block (at most once per rule per session)</td></tr></table>',
    'd.s9note': '<b>Note</b>All hook paths are fail-open: any internal error is only logged (<code>~/.openknowledge/ok.log</code>) and never disrupts the session.',
    'd.foot': '<a href="index.html">Home</a>· <a href="changelog.html">Changelog</a>· <a href="https://github.com/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>',

    /* ── 更新日志页 ── */
    'meta.title.c': 'Changelog — OpenKnowledge',
    'c.ledger': 'Release ledger',
    'c.h1': 'Changelog',
    'c.lede': 'OpenKnowledge release history. Dates are in 2026.',
    'c.cta': 'Download latest v2.18.0',
    'c.latest': 'Latest',
    'c.2101.f1': 'Export/import now covers wiki state: <code>state/wiki.json</code> (base branch + per-branch cursors + merge lineage) travels with the zip backup and is restored verbatim. Previously only entries and config were exported — the "export zip backup" of project deletion therefore lacked the lineage, and re-importing zeroed the GUI lineage rows and lost the wiki cursors, forcing a full rescan',
    'c.2101.n1': 'Backups without wiki.json skip the restore — backward compatible; the entry index is still rebuilt automatically on import',
    'c.2110.n1': 'New opencode adapter (fifth AI assistant integration): <code>ok setup</code> or the GUI Guide tab installs a global TypeScript plugin (<code>~/.config/opencode/plugins/openknowledge.ts</code>) — retrieval injection on every prompt (synthetic part, invisible in the UI), write-tracking across write/edit and apply_patch (gpt-family models), and an SDK-delivered capture reminder when the session goes idle (stop loop); idempotent install, self-healing, clean uninstall',
    'c.2111.n1': 'New claude-ecosystem adapter (sixth AI assistant integration): a merged hooks write to <code>~/.claude/settings.json</code> (backup + idempotent + third-party entries preserved), shared by Claude Code, CodePilot and other claude-agent-sdk-compatible hosts — install once, works across hosts; verified on CodePilot that UserPromptSubmit/Stop run natively (the agent kernel&#39;s settingSources includes the user layer; the shadow HOME only strips ANTHROPIC_* env keys); skills directory <code>~/.claude/skills</code>',
    'c.2110.i1': 'The "no supported agent detected" notice and the <code>ok doctor</code> list are now generated from the registry; setup and Guide "next steps" no longer hardcode kimi',
    'c.2111.i1': 'Fix: <code>registry.Home()</code> is now immune to environment-variable redirection (Windows resolves the real config directory via <code>SHGetKnownFolderPath</code>, other platforms via <code>os/user</code>) — hosts like CodePilot running as a DB provider redirect the child process <code>HOME</code>/<code>USERPROFILE</code> to a shadow temp directory for isolation; the old implementation followed the redirect, saw an empty data root, and hook injection silently failed with no log trace',
    'c.2120.n1': 'New codex adapter (seventh AI assistant integration): a merged hooks write to <code>~/.codex/hooks.json</code> (backup + idempotent + third-party entries preserved); the hook contract is byte-compatible with Claude Code (injection via <code>hookSpecificOutput.additionalContext</code>, Stop via <code>decision:block</code>), so the output protocol is reused unchanged; write-tracking parses apply_patch patch headers (<code>*** Add File:</code> and friends), keeping auto-capture and enforce rules at full Claude parity; zero skill adaptation — Codex natively scans the shared <code>~/.agents/skills</code>; note the Codex trust gate: first run after install prompts to trust the hooks (managed via <code>/hooks</code>); hooks are an under-development feature in Codex 0.118 (off by default) — ok auto-enables <code>codex_hooks</code> in config.toml on install (line-level surgical edit, backed up), and the GUI guide page gains a Codex-only help card; ok also writes hooks.state trust records on install/self-heal (content changes no longer silently disable hooks), and on Windows the hook commands use <code>ok-hook-*.cmd</code> wrappers to dodge the upstream outer-quote spawn bug (#38168); verified working on desktop app 26.707 and CLI 0.147+)',
    'c.2140.n1': 'Multi-service embedding configuration (multiple profiles with an "in use" switch) in three forms: custom (OpenAI-compatible, key optional for unauthenticated servers) / Ollama (local or LAN, key-free, installed-model list auto-detected) / builtin local model; legacy flat config auto-migrates to a default profile — no manual intervention',
    'c.2140.n2': 'Builtin llama.cpp local model (<b>fully offline — knowledge never leaves the machine</b>): the llama-server sidecar is managed by the daemon (want-flag spawn / 10-min idle reclaim / bounded crash restarts / reclaimed on exit); GGUF models download per manifest — default Qwen3-Embedding-0.6B (639MB, strong on Chinese + code), plus two bge-m3 tiers and a tiny nomic tier, with repo/size/sha256 pinned and verified, hf-mirror as the default mirror, and resumable downloads; the runtime ships with the installer (llama.cpp b10405 CPU build, Windows + Linux); the models directory defaults to <code>&lt;install dir&gt;/models</code>, is configurable, and can be opened in the file manager from the dialog',
    'c.2140.n3': 'GUI configuration dialog: the guide-page card shows a one-line summary of the active service; the "Configure…" dialog has a profile list on the left and the form on the right (type locked after creation, download progress bar, explicit "set as active", and Test runs against the current form values with an empty api_key falling back to the saved one)',
    'c.2140.n4': 'CLI: the <code>ok setup</code> embedding step is now a three-way choice (online / Ollama / builtin, with model selection and download progress); <code>ok doctor</code> reports builtin-specific checks (runtime / model file / sidecar state)',
    'c.2140.i1': 'Model identity management: kb.db meta records the model identity and dimension used to build vectors; after a model switch the semantic channel is explicitly skipped with an <code>ok index</code> hint (replacing the old silent zero-score behavior on dimension mismatch); <code>ok index</code> detects the switch and rebuilds all vectors in batches of 32; Sync refuses to write vectors on identity mismatch, preventing mixed-model vectors',
    'c.2140.i2': 'The embed interface splits query/document paths (Qwen3 query-side Instruct prefix, nomic dual prefixes) and supports batch requests; the index/rebuild path timeout is decoupled from the hook query path (120s floor)',
    'c.2140.s1': 'Migration: pre-2.13 config auto-migrates to a profile; legacy vectors (without identity records) trigger a hint on first retrieval and need one full <code>ok index</code> rebuild. Installer size grows to ~50MB (includes the llama.cpp CPU runtime; models are not bundled — they download on first enable, ~146–639MB per tier). Builtin mode requires the installed edition (bare exe has no runtime); physical offline verification and large-scale rebuilds (≥200 entries) remain as follow-up checks',
    'c.2162.n1': 'Every log line is now timestamped: <code>daemon.log</code> (including http.Server internal errors) and <code>embed-sidecar.log</code> (llama-server output) get per-line timestamps, and the latter gains start/exit separator markers — interleaved output from multiple launches can finally be placed on a timeline',
    'c.2162.i1': 'All on-disk writes are now atomic (same-directory temp file + fsync + rename): INDEX.md, registry.toml, config.toml, every host\'s settings files, backup imports, knowledge entries, wiki cursors — a crash or power loss no longer leaves half-written files, and hosts never read a truncated JSON',
    'c.2162.i2': 'Injection budgets are estimated by density: CJK text at ~1 token per character, latin at ~4 characters per token (the old uniform 2-chars-per-token rule underestimated Chinese-heavy entries by ~2×); a negative <code>max_tokens</code> typo no longer crashes the hook',
    'c.2162.f1': 'Fixed a session-state race: multiple hook processes dispatched by parallel tool calls did unguarded read-modify-write on the same session file, overwriting each other (lost Touched → changelog rules missed) and in the worst case zeroing <code>BaseInjected</code> → mandatory full text re-injected — replaced with a cross-process lock + in-lock merge + atomic save',
    'c.2162.f2': 'Fixed a strong negative cosine evicting entries that had already passed the keyword gate (the semantic channel must not hold a unilateral veto; its score only affects ranking)',
    'c.2162.f3': 'Fixed under-filled injections caused by <code>top_n</code> truncating before the branch filter: entries from other branches consumed slots and same-branch entries had no backfill',
    'c.2162.f4': 'Fixed kimi hook commands silently breaking when the exe path contains spaces (now quoted; existing configs self-heal in place); fixed a sed-delimiter clash in the version-sync script that aborted winres syncing',
    'c.2162.s1': 'This is a robustness batch: no injection-mechanism behavior changes, mostly concurrency and edge-condition fixes; the installer stops the old daemon automatically before upgrade, and log timestamps take effect once the new daemon starts',
    'c.2170.n1': 'Generic-prompt gating: content-free prompts like "continue", "ok", "thanks" no longer trigger retrieval at all — even the embedding network round-trip is skipped; 21 built-in Chinese/English confirmation phrases (evolve with releases), plus a new "Prompt gate" card on the GUI guide page with a phrase-manager dialog for custom additions (unioned with built-ins, auto-deduped, ≤64 chars each); mandatory/INDEX/wiki-nudge injections are unaffected',
    'c.2170.n2': 'Recency signal: retrieval scores are multiplied by a freshness factor based on entry file mtime — 1.0 within the fresh window, 0.85 once stale, linear in between, with per-type windows (pitfall 90/365 days, note 60/180, rule/reference 180/730); never affects admission, only breaks near-ties; editing an entry refreshes its freshness',
    'c.2170.n3': 'Injection→adoption feedback loop: a new entry_events table records inject/adopt events — an adoption is an entry injected in the current session being read (the post-tool hook never opens the DB; adoptions are parked in session state and booked on the next prompt); entries injected ≥4 times in 30 days with zero adoptions are demoted ×0.8 (v1 demotes only, never boosts)',
    'c.2170.i1': 'Retrieval fusion is now RRF (Reciprocal Rank Fusion — ranks, not scores): swapping embedding models no longer breaks the two-channel balance when cosine distributions drift; entries admitted by both channels naturally rank first ("cross-validation wins"); admission logic unchanged; <code>ok search</code> prints scores with 4 decimal places',
    'c.2170.s1': 'Feedback demotion is <strong>off by default</strong> (<code>retrieve.feedback.enabled = false</code>): every current host only dispatches write tools to PostTool hooks, reads never reach the tracking chain, so the adoption signal is always zero and enabling demotion would punish frequently-injected entries; events are still recorded, and the default flips back on once read dispatch lands',
    'c.2170.s2': 'To restore the old ranking, set <code>fusion = "weighted"</code> (legacy α/β blend); recency and gating can be disabled individually via <code>retrieve.recency.enabled</code> / <code>retrieve.gate.enabled</code>',
    'c.2160.n1': 'Retrieval injection now admits per channel: the total score only ranks, and an entry is injected only when its keyword-channel normalized BM25 (unscaled by α) reaches the threshold <b>or</b> its semantic cosine reaches the semantic gate — better none than noise, <code>top_n</code> is never force-filled; new <code>retrieve.min_score</code> (default 0.5, ≤0 disables), scaled by corpus size (disabled below 10 entries, linear 10→30, full from 30)',
    'c.2160.n2': 'Model-agnostic semantic gate (<code>SemanticFloor</code>): judged against the cosine distribution of the current query — the relative gate median+0.5·gap applies only when the head separates from the median significantly (<code>retrieve.min_gap</code>, default 0.25, configurable); without a significant head the semantic channel admits nothing; calibrated on 12 scenarios across four models (bge-m3 / bge-large-zh-v1.5 / Qwen3-Embedding-8B / qwen3-emb-0.6b)',
    'c.2160.n3': 'A fourth GUI tab "Logs": <code>GET /api/logs</code> tails three sources (ok / daemon / embedding sidecar), 2-second polling for live refresh, multi-select source chips plus a "semantic only" toggle and a free-text filter, auto-scroll-to-bottom pauses when you scroll up',
    'c.2160.n4': 'Semantic diagnostics are now visible: when the semantic channel rejects every candidate, a <code>prompt semantic</code> log line records samples/max/median/relGap, <code>ok search</code> prints the same with tuning guidance, and semantic degradation (missing/switched model identity) appends a once-per-session hint to the injection',
    'c.2160.i1': 'Release process hardening: <code>scripts/sync-version.sh</code> now also syncs the exe version resources in <code>cmd/ok/winres.json</code> (previously easy to miss on bump — they had drifted at 2.8.0.0 since v2.9.0)',
    'c.2160.f1': 'Fixed injecting knowledge unrelated to the question: CJK bigram pseudo-terms (e.g. "已经验证" splitting out "经验") hitting a title used to inject, and the cosine baseline of unrelated texts in a same-domain corpus (bge-m3 cross-domain noise measured as high as 0.52) pushed the blended score over the gate — with per-channel admission, unrelated questions now inject nothing while related ones hit precisely',
    'c.2160.f2': 'Fixed the exe file version resource drifting at 2.8.0.0 since v2.9.0',
    'c.2160.s1': 'After switching the embedding model you must run <code>ok index</code> to rebuild vectors (the identity guard blocks stale vectors); low-contrast custom models can lower <code>min_gap</code> (with <code>min_score</code> if needed) to relax semantic admission; in extreme low-contrast cases the semantic channel stays silently closed (fail-closed, keyword channel covers) — watch the "semantic only" view on the GUI Logs tab for the diagnostics line',
    'c.2150.n1': 'New dsh adapter (tenth AI assistant integration, DeepSeek Harness): a local JS plugin written to <code>$DSH_HOME/plugins/openknowledge/index.js</code> (header marker + fingerprint managed) plus a marker block in the home-level <code>$DSH_HOME/cordis.patch.yml</code> (a <code>file://</code> URL, shared by all profiles); three events wired directly to the Cordis bus — <code>agent/pre-step</code> injection / <code>tools/post-execute</code> write|edit tracking / <code>agent/turn-stopping</code> continuation via <code>agent.steer()</code>; the subprocess is spawned via <code>execFile</code> (no shell layer); zero skill adaptation (it natively scans <code>~/.agents/skills</code>)',
    'c.2150.i1': 'Tests: the seven registry-traversal test files gained <code>OK_DSH_HOME</code> isolation (20 sites) and the GUI agent count went 9→10; the full <code>go test ./...</code> is green across 26 packages',
    'c.2150.s1': 'DeepSeek Harness is a developer preview and may make breaking changes; the local plugin must be mounted as a <code>file:///</code> URL (the vendored cordis loader calls <code>import(name)</code> directly, and a Windows drive-letter absolute path fails with <code>ERR_UNSUPPORTED_ESM_URL_SCHEME</code>). A real DeepSeek API session verified prompt (injection) / post-tool (touch tracking) / stop (<code>stop_count</code> increment) end-to-end; the turn-stopping continuation was not observed on the first turn because the default <code>turn_interval=5</code> throttles it',
    'c.2130.n1': 'New qoder adapter (eighth AI assistant integration, Qoder CN terminal CLI): a merged hooks write to <code>~/.qoder-cn/settings.json</code> (backup + idempotent + third-party entries preserved); the hook contract is byte-compatible with Claude Code (injection via <code>hookSpecificOutput.additionalContext</code>, Stop via <code>decision:block</code>), so the output protocol is reused unchanged; auto-enables the <code>hooksConfig.enabled</code> switch (off by default in Qoder — hooks would silently never dispatch otherwise); on Windows the hook commands use <code>ok-hook-*.cmd</code> wrappers to dodge the <code>cmd /s</code> quote-stripping (exe migrations self-heal without rewriting the config); zero skill adaptation into <code>~/.qoder-cn/skills</code>',
    'c.2130.n2': 'New qoder-ide adapter (ninth AI assistant integration, Qoder CN IDE Lingma core): a merged hooks write to <code>~/.lingma/settings.json</code> plus skills into <code>~/.lingma/skills</code> — the IDE and the terminal CLI use two separate hooks systems (the IDE reads ~/.lingma, not ~/.qoder-cn); the IDE supports only 5 events and Stop/PostToolUse are not blockable, so knowledge injection and touch tracking work while enforcement/auto-capture degrade; no enabled switch, and an IDE restart is required for config changes to take effect',
    'c.2130.i1': 'Linux-portable test suite: agentx/setupx test fixtures are now platform-dual (ToSlash expectations, platform-aware .cmd cases, and split wrapper/assertion and exe-migration semantics), with the full <code>go test ./...</code> green on both WSL (Ubuntu 22.04 + Go 1.25) Linux and Windows',
    'c.2130.s1': 'The CLI side is verified against both the official hooks-reference docs and the npm package source; the IDE side is verified against the official IDE hooks docs. Still to verify on real hardware: no terminal CLI (qoderclicn) installed on the dev machine, so the CLI version matrix is pending; IDE hook dispatch needs an IDE restart test',
    'c.2110.s1': 'Zero skill adaptation: opencode natively scans the shared <code>~/.agents/skills</code> directory; it has no hooks config field, so integration is plugin hooks (same shape as pi)',
    'c.2100.n1': 'New Linux release line (amd64): <code>.tar.gz</code> (extract &amp; run) and <code>.deb</code> (installs to /usr/lib/openknowledge/, ok lands in PATH)',
    'c.2100.n2': 'Linux desktop experience completed: <code>ok gui</code> opens the default browser via xdg-open; <code>ok setup</code> writes XDG login autostart, removed on uninstall',
    'c.2100.s1': 'Zero behavior change on Windows; Linux binaries are fully static (no CGO) and work on any distro',
    'c.290.n1': 'The Misc tab adds "Delete project knowledge base": permanently deletes the selected project&#39;s knowledge, index and config, and unregisters it (hooks stop injecting); the project source directory is untouched',
    'c.290.n2': 'Multiple confirmations against accidents: impact summary → pre-delete zip backup (checked by default) → "I understand" checkbox + typing the full project name to unlock',
    'c.290.n3': 'The backend unregisters before deleting the directory: any failure leans toward "keep the data"',
    'c.290.f1': 'Deleting the currently selected project no longer pops a spurious "project not registered" error banner',
    'c.282.i1': 'The project dropdown now sorts by most recent knowledge update — the latest writer comes first and is selected by default',
    'c.282.i2': 'The refresh button now re-fetches everything (projects + status + entries) with "Refreshing… → Refreshed ✓" feedback and double-click protection',
    'c.281.i1': 'GUI table headers localized to Chinese: tags → 标签, mandatory → 必注入 (hover keeps the original field names)',
    'c.281.i2': 'The summary column clamps to two lines with an ellipsis; a hover tooltip shows the full text (follows the mouse, flips on overflow, dismisses on scroll)',
    'c.281.i3': 'Entries per page 20 → 12 for a calmer information density',
    'c.280.n1': 'Entry-level branch provenance: every new entry auto-records its born branch (born tag), shown as a "⎇ born" badge in the GUI branch column; <code>ok backfill-born</code> backfills existing entries',
    'c.280.n2': 'Merge lineage persisted: merges into the base branch are recorded, and the GUI shows "dev → master"',
    'c.280.n3': 'GUI branch context: the toolbar shows "base branch · current branch" and warns when they differ',
    'c.280.s1': 'born and branch tags are orthogonal: branch governs "where it applies" (injection filter), born records "where it was born" (display); zero migration, existing knowledge bases work out of the box',
    'c.271.n1': 'The Misc tab adds a "User guide" card: how to invoke, how to configure, FAQ — shipped with the installer, available offline',
    'c.271.n2': 'Entry types now display in Chinese in the GUI (规则/踩坑/笔记/参考; stored values stay English)',
    'c.270.n1': 'Wiki branch-delta entries: long-lived parallel branches maintain only structural differences from the base branch; injection filters by the current branch so other branches&#39; deltas no longer mislead the agent',
    'c.270.n2': 'Merge awareness: after dev merges into master, a prompt reminds you to clean up stale delta entries (once per session)',
    'c.270.n3': 'GUI Manage tab: branch column + branch filter + pinned action column',
    'c.270.f1': 'Running wiki updates on a non-base branch no longer pollutes the full-entry set (the skill flow now writes delta entries only)',
    'c.270.f2': 'Fewer git calls on the prompt hot path — 1-2 fewer git subprocesses per question',
    'c.270.f3': '<code>ok wiki mark</code> now rev-parses user input first — fixes short-hash cursors being misjudged as diverged',
    'c.260.n1': 'Wiki branch awareness: cursors are recorded per branch (new <code>state/wiki.json</code> format); the legacy single-cursor format migrates lazily via merge-base reachability; switching to a diverged branch adds a wiki-provenance hint to the injection',
    'c.260.n2': 'Cursors rewritten by rebase/squash now produce an explicit "cursor stale" notice',
    'c.260.n3': '<code>ok wiki base</code> views/sets the base branch; <code>ok wiki status</code> outputs branch/base_branch/branch_state',
    'c.250.n1': 'New Reasonix integration (fourth adapter): an Extension Protocol plugin package + sidecar (<code>ok extension-serve</code>) — a sidecar crash degrades gracefully without blocking the host',
    'c.250.n2': 'Three-mode enforcement switch (Reasonix only): <code>mixed</code> / <code>soft</code> / <code>hard</code>, configurable in the GUI Guide tab, effective immediately',
    'c.250.s1': 'The sidecar is fully fail-open: interceptor errors always pass through without blocking input; <code>ok off</code> applies to the reasonix sidecar too',
    'c.240.n1': 'New ZCode integration: hook configuration merged into <code>~/.zcode/cli/config.json</code>, preserving unknown user fields and existing hooks, with a backup before writing',
    'c.240.n2': 'Hook output gains the Claude/ZCode JSON protocol: prompt injection wrapped as additionalContext, Stop blocking via decision:block',
    'c.240.n3': 'Per-agent skills directories (ZCode installs to <code>~/.zcode/skills</code>); <code>ok doctor</code> reports hook status per agent',
    'c.240.s1': 'To activate on the ZCode side: run <code>ok setup --agent zcode</code>, then <b>start a new session</b> (ZCode snapshots hook config at session start)',
    'c.233.n1': 'After an upgrade, the GUI automatically pops up the changelog: skipped versions accumulate; the Misc tab gains a permanent entry to revisit all history',
    'c.233.n2': 'New APIs: <code>GET /api/changelog</code>, <code>POST /api/changelog/seen</code> (no popup on first run, dev builds, or downgrades)',
    'c.232.n1': 'Hook timeout is now configurable: all three hooks read the global <code>[hooks] timeout_sec</code>; the GUI Guide tab gains a "hook timeout" card',
    'c.232.n2': 'Capture flow now routes "new requirements" to the wiki: the propose skill gained a "classify first" guide (experience → draft / structural → wiki)',
    'c.232.f1': 'PostToolUse&#39;s three silent-return branches now log to ok.log — previously, touched records could be lost for an entire session under high load without any trace',
    'c.230.n1': 'New Pi integration (TypeScript extension hook), alongside Kimi Code; the <code>internal/agentx</code> adapter registry lets a new agent plug in with a single file',
    'c.230.n2': 'The GUI Guide tab adds an agent dropdown: the hooks card follows the selected agent; <code>ok setup --agent &lt;id&gt;</code> installs one agent only',
    'c.230.f1': 'Hook install/uninstall now collects errors per agent: one agent&#39;s failure no longer affects the others',
    'c.224.i1': 'The tray menu now triggers on right-click (left-click conflicted with double-click); double-click still opens/focuses the GUI',
    'c.223.n1': 'System tray: an icon while the daemon runs; right-click menu (version + quit), double-click opens/focuses the single GUI window',
    'c.223.f1': 'Hook invocations no longer flash a cmd window (subprocesses silenced)',
    'c.223.f2': 'The installer stops the daemon before copying files, avoiding ok.exe being locked during upgrades',
    'c.223.i1': 'Leaner hook injection: default budget 1500→800, top_n 3→2; retrieval hits inject summary + file path instead of full text (mandatory entries still inject full text)',
    'c.older.t': 'Earlier',
    'c.older.l': 'v1.x early development logs live as dated files in the repo&#39;s <a href="https://github.com/zhangxyfs/OpenKnowledge/blob/master/docs/changelogs/" target="_blank" rel="noopener">docs/changelogs/</a>',
    'c.foot': '<a href="index.html">Home</a>· <a href="docs.html">Docs</a>· <a href="https://github.com/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>'
  };

  var root = document.documentElement;

  /* ── 主题 ── */
  var themeBtn = document.getElementById('themeToggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var t = root.dataset.theme === 'dark' ? 'light' : 'dark';
      root.dataset.theme = t;
      try { localStorage.setItem('ok-theme', t); } catch (e) {}
    });
  }

  /* ── 语言 ── */
  // 优先级：URL 参数（?lang=en 可分享）> 手动选择（localStorage 记忆）> 系统语言（中文→zh，其他→en）
  var q = new URLSearchParams(location.search);
  var savedLang = null;
  try { savedLang = localStorage.getItem('ok-lang'); } catch (e) {}
  var sysLang = 'zh';
  try { sysLang = /^zh/i.test(navigator.language || '') ? 'zh' : 'en'; } catch (e) {}
  var lang = q.get('lang') || savedLang || sysLang;
  if (lang !== 'en') lang = 'zh';

  /* URL 参数 ?theme=dark|light 同理（首屏防闪烁由 head 内联脚本负责同参数）。
     主题默认跟随系统；未手动选择时，系统深浅色切换实时跟随。 */
  var themeParam = q.get('theme');
  if (themeParam === 'dark' || themeParam === 'light') {
    root.dataset.theme = themeParam;
    try { localStorage.setItem('ok-theme', themeParam); } catch (e) {}
  }
  try {
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function (e) {
      var saved = null;
      try { saved = localStorage.getItem('ok-theme'); } catch (err) {}
      if (!saved) root.dataset.theme = e.matches ? 'dark' : 'light';
    });
  } catch (e) {}

  function applyLang(l) {
    lang = l;
    try { localStorage.setItem('ok-lang', l); } catch (e) {}
    root.lang = l === 'en' ? 'en' : 'zh-CN';
    var els = document.querySelectorAll('[data-i18n]');
    for (var i = 0; i < els.length; i++) {
      var el = els[i];
      var k = el.getAttribute('data-i18n');
      if (l === 'en') {
        if (el._zh === undefined) el._zh = el.innerHTML;   // 首次缓存中文原文
        if (EN[k] !== undefined) el.innerHTML = EN[k];
      } else if (el._zh !== undefined) {
        el.innerHTML = el._zh;
      }
    }
    // 语言切换按钮高亮
    var spans = document.querySelectorAll('.lang-toggle span[data-lang]');
    for (var j = 0; j < spans.length; j++) {
      spans[j].classList.toggle('on', spans[j].getAttribute('data-lang') === l);
    }
    // 首页下载按钮文案跟随语言
    if (window.OKUpdateDownload) window.OKUpdateDownload(l);
  }

  var langBtn = document.getElementById('langToggle');
  if (langBtn) {
    langBtn.addEventListener('click', function () {
      applyLang(lang === 'en' ? 'zh' : 'en');
    });
  }

  applyLang(lang);
})();
