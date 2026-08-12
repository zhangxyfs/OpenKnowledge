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
    'i.hint': 'Auto-detects your OS · <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/releases/download/v2.10.1/OpenKnowledgeSetup-2.10.1.exe">Windows</a> / <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/releases/download/v2.10.1/openknowledge_2.10.1_amd64.deb">Linux .deb</a> / <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/releases/download/v2.10.1/openknowledge_2.10.1_linux_amd64.tar.gz">Linux .tar.gz</a> · <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/releases" target="_blank" rel="noopener">All releases</a>',
    'i.feat.h': 'Features',
    'i.feat.sub': 'Single-binary Go CLI (ok) with zero runtime dependencies',
    'i.f1t': 'Base injection',
    'i.f1p': 'On the first question of each session, the project&#39;s mandatory entries (full text) plus the knowledge index are sent to the AI.',
    'i.f2t': 'Retrieval injection',
    'i.f2p': 'Every question is matched by hybrid keyword + vector-semantic retrieval, injecting the most relevant entries — e.g. your git commit conventions.',
    'i.f3t': 'Enforcement',
    'i.f3p': 'Tracks files the AI modified; if code changed but no changelog was written by the end of the turn, the turn is blocked until it&#39;s fixed.',
    'i.f4t': 'Multi-agent support',
    'i.f4p': 'Kimi Code, Pi, ZCode and Reasonix share the same knowledge base through an extensible adapter architecture.',
    'i.f5t': 'One-step setup',
    'i.f5p': '<code>ok setup</code> configures hooks, skills and embeddings; <code>ok off</code> / <code>ok on</code> toggles everything anytime.',
    'i.f6t': 'Web GUI & daemon',
    'i.f6p': 'Double-click the exe or run <code>ok gui</code> to open the admin UI; a single resident process serves the GUI and every agent&#39;s hook requests with millisecond forwarding.',
    'i.shots.h': 'Screenshots',
    'i.shots.sub': 'The three GUI tabs + knowledge capture in a real session',
    'i.s1c': 'Manage tab: project / entry list, search preview and the global switch',
    'i.s2c': 'Guide tab: one-click hooks, skills and embedding setup',
    'i.s3c': 'In a real session: after finishing a release, the AI captures wiki entries on its own (propose mode)',
    'i.inst.h': 'Install',
    'i.inst.sub': 'Windows installer / Linux packages / manual build — pick one',
    'i.w1h': 'Windows installer',
    'i.w1tag': 'Recommended',
    'i.w1p': 'No Go toolchain needed; installs to <code>%LOCALAPPDATA%\\Programs\\OpenKnowledge</code> (no admin rights). Uninstall keeps your knowledge base by default.',
    'i.w1pre': '<code><span class="c"># Download from Releases, then run</span>\nOpenKnowledgeSetup-2.10.1.exe</code>',
    'i.lxh': 'Linux (amd64)',
    'i.lxp': 'Statically compiled, zero dependencies.',
    'i.lxpre': '<code><span class="c"># tar.gz</span>\ntar xzf openknowledge_*_linux_amd64.tar.gz\ncd openknowledge_* &amp;&amp; ./ok setup\n\n<span class="c"># or .deb</span>\nsudo dpkg -i openknowledge_*_amd64.deb\nok setup</code>',
    'i.mbh': 'Manual build',
    'i.mbp': 'Requires Go ≥ 1.25.',
    'i.mbpre': '<code>go build -o ok.exe ./cmd/ok   <span class="c"># Windows</span>\ngo build -o ok ./cmd/ok       <span class="c"># Linux/macOS</span>\n./ok setup                    <span class="c"># first-run wizard (idempotent)</span></code>',
    'i.foot': '<a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>· <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/releases" target="_blank" rel="noopener">Releases</a>· <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/src/branch/master/docs/ARCHITECTURE.md" target="_blank" rel="noopener">Architecture</a>',

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
    'd.s1l1': '<b>Writes hook configurations</b> — covering every detected AI assistant: Kimi Code gets 3 hook marker blocks in <code>~/.kimi-code/config.toml</code> (backup + idempotent overwrite), Pi gets a TypeScript extension, ZCode gets a merged <code>config.json</code> write, Reasonix gets an Extension Protocol plugin package, opencode gets a TypeScript plugin in <code>~/.config/opencode/plugins/</code>',
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
    'd.s6p': '<b>Double-click <code>ok.exe</code> (no arguments) or run <code>ok gui</code></b> to launch the web admin UI; the browser opens <code>http://127.0.0.1:17888</code>. The GUI is hosted by the resident daemon — closing the page does not stop it; use <code>ok daemon stop</code> to stop the service. Three tabs:',
    'd.s6l1': '<b>Manage</b>: project/entry list, create/edit/delete, search preview, global switch, branch filter',
    'd.s6l2': '<b>Guide</b>: a graphical <code>ok setup</code>, plus hook timeout, enforcement mode (reasonix) and uninstall cards',
    'd.s6l3': '<b>Misc</b>: data export/import, changelogs, user guide, delete project knowledge base (triple confirmation)',
    'd.s7h': 'Configuration',
    'd.s7p': 'Effective config = built-in defaults ← global <code>~/.openknowledge/config.toml</code> ← per-project <code>~/.openknowledge/projects/&lt;name&gt;/config.toml</code> (each layer overrides the previous).',
    'd.s7pre': '<code><span class="c"># Global config (ok setup can write this interactively)</span>\n[embedding]\nbase_url = "https://api.openai.com/v1"   <span class="c"># any OpenAI-compatible service</span>\napi_key = "sk-..."                        <span class="c"># or use api_key_env for an environment variable</span>\nmodel = "text-embedding-3-small"\n\n<span class="c"># Project config: enforcement rule example</span>\n[[enforce]]\ntype = "changelog_required"\ncode_globs = ["**/*.go"]                  <span class="c"># touching these = changed code</span>\nchangelog_glob = "docs/changelogs/**"     <span class="c"># touching these = wrote a changelog</span>\nmessage = "Code was changed this session without a changelog update; please add one first."</code>',
    'd.s8h': 'Retrieval algorithm',
    'd.s8p': 'Hybrid retrieval on SQLite + FTS5 — the two scores are normalized and blended (α/β tunable), top-2 injected:',
    'd.s8l1': '<b>Keyword channel</b>: FTS5 full-text index + BM25 scoring, weighted across title/tags/summary/body; Chinese uses bigram tokenization, zero dependencies',
    'd.s8l2': '<b>Semantic channel</b>: OpenAI-compatible embeddings + cosine similarity, recalling entries that "ask differently but mean the same"',
    'd.s8l3': '<b>Drafts stay out of both channels</b>: excluded from FTS and vectors until approved',
    'd.s8tail': '~30ms per query over 10k entries; when the embedding service is down it degrades to keyword-only retrieval and injection never goes missing. Implementation details in <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/src/branch/master/docs/ARCHITECTURE.md" target="_blank" rel="noopener">ARCHITECTURE §17</a>.',
    'd.s9h': 'How it works',
    'd.s9p': 'Using Kimi Code as the example, the assistant calls <code>ok</code> at three moments (other agents trigger the equivalents through their own adapters):',
    'd.s9tbl': '<table class="doc"><tr><th>Hook</th><th>When it runs</th><th>Effect</th></tr><tr><td><code>UserPromptSubmit</code></td><td>Every user message, before the model call</td><td>First question: mandatory entries + index; every question: retrieval injection</td></tr><tr><td><code>PostToolUse</code></td><td>After the AI successfully writes/edits a file</td><td>Records the touched file into session state</td></tr><tr><td><code>Stop</code></td><td>When the AI&#39;s turn is about to end</td><td>Code changed without changelog → exit 2 block (at most once per rule per session)</td></tr></table>',
    'd.s9note': '<b>Note</b>All hook paths are fail-open: any internal error is only logged (<code>~/.openknowledge/ok.log</code>) and never disrupts the session.',
    'd.foot': '<a href="index.html">Home</a>· <a href="changelog.html">Changelog</a>· <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>',

    /* ── 更新日志页 ── */
    'meta.title.c': 'Changelog — OpenKnowledge',
    'c.ledger': 'Release ledger',
    'c.h1': 'Changelog',
    'c.lede': 'OpenKnowledge release history. Dates are in 2026.',
    'c.cta': 'Download latest v2.10.1',
    'c.latest': 'Latest',
    'c.2101.f1': 'Export/import now covers wiki state: <code>state/wiki.json</code> (base branch + per-branch cursors + merge lineage) travels with the zip backup and is restored verbatim. Previously only entries and config were exported — the "export zip backup" of project deletion therefore lacked the lineage, and re-importing zeroed the GUI lineage rows and lost the wiki cursors, forcing a full rescan',
    'c.2101.n1': 'Backups without wiki.json skip the restore — backward compatible; the entry index is still rebuilt automatically on import',
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
    'c.older.l': 'v1.x early development logs live as dated files in the repo&#39;s <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge/src/branch/master/docs/changelogs/" target="_blank" rel="noopener">docs/changelogs/</a>',
    'c.foot': '<a href="index.html">Home</a>· <a href="docs.html">Docs</a>· <a href="https://z7dream-gitea.iepose.cn/zhangxyfs/OpenKnowledge" target="_blank" rel="noopener">Source</a>'
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
