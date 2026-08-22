"use strict";
/* OkManager 配置中心外壳：五菜单左右栏 + 顶栏（侧栏折叠 / 中英 / 昼夜）+ hash 路由 + 整页重渲滚动保持。
   规范源 docs/prototypes/prototype-manager-v2.html——I18N/ICON/MENUS/el/esc/t/state/render/renderBody/
   设置卡助手（pswitch/pcard/prow/pnumLive/ptext/pDirtyLive 等）均为原型平移；日志页（Task 3）、其他页
   （Task 4）、管理页（Task 5）、设置页（Task 6）与引导页（Task 7，含 Reasonix/Codex 专属件）均已接真后端。 */

/* ================= 后端 API ================= */
// 语义沿用旧 GUI 的 api()：X-Ok-Token 鉴权头；对象 body 自动 JSON + Content-Type；204 → null；
// 401 视为 daemon 被替换（多 exe 共存/重启）后页面 token 过期，自动刷新一次取新 token
// （sessionStorage 标志防刷新循环，任一成功响应后清除）；网络层错误包装为"网络错误"。
async function api(path, opts){
  opts = opts || {};
  const headers = { "X-Ok-Token": window.OK_TOKEN || "" };
  let body = opts.body;
  if(body != null){
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, { method: opts.method || "GET", headers: headers, body: body, signal: opts.signal });
  } catch(err){
    if(err && err.name === "AbortError") throw err;   // 主动取消原样上抛（调用方静默处理）
    throw new Error("网络错误: " + err.message);
  }
  if(res.ok) sessionStorage.removeItem("ok-401-reload");
  if(res.status === 204) return null;
  const data = await res.json().catch(()=>({}));
  if(!res.ok){
    if(res.status === 401 && !sessionStorage.getItem("ok-401-reload")){
      sessionStorage.setItem("ok-401-reload", "1");
      setTimeout(()=>{ location.reload(); }, 800);
      return new Promise(()=>{});
    }
    const err = new Error(data.error || ("请求失败: " + res.status));
    err.status = res.status;
    throw err;
  }
  return data;
}

/* ================= i18n（仅界面 chrome，条目数据不翻译） ================= */
const I18N = {
  zh: {
    manage:"管理", setup:"引导", prefs:"设置", logs:"日志", misc:"其他",
    treeCaption:"知识条目", filter:"过滤条目…", pickEntry:"← 从树中选择一条知识条目",
    modified:"修改于",
    mandatory:"★ mandatory", optional:"非 mandatory", draft:"草稿", archived:"已归档",
    collapseTip:"收起/展开侧栏",
    stDetected:"已检测到", stAgentUnit:"个 agent", stHooked:"已接入", stHookedUnit:"个",
    setupSub:"接入 = 写入 hooks 配置 + 安装 ok 技能（等同 CLI 的 ok setup）",
    redetect:"↻ 重新检测", detecting:"↻ 检测中…",
    hookOn:"Hook 已接入", hookOff:"Hook 未接入", skillOn:"技能已安装", skillOff:"技能未安装",
    install:"安装", uninstall:"卸载", installing:"安装中…", uninstalling:"卸载中…",
    kindHook:"HOOK", kindPlugin:"插件",
    detailQ:"▸ 安装会动哪些文件？", detailClose:"▾ 收起明细",
    detailInstall:"<b>安装</b>：写入 hooks 配置", detailJoin:" + 安装 ok 技能到技能目录；",
    detailRemove:"<b>卸载</b>：按标记移除 ok 写入的 hooks 段，不动用户其他配置与已安装技能。",
    fbInstall:"✓ 已接入：hooks 已写入，技能已安装（下次会话生效）",
    fbUninstall:"✓ 已卸载：ok 写入的 hooks 已移除",
    noneDetected:"未检测到任何已安装的 agent",
    rxTitle:"强制检查方式", rxDesc:"仅对 Reasonix 生效：sidecar 每条输入实时读取，改动即时生效。auto 自省 = 经验沉淀提醒；changelog = 改了代码未更新日志的检查。",
    rxMixed:"软+硬（默认）：自省提醒、changelog 阻断输入", rxSoft:"全软提示：都以前缀提醒，不打断输入", rxHard:"全硬阻断：都阻断输入",
    rxSaveFail:"强制检查方式保存失败：",
    cxHelpQ:"▸ Codex 信任门与版本说明", cxHelpClose:"▾ 收起说明",
    cxHelpBody:"① Codex 0.118 把 hooks 列为实验特性（默认关闭）：ok 安装时已自动在 <code>~/.codex/config.toml</code> 开启并写入信任记录，<strong>无需手动确认信任</strong>；在 Codex 的 设置 → 编码 → 钩子 页面可随时查看或逐条关闭。<br>② Windows 下 hook 命令经 <code>ok-hook-*.cmd</code> 包装文件执行（规避 Codex 引号 bug），exe 迁移/升级后 ok 自愈不再失效。<br>③ <strong>版本注意</strong>：桌面端 26.707 与命令行 codex 0.147+ 均已实证可用（更早版本未验证）；若集成不生效，先确认 Codex 已完全重启。<br>④ 手动修改 config.toml 后需重启 Codex 才能生效。",
    opInstallFail:"安装失败：", opUninstallFail:"卸载失败：",
    save:"保存", saved:"✓ 已保存",
    gTitle:"全局开关", gDesc:"一键启停全部 agent 的 hooks 注入与强制检查（等同 CLI 的 ok on / ok off）",
    gOnFb:"✓ 已开启全部 hooks", gOffFb:"✓ 已关闭全部 hooks",
    eManage:"管理配置", eProfiles:"服务列表", eAdd:"+ 新增服务", eEdit:"编辑", eDel:"删除", eSetActive:"设为使用中",
    fName:"名称", fType:"类型", fBase:"base_url", fModel:"模型", fKey:"api_key", fMirror:"下载源（仅 builtin）",
    eAddTitle:"新增服务", eEditTitle:"编辑服务", fOk:"确定", fCancel:"取消",
    typeBuiltin:"内置本地模型（ok 托管 · 无需联网）", typeOllama:"Ollama（本机/局域网服务）", typeOpenai:"自定义（OpenAI 兼容服务）",
    tagBuiltin:"内置", tagOllama:"Ollama", tagCustom:"自定义",
    mirrorHf:"hf-mirror 镜像（国内推荐）", mirrorOfficial:"huggingface 官方", downloaded:"（已下载）",
    fOlUrl:"服务地址", keySaved:"已保存（留空保持不变）", eGlobal:"全局",
    kindOpenai:"OpenAI 兼容（/chat/completions）", kindAnthropic:"Anthropic 兼容（/v1/messages）",
    tagOpenai:"OpenAI 兼容", tagAnthropic:"Anthropic 兼容",
    fTemp:"temperature（高级，留空 = 不传）", fMaxTokens:"max_tokens（高级，0 = 默认）",
    eTitle:"语义检索（embedding）", eDesc:"混合检索的语义通道；不配置任何服务时退化为纯关键词检索",
    eNone:"未配置（仅关键词检索）", eDir:"内置模型目录", eActive:"使用中",
    lTitle:"模型配置（LLM）", lDesc:"生成场景（条目优化等）调用的大模型服务；temperature 留空 = 不传",
    lTest:"测试连接", lTesting:"测试中…", lTestOk:"✓ 连通（{ms}ms）",
    lNone:"未配置（✨ 优化不可用）",
    hTitle:"Hook 超时", hDesc:"写入各 agent hooks 的超时秒数。2026-08-04 曾发生 Windows 高负载下 5s 超时致 PostToolUse 整会话静默丢失，故默认 10", hSec:"超时（秒）",
    gtTitle:"泛化门控", gtDesc:"命中内置/自定义短语的泛化 prompt 跳过检索注入与 embed 调用；全局生效（对所有项目生效）",
    gtOn:"启用门控", gtStatus:"内置 {b} 条 · 自定义 {n} 条", gtManage:"管理短语表",
    gtBuiltin:"内置短语（只读，随版本演进）", gtCustom:"自定义短语", gtAdd:"+ 添加", gtPh:"新短语…",
    eDlReady:"✓ 模型已就绪（{dim} 维），sidecar 按需拉起、空闲自动退出",
    eDlBtn:"下载模型（{size}）", eDlDoing:"正在下载 — {done} / {total}", eDlErr:"上次下载失败：",
    eDlNoRt:"⚠ 推理运行时缺失——内置模式仅安装版可用（裸 exe 形态请用 Ollama/自定义）",
    eDirOpen:"打开",
    eIdxWarn:"⚠ 使用中模型（{a}）与当前项目索引（{i}）不符——切换后请运行 ok index 重建",
    cTitle:"跨轮注入冷却", cDesc:"同会话内已注入的检索条目冷却 N 个 prompt 轮不再注入（门控轮也计）；0 = 关闭（每轮都可注入）；全局生效（对所有项目生效）", cTurns:"冷却轮数",
    rTitle:"规则配置（强制检查）", rDesc:"AI 改动命中 code globs 的文件时，回合结束校验 changelog 是否同步更新；全局生效（对所有项目生效）",
    rType:"类型", rGlobs:"code globs", rCl:"changelog glob", rMsg:"提示语", rAdd:"+ 添加规则",
    capTitle:"经验沉淀", capDesc:"propose = AI 提议草稿、人批准后入库；auto = 按轮次间隔自动提取；全局生效（对所有项目生效）",
    capMode:"模式", capPropose:"propose（人批准）", capAuto:"auto（自动提取）", capInterval:"轮次间隔",
    lgSemantic:"◆ 语义", lgFilter:"过滤日志…", lgAuto:"自动刷新",
    lgMeta:"共 {n} 行 · 显示 {m} 行", lgEmpty:"（无匹配日志）",
    xExport:"数据导出", xExportDesc:"导出 registry 与条目（不含索引，导入时自动重建）",
    xImport:"数据导入", xImportDesc:"导入 zip 备份（数据导出的产物）；条目合并入库，索引自动重建",
    xProject:"项目", xAllProjects:"全部项目", xDoExport:"导出", xDoImport:"导入",
    xExported:"✓ 已导出", xImported:"✓ 已导入 {n} 条，跳过 {s} 条（格式损坏），涉及项目：{ps}；同名条目已覆盖", xFile:"文件",
    xExportFail:"导出失败：", xImportFail:"导入失败：", xImportPick:"请先选择 zip 文件",
    xChlog:"更新日志", xChlogDesc:"查看各版本的新功能与修复；升级后首次打开会自动弹出",
    xChlogEmpty:"暂无更新日志",
    upTitle1:"新版本 v{v} 更新内容", upTitleN:"已更新到 v{v}（含最近 {n} 个版本）",
    xHelp:"使用帮助", xHelpDesc:"怎么调用、怎么配置、常见问题", xView:"查看", xGotIt:"知道了",
    xHelpErr:"帮助文档加载失败，请检查安装是否完整。",
    xDel:"删除项目知识库", xDelDesc:"永久删除所选项目的全部知识、索引与配置，并注销注册表（hooks 不再注入）。项目源码目录不受影响。",
    xDelBtn:"删除…", xDelImpact:"将永久删除 {p} 的 {n} 条知识条目、索引与项目配置，并从注册表注销（hooks 不再注入）。",
    xDelCounting:"正在统计条目…",
    xDelImpactFail:"条目统计失败（不影响删除操作）。将删除项目「{p}」的知识库、索引与项目配置，并注销注册表（hooks 不再注入）。",
    xDelBackup:"删除前先导出 zip 备份", xDelAck:"我已了解后果，此操作不可撤销", xDelHint:"请输入完整项目名以确认",
    xDelConfirm:"永久删除", xDelDeleting:"删除中…", xDeleted:"✓ 已删除 {p}",
    xDelBackupFail:"备份导出失败（{s}），已中止删除",
    xDelPartial:"项目已注销，但{w}，请手动清理 {d}",
    xAbout:"关于", xVer:"版本", xHome:"数据目录", xProjCount:"已注册项目", xProjUnit:" 个",
    xLoadFail:"数据加载失败：",
    mgNew:"+ 新建", mgLoading:"加载中…", mgTreeErr:"条目加载失败：",
    readmeEmpty:"该项目没有 README，也没有 wiki 概述条目。可在项目根目录添加 README.md，或用 ok wiki / openknowledge-wiki 技能生成项目概述。",
    readmeSrcReadme:"项目 README · {p}", readmeSrcWiki:"项目 wiki 概述 · {p}",
    readmeLoadFail:"README 加载失败：", lazyMore:"还有 {n} 条…",
    opEdit:"编辑", opApprove:"批准", opArchive:"归档", opUnarchive:"取消归档", opDelete:"删除",
    cfmDelete:"确定删除条目「{t}」？",
    cfmArchive:"归档条目「{t}」？归档后退出 INDEX 与强制注入，仍可被检索命中。",
    emNew:"新建条目", emEdit:"编辑条目", emExists:"条目已存在", emNoProject:"尚无已注册项目，请先 ok init",
    editingSub:"（详情区内联编辑 · 保存前不落盘）",
    fTitle:"标题", fTags:"tags（逗号分隔）", fMand:"mandatory（每会话必注入）",
    fSummary:"摘要", fBody:"正文（markdown）", fBodyHint:"编辑区撑满剩余高度",
    typeRule:"规则", typePitfall:"踩坑", typeNote:"笔记", typeReference:"参考",
    optBtn:"✨ 优化", optBusy:"优化中…", optEmpty:"正文为空，无可优化内容",
    optTip:"结合项目真实代码与相关条目据实润色标题/标签/摘要/正文（类型与 mandatory 不动）；先出对照预览，确认回填后点保存才生效。",
    cmpTitle:"✨ 优化对照", cmpBasis:"依据：条目引用的真实代码 + 相关条目 + INDEX 摘录",
    cmpNotice:"模型判断：当前内容已足够简练准确，无需优化。如下仍有差异仅为排版/标点，可逐字段回填或直接放弃。",
    cmpApply:"全部接受并回填", cmpDiscard:"放弃", cmpFill:"回填", cmpFilled:"✓ 已回填",
    cmpOld:"原内容", cmpNew:"优化后", cmpNote:"回填只改表单，点「保存」才写入 .md",
    cmpUsage:"消耗 {t} token（入 {p} / 出 {c}）",
    llmTitle:"尚未配置模型",
    llmMsg:"「优化」需要调用大模型。请先到 设置页 → 模型配置 添加一个 OpenAI 兼容或 Anthropic 兼容服务。",
    llmGo:"去配置",
    inheritBadge:"继承基线",
    inheritBubble:"wiki 基线继承自 {src} · 落后 {n} commit",
  },
  en: {
    manage:"Manage", setup:"Setup", prefs:"Settings", logs:"Logs", misc:"Misc",
    treeCaption:"Entries", filter:"Filter entries…", pickEntry:"← Select an entry from the tree",
    modified:"Modified",
    mandatory:"★ mandatory", optional:"optional", draft:"Draft", archived:"Archived",
    collapseTip:"Collapse/expand sidebar",
    stDetected:"Detected", stAgentUnit:"agents", stHooked:"integrated", stHookedUnit:"",
    setupSub:"Integrating = writing hooks config + installing ok skills (same as CLI ok setup)",
    redetect:"↻ Re-detect", detecting:"↻ Detecting…",
    hookOn:"Hook integrated", hookOff:"Hook not integrated", skillOn:"Skills installed", skillOff:"Skills not installed",
    install:"Install", uninstall:"Uninstall", installing:"Installing…", uninstalling:"Uninstalling…",
    kindHook:"HOOK", kindPlugin:"PLUGIN",
    detailQ:"▸ What files does install touch?", detailClose:"▾ Collapse",
    detailInstall:"<b>Install</b>: writes hooks config to ", detailJoin:" and installs ok skills into the skills dir; ",
    detailRemove:"<b>Uninstall</b>: removes only the ok-marked hooks block; other config and installed skills stay untouched.",
    fbInstall:"✓ Integrated: hooks written, skills installed (takes effect next session)",
    fbUninstall:"✓ Uninstalled: ok-written hooks removed",
    noneDetected:"No installed agents detected",
    rxTitle:"Enforce mode", rxDesc:"Reasonix only: the sidecar re-reads this on every input, so changes take effect immediately. auto = capture reminder; changelog = code changed without a changelog update.",
    rxMixed:"Mixed (default): reminder for auto, changelog blocks input", rxSoft:"All soft: prefix reminders only, never interrupt", rxHard:"All hard: both block input",
    rxSaveFail:"Failed to save enforce mode: ",
    cxHelpQ:"▸ Codex trust gate & version notes", cxHelpClose:"▾ Collapse",
    cxHelpBody:"① Codex 0.118 marks hooks experimental (off by default): ok already enabled them in <code>~/.codex/config.toml</code> and wrote the trust record at install time — <strong>no manual trust confirmation needed</strong>; review or disable individual hooks under Codex Settings → Coding → Hooks.<br>② On Windows, hook commands run via <code>ok-hook-*.cmd</code> wrappers (working around a Codex quoting bug); ok self-heals after exe moves/upgrades.<br>③ <strong>Versions</strong>: desktop 26.707 and CLI codex 0.147+ are verified (earlier versions untested); if integration does not take effect, fully restart Codex first.<br>④ Restart Codex after manually editing config.toml.",
    opInstallFail:"Install failed: ", opUninstallFail:"Uninstall failed: ",
    save:"Save", saved:"✓ Saved",
    gTitle:"Global switch", gDesc:"Enable/disable hooks injection and enforce checks for all agents (same as CLI ok on / ok off)",
    gOnFb:"✓ All hooks enabled", gOffFb:"✓ All hooks disabled",
    eManage:"Manage", eProfiles:"Services", eAdd:"+ Add service", eEdit:"Edit", eDel:"Delete", eSetActive:"Set active",
    fName:"Name", fType:"Type", fBase:"base_url", fModel:"Model", fKey:"api_key", fMirror:"Mirror (builtin only)",
    eAddTitle:"Add service", eEditTitle:"Edit service", fOk:"OK", fCancel:"Cancel",
    typeBuiltin:"Builtin local model (ok-managed, offline)", typeOllama:"Ollama (local/LAN service)", typeOpenai:"Custom (OpenAI-compatible)",
    tagBuiltin:"Builtin", tagOllama:"Ollama", tagCustom:"Custom",
    mirrorHf:"hf-mirror (recommended in CN)", mirrorOfficial:"huggingface official", downloaded:" (downloaded)",
    fOlUrl:"Service URL", keySaved:"Saved (leave empty to keep)", eGlobal:"Global",
    kindOpenai:"OpenAI-compatible (/chat/completions)", kindAnthropic:"Anthropic-compatible (/v1/messages)",
    tagOpenai:"OpenAI-compat", tagAnthropic:"Anthropic-compat",
    fTemp:"temperature (advanced, empty = not sent)", fMaxTokens:"max_tokens (advanced, 0 = default)",
    eTitle:"Semantic retrieval (embedding)", eDesc:"The semantic channel of hybrid retrieval; degrades to keyword-only when no service is configured",
    eNone:"Not configured (keyword-only)", eDir:"Builtin models dir", eActive:"Active",
    lTitle:"Model config (LLM)", lDesc:"LLM services for generation tasks (entry polishing etc.); empty temperature = not sent",
    lTest:"Test connection", lTesting:"Testing…", lTestOk:"✓ Connected ({ms}ms)",
    lNone:"Not configured (✨ polish unavailable)",
    hTitle:"Hook timeout", hDesc:"Timeout seconds written into each agent's hooks. On 2026-08-04 a 5s timeout under Windows load silently dropped PostToolUse for an entire session — hence default 10", hSec:"Timeout (s)",
    gtTitle:"Generalization gate", gtDesc:"Prompts matching builtin/custom phrases skip retrieval injection and embed calls; applies globally to all projects",
    gtOn:"Enable gate", gtStatus:"{b} builtin · {n} custom", gtManage:"Manage phrases",
    gtBuiltin:"Builtin phrases (read-only, evolve with releases)", gtCustom:"Custom phrases", gtAdd:"+ Add", gtPh:"New phrase…",
    eDlReady:"✓ Model ready ({dim} dim); sidecar starts on demand and exits when idle",
    eDlBtn:"Download model ({size})", eDlDoing:"Downloading — {done} / {total}", eDlErr:"Last download failed: ",
    eDlNoRt:"⚠ Inference runtime missing — builtin mode requires the installer edition (bare exe: use Ollama/custom)",
    eDirOpen:"Open",
    eIdxWarn:"⚠ Active model ({a}) differs from the project index ({i}) — run ok index to rebuild after switching",
    cTitle:"Cross-turn injection cooldown", cDesc:"Retrieved entries already injected in this session cool down for N prompt turns (gate turns count too); 0 = off; applies globally to all projects", cTurns:"Cooldown turns",
    rTitle:"Rules (enforce checks)", rDesc:"When AI edits files matching code globs, session end verifies the changelog was updated; applies globally to all projects",
    rType:"Type", rGlobs:"code globs", rCl:"changelog glob", rMsg:"Message", rAdd:"+ Add rule",
    capTitle:"Experience capture", capDesc:"propose = AI drafts, human approves; auto = extract every N turns; applies globally to all projects",
    capMode:"Mode", capPropose:"propose (human-approved)", capAuto:"auto (automatic)", capInterval:"Turn interval",
    lgSemantic:"◆ Semantic", lgFilter:"Filter logs…", lgAuto:"Auto-refresh",
    lgMeta:"{n} lines · showing {m}", lgEmpty:"(no matching logs)",
    xExport:"Export data", xExportDesc:"Exports the registry and entries (no index — rebuilt on import).",
    xImport:"Import data", xImportDesc:"Import a zip backup (produced by Export); entries merge in, index rebuilds automatically",
    xProject:"Project", xAllProjects:"All projects", xDoExport:"Export", xDoImport:"Import",
    xExported:"✓ Exported", xImported:"✓ Imported {n} entries, skipped {s} (corrupt); projects: {ps}; same-name entries overwritten", xFile:"File",
    xExportFail:"Export failed: ", xImportFail:"Import failed: ", xImportPick:"Choose a zip file first",
    xChlog:"Changelog", xChlogDesc:"New features and fixes per release; pops up automatically on first open after an upgrade",
    xChlogEmpty:"No changelog entries",
    upTitle1:"What's new in v{v}", upTitleN:"Updated to v{v} (includes the last {n} versions)",
    xHelp:"User guide", xHelpDesc:"How to call, configure, and FAQ", xView:"View", xGotIt:"Got it",
    xHelpErr:"Failed to load the help doc; please check the installation.",
    xDel:"Delete project KB", xDelDesc:"Permanently deletes all entries, index and config of the selected project, and unregisters it (hooks stop injecting). Source directory is untouched.",
    xDelBtn:"Delete…", xDelImpact:"This will permanently delete {p}'s {n} entries, index and project config, and unregister it (hooks stop injecting).",
    xDelCounting:"Counting entries…",
    xDelImpactFail:"Entry count failed (delete still works). This deletes project {p}'s KB, index and config, and unregisters it (hooks stop injecting).",
    xDelBackup:"Export a zip backup first", xDelAck:"I understand this cannot be undone", xDelHint:"Type the full project name to confirm",
    xDelConfirm:"Delete forever", xDelDeleting:"Deleting…", xDeleted:"✓ Deleted {p}",
    xDelBackupFail:"Backup export failed ({s}); delete aborted",
    xDelPartial:"Project unregistered, but {w}; please remove {d} manually",
    xAbout:"About", xVer:"Version", xHome:"Data dir", xProjCount:"Registered projects", xProjUnit:"",
    xLoadFail:"Failed to load data: ",
    mgNew:"+ New", mgLoading:"Loading…", mgTreeErr:"Failed to load entries: ",
    readmeEmpty:"This project has no README and no wiki overview entry. Add a README.md to the project root, or generate an overview with ok wiki / the openknowledge-wiki skill.",
    readmeSrcReadme:"Project README · {p}", readmeSrcWiki:"Project wiki overview · {p}",
    readmeLoadFail:"Failed to load README: ", lazyMore:"{n} more…",
    opEdit:"Edit", opApprove:"Approve", opArchive:"Archive", opUnarchive:"Unarchive", opDelete:"Delete",
    cfmDelete:"Delete entry \"{t}\"?",
    cfmArchive:"Archive entry \"{t}\"? It leaves INDEX and mandatory injection, but stays searchable.",
    emNew:"New entry", emEdit:"Edit entry", emExists:"Entry already exists", emNoProject:"No registered project yet; run ok init first",
    editingSub:"(inline in detail pane · not written until Save)",
    fTitle:"Title", fTags:"tags (comma-separated)", fMand:"mandatory (injected every session)",
    fSummary:"Summary", fBody:"Body (markdown)", fBodyHint:"editor fills remaining height",
    typeRule:"Rule", typePitfall:"Pitfall", typeNote:"Note", typeReference:"Reference",
    optBtn:"✨ Optimize", optBusy:"Optimizing…", optEmpty:"Body is empty; nothing to optimize",
    optTip:"Polishes title/tags/summary/body against real code and related entries (type and mandatory untouched); shows a diff preview first — nothing is written until you fill back and save.",
    cmpTitle:"✨ Optimize comparison", cmpBasis:"Basis: real code referenced by the entry + related entries + INDEX excerpt",
    cmpNotice:"The model considers the content already concise and accurate — no optimization needed. Any difference below is layout/punctuation only; fill back per field or discard.",
    cmpApply:"Accept all & fill back", cmpDiscard:"Discard", cmpFill:"Fill", cmpFilled:"✓ Filled",
    cmpOld:"Original", cmpNew:"Optimized", cmpNote:"Fill-back only edits the form; Save writes the .md",
    cmpUsage:"Cost {t} tokens (in {p} / out {c})",
    llmTitle:"No model configured",
    llmMsg:"Optimize calls an LLM. Add an OpenAI-compatible or Anthropic-compatible service under Settings → Model config first.",
    llmGo:"Configure",
    inheritBadge:"Inherited baseline",
    inheritBubble:"wiki baseline inherited from {src} · {n} commits behind",
  },
};

/* 线性 SVG 图标（Lucide 风格，stroke=currentColor，随主题/选中态变色） */
function svg(inner, size){
  size = size||16;
  return '<svg viewBox="0 0 24 24" width="'+size+'" height="'+size+'" fill="none" stroke="currentColor"'
       + ' stroke-width="2" stroke-linecap="round" stroke-linejoin="round">'+inner+'</svg>';
}
const ICON = {
  manage: svg('<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>'),
  setup:  svg('<circle cx="12" cy="12" r="10"/><polygon points="16.24 7.76 14.12 14.12 7.76 16.24 9.88 9.88 16.24 7.76"/>'),
  prefs:  svg('<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>'),
  logs:   svg('<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>'),
  misc:   svg('<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/>'),
  folder: svg('<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>', 14),
  branch: svg('<line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>', 10),
  panel:  svg('<rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="3" x2="9" y2="21"/>'),
  moon:   svg('<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>'),
  sun:    svg('<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>'),
};
const MENUS = [
  { key:"manage", ico:ICON.manage }, { key:"setup", ico:ICON.setup }, { key:"prefs", ico:ICON.prefs },
  { key:"logs", ico:ICON.logs }, { key:"misc", ico:ICON.misc },
];

/* ================= 引导页：品牌字形（内联 data-URI） ================= */
/* 真实品牌字形（simple-icons / Wikimedia 官方 SVG 路径；内联 data-URI 是既定约束——静态白名单不新增文件路由） */
const LOGOS = {
  kimi: { bg:"#111827", fg:"#fff", vb:"0 0 24 24",
    d:"M21.765.351C22.998.351 24 1.353 24 2.586S22.998 4.82 21.765 4.82h-1.974c-.15 0-.26-.12-.26-.26V2.586A2.237 2.237 0 0 1 21.765.35M9.41 13.388l8.447-8.377c.16-.16.07-.471-.14-.471h-4.55s-.1.02-.14.06l-9.099 9.029c-.14.14-.35.02-.35-.21V4.81c0-.15-.1-.27-.221-.27H.22c-.12 0-.22.12-.22.27v18.57c0 .15.1.27.22.27h3.137c.12 0 .22-.12.22-.27v-3.79c0-.08.03-.16.08-.21l2.826-2.796c.07-.07.16-.08.241-.03l7.546 5.551a8.9 8.9 0 0 0 4.018 1.493c.12.01.23-.11.23-.27V19.76c0-.14-.08-.25-.19-.26a5.8 5.8 0 0 1-2.355-.942l-6.533-4.73c-.14-.09-.15-.32-.03-.441" },
  claude: { bg:"#D97757", fg:"#fff", vb:"0 0 24 24",
    d:"m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z" },
  codex: { bg:"#111827", fg:"#fff", vb:"0 0 20 20",
    d:"M11.248 18.25q-.825 0-1.568-.314a4.3 4.3 0 0 1-1.32-.874 4 4 0 0 1-1.304.214 4 4 0 0 1-2.046-.544 4.27 4.27 0 0 1-1.518-1.485 4 4 0 0 1-.56-2.095q0-.48.131-1.04A4.4 4.4 0 0 1 2.04 10.71a4.07 4.07 0 0 1 .017-3.4 4.2 4.2 0 0 1 1.056-1.418 3.8 3.8 0 0 1 1.6-.842 3.9 3.9 0 0 1 .76-1.683q.593-.759 1.451-1.188a4.04 4.04 0 0 1 1.832-.429q.825 0 1.567.313.742.314 1.32.875a4 4 0 0 1 1.304-.215q1.106 0 2.046.545a4.14 4.14 0 0 1 1.501 1.485q.578.941.578 2.095 0 .48-.132 1.04.66.61 1.023 1.419.363.792.363 1.666 0 .892-.38 1.717a4.3 4.3 0 0 1-1.072 1.435 3.8 3.8 0 0 1-1.584.825 3.8 3.8 0 0 1-.775 1.683 4.06 4.06 0 0 1-1.436 1.188 4.04 4.04 0 0 1-1.832.429m-4.076-2.062q.825 0 1.435-.347l3.103-1.782a.36.36 0 0 0 .164-.313v-1.42L7.881 14.62a.67.67 0 0 1-.726 0l-3.118-1.798a.5.5 0 0 1-.017.115v.198q0 .841.396 1.551.413.693 1.139 1.089a3.2 3.2 0 0 0 1.617.412m.165-2.69a.4.4 0 0 0 .181.05q.083 0 .165-.05l1.238-.71-3.977-2.31a.7.7 0 0 1-.363-.643v-3.58q-.825.362-1.32 1.122a2.9 2.9 0 0 0-.495 1.65q0 .809.413 1.55.412.743 1.072 1.123zm3.91 3.663q.875 0 1.585-.396a2.96 2.96 0 0 0 1.534-2.64v-3.564a.32.32 0 0 0-.165-.297l-1.254-.726v4.604a.7.7 0 0 1-.363.643l-3.119 1.799a3 3 0 0 0 1.783.577m.627-6.039V8.878L10.01 7.822 8.129 8.878v2.244l1.881 1.056zM7.057 5.859a.7.7 0 0 1 .363-.644l3.119-1.798a3 3 0 0 0-1.782-.578q-.874 0-1.584.396A2.96 2.96 0 0 0 6.05 4.324a3.07 3.07 0 0 0-.396 1.551v3.547q0 .199.165.314l1.237.726zm8.383 7.887q.825-.364 1.303-1.123.495-.758.495-1.65a3.15 3.15 0 0 0-.412-1.55q-.413-.743-1.073-1.123l-3.086-1.782q-.099-.065-.181-.049a.3.3 0 0 0-.165.05l-1.238.692 3.993 2.327a.6.6 0 0 1 .264.264.64.64 0 0 1 .1.363zm-3.317-8.382a.63.63 0 0 1 .726 0l3.135 1.831v-.297q0-.792-.396-1.501a2.86 2.86 0 0 0-1.105-1.155q-.71-.43-1.65-.43-.825 0-1.436.347L8.294 5.941a.36.36 0 0 0-.165.314v1.418z" },
  opencode: { bg:"#ffffff", fg:"#111827", vb:"0 0 24 24", border:true,
    d:"M22 24H2V0h20zM17 4.8H7v14.4h10z" },
  /* qoder 为全彩圆角方块图标（官网 favicon 压缩版），整幅铺满 .aicon，不走底色+白字形 */
  qoder: { raw:"<svg xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\" fill=\"none\" version=\"1.1\" width=\"180\" height=\"180\" viewBox=\"0 0 180 180\"><defs><clipPath id=\"master_svg0_381_5952\"><rect x=\"0\" y=\"0\" width=\"180\" height=\"180\" rx=\"40\"/></clipPath><clipPath id=\"master_svg1_381_5952/255_01913\"><rect x=\"18\" y=\"18\" width=\"144\" height=\"144\" rx=\"0\"/></clipPath></defs><g clip-path=\"url(#master_svg0_381_5952)\"><rect x=\"0\" y=\"0\" width=\"180\" height=\"180\" rx=\"40\" fill=\"#111113\" fill-opacity=\"1\"/><g clip-path=\"url(#master_svg1_381_5952/255_01913)\"><g><path d=\"M147.6,101.4L147.6,81.4C147.6,70.1,142.7,60.9,134.1,56.4L89.6,33.0L89.4,33.4L89.2,33.8C97.5,38.2,102.2,47.0,102.2,58.0L102.2,78.0C102.2,78.6,102.2,79.2,102.2,79.9C102.2,80.0,102.1,80.1,102.1,80.2L102.1,80.5C102.1,80.9,102.1,81.3,102.0,81.7C102.0,81.9,102.0,82.1,102.0,82.2L101.9,82.6C101.9,82.9,101.8,83.3,101.8,83.6C101.8,83.8,101.7,84.0,101.7,84.1L101.7,84.4C101.6,84.8,101.6,85.1,101.5,85.5C101.4,86.0,101.3,86.5,101.2,87.0L101.1,87.1C101.0,87.6,100.9,88.1,100.8,88.6L100.7,89.0C100.5,89.5,100.4,90.0,100.2,90.4L100.1,90.9C99.9,91.4,99.7,91.9,99.5,92.5C99.5,92.6,99.4,92.7,99.4,92.9L99.3,93.1C99.1,93.5,99.0,93.9,98.8,94.3C98.7,94.6,98.6,94.8,98.5,95.0L98.5,95.1C98.3,95.4,98.2,95.7,98.1,96.0C97.9,96.3,97.8,96.6,97.7,96.9C97.5,97.2,97.4,97.5,97.3,97.8C97.1,98.0,97.0,98.3,96.8,98.6C96.7,98.8,96.5,99.1,96.4,99.4C96.2,99.7,96.1,99.9,95.9,100.2C95.8,100.5,95.6,100.7,95.5,101.0C95.3,101.3,95.1,101.5,95.0,101.8C94.8,102.1,94.6,102.3,94.5,102.6C94.3,102.8,94.1,103.1,93.9,103.4C93.8,103.6,93.6,103.9,93.4,104.1C93.3,104.4,93.1,104.6,92.9,104.9C92.7,105.1,92.5,105.4,92.3,105.6C92.1,105.9,91.9,106.1,91.7,106.4C91.5,106.6,91.4,106.9,91.2,107.1C91.0,107.3,90.8,107.5,90.6,107.7L90.6,107.8C90.4,108.1,90.1,108.3,89.9,108.6C89.7,108.8,89.5,109.0,89.3,109.2C89.1,109.5,88.8,109.8,88.6,110.0C88.4,110.2,88.2,110.4,88.0,110.6C87.7,111.0,87.3,111.3,87.0,111.6L86.8,111.7C86.7,111.9,86.6,112.0,86.5,112.1C86.1,112.5,85.6,112.9,85.2,113.3L85.1,113.4C85.0,113.4,85.0,113.4,84.9,113.5L84.7,113.7C84.4,114.0,84.0,114.3,83.6,114.5C83.5,114.6,83.4,114.7,83.3,114.8L83.2,114.9C82.9,115.1,82.6,115.4,82.2,115.6C82.1,115.7,82.0,115.8,81.8,115.9C81.5,116.2,81.1,116.4,80.7,116.7L80.5,116.9C80.0,117.2,79.5,117.5,79.0,117.8L78.7,118.0C78.3,118.2,77.9,118.4,77.5,118.6C77.4,118.7,77.3,118.7,77.2,118.8L77.0,118.9C76.7,119.1,76.4,119.2,76.0,119.4C75.9,119.5,75.7,119.6,75.5,119.7L75.5,119.7C75.1,119.9,74.8,120.0,74.5,120.1C74.3,120.2,74.1,120.3,73.9,120.4C73.6,120.5,73.3,120.7,72.9,120.8L72.8,120.9C72.6,120.9,72.5,121.0,72.3,121.0C72.0,121.2,71.6,121.3,71.3,121.4L71.1,121.5C71.0,121.5,70.9,121.5,70.8,121.6C70.2,121.8,69.7,121.9,69.2,122.1L66.1,122.9C65.6,123.0,65.2,123.2,64.7,123.3C64.6,123.3,64.5,123.3,64.4,123.3L64.1,123.4C63.8,123.4,63.6,123.5,63.3,123.5C63.2,123.6,63.1,123.6,63.0,123.6L62.6,123.7C62.4,123.7,62.2,123.7,62.0,123.8C61.9,123.8,61.8,123.8,61.6,123.8L61.2,123.9C61.0,123.9,60.8,123.9,60.7,123.9C60.6,123.9,60.5,123.9,60.4,123.9L60.3,124.0C60.0,124.0,59.7,124.0,59.4,124.0C59.3,124.0,59.2,124.0,59.0,124.0L58.9,124.0C58.6,124.0,58.4,124.1,58.1,124.1C58.0,124.1,57.9,124.1,57.8,124.1L57.6,124.1C57.4,124.1,57.1,124.1,56.9,124.0C56.7,124.0,56.6,124.0,56.5,124.0L56.4,124.0C56.1,124.0,55.9,124.0,55.6,124.0L55.3,123.9C55.0,123.9,54.7,123.9,54.4,123.8L54.2,123.8C53.8,123.8,53.5,123.7,53.1,123.6L52.8,123.6C52.6,123.5,52.4,123.5,52.2,123.4C51.8,123.4,51.5,123.3,51.1,123.2L51.0,123.1C50.9,123.1,50.8,123.1,50.8,123.1C50.3,123.0,49.9,122.8,49.5,122.7L49.1,122.6C48.8,122.4,48.5,122.3,48.2,122.2C48.1,122.1,48.0,122.1,47.9,122.0L47.8,122.0C47.5,121.9,47.3,121.8,47.0,121.6L46.9,121.6Q46.8,121.6,46.8,121.6L43.5,119.8L45.9,122.2L46.0,122.2L90.3,145.6C90.5,145.7,90.6,145.7,90.8,145.8C90.8,145.8,90.9,145.9,90.9,145.9L91.0,145.9C91.3,146.1,91.6,146.2,91.8,146.3L91.9,146.4C92.0,146.4,92.2,146.5,92.3,146.5C92.6,146.6,92.9,146.8,93.2,146.9L93.3,146.9C93.4,146.9,93.5,147.0,93.5,147.0L93.6,147.0C94.0,147.2,94.5,147.3,94.9,147.4C95.0,147.5,95.1,147.5,95.2,147.5L95.3,147.5C95.6,147.6,96.0,147.7,96.4,147.8L96.5,147.8C96.5,147.8,96.5,147.8,96.6,147.9C96.7,147.9,96.8,147.9,96.9,147.9L97.4,148.0C97.7,148.1,98.1,148.1,98.5,148.2L98.7,148.2C99.0,148.2,99.3,148.3,99.6,148.3L100.2,148.4C100.3,148.4,100.4,148.4,100.5,148.4L100.8,148.4C100.9,148.4,101.1,148.4,101.3,148.4C101.5,148.4,101.7,148.4,102.0,148.4L102.2,148.4C102.2,148.4,102.3,148.4,102.4,148.4C102.5,148.4,102.5,148.4,102.6,148.4C102.8,148.4,103.0,148.4,103.3,148.4L103.5,148.4C103.6,148.4,103.7,148.4,103.9,148.4C104.2,148.4,104.5,148.3,104.8,148.3L104.9,148.3C105.0,148.3,105.1,148.3,105.2,148.3C105.4,148.3,105.6,148.2,105.7,148.2L106.1,148.2C106.3,148.2,106.4,148.1,106.5,148.1C106.7,148.1,107.0,148.1,107.2,148.0L107.6,148.0C107.7,147.9,107.8,147.9,107.9,147.9C108.2,147.8,108.5,147.8,108.7,147.7L109.0,147.7C109.1,147.6,109.2,147.6,109.3,147.6C109.8,147.5,110.3,147.4,110.7,147.2L113.8,146.4C114.4,146.3,114.9,146.1,115.5,145.9C115.6,145.9,115.7,145.8,115.8,145.8L116.0,145.7C116.4,145.6,116.7,145.5,117.1,145.3C117.3,145.3,117.4,145.2,117.6,145.2L117.7,145.1C118.0,145.0,118.4,144.8,118.7,144.7C118.9,144.6,119.1,144.5,119.3,144.4C119.6,144.3,120.0,144.1,120.3,144.0L120.4,143.9C120.6,143.8,120.7,143.8,120.9,143.7C121.2,143.5,121.5,143.3,121.9,143.2L122.1,143.1C122.2,143.0,122.3,142.9,122.4,142.9C122.8,142.7,123.2,142.4,123.6,142.2L123.7,142.1C123.8,142.1,123.9,142.0,123.9,142.0C124.4,141.7,124.9,141.4,125.3,141.1L125.6,140.9L135.4,145.4C141.1,147.9,147.6,143.8,147.6,137.5L147.6,101.4Q147.6,101.4,147.6,101.4L147.6,101.4Z\" fill=\"#2ADB5C\" fill-opacity=\"1\"/></g><g><path d=\"M89.7,33.0C89.5,32.9,89.3,32.8,89.2,32.8C89.1,32.7,89.0,32.7,88.9,32.6C88.7,32.5,88.4,32.4,88.1,32.3C88.0,32.2,87.8,32.1,87.7,32.1C87.4,31.9,87.1,31.8,86.7,31.7C86.6,31.7,86.6,31.6,86.5,31.6C86.4,31.6,86.4,31.6,86.4,31.6C86.0,31.4,85.5,31.3,85.0,31.1C84.9,31.1,84.8,31.1,84.7,31.1C84.3,31.0,84.0,30.9,83.6,30.8C83.5,30.8,83.5,30.8,83.4,30.7C83.2,30.7,83.1,30.7,82.9,30.6C82.8,30.6,82.7,30.6,82.6,30.6C82.3,30.5,81.9,30.5,81.5,30.4C81.5,30.4,81.4,30.4,81.3,30.4C81.0,30.3,80.7,30.3,80.4,30.3C80.3,30.3,80.2,30.3,80.1,30.3C80.0,30.2,79.9,30.2,79.8,30.2C79.6,30.2,79.4,30.2,79.3,30.2C79.1,30.2,78.9,30.2,78.7,30.2C78.5,30.2,78.3,30.2,78.0,30.2C77.8,30.2,77.6,30.2,77.5,30.2C77.2,30.2,76.9,30.2,76.7,30.2C76.5,30.2,76.3,30.2,76.2,30.2C75.9,30.2,75.6,30.2,75.3,30.3C75.1,30.3,75.0,30.3,74.9,30.3C74.9,30.3,74.8,30.3,74.8,30.3C74.6,30.3,74.3,30.4,74.1,30.4C73.9,30.4,73.7,30.4,73.5,30.5C73.2,30.5,72.9,30.5,72.7,30.6C72.5,30.6,72.3,30.7,72.1,30.7C71.8,30.7,71.5,30.8,71.2,30.9C71.0,30.9,70.9,30.9,70.7,31.0C70.2,31.1,69.8,31.2,69.3,31.3L66.2,32.2C65.7,32.3,65.1,32.5,64.6,32.7C64.4,32.7,64.2,32.8,64.0,32.9C63.7,33.0,63.3,33.1,63.0,33.2C62.7,33.3,62.5,33.4,62.3,33.5C62.0,33.6,61.7,33.7,61.4,33.9C61.1,34.0,60.9,34.1,60.7,34.2C60.4,34.3,60.1,34.5,59.8,34.6C59.6,34.7,59.4,34.8,59.2,34.9C58.8,35.1,58.5,35.2,58.2,35.4C58.0,35.5,57.8,35.6,57.6,35.7C57.2,35.9,56.8,36.2,56.4,36.4C56.3,36.5,56.2,36.5,56.1,36.6C55.6,36.9,55.2,37.2,54.7,37.5C54.7,37.5,54.7,37.5,54.6,37.5C54.5,37.6,54.4,37.7,54.3,37.7C54.0,38.0,53.6,38.2,53.2,38.5C53.1,38.6,52.9,38.7,52.8,38.8C52.5,39.1,52.1,39.3,51.8,39.5C51.7,39.6,51.5,39.8,51.4,39.9C51.0,40.2,50.6,40.5,50.3,40.8C50.2,40.8,50.1,40.9,50.1,41.0C50.0,41.0,50.0,41.0,50.0,41.0C49.9,41.1,49.9,41.1,49.8,41.2C49.3,41.6,48.9,42.0,48.5,42.4C48.3,42.5,48.1,42.7,47.9,42.9C47.6,43.2,47.3,43.5,46.9,43.9C46.8,44.0,46.7,44.1,46.7,44.1C46.5,44.3,46.5,44.4,46.3,44.5C46.1,44.7,45.8,45.0,45.6,45.3C45.4,45.5,45.2,45.7,45.0,45.9C44.7,46.2,44.5,46.5,44.3,46.8C44.1,46.9,44.0,47.1,43.9,47.2C43.8,47.3,43.8,47.4,43.7,47.5C43.5,47.7,43.3,48.0,43.1,48.2C42.9,48.5,42.7,48.7,42.5,49.0C42.3,49.2,42.1,49.5,41.9,49.8C41.8,49.9,41.7,50.1,41.6,50.3C41.5,50.4,41.4,50.4,41.4,50.5C41.2,50.8,41.0,51.0,40.8,51.3C40.7,51.6,40.5,51.8,40.3,52.1C40.1,52.4,40.0,52.6,39.8,52.9C39.7,53.1,39.6,53.2,39.5,53.4C39.4,53.5,39.4,53.6,39.3,53.7C39.1,54.0,39.0,54.3,38.8,54.5C38.7,54.8,38.5,55.1,38.3,55.4C38.2,55.6,38.0,55.9,37.9,56.2C37.8,56.4,37.7,56.5,37.6,56.7C37.6,56.8,37.5,56.9,37.5,57.0C37.3,57.3,37.2,57.7,37.0,58.0C36.9,58.2,36.7,58.5,36.6,58.8C36.5,59.1,36.3,59.4,36.2,59.7C36.1,59.9,36.0,60.1,36.0,60.3C35.9,60.4,35.9,60.5,35.9,60.6C35.7,61.0,35.5,61.4,35.4,61.8C35.3,62.0,35.2,62.2,35.1,62.5C34.9,63.0,34.7,63.5,34.6,64.1C34.5,64.2,34.5,64.3,34.5,64.4C34.5,64.4,34.5,64.4,34.4,64.4C34.4,64.4,34.4,64.5,34.4,64.5C34.2,65.0,34.1,65.5,34.0,66.0C33.9,66.2,33.9,66.3,33.8,66.5C33.7,66.9,33.6,67.4,33.5,67.9C33.5,68.0,33.5,68.0,33.4,68.1C33.3,68.5,33.3,69.0,33.2,69.4C33.1,69.5,33.1,69.6,33.1,69.6C33.1,70.0,33.0,70.4,32.9,70.7C32.9,71.0,32.8,71.3,32.8,71.5C32.7,71.9,32.7,72.3,32.7,72.7C32.6,73.0,32.6,73.2,32.6,73.5C32.5,73.9,32.5,74.3,32.5,74.7C32.5,75.0,32.4,75.2,32.4,75.4C32.4,76.0,32.4,76.6,32.4,77.3L32.4,97.3C32.4,104.6,34.5,110.8,38.0,115.5C38.4,115.9,38.7,116.4,39.1,116.8C39.7,117.5,40.3,118.1,40.9,118.7C42.4,120.1,44.1,121.2,45.9,122.2L90.3,145.6C82.1,141.3,76.8,132.4,76.8,120.7L76.8,100.7C76.8,100.1,76.8,99.4,76.9,98.8C76.9,98.6,76.9,98.4,76.9,98.2C77.0,97.7,77.0,97.3,77.0,96.9C77.0,96.6,77.1,96.4,77.1,96.1C77.1,95.7,77.2,95.3,77.2,95.0C77.3,94.7,77.3,94.4,77.4,94.1C77.4,93.8,77.5,93.4,77.6,93.0C77.7,92.5,77.8,92.0,77.9,91.5C77.9,91.5,77.9,91.4,77.9,91.3C78.0,90.9,78.2,90.4,78.3,89.9C78.3,89.7,78.4,89.6,78.4,89.4C78.5,88.9,78.7,88.4,78.8,87.9C78.9,87.9,78.9,87.9,78.9,87.8C78.9,87.7,79.0,87.6,79.0,87.5C79.2,87.0,79.4,86.4,79.5,85.9C79.6,85.7,79.7,85.4,79.8,85.2C80.0,84.8,80.1,84.4,80.3,84.0C80.4,83.7,80.5,83.4,80.6,83.2C80.8,82.8,80.9,82.5,81.1,82.2C81.2,81.9,81.3,81.7,81.4,81.4C81.6,81.1,81.7,80.8,81.9,80.5C82.0,80.2,82.2,79.9,82.3,79.6C82.5,79.3,82.6,79.1,82.8,78.8C82.9,78.5,83.1,78.2,83.3,78.0C83.4,77.7,83.6,77.4,83.7,77.1C83.9,76.9,84.1,76.6,84.2,76.3C84.4,76.1,84.6,75.8,84.7,75.5C84.9,75.3,85.1,75.0,85.3,74.7C85.5,74.5,85.6,74.2,85.8,74.0C86.0,73.7,86.2,73.4,86.4,73.2C86.6,72.9,86.7,72.7,86.9,72.4C87.1,72.2,87.3,71.9,87.5,71.7C87.7,71.4,87.9,71.2,88.1,70.9C88.3,70.7,88.5,70.4,88.7,70.2C89.0,69.9,89.2,69.6,89.4,69.4C89.6,69.2,89.8,68.9,90.0,68.7C90.3,68.4,90.5,68.2,90.8,67.9C91.0,67.7,91.2,67.5,91.4,67.3C91.7,67.0,92.0,66.6,92.4,66.3C92.6,66.1,92.7,66.0,92.9,65.8C93.3,65.4,93.8,65.0,94.2,64.6C94.3,64.5,94.4,64.5,94.5,64.4C94.6,64.3,94.6,64.3,94.7,64.2C95.1,63.9,95.4,63.6,95.8,63.3C96.0,63.2,96.1,63.1,96.3,63.0C96.6,62.7,96.9,62.5,97.2,62.2C97.4,62.1,97.5,62.0,97.7,61.9C98.0,61.6,98.4,61.4,98.8,61.1C98.9,61.1,99.0,61.0,99.1,60.9C99.6,60.6,100.1,60.3,100.6,60.0C100.7,59.9,100.8,59.9,100.9,59.8C101.3,59.6,101.7,59.4,102.1,59.1C102.2,59.0,102.4,58.9,102.6,58.9C102.9,58.7,103.3,58.5,103.6,58.3C103.8,58.2,104.0,58.1,104.2,58.0C104.5,57.9,104.8,57.7,105.2,57.6C105.4,57.5,105.6,57.4,105.8,57.3C106.1,57.2,106.4,57.0,106.8,56.9C107.0,56.8,107.2,56.8,107.4,56.7C107.7,56.5,108.1,56.4,108.5,56.3C108.7,56.2,108.8,56.2,109.0,56.1C109.5,55.9,110.1,55.8,110.6,55.6L113.7,54.8C114.2,54.6,114.7,54.5,115.1,54.4C115.3,54.4,115.5,54.3,115.6,54.3C115.9,54.2,116.2,54.2,116.5,54.1C116.7,54.1,116.9,54.1,117.1,54.0C117.4,54.0,117.7,53.9,117.9,53.9C118.1,53.9,118.3,53.8,118.5,53.8C118.8,53.8,119.0,53.8,119.3,53.7C119.4,53.7,119.6,53.7,119.7,53.7C120.0,53.7,120.3,53.6,120.6,53.6C120.8,53.6,120.9,53.6,121.1,53.6C121.4,53.6,121.6,53.6,121.9,53.6C122.1,53.6,122.3,53.6,122.4,53.6C122.7,53.6,122.9,53.6,123.2,53.6C123.4,53.6,123.5,53.6,123.7,53.6C124.0,53.6,124.2,53.7,124.5,53.7C124.6,53.7,124.7,53.7,124.8,53.7C125.1,53.7,125.4,53.8,125.8,53.8C125.8,53.8,125.9,53.8,126.0,53.8C126.3,53.9,126.7,54.0,127.1,54.0C127.2,54.0,127.3,54.1,127.4,54.1C127.6,54.1,127.8,54.2,128.0,54.2C128.4,54.3,128.8,54.4,129.1,54.5C129.2,54.5,129.4,54.5,129.5,54.6C129.9,54.7,130.4,54.8,130.8,55.0C130.9,55.0,131.1,55.1,131.2,55.1C131.5,55.2,131.8,55.4,132.1,55.5C132.3,55.6,132.4,55.6,132.6,55.7C132.8,55.8,133.1,55.9,133.4,56.1C133.6,56.2,133.8,56.3,134.1,56.4L89.7,33.0Z\" fill=\"#FFFFFF\" fill-opacity=\"1\" style=\"mix-blend-mode:passthrough\"/></g></g></g></svg>" },
  zcode: { img:"data:image/png;base64,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" },
  reasonix: { img:"data:image/png;base64,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" },
  pi: { padded:"<svg width=\"24\" height=\"24\" style=\"width:24px;height:24px\" xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 800 800\">\n  <path fill=\"#09090b\" fill-rule=\"evenodd\" d=\"\n    M165.29 165.29\n    H517.36\n    V400\n    H400\n    V517.36\n    H282.65\n    V634.72\n    H165.29\n    Z\n    M282.65 282.65\n    V400\n    H400\n    V282.65\n    Z\n  \"/>\n  <path fill=\"#09090b\" d=\"M517.36 400 H634.72 V634.72 H517.36 Z\"/>\n</svg>", bg:"#ffffff", border:true },
  dsh: { padded:"<svg width=\"24\" height=\"24\" style=\"width:24px;height:24px\" xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 50 50\" fill=\"none\">\n\n<path d=\"M48.8354 10.0479C48.3232 9.79199 48.1025 10.2798 47.8032 10.5278C47.7007 10.6079 47.6143 10.7119 47.5273 10.8076C46.7793 11.624 45.9048 12.1597 44.7622 12.0957C43.0923 12 41.666 12.5356 40.4058 13.8398C40.1377 12.2319 39.2476 11.272 37.8926 10.6558C37.1836 10.3359 36.4668 10.0156 35.9702 9.31982C35.6235 8.82373 35.5293 8.27197 35.356 7.72754C35.2456 7.3999 35.1353 7.06396 34.7651 7.00781C34.3633 6.94385 34.2056 7.2876 34.0479 7.57568C33.418 8.75195 33.1733 10.0479 33.1973 11.3599C33.2524 14.312 34.4736 16.6641 36.8999 18.3359C37.1758 18.5278 37.2466 18.7197 37.1597 19C36.9946 19.5757 36.7974 20.1357 36.624 20.7119C36.5137 21.0801 36.3486 21.1597 35.9624 21C34.6309 20.4321 33.481 19.5918 32.4644 18.5757C30.7393 16.8721 29.1792 14.9917 27.2334 13.52C26.7764 13.1758 26.3193 12.856 25.8467 12.5518C23.8618 10.584 26.1069 8.96777 26.627 8.77588C27.1704 8.57568 26.8159 7.8877 25.0591 7.896C23.3022 7.90381 21.6953 8.50391 19.647 9.30371C19.3477 9.42383 19.0322 9.51172 18.7095 9.58398C16.8501 9.22363 14.9199 9.14355 12.9033 9.37598C9.10596 9.80762 6.07275 11.6396 3.84326 14.7681C1.16455 18.5278 0.53418 22.7998 1.30664 27.2559C2.11768 31.9521 4.46582 35.8398 8.07373 38.8799C11.8159 42.0322 16.1255 43.5762 21.041 43.2803C24.0269 43.104 27.3516 42.6963 31.1016 39.4561C32.0469 39.936 33.0396 40.1279 34.686 40.272C35.9546 40.3921 37.1758 40.208 38.1211 40.0078C39.6021 39.688 39.4995 38.2881 38.9639 38.0322C34.623 35.9678 35.5762 36.8081 34.71 36.1279C36.9155 33.4639 40.2402 30.6958 41.54 21.728C41.6426 21.0161 41.5557 20.5679 41.54 19.9917C41.5322 19.6396 41.6108 19.5039 42.0049 19.4639C43.0923 19.3359 44.1479 19.0317 45.1167 18.4878C47.9292 16.9199 49.064 14.3438 49.3315 11.2559C49.3711 10.7837 49.3237 10.2959 48.8354 10.0479ZM24.3262 37.8398C20.1196 34.4639 18.0791 33.3521 17.2358 33.3999C16.4482 33.4482 16.5898 34.3682 16.7632 34.9678C16.9443 35.5601 17.1812 35.9683 17.5117 36.4878C17.7402 36.832 17.8979 37.3442 17.2832 37.728C15.9282 38.584 13.5728 37.4399 13.4624 37.3838C10.7207 35.7358 8.42822 33.5601 6.81348 30.584C5.25342 27.7197 4.34766 24.6479 4.19775 21.3677C4.1582 20.5757 4.38672 20.2959 5.15869 20.1519C6.17529 19.96 7.22314 19.9199 8.23926 20.0718C12.5327 20.7119 16.1885 22.6719 19.2529 25.7759C21.002 27.5439 22.3252 29.6558 23.6885 31.7202C25.1377 33.9121 26.6978 36 28.6831 37.7119C29.3843 38.312 29.9434 38.7681 30.479 39.104C28.8643 39.2881 26.1699 39.3281 24.3262 37.8398ZM26.3433 24.6001C26.3433 24.248 26.6191 23.9678 26.9658 23.9678C27.0444 23.9678 27.1152 23.9839 27.1782 24.0078C27.2651 24.04 27.3438 24.0879 27.4067 24.1602C27.5171 24.272 27.5801 24.4321 27.5801 24.6001C27.5801 24.9521 27.3042 25.2319 26.9575 25.2319C26.6108 25.2319 26.3433 24.9521 26.3433 24.6001ZM32.6064 27.8799C32.2046 28.0479 31.8027 28.1919 31.4165 28.208C30.8179 28.2397 30.1641 27.9922 29.8096 27.688C29.2583 27.2158 28.8643 26.9521 28.6987 26.1279C28.6279 25.7759 28.6675 25.2319 28.7305 24.9199C28.8721 24.248 28.7144 23.8159 28.2495 23.4238C27.8716 23.104 27.3911 23.0161 26.8633 23.0161C26.666 23.0161 26.4849 22.9277 26.3511 22.856C26.1304 22.7441 25.9492 22.4639 26.1226 22.1201C26.1777 22.0078 26.4458 21.7358 26.5088 21.688C27.2256 21.272 28.0527 21.4077 28.8169 21.7197C29.5259 22.0161 30.0615 22.5601 30.834 23.3281C31.6216 24.2559 31.7632 24.5117 32.2124 25.208C32.5669 25.752 32.8901 26.312 33.1104 26.9521C33.2446 27.3521 33.0713 27.6802 32.6064 27.8799Z\" fill=\"#000000\"/>\n</svg>", bg:"#ffffff", border:true },
};
function logoSvg(L){
  return '<svg viewBox="'+L.vb+'" width="20" height="20" fill="currentColor"><path d="'+L.d+'"/></svg>';
}

/* ================= 引导页：agent 静态元数据与卡片（Task 7） ================= */
/* /api/status 只回传实时状态（id/name/detected/hooksInstalled + 全局 skillsInstalled/rxEnforceMode，
   api.go apiStatus 核实），类型徽标/简介/安装目标为展示文案，按原型内置于此（target 已按
   internal/agentx 各适配器 HooksTarget 核实）；未登记 meta 的新适配器兜底字母磁贴。 */
const AGENT_META = {
  kimi:      { kind:"hook",   color:"#2563eb", abbr:"Ki",
    target:"~/.kimi-code/config.toml",
    desc:"会话注入、强制检查、沉淀提议全链路接入。",
    descEn:"Full-chain integration: session injection, enforce checks, capture proposals." },
  claude:    { kind:"hook",   color:"#b45309", abbr:"Cl",
    target:"~/.claude/settings.json",
    desc:"为 Claude Code 及 CodePilot 等兼容宿主接入 hooks 注入。",
    descEn:"Hooks injection for Claude Code and compatible hosts such as CodePilot." },
  codex:     { kind:"hook",   color:"#111827", abbr:"Cx",
    target:"~/.codex/hooks.json + config.toml",
    desc:"hooks 注入 + 会话后增量捕获。",
    descEn:"Hooks injection + post-session incremental capture." },
  qoder:     { kind:"hook",   color:"#0d9488", abbr:"Qo",
    target:"~/.qoder-cn/settings.json",
    desc:"Qoder 命令行 hooks 接入。",
    descEn:"Hooks integration for the Qoder command line." },
  "qoder-ide": { kind:"hook", color:"#0f766e", abbr:"QI",
    target:"~/.lingma/settings.json",
    desc:"灵码内核 IDE 的 hooks 接入。",
    descEn:"Hooks integration for the Lingma-kernel IDE." },
  opencode:  { kind:"plugin", color:"#7c3aed", abbr:"oc",
    target:"~/.config/opencode/plugins/openknowledge.ts",
    desc:"TS 插件：仓库上下文 + 会话同步。",
    descEn:"TS plugin: repo context + session sync." },
  pi:        { kind:"plugin", color:"#db2777", abbr:"Pi",
    target:"~/.pi/agent/extensions/openknowledge.ts",
    desc:"Pi 扩展方式接入。",
    descEn:"Integrated as a Pi extension." },
  zcode:     { kind:"hook",   color:"#ea580c", abbr:"Zc",
    target:"~/.zcode/cli/config.json + ~/.zcode/skills/",
    desc:"hooks 注入（claude 协议），技能目录独立。",
    descEn:"Hooks injection (claude protocol) with its own skills dir." },
  reasonix:  { kind:"hook",   color:"#4f46e5", abbr:"Rx",
    target:"%APPDATA%\\reasonix\\plugins\\openknowledge\\（含 extensions 注册）",
    targetEn:"%APPDATA%\\reasonix\\plugins\\openknowledge\\ (incl. extensions registry)",
    desc:"sidecar 扩展接入，强制检查走拦截器。",
    descEn:"Sidecar extension; enforce checks run through the interceptor." },
  dsh:       { kind:"plugin", color:"#0284c7", abbr:"DS",
    target:"<DSH home>/plugins/openknowledge/ + cordis.patch.yml",
    desc:"file-URL 挂载的本地 JS 插件。",
    descEn:"Local JS plugin mounted via file:// URL." },
};

let SETUP = null;        // {status, loadErr} 缓存；loadSetup 惰性加载，装/卸载后 refreshSetup 原位刷新
const setupState = {};   // id → {busy, open, cxOpen, fb, fbErr}：明细展开/反馈跨整页重渲保留
let setupDetecting = false;

function loadSetup(){
  if(SETUP) return;
  SETUP = { status:null };
  refreshSetup();
}
function refreshSetup(){
  return api("/api/status").then(st=>{
    SETUP = { status: st || null };
  }).catch(err=>{
    SETUP = { status:null, loadErr: err.message };
  }).then(()=>{ if(state.menu==="setup") render(); });
}
function aState(id){
  if(!setupState[id]) setupState[id] = { busy:false, open:false, cxOpen:false, fb:"", fbErr:false };
  return setupState[id];
}

function renderSetup(){
  loadSetup();
  const d = el("div","setup");
  const st = SETUP && SETUP.status;
  if(!st){
    d.appendChild(Object.assign(el("div","placeholder"),
      {textContent: SETUP && SETUP.loadErr ? t("xLoadFail")+SETUP.loadErr : t("mgLoading")}));
    return d;
  }
  const list = (st.agents||[]).filter(a=>a.detected);   // 未检测到的不显示
  const head = el("div","setup-head");
  const stat = el("div","stat");
  stat.innerHTML = t("stDetected")+" <b>"+list.length+"</b> "+t("stAgentUnit")
    +' · <span class="ok">'+t("stHooked")+" <b>"+list.filter(a=>a.hooksInstalled).length+"</b> "+t("stHookedUnit")+"</span>";
  head.appendChild(stat);
  head.appendChild(Object.assign(el("div","sub"),{textContent:t("setupSub")}));
  const rd = el("button","btn");
  rd.textContent = setupDetecting ? t("detecting") : t("redetect");
  rd.disabled = setupDetecting;
  rd.onclick = async ()=>{            // 检测是 apiStatus 的实时行为：重拉 status 即重新检测
    setupDetecting = true; render();
    await refreshSetup();
    setupDetecting = false;
    if(state.menu==="setup") render();
  };
  head.appendChild(rd);
  d.appendChild(head);

  if(!list.length){
    d.appendChild(Object.assign(el("div","placeholder"),{textContent:t("noneDetected")}));
    return d;
  }
  const cards = el("div","acards");
  list.forEach(a=>{
    const meta = AGENT_META[a.id] || {};
    const s = aState(a.id);
    const card = el("div","acard");
    const icon = el("div","aicon");
    const L = LOGOS[a.id];
    if(L && (L.raw || L.img)){
      icon.innerHTML = L.raw || '<img src="'+L.img+'" alt="">';   // 全彩图标，铺满磁贴
    } else if(L && L.padded){
      icon.style.background = L.bg||"#fff";
      if(L.border) icon.style.boxShadow = "inset 0 0 0 1px var(--border)";
      icon.innerHTML = L.padded;                    // 浅色底字形：白磁贴 24px 居中
    } else if(L){
      icon.style.background = L.bg; icon.style.color = L.fg;
      if(L.border) icon.style.boxShadow = "inset 0 0 0 1px var(--border)";
      icon.innerHTML = logoSvg(L);
    } else {
      icon.style.background = meta.color||"#64748b"; icon.textContent = meta.abbr||a.name.slice(0,2);
    }
    card.appendChild(icon);

    const main = el("div","amain");
    const r1 = el("div","arow1");
    r1.appendChild(Object.assign(el("span","aname"),{textContent:a.name}));
    const kind = el("span","akind "+(meta.kind==="plugin"?"k-plugin":"k-hook"));
    kind.textContent = meta.kind==="plugin" ? t("kindPlugin") : t("kindHook");
    r1.appendChild(kind);
    main.appendChild(r1);
    const desc = state.lang==="en" ? (meta.descEn||meta.desc) : meta.desc;
    if(desc) main.appendChild(Object.assign(el("div","adesc"),{textContent:desc}));
    const target = state.lang==="en" ? (meta.targetEn||meta.target) : meta.target;
    const det = el("div","adetect");
    det.innerHTML = '<span class="yes">✓</span>'
      + (target ? '<span class="path" title="target">'+esc(target)+'</span>' : '');
    main.appendChild(det);
    // Reasonix 专属件：强制检查方式三档（仅 reasonix 卡显示；旧 GUI renderRxEnforce 语义）
    if(a.id==="reasonix") main.appendChild(renderRxModes(st, s));
    // Codex 专属件：信任门与版本说明（旧 GUI codex-help-card 语义，默认收起）
    if(a.id==="codex"){
      const cto = el("button","detail-toggle");
      cto.textContent = s.cxOpen ? t("cxHelpClose") : t("cxHelpQ");
      cto.onclick = ()=>{ s.cxOpen=!s.cxOpen; render(); };
      main.appendChild(cto);
      const cd = el("div","adetail"+(s.cxOpen?" open":""));
      cd.innerHTML = t("cxHelpBody");
      main.appendChild(cd);
    }
    const tog = el("button","detail-toggle");
    tog.textContent = s.open ? t("detailClose") : t("detailQ");
    tog.onclick = ()=>{ s.open=!s.open; render(); };
    main.appendChild(tog);
    const dl = el("div","adetail"+(s.open?" open":""));
    dl.innerHTML = t("detailInstall")+(target?"<code>"+esc(target)+"</code>":"")+t("detailJoin")+t("detailRemove");
    main.appendChild(dl);
    const fb = el("div","fb"+(s.fb?" show":"")+(s.fbErr?" err":""));
    fb.textContent = s.fb||"";
    main.appendChild(fb);
    card.appendChild(main);

    const aside = el("div","aside2");
    const ch1 = el("span","chip "+(a.hooksInstalled?"on":"off"));
    ch1.textContent = a.hooksInstalled ? t("hookOn") : t("hookOff");
    aside.appendChild(ch1);
    const ch2 = el("span","chip "+(st.skillsInstalled?"on":"off"));
    ch2.textContent = st.skillsInstalled ? t("skillOn") : t("skillOff");
    aside.appendChild(ch2);
    const act = el("button","btn "+(a.hooksInstalled?"btn-danger":"btn-primary"));
    act.disabled = s.busy;
    act.textContent = s.busy ? (a.hooksInstalled?t("uninstalling"):t("installing")) : (a.hooksInstalled?t("uninstall"):t("install"));
    act.onclick = ()=>toggleAgent(a);
    aside.appendChild(act);
    card.appendChild(aside);
    cards.appendChild(card);
  });
  d.appendChild(cards);
  return d;
}

/* Reasonix 三档（旧 GUI renderRxEnforce 语义平移）：radio 变更即保存（sidecar 每条输入
   实时读配置，即时生效）；失败时卡片驻留错误并整页重渲，radio 回退到已保存档位。 */
function renderRxModes(st, s){
  const box = el("div","rxmodes");
  box.appendChild(Object.assign(el("div","rx-title"),{textContent:t("rxTitle")}));
  box.appendChild(Object.assign(el("div","rx-desc"),{textContent:t("rxDesc")}));
  const mode = st.rxEnforceMode || "mixed";
  [["mixed","rxMixed"],["soft","rxSoft"],["hard","rxHard"]].forEach(([v,k])=>{
    const lab = el("label","rx-opt");
    const r = el("input");
    r.type = "radio"; r.name = "rx-enforce"; r.value = v; r.checked = mode===v;
    r.onchange = ()=>{
      api("/api/reasonix/enforce-mode", { method:"POST", body:{ mode:v } }).then(()=>{
        if(SETUP && SETUP.status) SETUP.status.rxEnforceMode = v;
      }).catch(err=>{
        s.fb = t("rxSaveFail")+err.message; s.fbErr = true;
        render();
      });
    };
    lab.appendChild(r);
    lab.appendChild(document.createTextNode(" "+t(k)));
    box.appendChild(lab);
  });
  return box;
}

/* 安装 = 写 hooks（/api/setup/hooks {"agent":id}）+ 装技能（/api/setup/skills），等同 CLI
   ok setup 的两步；卸载 = Task 1 的 /api/setup/hooks/remove 单 agent 卸载。完成后重拉
   status 刷新卡片（技能芯片为全局 skillsInstalled，随刷新联动）。 */
async function toggleAgent(a){
  const s = aState(a.id);
  const wasHooked = a.hooksInstalled;
  s.busy = true; s.fb = ""; s.fbErr = false; render();
  try {
    if(wasHooked){
      await api("/api/setup/hooks/remove", { method:"POST", body:{ agent:a.id } });
      s.fb = t("fbUninstall");
    } else {
      await api("/api/setup/hooks", { method:"POST", body:{ agent:a.id } });
      await api("/api/setup/skills", { method:"POST" });
      s.fb = t("fbInstall");
    }
  } catch(err){
    s.fb = (wasHooked ? t("opUninstallFail") : t("opInstallFail")) + err.message;
    s.fbErr = true;
  }
  s.busy = false;
  await refreshSetup();
}

/* ================= 状态 ================= */
const state = { menu:"manage", lang:"zh", theme:"light", collapsed:false,
                open:{}, openTouched:false, sel:null, projSel:null, q:"", mgmtFb:null, treeShown:{},
                edView:"read",   // 详情区态：read 只读 | edit 内联编辑 | cmp 优化对照
                logSrc:{ ok:true, daemon:true, sidecar:true }, logSem:false, logQ:"",
                logAuto:true, logStick:true, miscFb:null };
const t = k => I18N[state.lang][k];

/* 设置卡脏态/已保存反馈（pcard/pDirtyLive/pSave 用）；prefsErr 驻留保存失败的后端错误（400 信息等） */
const prefsDirty = {}, prefsSaved = {}, prefsErr = {};

/* 摘要行：标签紧贴内容（不吃 .prow .k 的 140px 固定宽），动作按钮靠右 */
function sumRow(label, content, btn){
  const r = el("div","prow");
  const k = Object.assign(el("span","k"),{textContent:label});
  k.style.width = "auto"; k.style.marginRight = "6px";
  r.appendChild(k); r.appendChild(content);
  if(btn){ btn.style.marginLeft = "auto"; r.appendChild(btn); }
  return r;
}

// 实时脏态：不重渲，直接同步该卡保存按钮的禁用态（数字输入边打边响应，且改回原值即变回灰）
function pDirtyLive(k, dirty){
  prefsDirty[k]=dirty;
  document.querySelectorAll('[data-save="'+k+'"]').forEach(b=>{ b.disabled = !dirty; });
}
function pSave(k){
  prefsDirty[k]=false; prefsErr[k]=null; prefsSaved[k]=true; render();
  setTimeout(()=>{ prefsSaved[k]=false; render(); }, 1500);
}
// 数字输入（设置卡用）：oninput 实时上报，由调用方 apply + 计算脏态；不重渲，避免丢焦点
function pnumLive(val, min, max, onVal){
  const i = el("input","pinput"); i.type="number"; i.min=min; i.max=max; i.value=val;
  i.style.width="90px";
  i.oninput = ()=>onVal(+i.value);
  return i;
}
function pcard(key, title, desc, body, inlineRow){
  const c = el("div","pcard");
  const h = el("h3"); h.textContent = title; c.appendChild(h);
  if(desc){ c.appendChild(Object.assign(el("div","pdesc"),{textContent:desc})); }
  c.appendChild(body);
  const sv = el("button","btn btn-primary");
  sv.textContent = t("save"); sv.disabled = !prefsDirty[key];
  sv.dataset.save = key;
  sv.onclick = ()=>pSave(key);
  if(inlineRow){
    // 简单输入卡：保存按钮进控件行右端，不独占页脚
    const wrap = el("span");
    wrap.style.cssText = "margin-left:auto;display:flex;align-items:center;gap:10px;flex:none";
    if(prefsSaved[key]) wrap.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
    if(prefsErr[key]) wrap.appendChild(Object.assign(el("span","fb2 err"),{textContent:prefsErr[key]}));
    wrap.appendChild(sv);
    inlineRow.appendChild(wrap);
  } else {
    const f = el("div","pfoot");
    f.appendChild(sv);
    if(prefsSaved[key]) f.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
    if(prefsErr[key]) f.appendChild(Object.assign(el("span","fb2 err"),{textContent:prefsErr[key]}));
    c.appendChild(f);
  }
  return c;
}
function prow(k, input){
  const r = el("div","prow");
  r.appendChild(Object.assign(el("span","k"),{textContent:k}));
  r.appendChild(input);
  return r;
}
function pswitch(on, flip){
  const s = el("button","switch"+(on?" on":""));
  s.setAttribute("role","switch"); s.onclick = flip;
  return s;
}
function pnum(val, min, max, commit){
  const i = el("input","pinput"); i.type="number"; i.min=min; i.max=max; i.value=val;
  i.style.width="90px"; i.onchange = ()=>commit(+i.value);
  return i;
}
function ptext(val, commit, width){
  const i = el("input","pinput"); i.value=val; if(width) i.style.width=width;
  i.onchange = ()=>commit(i.value);   // onchange 提交，避免每键重渲丢焦点
  return i;
}
/* ================= 极简 markdown 渲染 ================= */
// esc 转义引号：badges 等处会把 esc 结果拼进 title="..." 属性上下文（Task 5 评审 Minor 1）
function esc(s){ return s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;"); }
function inline(s){
  return esc(s).replace(/`([^`]+)`/g,"<code>$1</code>").replace(/\*\*([^*]+)\*\*/g,"<b>$1</b>");
}
function renderMd(src){
  const lines = src.split("\n"); let html="", i=0;
  while(i<lines.length){
    const l = lines[i];
    if(l.startsWith("```")){
      const buf=[]; i++;
      while(i<lines.length && !lines[i].startsWith("```")) buf.push(lines[i++]);
      i++; html += "<pre><code>"+esc(buf.join("\n"))+"</code></pre>"; continue;
    }
    const h = l.match(/^(#{1,3})\s+(.*)/);
    if(h){ html += "<h"+h[1].length+">"+inline(h[2])+"</h"+h[1].length+">"; i++; continue; }
    if(/^\s*[-*]\s+/.test(l)){
      const items=[];
      while(i<lines.length && /^\s*[-*]\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*[-*]\s+/,""));
      html += "<ul>"+items.map(x=>"<li>"+inline(x)+"</li>").join("")+"</ul>"; continue;
    }
    if(l.trim()===""){ i++; continue; }
    html += "<p>"+inline(l)+"</p>"; i++;
  }
  return html;
}

/* ---- README 增强渲染（manage-fix3 反馈3）：仅 renderProjectReadme 使用，条目正文仍走上方
   renderMd 不动。在 renderMd 基础上加：GFM 管道表格、h4-h6、有序列表、引用块、hr、
   行内链接/图片、白名单内联 HTML；最终整串过 sanitizeHtml（DOMParser 解析后按白名单
   重建，见下）兜底。README 与条目正文都不可全信：白名单外标签拆壳留内容，script/iframe/
   object 等连内容剥除，on 事件属性与 style 等一律剥除，href/src 走协议白名单。 ---- */
const RICH_TAGS = {p:1,br:1,b:1,strong:1,i:1,em:1,a:1,img:1,code:1,pre:1,span:1,div:1,
  h1:1,h2:1,h3:1,h4:1,h5:1,h6:1,ul:1,ol:1,li:1,blockquote:1,sub:1,sup:1,hr:1,
  table:1,thead:1,tbody:1,tr:1,th:1,td:1};
const RICH_DROP = {script:1,iframe:1,object:1,style:1,svg:1,math:1,template:1,noscript:1};
// inlineRich：先整体 esc；code 段占位保护（内容保持字面）；白名单标签的转义序列还原为真
// 标签（属性仍带实体，交由 sanitizeHtml 清洗重建）；再做加粗/斜体/链接/图片行内替换
function inlineRich(s){
  const codes = [];
  let t2 = esc(s).replace(/`([^`]+)`/g, (m,c)=>{ codes.push(c); return "\x00"+(codes.length-1)+"\x00"; });
  t2 = t2.replace(/&lt;(\/?)([a-zA-Z][a-zA-Z0-9]*)((?:(?!&gt;)[\s\S])*?)&gt;/g, (m,slash,tag,attrs)=>{
    if(!RICH_TAGS[tag.toLowerCase()]) return m;
    return "<"+slash+tag.toLowerCase()+attrs+">";
  });
  t2 = t2.replace(/\*\*([^*]+)\*\*/g,"<b>$1</b>").replace(/\*([^*]+)\*/g,"<i>$1</i>");
  t2 = t2.replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g,'<img alt="$1" src="$2">');
  t2 = t2.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g,'<a href="$2">$1</a>');
  return t2.replace(/\x00(\d+)\x00/g, (m,n)=>"<code>"+codes[+n]+"</code>");
}
function richAttrs(n, extra){
  let s = "";
  const allow = ["align","title"].concat(extra||[]);
  for(let i=0;i<allow.length;i++){
    const v = n.getAttribute(allow[i]);
    if(v===null) continue;
    const tv = v.trim();
    if(allow[i]==="href" && !/^(https?:|mailto:|#)/i.test(tv)) continue;   // javascript:/相对链接剥除
    if(allow[i]==="align" && !/^(left|right|center|justify)$/i.test(tv)) continue;
    if(/^(width|height)$/.test(allow[i]) && !/^\d{1,4}%?$/.test(tv)) continue;
    if(/^(colspan|rowspan)$/.test(allow[i]) && !/^\d{1,2}$/.test(tv)) continue;
    s += " "+allow[i]+'="'+esc(v)+'"';
  }
  return s;
}
function serKids(n){
  let out = "";
  for(let i=0;i<n.childNodes.length;i++) out += serNode(n.childNodes[i]);
  return out;
}
function serNode(n){
  if(n.nodeType===3) return esc(n.nodeValue);
  if(n.nodeType!==1) return "";
  const tag = n.tagName.toLowerCase();
  if(RICH_DROP[tag]) return "";
  if(!RICH_TAGS[tag]) return serKids(n);   // 白名单外标签拆壳、保留子内容
  if(tag==="img"){
    const src = (n.getAttribute("src")||"").trim();
    if(/^https?:\/\//i.test(src))   // shields.io 等外链图正常放行
      return '<img src="'+esc(src)+'"'+richAttrs(n,["alt","width","height"])+'>';
    // 相对路径图 daemon 不分发（白名单静态文件）→ 占位徽标，避免 404 刷屏
    const label = (n.getAttribute("alt")||"").trim() || src.split("/").pop() || "image";
    return '<span class="img-ph" title="'+esc(src)+'">'+esc(label)+'</span>';
  }
  if(tag==="a"){
    const at = richAttrs(n,["href"]);
    // 外链新窗口打开，避免整窗导航把 GUI 顶掉
    const ext = /\shref="https?:\/\//i.test(at) ? ' target="_blank" rel="noopener noreferrer"' : "";
    return "<a"+at+ext+">"+serKids(n)+"</a>";
  }
  if(tag==="td"||tag==="th") return "<"+tag+richAttrs(n,["colspan","rowspan"])+">"+serKids(n)+"</"+tag+">";
  if(tag==="br"||tag==="hr") return "<"+tag+">";
  return "<"+tag+richAttrs(n)+">"+serKids(n)+"</"+tag+">";
}
function sanitizeHtml(html){
  const doc = new DOMParser().parseFromString(html, "text/html");
  return serKids(doc.body);
}
function renderMdRich(src){
  const lines = src.split("\n"); let html="", i=0;
  const cells = l=>{ let s=l.trim(); if(s.startsWith("|")) s=s.slice(1); if(s.endsWith("|")) s=s.slice(0,-1); return s.split("|").map(c=>c.trim()); };
  const isDelim = l=>{ const cs=cells(l); return cs.length>0 && cs.every(c=>/^:?-+:?$/.test(c)); };
  const aligns = l=>cells(l).map(c=>c.startsWith(":")&&c.endsWith(":")?"center":c.endsWith(":")?"right":"");
  while(i<lines.length){
    const l = lines[i];
    if(l.startsWith("```")){
      const buf=[]; i++;
      while(i<lines.length && !lines[i].startsWith("```")) buf.push(lines[i++]);
      i++; html += "<pre><code>"+esc(buf.join("\n"))+"</code></pre>"; continue;
    }
    // GFM 管道表格：表头行 + 分隔行（| --- | :---: |），分隔行冒号给对齐
    if(l.trim().startsWith("|") && i+1<lines.length && isDelim(lines[i+1])){
      const heads = cells(l), al = aligns(lines[i+1]); i += 2;
      const rows = [];
      while(i<lines.length && lines[i].trim().startsWith("|")) rows.push(cells(lines[i++]));
      const at = j=>al[j]?' align="'+al[j]+'"':"";
      html += "<table><thead><tr>"+heads.map((x,j)=>"<th"+at(j)+">"+inlineRich(x)+"</th>").join("")
            + "</tr></thead><tbody>"
            + rows.map(r=>"<tr>"+r.map((c,j)=>"<td"+at(j)+">"+inlineRich(c)+"</td>").join("")+"</tr>").join("")
            + "</tbody></table>";
      continue;
    }
    const h = l.match(/^(#{1,6})\s+(.*)/);
    if(h){ html += "<h"+h[1].length+">"+inlineRich(h[2])+"</h"+h[1].length+">"; i++; continue; }
    if(/^\s*(---+|\*\*\*+|___+)\s*$/.test(l)){ html += "<hr>"; i++; continue; }
    if(/^\s*>/.test(l)){
      const buf=[];
      while(i<lines.length && /^\s*>/.test(lines[i])) buf.push(lines[i++].replace(/^\s*>\s?/,""));
      html += "<blockquote>"+buf.map(x=>x.trim()===""?"":"<p>"+inlineRich(x)+"</p>").join("")+"</blockquote>"; continue;
    }
    if(/^\s*[-*]\s+/.test(l)){
      const items=[];
      while(i<lines.length && /^\s*[-*]\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*[-*]\s+/,""));
      html += "<ul>"+items.map(x=>"<li>"+inlineRich(x)+"</li>").join("")+"</ul>"; continue;
    }
    if(/^\s*\d+[.)]\s+/.test(l)){
      const items=[];
      while(i<lines.length && /^\s*\d+[.)]\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*\d+[.)]\s+/,""));
      html += "<ol>"+items.map(x=>"<li>"+inlineRich(x)+"</li>").join("")+"</ol>"; continue;
    }
    // 白名单标签开头的整行内联 HTML（README 徽标段/截图段常见）原样放行，sanitizeHtml 收尾
    const raw = l.match(/^\s*<(\/?)([a-zA-Z][a-zA-Z0-9]*)[\s>\/]/);
    if(raw && RICH_TAGS[raw[2].toLowerCase()]){ html += l+"\n"; i++; continue; }
    if(l.trim()===""){ i++; continue; }
    html += "<p>"+inlineRich(l)+"</p>"; i++;
  }
  return sanitizeHtml(html);
}

/* ================= 管理页 ================= */
/* 两级树（项目→条目）+ markdown 详情，结构/交互照抄原型 renderTree/fillTree/renderDetail
   （prototype-manager-v2.html:1414-1473），mock 换真：项目=GET /api/projects，
   条目=GET /api/entries?project=（逐项目拉取），详情正文=GET /api/entry?project=&file=。
   响应字段已核 api.go:302-352（entrySummaryJSON：file/title/type/tags/mandatory/draft/
   archived/summary/mtime[unix 秒]；无 size/born——born 从 tags 的 born:<名> 提取，
   大小后端不返回故详情不展示）。条目操作沿用旧 GUI 语义（旧 app.js:638-825）：
   操作组在详情页右上（编辑/归档或取消归档/删除，草稿额外批准）；新建按钮在树头部
   搜索框旁；新建/编辑走详情区内联编辑态（规范源 docs/prototypes/prototype-edit-inline.html，
   原 640px 弹窗已废弃）+ ✨优化走 /api/entry/optimize（详情区对照态回填，保存才落盘）。
   born/继承徽标 hover 浮动窗沿用 v2.18.2 行为（branch-info，旧 app.js:321-374）。
   编辑/对照态期间轮询与后台刷新跳过整页重渲（edBusy 守卫）；表单值 oninput 直写
   edDraft，重渲不丢内容。
   试用反馈迭代（manage-v2）：树栏加宽至 360px + 可拖拽分隔条（localStorage 持久化，
   200px~50% 钳制）；点项目节点右侧显示项目 README（GET /api/project/readme，无 README
   回落 wiki 概述条目）；截断标题悬停出完整标题浮窗（事件委托）；项目按 last_update
   降序 + 4s 轮询已展开项目条目，有变化才重渲；项目手风琴展开（默认展开最近更新项）
   + 条目懒加载（>50 先渲 50，滚到底追加）。
   复验迭代（manage-fix3）：默认展开的项目同时默认选中（右侧直接显示其 README）；
   项目行双热区——名前图标/箭头区（.pj-toggle）=展开/收起，项目名区（.pj-name，含计数）
   =选中并预览 README；README 正文走 renderMdRich（GFM 表格 + 白名单内联 HTML +
   协议白名单图链，相对路径图渲染为 .img-ph 占位徽标），条目正文仍走 renderMd 不动。 */
const TYPE_LABEL = { pitfall:"坑", note:"注", rule:"规", reference:"参" };
let MGMT = null;        // {list:[{name,paths,lastUpdate,entries,err}], loadErr} 缓存；loadManage 惰性加载
let DETAIL = null;      // {key(project\nfile), data, err} 当前选中条目全文缓存
const BRANCH = {};      // project → branch-info（会话级缓存；null=已拉取但失败/无数据，占位防重拉）
let llmMask = null;   // 未配置模型提示弹窗节点（body 级，只存一个）
let optAbort = null;  // 优化进行中的 AbortController；退出编辑态即取消
let edDraft = null, edBase = null, edCmp = null;   // 内联编辑草稿/脏态基线/优化结果（同时只存一份）
let edErr = "", edSavedFb = false;   // 编辑态保存失败错误（操作行 fb2 err）/ 只读态 ✓已保存 闪示
const README = {};      // project → {data, err} 项目 README/wiki 概述缓存（条目变更即失效）
const LAZY_STEP = 50;   // 树内条目懒加载步进：项目条目 >50 时先渲 50，滚到底附近再追加下一批
const TREE_W_KEY = "ok-tree-w";   // 树栏宽度 localStorage 键（拖拽分隔条持久化）
let treeTip = null;     // 截断标题悬浮窗节点（body 级，同时只一个）
let mgmtPollBusy = false;         // 管理页 4s 轮询重入保护

function fmtTime(unix){
  if(!unix) return "";
  const d = new Date(unix*1000);
  const p = n=>String(n).padStart(2,"0");
  return d.getFullYear()+"-"+p(d.getMonth()+1)+"-"+p(d.getDate())+" "+p(d.getHours())+":"+p(d.getMinutes());
}
// bornOf 取条目出生分支（born:<名>，第一个）；无则空串（旧 app.js:312 同款）
function bornOf(e){
  const tags = e.tags || [];
  for(let i=0;i<tags.length;i++) if(tags[i].indexOf("born:")===0) return tags[i].slice(5);
  return "";
}
function findEntry(project, file){
  if(!MGMT || !MGMT.list) return null;
  for(const p of MGMT.list){
    if(p.name!==project) continue;
    for(const e of p.entries) if(e.file===file) return { project:project, entry:e };
  }
  return null;
}

function loadManage(){ if(MGMT) return; MGMT = { list:[] }; refreshManage(); }
// refreshManage 全量重拉项目+条目；项目按 last_update 降序（kb.db mtime，api.go listProjects
// 口径，最近有知识写入的排前）。完成时原位刷新（过滤框聚焦中只重填树、不整页重渲，保焦点）
function refreshManage(){
  api("/api/projects").then(ps=>{
    return Promise.all((ps||[]).map(p=>
      api("/api/entries?project="+encodeURIComponent(p.name))
        .then(es=>({ name:p.name, paths:p.paths||[], lastUpdate:p.last_update||0, entries:es||[] }))
        .catch(err=>({ name:p.name, paths:p.paths||[], lastUpdate:p.last_update||0, entries:[], err:err.message }))));
  }).then(list=>{
    list.sort((a,b)=>(b.lastUpdate||0)-(a.lastUpdate||0) || (a.name<b.name?-1:a.name>b.name?1:0));
    MGMT = { list:list };
    // 手风琴兜底（反馈7）：用户尚未手动展开/收起过且无展开项时，默认展开最近更新的项目；
    // 同时默认选中该项目（manage-fix3 反馈1）→ 右侧直接显示其 README 视图而非空态
    if(!state.openTouched && list.length && !list.some(p=>state.open[p.name]===true)){
      state.open = {}; state.open[list[0].name] = true;
      if(!state.sel){ state.projSel = list[0].name; loadReadme(list[0].name); }
    }
    // 选中项目已被外部删除 → 清项目选择，避免 README 视图卡在加载态
    if(!state.sel && state.projSel && !list.some(p=>p.name===state.projSel)) state.projSel = null;
    // 选中条目已消失（外部删除/项目删除）→ 清选择与详情缓存（编辑/对照态中不动选择，保存时报错兜底）
    if(!edBusy() && state.sel && !findEntry(state.sel.project, state.sel.file)){ state.sel = null; DETAIL = null; }
  }).catch(err=>{
    MGMT = { list:[], loadErr: err.message };
  }).then(()=>{
    if(state.menu!=="manage" || edBusy()) return;   // 编辑/对照态中不重渲（草稿优先）
    const ae = document.activeElement;
    if(ae && ae.classList && ae.classList.contains("search")){
      const sc = document.querySelector(".tree-scroll");
      if(sc) fillTree(sc);
      return;
    }
    render();
  });
}
// loadDetail 拉选中条目全文（force 用于条目操作后绕过缓存重拉）；竞态按 key 匹配丢弃过期响应
function loadDetail(force){
  if(!state.sel){ DETAIL = null; return; }
  const key = state.sel.project+"\n"+state.sel.file;
  if(!force && DETAIL && DETAIL.key===key && (DETAIL.data || DETAIL.err)) return;
  DETAIL = { key:key, data:null, err:"" };
  api("/api/entry?project="+encodeURIComponent(state.sel.project)+"&file="+encodeURIComponent(state.sel.file))
    .then(d=>{ if(DETAIL && DETAIL.key===key) DETAIL = { key:key, data:d, err:"" }; })
    .catch(err=>{ if(DETAIL && DETAIL.key===key) DETAIL = { key:key, data:null, err:err.message }; })
    .then(()=>{ if(state.menu==="manage" && !edBusy()) render(); });
}
// loadBranchInfo 惰性拉项目分支上下文（继承徽标 hover 数据，v2.18.2 既有端点）
function loadBranchInfo(project){
  if(!project || project in BRANCH) return;
  BRANCH[project] = null;
  api("/api/project/branch-info?project="+encodeURIComponent(project))
    .then(info=>{ BRANCH[project] = info || null; })
    .catch(()=>{ BRANCH[project] = null; })
    .then(()=>{ if(state.menu==="manage" && !edBusy()) render(); });
}
// loadReadme 拉项目 README/wiki 概述（点项目节点时，反馈3）；条目变更后由调用方删缓存重拉
function loadReadme(project, force){
  if(!project) return;
  if(!force && README[project] && (README[project].data || README[project].err)) return;
  README[project] = { data:null, err:"" };
  api("/api/project/readme?project="+encodeURIComponent(project))
    .then(d=>{ README[project] = { data:d, err:"" }; })
    .catch(err=>{ README[project] = { data:null, err:err.message }; })
    .then(()=>{ if(state.menu==="manage" && !state.sel && state.projSel===project && !edBusy()) render(); });
}

/* 管理页 ~4s 轮询（反馈6，沿用日志页轮询范式）：菜单在管理页且页面可见时，重拉
   /api/projects 与当前展开项目的 /api/entries，与 MGMT 缓存对比有变化才重渲；
   项目增删走 refreshManage 全量。重渲保持树滚动（render 外壳）、展开项（state.open
   不动）、当前选中条目与过滤框内容；CLI 侧 ok add/修改 4 秒内反映到界面。 */
async function pollManage(){
  if(state.menu!=="manage" || !MGMT || MGMT.loadErr || document.hidden || mgmtPollBusy) return;
  if(document.body.classList.contains("tree-resizing")) return;   // 拖拽分隔条期间不重渲
  mgmtPollBusy = true;
  try {
    const ps = await api("/api/projects").catch(()=>null);
    if(!ps) return;   // 轮询失败静默、下轮重试（401 由 api() 自动刷新取新 token）
    const names = ps.map(p=>p.name).sort().join("\n");
    if(names !== MGMT.list.map(p=>p.name).sort().join("\n")){ refreshManage(); return; }   // 项目增删 → 全量
    const lu = {}; ps.forEach(p=>{ lu[p.name] = p.last_update||0; });
    const luChanged = MGMT.list.some(p=>(p.lastUpdate||0)!==(lu[p.name]||0));
    const openP = MGMT.list.find(p=>state.open[p.name]===true);
    let newEntries = null;
    if(openP && !openP.err){
      const es = await api("/api/entries?project="+encodeURIComponent(openP.name)).catch(()=>null);
      if(es && JSON.stringify(es)!==JSON.stringify(openP.entries)) newEntries = es;
    }
    if(!luChanged && !newEntries) return;   // 无变化不重渲
    MGMT.list.forEach(p=>{ p.lastUpdate = lu[p.name]||0; });
    MGMT.list.sort((a,b)=>(b.lastUpdate||0)-(a.lastUpdate||0) || (a.name<b.name?-1:a.name>b.name?1:0));
    if(newEntries){
      openP.entries = newEntries;
      delete README[openP.name];   // wiki 概述回落可能随条目变化
      if(state.projSel===openP.name && !state.sel) loadReadme(openP.name);
    }
    if(!edBusy() && state.sel && !findEntry(state.sel.project, state.sel.file)){ state.sel = null; DETAIL = null; }
    if(edBusy()) return;   // 编辑/对照态中缓存照更、界面不重渲，不打断表单
    const ae = document.activeElement;
    if(ae && ae.classList && ae.classList.contains("search")){
      const sc = document.querySelector(".tree-scroll");
      if(sc) fillTree(sc);
      return;
    }
    render();
  } finally { mgmtPollBusy = false; }
}
setInterval(pollManage, 4000);
// 页面从最小化/切 tab 回来立即补一轮（不可见期间 pollManage 自行跳过，即停轮）
document.addEventListener("visibilitychange", ()=>{ if(!document.hidden) pollManage(); });

function badges(e){
  const born = bornOf(e);
  return (born ? '<span class="badge-born" title="born">'+ICON.branch+esc(born)+'</span>' : "")
       + '<span class="badge-type t-'+esc(e.type)+'" title="'+esc(e.type)+'">'+(TYPE_LABEL[e.type]||"?")+'</span>';
}
function detailAttrs(e, proj){
  let s = "";
  const born = bornOf(e);
  if(born) s += '<span class="badge-born" title="born">'+ICON.branch+esc(born)+'</span>';
  s += (e.tags||[]).map(x=>'<span class="tag">'+esc(x)+'</span>').join("");
  s += e.mandatory ? '<span class="badge-mand">'+t("mandatory")+'</span>'
                   : '<span class="tag" style="opacity:.6">'+t("optional")+'</span>';
  if(e.draft) s += '<span class="badge-draft">'+t("draft")+'</span>';
  if(e.archived) s += '<span class="badge-arch">'+t("archived")+'</span>';
  // 继承徽标 + hover 浮动窗（v2.18.2 行为）：inherited 态显示「当前分支 · 继承基线」，
  // hover 出 bubble——wiki 基线继承自 <来源>@<commit 短 7 位> · 落后 N commit
  const bi = BRANCH[proj];
  if(bi && bi.branch_state==="inherited"){
    const src = bi.inherited_from || bi.base_branch || "—";
    const short = String(bi.last_commit || "").slice(0,7);
    const bubble = t("inheritBubble")
      .replace("{src}", src+(short?"@"+short:"")).replace("{n}", String(bi.behind||0));
    s += '<span class="tip"><span class="badge-inherit">'
       + esc((bi.current_branch||"—")+" · "+t("inheritBadge"))+'</span>'
       + '<span class="bubble">'+esc(bubble)+'</span></span>';
  }
  return s;
}

function renderTree(){
  const tree = el("div","tree");
  tree.style.width = treeWidth()+"px";   // 拖拽分隔条的持久化宽度（反馈2）
  const head = el("div","tree-head");
  head.appendChild(Object.assign(el("span","caption"),{textContent:t("treeCaption")}));
  tree.appendChild(head);
  // 工具行：过滤框 + 新建按钮（方案决策：新建在树头部搜索框旁）
  const tools = el("div","tree-tools");
  const search = el("input","search");
  search.placeholder = t("filter"); search.value = state.q;
  search.oninput = ()=>{ state.q = search.value; fillTree(scroll); };
  tools.appendChild(search);
  const add = el("button","btn tree-add");
  add.textContent = t("mgNew");
  add.disabled = !MGMT || !MGMT.list.length;
  add.onclick = ()=>{
    const def = (state.sel && findEntry(state.sel.project, state.sel.file) && state.sel.project)
      || state.projSel
      || (MGMT.list[0] && MGMT.list[0].name) || "";
    startNew(def);
  };
  tools.appendChild(add);
  tree.appendChild(tools);
  const scroll = el("div","tree-scroll");
  // 截断标题悬浮窗（反馈5）：事件委托挂在树容器，仅当标题被截断（scrollWidth>clientWidth）才显示
  scroll.addEventListener("mouseover", ev=>{
    const leaf = ev.target && ev.target.closest ? ev.target.closest(".leaf") : null;
    const t2 = leaf && leaf.querySelector(".t2");
    if(!t2 || t2.scrollWidth <= t2.clientWidth){ hideTreeTip(); return; }
    if(treeTip && treeTip._for === leaf) return;
    showTreeTip(leaf, t2.textContent);
  });
  scroll.addEventListener("mouseleave", hideTreeTip);
  // 懒加载（反馈7）：滚到底附近时给还有未渲条目的展开项目追加一批
  scroll.addEventListener("scroll", ()=>{
    hideTreeTip();
    if(scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 60) growTreeShown(scroll);
  });
  tree.appendChild(scroll);
  fillTree(scroll);
  return tree;
}
function fillTree(scroll){
  hideTreeTip();
  scroll.innerHTML = "";
  if(!MGMT) return;
  if(MGMT.loadErr){
    scroll.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:t("mgTreeErr")+MGMT.loadErr}));
    return;
  }
  const q = state.q.trim().toLowerCase();
  MGMT.list.forEach(p=>{
    const list = p.entries.filter(e=>!q || e.title.toLowerCase().includes(q));
    if(q && !list.length) return;   // 过滤时无命中项目整组隐藏（原型语义）；无过滤时空项目也显示（否则树里不可见）
    const open = q ? true : state.open[p.name] === true;   // 手风琴（反馈7）：至多一个项目展开
    // 项目行双热区（manage-fix3 反馈2）：名前 图标/箭头区=展开/收起；项目名区（含计数）=选中
    // 项目并右侧预览 README。两区行为独立，hover 各有视觉提示（style.css .pj-toggle/.pj-name）
    const pj = el("div","tn-proj"+(open?" open":"")+(!state.sel && state.projSel===p.name?" psel":""));
    pj.title = p.err ? t("mgTreeErr")+p.err : (p.paths||[]).join("\n");
    const tg = el("span","pj-toggle");
    tg.innerHTML = '<span class="caret">▶</span><span class="folder">'+ICON.folder+'</span>';
    tg.onclick = ev=>{
      ev.stopPropagation();
      exitEdit();
      state.openTouched = true;
      if(open){ state.open[p.name] = false; }              // 再点收起 → 全收起
      else { state.open = {}; state.open[p.name] = true; } // 展开即互斥收起其他
      render();
    };
    const nm = el("span","pj-name");
    nm.innerHTML = '<span class="nm">'+esc(p.name)+'</span><span class="cnt">'+(p.err?"!":list.length)+'</span>';
    nm.onclick = ()=>{
      exitEdit();
      state.sel = null; DETAIL = null;
      state.projSel = p.name; loadReadme(p.name);          // 点项目名 → 右侧显示项目 README（反馈3）
      render();
    };
    pj.appendChild(tg); pj.appendChild(nm);
    scroll.appendChild(pj);
    if(open){
      const kids = el("div","tn-kids");
      const shown = state.treeShown[p.name] || LAZY_STEP;
      list.slice(0, shown).forEach(e=>{
        const sel = state.sel && state.sel.project===p.name && state.sel.file===e.file;
        const leaf = el("button","leaf"+(sel?" sel":"")+(e.archived?" archived":""));
        leaf.innerHTML = '<span class="l1">'+badges(e)
          +(e.mandatory?'<span class="badge-mand">★</span>':"")
          +(e.draft?'<span class="badge-draft">'+t("draft")+'</span>':"")+'</span>'
          +'<span class="t2">'+esc(e.title)+'</span>';
        leaf.onclick = ()=>{ exitEdit(); state.sel={ project:p.name, file:e.file }; state.mgmtFb=null; loadDetail(); render(); };
        kids.appendChild(leaf);
      });
      if(list.length > shown){
        kids.appendChild(Object.assign(el("div","lazy-more"),
          {textContent:t("lazyMore").replace("{n}", String(list.length - shown))}));
      }
      scroll.appendChild(kids);
    }
  });
}
// growTreeShown 懒加载追加（反馈7）：第一个还有未渲条目的展开项目步进 LAZY_STEP，
// 原位重填树；追加在列表尾部，scrollTop 天然不变
function growTreeShown(scroll){
  if(!MGMT || !MGMT.list) return;
  const q = state.q.trim().toLowerCase();
  for(const p of MGMT.list){
    if(!(q ? true : state.open[p.name]===true)) continue;
    const total = p.entries.filter(e=>!q || e.title.toLowerCase().includes(q)).length;
    const shown = state.treeShown[p.name] || LAZY_STEP;
    if(total > shown){
      state.treeShown[p.name] = shown + LAZY_STEP;
      fillTree(scroll);
      return;
    }
  }
}

/* 截断标题悬浮窗（反馈5）：继承徽标 bubble 同款配色（#111827 深底）的 body 级浮窗，
   定位在条目右侧；右边界放不下时收回视口内 */
function showTreeTip(leaf, text){
  hideTreeTip();
  const n = el("div","tt-pop");
  n.textContent = text;
  n._for = leaf;
  document.body.appendChild(n);
  const r = leaf.getBoundingClientRect();
  n.style.left = Math.max(8, Math.min(r.right+8, window.innerWidth-n.offsetWidth-8))+"px";
  n.style.top = (r.top + r.height/2)+"px";
  treeTip = n;
}
function hideTreeTip(){
  if(treeTip){ treeTip.remove(); treeTip = null; }
}

/* 树栏宽度（反馈2）：localStorage 持久化，钳制 200px ~ 视口 50% */
function clampTreeW(w){
  return Math.round(Math.min(Math.max(240, window.innerWidth*0.5), Math.max(200, w)));
}
function treeWidth(){
  const w = parseInt(localStorage.getItem(TREE_W_KEY)||"", 10);
  return isNaN(w) ? 360 : clampTreeW(w);
}
// 拖拽分隔条：mousedown 后 document 级 mousemove/mouseup 跟随，松手写回 localStorage
function renderTreeResizer(){
  const bar = el("div","tree-resizer");
  bar.addEventListener("mousedown", ev=>{
    ev.preventDefault();
    const treeEl = document.querySelector(".tree");
    if(!treeEl) return;
    const startX = ev.clientX, startW = treeEl.getBoundingClientRect().width;
    document.body.classList.add("tree-resizing");
    const move = e=>{ treeEl.style.width = clampTreeW(startW + e.clientX - startX)+"px"; };
    const up = ()=>{
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", up);
      document.body.classList.remove("tree-resizing");
      localStorage.setItem(TREE_W_KEY, String(clampTreeW(treeEl.getBoundingClientRect().width)));
    };
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
  });
  return bar;
}

// 项目节点详情（反馈3）：项目名 + README/wiki 概述来源行 + renderMdRich 正文（manage-fix3：
// 表格/白名单内联 HTML/外链图，相对路径图显示占位徽标）；无 README 给空态提示
function renderProjectReadme(d, project){
  const rm = README[project];
  d.appendChild(Object.assign(el("div","d-path"),{textContent:"projects/"+project+"/"}));
  d.appendChild(Object.assign(el("h1","d-title"),{textContent:project}));
  if(!rm || (!rm.data && !rm.err)){
    d.appendChild(Object.assign(el("div","pdesc"),{textContent:t("mgLoading")}));
    return;
  }
  if(rm.err){
    d.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:t("readmeLoadFail")+rm.err}));
    return;
  }
  const r = rm.data;
  if(!r || !r.found){
    d.appendChild(Object.assign(el("div","d-summary pdesc"),{textContent:t("readmeEmpty")}));
    return;
  }
  const src = r.source==="wiki" ? t("readmeSrcWiki") : t("readmeSrcReadme");
  d.appendChild(Object.assign(el("div","d-filemeta"),{textContent:src.replace("{p}", r.path||"")}));
  const bd = el("div","d-body md");
  bd.innerHTML = renderMdRich(r.content||"");
  d.appendChild(bd);
}

function renderDetail(){
  // 详情区三态分派（内联编辑改版，规范源 prototype-edit-inline.html）：编辑/对照态接管详情区
  if(edBusy()){
    if(state.edView==="cmp" && edCmp) return renderEntryCmp();
    if(state.edView==="edit") return renderEntryEdit();
    exitEdit();   // 数据缺失兜底：回只读
  }
  const d = el("div","detail");
  const sel = state.sel && findEntry(state.sel.project, state.sel.file);
  if(!sel){
    if(state.projSel){ renderProjectReadme(d, state.projSel); return d; }   // 点项目节点 → README（反馈3）
    d.appendChild(Object.assign(el("div","placeholder"),{textContent:t("pickEntry")}));
    return d;
  }
  const e = sel.entry, proj = sel.project;
  // 右上操作组（方案决策位置）：编辑 /（草稿额外）批准 / 归档或取消归档 / 删除；左侧为操作反馈行
  const ops = el("div","d-ops");
  if(state.mgmtFb)
    ops.appendChild(Object.assign(el("span","fb2"+(state.mgmtFb.err?" err":"")),{textContent:state.mgmtFb.txt}));
  const mk = (label, cls, fn)=>{ const b=el("button",cls); b.textContent=label; b.onclick=fn; return b; };
  ops.appendChild(mk(t("opEdit"), "btn", ()=>startEdit(proj, e.file)));
  if(e.draft) ops.appendChild(mk(t("opApprove"), "btn", ()=>approveEntry(proj, e)));
  ops.appendChild(mk(e.archived?t("opUnarchive"):t("opArchive"), "btn", ()=>archiveEntry(proj, e, e.archived)));
  ops.appendChild(mk(t("opDelete"), "btn btn-danger", ()=>delEntry(proj, e)));
  d.appendChild(ops);
  // 相对路径（相对数据目录）/ 文件名 + mtime（后端不返回 size，见段头注释）
  d.appendChild(Object.assign(el("div","d-path"),
    {textContent:"projects/"+proj+"/knowledge/"+e.file}));
  const fm = el("div","d-filemeta");
  fm.innerHTML = "<b>"+esc(e.file)+"</b> · "+t("modified")+" "+fmtTime(e.mtime);
  d.appendChild(fm);
  d.appendChild(Object.assign(el("h1","d-title"),{textContent:e.title}));
  const at = el("div","d-attrs"); at.innerHTML = detailAttrs(e, proj);
  if(edSavedFb) at.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));   // 保存成功闪示（1.5s 自消）
  d.appendChild(at);
  const sm = el("div","d-summary"); sm.textContent = e.summary; d.appendChild(sm);
  const bd = el("div","d-body md");
  const det = DETAIL && DETAIL.key===(proj+"\n"+e.file) ? DETAIL : null;
  if(det && det.err) bd.innerHTML = '<p class="fb2 err">'+esc(det.err)+'</p>';
  else if(det && det.data) bd.innerHTML = renderMd(det.data.body||"");
  else bd.innerHTML = "<p>"+esc(t("mgLoading"))+"</p>";
  d.appendChild(bd);
  loadBranchInfo(proj);
  return d;
}

/* ---------- 条目操作（旧 GUI 语义：批准/归档/删除后刷新树与详情） ---------- */
function afterEntryOp(){
  state.mgmtFb = null;
  for(const k in README) delete README[k];   // 条目增删改可能影响 wiki 概述回落，README 缓存全失效
  refreshManage();               // 树刷新（选中项消失则自动清选择）
  if(state.sel) loadDetail(true);
  if(!state.sel && state.projSel) loadReadme(state.projSel);   // README 视图原位重拉
  if(MISC) refreshMisc();        // 其他页缓存联动（Task 4 评审约定：项目/条目增删改后其他页可刷新）
  render();
}
function opFail(err){ state.mgmtFb = { txt:err.message, err:true }; render(); }
// 批准草稿：draft 翻正并同步索引与向量（POST /api/approve）
function approveEntry(proj, e){
  api("/api/approve", { method:"POST", body:{ project:proj, file:e.file } })
    .then(afterEntryOp).catch(opFail);
}
// 归档/取消归档（POST /api/entry/archive {undo}）；归档需确认（旧 app.js:819 文案）
function archiveEntry(proj, e, undo){
  if(!undo && !confirm(t("cfmArchive").replace("{t}", e.title))) return;
  api("/api/entry/archive", { method:"POST", body:{ project:proj, file:e.file, undo:!!undo } })
    .then(afterEntryOp).catch(opFail);
}
// 删除（DELETE /api/entry?project=&file=），确认后执行；删除的是当前选中项时清选择
function delEntry(proj, e){
  if(!confirm(t("cfmDelete").replace("{t}", e.title))) return;
  api("/api/entry?project="+encodeURIComponent(proj)+"&file="+encodeURIComponent(e.file),
      { method:"DELETE" })
    .then(()=>{ state.sel=null; DETAIL=null; afterEntryOp(); })
    .catch(opFail);
}

/* ---------- 条目内联编辑（详情区三态：只读/编辑/优化对照） ----------
   规范源 docs/prototypes/prototype-edit-inline.html（用户已验收），DOM/样式对齐原型。
   编辑态：标题整行 → 类型/tags/mandatory 一行 → 摘要整行 → 正文撑满剩余高度；
   取消/保存/✨优化在右上操作行。表单值 oninput 直写 edDraft（不重渲不丢焦点），
   保存才落盘——新建 POST /api/entry（保存后选中新条目）、编辑 PUT /api/entry
   （file 为身份不可改名；created/draft/archived 由后端继承）。
   取消无脏检查确认：原型未画确认弹窗，取消即弃稿（与原型切条目即弃稿同语义）。 */
function edBusy(){ return state.edView!=="read" && !!edDraft; }
// 退出编辑/对照态：取消进行中的优化请求，草稿/基线/对照数据一并丢弃
function exitEdit(){
  if(optAbort){ optAbort.abort(); optAbort=null; }
  edDraft=null; edBase=null; edCmp=null; edErr=""; state.edView="read";
}
function newDraft(project, file, d){
  d = d || {};
  edDraft = { project:project, file:file, title:d.title||"", type:d.type||"note",
    tags:(d.tags||[]).join(", "), mandatory:!!d.mandatory,
    summary:d.summary||"", body:d.body||"" };
  edBase = Object.assign({}, edDraft);   // 脏态基线快照（改回基线值保存按钮自动变灰）
  edCmp=null; edErr=""; state.edView="edit"; state.mgmtFb=null;
}
// 编辑入口（只读态「编辑」按钮）：详情全文已在缓存（DETAIL）则直接用，否则拉取后进编辑态
function startEdit(project, file){
  const det = DETAIL && DETAIL.key===(project+"\n"+file) && DETAIL.data ? DETAIL.data : null;
  if(det){ newDraft(project, file, det); render(); return; }
  api("/api/entry?project="+encodeURIComponent(project)+"&file="+encodeURIComponent(file))
    .then(d=>{ newDraft(project, file, d); render(); })
    .catch(opFail);
}
// 新建入口（树头部「+ 新建」）：复用内联编辑态（原型未覆盖新建，决策记录见编辑报告）——
// 空白表单进编辑态，保存成功后选中新条目并展开其项目
function startNew(defProject){
  if(!defProject){
    state.mgmtFb = { txt:t("emNoProject"), err:true }; render(); return;
  }
  exitEdit();
  newDraft(defProject, null, null);
  render();
}
/* 编辑态（原型 renderEdit 平移，mock 换真）：详情区等高 flex 列——顶部操作行 + 字段组 +
   正文 textarea 撑满剩余高度。表单值 oninput 直写 edDraft（不重渲不丢焦点）。 */
function renderEntryEdit(){
  const isNew = !edDraft.file;
  const d = el("div","detail editing");
  d.appendChild(Object.assign(el("div","d-path"),
    {textContent:"projects/"+edDraft.project+"/knowledge/"+(edDraft.file||"")}));
  if(!isNew){
    const found = findEntry(edDraft.project, edDraft.file);
    const fm = el("div","d-filemeta");
    fm.innerHTML = "<b>"+esc(edDraft.file)+"</b>"
      +(found ? " · "+t("modified")+" "+fmtTime(found.entry.mtime) : "");
    d.appendChild(fm);
  }

  // 顶部操作行：标题+副题 | 保存失败错误（fb2 err 范式，沿用 prefsErr 驻留语义）+ 取消/保存/✨优化
  const bar = el("div","editbar");
  const bt = el("span","eb-title");
  bt.textContent = isNew ? t("emNew") : t("emEdit");
  bt.appendChild(Object.assign(el("span","eb-sub"),{textContent:t("editingSub")}));
  bar.appendChild(bt);
  const acts = el("span","eb-acts");
  if(edErr) acts.appendChild(Object.assign(el("span","fb2 err"),{textContent:edErr}));
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = ()=>{ exitEdit(); render(); };
  const save = el("button","btn btn-primary"); save.textContent = t("save");
  const opt = el("button","btn"); opt.textContent = t("optBtn"); opt.title = t("optTip");
  acts.appendChild(cancel); acts.appendChild(save); acts.appendChild(opt);
  bar.appendChild(acts);
  d.appendChild(bar);

  // 脏态只同步保存按钮禁用态（原型语义：无改动不可点保存；基线=进入编辑时的快照 edBase）
  const dirtyChk = ()=>{ save.disabled = !isDirty(); };
  const isDirty = ()=> fTitle.value!==edBase.title || fType.value!==edBase.type
    || fTags.value!==edBase.tags || fMand.checked!==edBase.mandatory
    || fSummary.value!==edBase.summary || fBody.value!==edBase.body
    || (isNew && projSel.value!==edBase.project);

  // 项目（仅新建可选，条目身份=项目+文件名；编辑态项目即身份不可改）
  let projSel = null;
  if(isNew){
    projSel = el("select","pselect");
    MGMT.list.forEach(p=>{ const o=el("option"); o.value=p.name; o.textContent=p.name; projSel.appendChild(o); });
    projSel.value = edDraft.project;
    projSel.onchange = ()=>{
      edDraft.project = projSel.value; dirtyChk();
      const dp = d.querySelector(".d-path");   // 路径行随项目联动（原位改文本，不重渲）
      if(dp) dp.textContent = "projects/"+edDraft.project+"/knowledge/";
    };
    d.appendChild(efField(t("xProject"), projSel));
  }

  // 标题（整行）
  const fTitle = el("input","pinput"); fTitle.value = edDraft.title;
  fTitle.oninput = ()=>{ edDraft.title=fTitle.value; dirtyChk(); };
  d.appendChild(efField(t("fTitle"), fTitle));

  // 类型 / tags / mandatory：小控件收进一行（tags 弹性占满）
  const meta = el("div","ef-meta");
  const fType = el("select","pselect");
  [["rule","typeRule"],["pitfall","typePitfall"],["note","typeNote"],["reference","typeReference"]]
    .forEach(([v,k])=>{ const o=el("option"); o.value=v; o.textContent=t(k); fType.appendChild(o); });
  fType.value = edDraft.type;
  fType.onchange = ()=>{ edDraft.type=fType.value; dirtyChk(); };
  meta.appendChild(efField(t("fType"), fType));
  const fTags = el("input","pinput"); fTags.value = edDraft.tags;
  fTags.oninput = ()=>{ edDraft.tags=fTags.value; dirtyChk(); };
  const tagsF = efField(t("fTags"), fTags); tagsF.classList.add("ef-tags");
  meta.appendChild(tagsF);
  const fMand = el("input"); fMand.type="checkbox"; fMand.className="radio"; fMand.checked = edDraft.mandatory;
  fMand.onchange = ()=>{ edDraft.mandatory=fMand.checked; dirtyChk(); };
  const mandL = el("label","ef-mand");
  mandL.appendChild(fMand); mandL.appendChild(document.createTextNode(t("fMand")));
  meta.appendChild(mandL);
  d.appendChild(meta);

  // 摘要（整行）
  const fSummary = el("input","pinput"); fSummary.value = edDraft.summary;
  fSummary.oninput = ()=>{ edDraft.summary=fSummary.value; dirtyChk(); };
  d.appendChild(efField(t("fSummary"), fSummary));

  // 正文：撑满剩余高度（本次改版主要诉求；resize:none 不允许拖拽变形）
  const bodyF = el("div","ef ef-grow");
  const bl = el("div","ef-label");
  bl.textContent = t("fBody");
  bl.appendChild(Object.assign(el("span","hint"),{textContent:"· "+t("fBodyHint")}));
  bodyF.appendChild(bl);
  const fBody = el("textarea","pinput ef-body"); fBody.value = edDraft.body;
  fBody.oninput = ()=>{ edDraft.body=fBody.value; dirtyChk(); };
  bodyF.appendChild(fBody);
  d.appendChild(bodyF);

  save.disabled = !isDirty();
  save.onclick = ()=>{
    const payload = { project:edDraft.project, title:fTitle.value, type:fType.value,
      tags:fTags.value.split(",").map(s=>s.trim()).filter(Boolean),
      mandatory:fMand.checked, summary:fSummary.value, body:fBody.value };
    save.disabled = true;
    if(!isNew) payload.file = edDraft.file;
    api("/api/entry", { method:isNew?"POST":"PUT", body:payload })
      .then(resp=>{
        // 保存成功回只读详情（三按钮随编辑态消失）；新建选中新条目并展开其项目
        if(isNew && resp && resp.file){
          state.sel = { project:payload.project, file:resp.file };
          state.projSel = payload.project;
          if(state.open[payload.project]!==true){
            state.openTouched = true; state.open = {}; state.open[payload.project] = true;
          }
        }
        exitEdit();
        afterEntryOp();   // 树/详情/README/其他页联动刷新（Task 5 既有范式）
        flashSaved();
      })
      .catch(err=>{
        edErr = err.status===409 ? t("emExists") : err.message;   // 错误驻留操作行，表单值由 edDraft 恢复
        render();
      });
  };
  // ✨优化：loading（禁用+文案）→ 对照态；409=未配置模型弹窗；退出编辑态取消请求
  opt.onclick = ()=>{
    if(!fBody.value.trim()){ edErr = t("optEmpty"); render(); return; }
    edErr = "";
    opt.disabled = true;
    opt.textContent = t("optBusy");
    optAbort = new AbortController();
    api("/api/entry/optimize", {
      method:"POST", signal:optAbort.signal,
      body:{ project:edDraft.project, file:edDraft.file||"", title:fTitle.value,
             tags:fTags.value, summary:fSummary.value, body:fBody.value },
    }).then(out=>{
      edCmp = out||{}; state.edView="cmp"; render();   // 一律进对照态（no_change 提示在态内展示）
    }).catch(err=>{
      if(err && err.name==="AbortError") return;   // 退出编辑态主动取消，静默
      if(err.status===409) openLlmNeededModal();
      else { edErr = err.message; render(); }
    }).finally(()=>{
      optAbort = null;
      if(state.edView==="edit"){ opt.disabled = false; opt.textContent = t("optBtn"); }
    });
  };
  return d;
}
function efField(label, input){
  const f = el("div","ef");
  f.appendChild(Object.assign(el("div","ef-label"),{textContent:label}));
  f.appendChild(input);
  return f;
}

/* ---------- 优化对照态（原型 renderCmp 平移；回填只改 edDraft，保存才落盘） ---------- */
function usageText(u){
  if(!u || (!u.prompt && !u.completion)) return "";
  return t("cmpUsage").replace("{t}", u.prompt+u.completion)
    .replace("{p}", u.prompt).replace("{c}", u.completion);
}
// 保存成功反馈（原型语义：回只读详情后徽标行闪 ✓ 已保存，1.5s 自消）
function flashSaved(){
  edSavedFb = true; render();
  setTimeout(()=>{ edSavedFb=false; if(!edBusy()) render(); }, 1500);
}
/* 对照态：顶部 tokens+放弃/全部接受并回填；标题/tags/摘要 old→new 紧凑对照（逐字段回填）；
   正文左右分栏（两栏等高各自滚动，新栏头回填按钮）。类型/mandatory 不参与回填（沿用旧 cmp 语义）。 */
function renderEntryCmp(){
  const d = el("div","detail compare");
  d.appendChild(Object.assign(el("div","d-path"),
    {textContent:"projects/"+edDraft.project+"/knowledge/"+(edDraft.file||"")}));
  // 单字段回填值：no_change 场景模型可能不回字段，空值回退草稿原值（不得清空表单）
  const fillVal = key=>{
    if(key==="tags") return (edCmp.tags||[]).join(", ") || edDraft.tags || "";
    return edCmp[key] || edDraft[key] || "";
  };

  // 顶部操作行：标题+依据 | tokens 用量 + 放弃 / 全部接受并回填
  const bar = el("div","editbar");
  const bt = el("span","eb-title");
  bt.textContent = t("cmpTitle");
  bt.appendChild(Object.assign(el("span","cmp-basis"),{textContent:" · "+t("cmpBasis")}));
  bar.appendChild(bt);
  const acts = el("span","eb-acts");
  acts.appendChild(Object.assign(el("span","cmp-note"),{textContent:usageText(edCmp.usage)}));
  const discard = el("button","btn"); discard.textContent = t("cmpDiscard");
  discard.onclick = ()=>{ state.edView="edit"; render(); };   // 放弃 → 回编辑态（草稿不动）
  const apply = el("button","btn btn-primary"); apply.textContent = t("cmpApply");
  apply.onclick = ()=>{                                       // 接受 → 回填草稿回编辑态
    edDraft.title=fillVal("title"); edDraft.tags=fillVal("tags");
    edDraft.summary=fillVal("summary"); edDraft.body=fillVal("body");
    state.edView="edit"; render();
  };
  acts.appendChild(discard); acts.appendChild(apply);
  bar.appendChild(acts);
  d.appendChild(bar);
  if(edCmp.no_change){
    d.appendChild(Object.assign(el("div","cmp-notice"),{textContent:t("cmpNotice")}));
  }

  // 小字段对照（标题/tags/摘要）：old（删除线灰）→ new 一行一个，右端逐字段回填
  const meta = el("div","cmp-meta");
  [["title","fTitle"],["tags","fTags"],["summary","fSummary"]].forEach(([key,labelKey])=>{
    const row = el("div","cmp-mrow");
    row.appendChild(Object.assign(el("span","k"),{textContent:t(labelKey)}));
    row.appendChild(Object.assign(el("span","old"),{textContent:edDraft[key]}));
    row.appendChild(Object.assign(el("span","arrow"),{textContent:"→"}));
    row.appendChild(Object.assign(el("span","new"),{textContent:fillVal(key)}));
    const fill = el("button","btn cmp-fill"); fill.textContent = t("cmpFill");
    fill.onclick = ()=>{ edDraft[key]=fillVal(key); fill.textContent=t("cmpFilled"); fill.disabled=true; };
    row.appendChild(fill);
    meta.appendChild(row);
  });
  d.appendChild(meta);

  // 正文左右分栏对照（两栏等高、各自滚动）
  const split = el("div","cmp-split");
  const pnOld = el("div","cmp-pane old");
  const hOld = el("div","cmp-pane-head");
  hOld.innerHTML = '<span class="cmp-tag old">'+t("cmpOld")+'</span>';
  pnOld.appendChild(hOld);
  pnOld.appendChild(Object.assign(el("div","cmp-pane-body"),{textContent:edDraft.body}));
  const pnNew = el("div","cmp-pane new");
  const hNew = el("div","cmp-pane-head");
  hNew.innerHTML = '<span class="cmp-tag new">'+t("cmpNew")+'</span>';
  const fillBody = el("button","btn cmp-fill"); fillBody.textContent = t("cmpFill");
  fillBody.onclick = ()=>{ edDraft.body=fillVal("body"); fillBody.textContent=t("cmpFilled"); fillBody.disabled=true; };
  hNew.appendChild(fillBody);
  pnNew.appendChild(hNew);
  pnNew.appendChild(Object.assign(el("div","cmp-pane-body"),{textContent:fillVal("body")}));
  split.appendChild(pnOld); split.appendChild(pnNew);
  d.appendChild(split);

  const note = el("div","cmp-note");
  note.style.marginTop = "8px";
  note.textContent = t("cmpNote");
  d.appendChild(note);
  return d;
}

/* ---------- 未配置模型提示弹窗（409 no_llm；去配置 → 设置页） ---------- */
function openLlmNeededModal(){
  const mask = el("div","mask");
  llmMask = mask;
  const m = el("div","modal"); m.style.width = "400px";
  m.appendChild(Object.assign(el("h3"),{textContent:t("llmTitle")}));
  m.appendChild(Object.assign(el("div","pdesc"),{textContent:t("llmMsg")}));
  const foot = el("div","mfoot");
  const close = ()=>{ mask.remove(); if(llmMask===mask) llmMask=null; };
  const go = el("button","btn btn-primary"); go.textContent = t("llmGo");
  go.onclick = ()=>{
    exitEdit();   // 去配置 = 离开管理页，编辑草稿一并丢弃（沿用旧弹窗关闭语义）
    close();
    state.menu = "prefs"; location.hash = "prefs"; render();
  };
  const ok = el("button","btn"); ok.textContent = t("xGotIt"); ok.onclick = close;
  foot.appendChild(go); foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = ev=>{ if(ev.target===mask) close(); };
  document.body.appendChild(mask);
}

/* ================= 设置页 ================= */
/* 八卡照抄原型 renderPrefs（docs/prototypes/prototype-manager-v2.html:801-979），mock 换真。
   初始聚合拉取：status 先行（取项目名 + hooksTimeout/disabled），随后 embedding/llm 两件与
   retrieve/capture/gate/enforce-rules 四件并行；单件失败只记 PREFS.errs[key]，不拖垮整页。
   冷却/沉淀/门控/规则四卡为全局配置（端点 project 缺省 = 读写全局 config.toml，无项目也可用）；
   项目名仅 embedding 卡用于索引模型比对。保存语义（Global Constraints 范式 2）：
   - 开关即开即存：全局总闸 POST /api/toggle、门控 POST /api/gate {enabled}；
   - 行内保存：Hook 超时 POST /api/hooks/timeout、冷却 POST /api/retrieve、沉淀 POST /api/capture
     ——pDirtyLive 对「读回的服务端原值」（PREFS 各件）实时判脏，改回原值自动变灰，oninput 不重渲不丢焦点；
   - 确定即生效：embedding/LLM/短语表弹窗与规则卡保存按钮，成功后卡片闪 ✓。
   工作副本 PREFS_D 与服务端读回值 PREFS 分离：整页重渲/他卡保存不丢本卡脏态。
   embedding 弹窗下载进度沿用旧 app.js 的 dlJob 机制（internal/gui/embedding.go:22-51 dlSnapshot）：
   POST download 后每 1s 重拉 GET /api/setup/embedding，只重画状态条（paintEmbDl，不整页重渲，
   保表单输入态），state 离开 downloading 停轮，关弹窗清计时器。
   两处原型字段无对应端点已裁剪（config 有 [embedding]/[llm] timeout_sec 键但无读写端点）：
   embedding/LLM 弹窗「全局」段的调用/生成超时。Hook 超时上限按后端校验取 1~60（原型 120 会 400）。 */
let PREFS = null;      // 服务端读回值 {status,project,emb,llm,retr,cap,gate,rules,errs{}}
let PREFS_D = null;    // 工作副本 {hooksTimeout,dedupTurns,capMode,capInterval,rules:[{type,globs,cl,msg}]}
const prefsFb = {};    // 开关/弹窗卡的 ✓ 反馈（g/gt/e/l），1.5s 自消
let gateModal = false, gateDraft = [], gateErr = "";
let embModal = false, embDraft = null, embEdit = -1, embForm = null, embErr = "";
let embDlEl = null, embPollT = null;
let llmModal = false, llmDraft = null, llmEdit = -1, llmForm = null, llmErr = "";

function flashPrefs(key){
  prefsFb[key] = true; render();
  setTimeout(()=>{ prefsFb[key] = false; render(); }, 1500);
}
function loadPrefs(){ if(PREFS) return; PREFS = { errs:{} }; refreshPrefs(); }
// 聚合拉取（多请求并行，非新聚合端点）；冷却/沉淀/门控/规则四件为全局配置，不带 project
function refreshPrefs(){
  api("/api/status").then(st=>{
    const project = (st.projects && st.projects[0] && st.projects[0].name) || "";
    const jobs = {
      emb: api("/api/setup/embedding"+(project?"?project="+encodeURIComponent(project):"")),
      llm: api("/api/llm"),
      retr: api("/api/retrieve"), cap: api("/api/capture"),
      gate: api("/api/gate"), rules: api("/api/enforce/rules"),
    };
    const keys = Object.keys(jobs);
    return Promise.all(keys.map(k=>jobs[k].catch(e=>({ __err:e.message })))).then(vals=>{
      const out = { status:st, project:project, errs:{} };
      keys.forEach((k,i)=>{
        if(vals[i] && vals[i].__err) out.errs[k] = vals[i].__err;
        else out[k] = vals[i];
      });
      PREFS = out;
      syncPrefsDraft();
    });
  }).catch(err=>{
    PREFS = { errs:{}, loadErr: err.message };
  }).then(()=>{ if(state.menu==="prefs" && !embModal && !llmModal && !gateModal) render(); });
}
// 服务端值 → 工作副本（规则转文本形：code_globs 数组 ↔ 逗号分隔串）
function syncRulesDraft(){
  const rules = (PREFS.rules && PREFS.rules.rules) || [];
  PREFS_D.rules = rules.map(r=>({ type:r.type, globs:(r.code_globs||[]).join(", "),
    cl:r.changelog_glob||"", msg:r.message||"" }));
}
function syncPrefsDraft(){
  PREFS_D = {
    hooksTimeout: PREFS.status.hooksTimeout || 10,
    dedupTurns: PREFS.retr ? PREFS.retr.dedup_turns : 0,
    capMode: PREFS.cap ? PREFS.cap.mode : "propose",
    capInterval: PREFS.cap ? PREFS.cap.turn_interval : 5,
    rules: [],
  };
  syncRulesDraft();
}
// 弹窗应用成功后的局部刷新（不碰其他卡的工作副本/脏态）
function refreshEmb(){
  return api("/api/setup/embedding"+(PREFS.project?"?project="+encodeURIComponent(PREFS.project):""))
    .then(d=>{ PREFS.emb = d; PREFS.errs.emb = ""; });
}
function refreshLlm(){
  return api("/api/llm").then(d=>{ PREFS.llm = d; PREFS.errs.llm = ""; });
}
function builtinLabel(id){
  const list = (PREFS.emb && PREFS.emb.builtin_models) || [];
  const m = list.find(x=>x.id===id);
  return m ? m.label : id;
}
// 无项目/单件加载失败时的只读提示卡
function prefsNoteCard(title, desc, note, isErr){
  const c = el("div","pcard");
  c.appendChild(Object.assign(el("h3"),{textContent:title}));
  if(desc) c.appendChild(Object.assign(el("div","pdesc"),{textContent:desc}));
  const n = Object.assign(el("div","pdesc"),{textContent:note});
  if(isErr) n.className = "pdesc fb2 err";
  c.appendChild(n);
  return c;
}
// pcard 的保存按钮默认 onclick 是 mock 的 pSave——设置页逐卡换成真实保存
function wireSave(card, key, fn){
  card.querySelector('[data-save="'+key+'"]').onclick = fn;
}

function splitGlobs(s){ return String(s||"").split(/[,，]/).map(x=>x.trim()).filter(Boolean); }
function rulesNorm(list){
  return JSON.stringify(list.map(r=>({ type:r.type, globs:splitGlobs(r.globs),
    cl:String(r.cl||"").trim(), msg:String(r.msg||"").trim() })));
}
// 规则卡判脏：工作副本 vs 服务端读回值（文本形同构后比较）——改回原值即变回灰
function rulesDirty(){
  const src = ((PREFS.rules && PREFS.rules.rules) || []).map(r=>({ type:r.type,
    globs:(r.code_globs||[]).join(", "), cl:r.changelog_glob||"", msg:r.message||"" }));
  return rulesNorm(PREFS_D.rules) !== rulesNorm(src);
}
function saveRules(){
  prefsErr.r = "";
  const payload = PREFS_D.rules.map(r=>({ type:r.type, code_globs:splitGlobs(r.globs),
    changelog_glob:String(r.cl||"").trim(), message:String(r.msg||"").trim() }));
  api("/api/enforce/rules", { method:"POST", body:{ rules:payload } })
    .then(()=>api("/api/enforce/rules"))   // 复读核对
    .then(d=>{ PREFS.rules = d; syncRulesDraft(); pSave("r"); })
    .catch(err=>{ prefsErr.r = err.message; render(); });
}

function renderPrefs(){
  loadPrefs();
  const d = el("div","prefs");
  if(PREFS.loadErr){
    d.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:t("xLoadFail")+PREFS.loadErr}));
    return d;
  }
  if(!PREFS.status){
    d.appendChild(Object.assign(el("div","placeholder"),{textContent:t("mgLoading")}));
    return d;
  }
  // 1. 全局开关：总闸即开即存（setupx Enable/Disable ⇔ OK_HOME/hooks-disabled 标志文件）
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("gTitle")}));
    const r = el("div","prow"); r.style.margin = "0";
    const dsc = Object.assign(el("span","pdesc"),{textContent:t("gDesc")}); dsc.style.margin = "0";
    r.appendChild(dsc);
    const right = el("span");
    right.style.cssText = "margin-left:auto;display:flex;align-items:center;gap:10px;flex:none";
    if(prefsFb.g) right.appendChild(Object.assign(el("span","fb2"),
      {textContent:PREFS.status.disabled?t("gOffFb"):t("gOnFb")}));
    if(prefsErr.g) right.appendChild(Object.assign(el("span","fb2 err"),{textContent:prefsErr.g}));
    const on = !PREFS.status.disabled;
    right.appendChild(pswitch(on, ()=>{
      const want = !on;
      prefsErr.g = "";
      api("/api/toggle", { method:"POST", body:{ on:want } }).then(()=>{
        PREFS.status.disabled = !want;
        flashPrefs("g");
      }).catch(err=>{ prefsErr.g = err.message; render(); });
    }));
    r.appendChild(right);
    c.appendChild(r);
    d.appendChild(c);
  }
  // 2. 语义检索（embedding）：摘要行 + 管理配置弹窗；增删改/使用中/模型目录全在弹窗内，确定即生效
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("eTitle")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("eDesc")}));
    const sumBody = el("span");
    if(PREFS.errs.emb){
      sumBody.appendChild(Object.assign(el("span","fb2 err"),{textContent:PREFS.errs.emb}));
    } else if(PREFS.emb){
      const cur = (PREFS.emb.profiles||[]).find(p=>p.name===PREFS.emb.active);
      if(cur){
        const detail = cur.type==="builtin" ? builtinLabel(cur.model)
                     : (cur.model||"")+(cur.base_url?" @ "+cur.base_url:"");
        sumBody.innerHTML = '<span class="badge-type t-'+(cur.type==="builtin"?"reference":cur.type==="ollama"?"note":"rule")+'">'
          + esc(cur.type==="builtin"?t("tagBuiltin"):cur.type==="ollama"?t("tagOllama"):t("tagCustom")) + '</span>'
          + ' <b>'+esc(cur.name)+'</b> <span class="mono">'+esc(detail)+'</span>';
      } else {
        sumBody.innerHTML = '<span class="muted">'+t("eNone")+'</span>';
      }
    }
    const mg = el("button","btn"); mg.textContent = t("eManage");
    mg.disabled = !PREFS.emb;
    mg.onclick = openEmbModal;
    c.appendChild(sumRow(t("eActive"), sumBody, mg));
    if(prefsFb.e) c.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
    d.appendChild(c);
  }
  // 3. 模型配置（LLM）：与 embedding 同构——摘要行 + 管理配置弹窗，确定即生效
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("lTitle")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("lDesc")}));
    const sumBody = el("span");
    if(PREFS.errs.llm){
      sumBody.appendChild(Object.assign(el("span","fb2 err"),{textContent:PREFS.errs.llm}));
    } else if(PREFS.llm){
      const cur = (PREFS.llm.profiles||[]).find(p=>p.name===PREFS.llm.active);
      if(cur){
        sumBody.innerHTML = '<span class="badge-type t-'+(cur.kind==="anthropic"?"pitfall":"rule")+'">'
          + esc(cur.kind==="anthropic"?t("tagAnthropic"):t("tagOpenai")) + '</span>'
          + ' <b>'+esc(cur.name)+'</b> <span class="mono">'+esc((cur.model||"")+(cur.base_url?" @ "+cur.base_url:""))+'</span>';
      } else {
        sumBody.innerHTML = '<span class="muted">'+t("lNone")+'</span>';
      }
    }
    const mg = el("button","btn"); mg.textContent = t("eManage");
    mg.disabled = !PREFS.llm;
    mg.onclick = openLlmModal;
    c.appendChild(sumRow(t("eActive"), sumBody, mg));
    if(prefsFb.l) c.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
    d.appendChild(c);
  }
  // 4. Hook 超时（全局；独立写端点，不重装 hooks）
  {
    const b = el("div");
    const orig = PREFS.status.hooksTimeout || 10;
    const row = prow(t("hSec"), pnumLive(PREFS_D.hooksTimeout,1,60,v=>{
      PREFS_D.hooksTimeout=v; pDirtyLive("h", v!==orig);
    }));
    b.appendChild(row);
    const card = pcard("h", t("hTitle"), t("hDesc"), b, row);
    wireSave(card, "h", ()=>{
      const v = PREFS_D.hooksTimeout;
      prefsErr.h = "";
      api("/api/hooks/timeout", { method:"POST", body:{ timeout_sec:v } }).then(()=>{
        PREFS.status.hooksTimeout = v; pSave("h");
      }).catch(err=>{ prefsErr.h = err.message; render(); });
    });
    d.appendChild(card);
  }
  // 5. 跨轮注入冷却（全局，写全局 config.toml）
  if(PREFS.errs.retr){
    d.appendChild(prefsNoteCard(t("cTitle"), t("cDesc"), t("xLoadFail")+PREFS.errs.retr, true));
  } else {
    const b = el("div");
    const orig = PREFS.retr ? PREFS.retr.dedup_turns : 0;
    const row = prow(t("cTurns"), pnumLive(PREFS_D.dedupTurns,0,99,v=>{
      PREFS_D.dedupTurns=v; pDirtyLive("c", v!==orig);
    }));
    b.appendChild(row);
    const card = pcard("c", t("cTitle"), t("cDesc"), b, row);
    wireSave(card, "c", ()=>{
      const v = PREFS_D.dedupTurns;
      prefsErr.c = "";
      api("/api/retrieve", { method:"POST", body:{ dedup_turns:v } }).then(()=>{
        PREFS.retr.dedup_turns = v; pSave("c");
      }).catch(err=>{ prefsErr.c = err.message; render(); });
    });
    d.appendChild(card);
  }
  // 6. 经验沉淀（全局）：保存按钮收进「轮次间隔」行右端；模式+间隔合并判脏
  if(PREFS.errs.cap){
    d.appendChild(prefsNoteCard(t("capTitle"), t("capDesc"), t("xLoadFail")+PREFS.errs.cap, true));
  } else {
    const b = el("div");
    const origM = PREFS.cap.mode, origI = PREFS.cap.turn_interval;
    const chk = ()=>pDirtyLive("cap", PREFS_D.capMode!==origM || PREFS_D.capInterval!==origI);
    const modeRow = el("div","prow");
    modeRow.appendChild(Object.assign(el("span","k"),{textContent:t("capMode")}));
    [["propose",t("capPropose")],["auto",t("capAuto")]].forEach(([v,label])=>{
      const lab = el("label","prow"); lab.style.margin="0";
      const rd = el("input"); rd.type="radio"; rd.name="capmode"; rd.className="radio";
      rd.checked = PREFS_D.capMode===v;
      rd.onchange = ()=>{ PREFS_D.capMode=v; chk(); };
      lab.appendChild(rd); lab.appendChild(document.createTextNode(label));
      modeRow.appendChild(lab);
    });
    b.appendChild(modeRow);
    const iRow = prow(t("capInterval"), pnumLive(PREFS_D.capInterval,1,100,v=>{
      PREFS_D.capInterval=v; chk();
    }));
    b.appendChild(iRow);
    const card = pcard("cap", t("capTitle"), t("capDesc"), b, iRow);
    wireSave(card, "cap", ()=>{
      prefsErr.cap = "";
      api("/api/capture", { method:"POST", body:{
        mode:PREFS_D.capMode, turn_interval:PREFS_D.capInterval } }).then(r=>{
        // 后端 turn_interval=0 语义为"保持不变"（api.go）：草稿回写响应真值，显示不与服务端背离
        PREFS.cap.mode = r.mode; PREFS.cap.turn_interval = r.turn_interval;
        PREFS_D.capMode = r.mode; PREFS_D.capInterval = r.turn_interval; pSave("cap");
      }).catch(err=>{ prefsErr.cap = err.message; render(); });
    });
    d.appendChild(card);
  }
  // 7. 泛化门控（全局）：单行布局（开关即开即存，短语表弹窗内编辑、确定即生效），无保存按钮
  if(PREFS.errs.gate){
    d.appendChild(prefsNoteCard(t("gtTitle"), t("gtDesc"), t("xLoadFail")+PREFS.errs.gate, true));
  } else {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("gtTitle")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("gtDesc")}));
    const r = el("div","prow"); r.style.margin = "0";
    r.appendChild(Object.assign(el("span","k"),{textContent:t("gtOn")}));
    const g = PREFS.gate || { enabled:false, builtin:[], extra:[] };
    r.appendChild(pswitch(!!g.enabled, ()=>{
      const want = !g.enabled;
      prefsErr.gt = "";
      api("/api/gate", { method:"POST", body:{ enabled:want } }).then(ng=>{
        PREFS.gate = ng; flashPrefs("gt");
      }).catch(err=>{ prefsErr.gt = err.message; render(); });
    }));
    r.appendChild(Object.assign(el("span","muted"),{textContent:
      t("gtStatus").replace("{b}",(g.builtin||[]).length).replace("{n}",(g.extra||[]).length)}));
    const right = el("span");
    right.style.cssText = "margin-left:auto;display:flex;align-items:center;gap:10px;flex:none";
    if(prefsFb.gt) right.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
    if(prefsErr.gt) right.appendChild(Object.assign(el("span","fb2 err"),{textContent:prefsErr.gt}));
    const mg = el("button","btn"); mg.textContent = t("gtManage");
    mg.onclick = ()=>{ gateDraft = (g.extra||[]).slice(); gateErr = ""; gateModal = true; render(); };
    right.appendChild(mg);
    r.appendChild(right);
    c.appendChild(r);
    d.appendChild(c);
  }
  // 8. 规则配置（全局）：保存按钮收进「添加规则」行右端；后端 400 信息经 prefsErr 展示
  if(PREFS.errs.rules){
    d.appendChild(prefsNoteCard(t("rTitle"), t("rDesc"), t("xLoadFail")+PREFS.errs.rules, true));
  } else {
    const b = el("div");
    const tb = el("table","rules");
    tb.innerHTML = "<thead><tr><th style='width:110px'>"+t("rType")+"</th><th>"+t("rGlobs")+"</th><th>"+t("rCl")+"</th><th>"+t("rMsg")+"</th><th style='width:40px'></th></tr></thead>";
    const tbBody = el("tbody");
    PREFS_D.rules.forEach((rule,idx)=>{
      const tr = el("tr");
      const td0 = el("td");
      const sel = el("select","pselect");
      ["changelog"].forEach(o=>{ const op=el("option"); op.value=o; op.textContent=o; sel.appendChild(op); });
      sel.value = rule.type;
      sel.onchange = ()=>{ rule.type=sel.value; pDirtyLive("r", rulesDirty()); };
      td0.appendChild(sel); tr.appendChild(td0);
      [["globs"],["cl"],["msg"]].forEach(([f])=>{
        const td = el("td");
        td.appendChild(ptext(rule[f], v=>{ rule[f]=v; pDirtyLive("r", rulesDirty()); }));
        tr.appendChild(td);
      });
      const tdX = el("td");
      const del = el("button","btn"); del.textContent = "×"; del.style.padding="2px 10px";
      del.onclick = ()=>{ PREFS_D.rules.splice(idx,1); prefsDirty.r = true; render(); };
      tdX.appendChild(del); tr.appendChild(tdX);
      tbBody.appendChild(tr);
    });
    tb.appendChild(tbBody);
    b.appendChild(tb);
    const addRow = el("div");
    addRow.style.cssText = "display:flex;align-items:center;margin-top:8px";
    const add = el("button","btn"); add.textContent = t("rAdd");
    add.onclick = ()=>{ PREFS_D.rules.push({type:"changelog",globs:"",cl:"",msg:""}); prefsDirty.r = true; render(); };
    addRow.appendChild(add);
    b.appendChild(addRow);
    const card = pcard("r", t("rTitle"), t("rDesc"), b, addRow);
    wireSave(card, "r", saveRules);
    d.appendChild(card);
  }

  if(gateModal) d.appendChild(renderGateModal());
  if(embModal) d.appendChild(renderEmbModal());
  if(llmModal) d.appendChild(renderLlmModal());
  return d;
}

/* ---------- 门控短语表弹窗：内置只读列表 + 自定义草稿（增删）+ 确定全量替换（POST /api/gate {extra}） ---------- */
function renderGateModal(){
  const g = PREFS.gate || { builtin:[], extra:[] };
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:t("gtManage")}));
  m.appendChild(Object.assign(el("div","sec-label"),{textContent:t("gtBuiltin")}));
  (g.builtin||[]).forEach(p=>{
    const row = el("div","prof");
    row.style.cursor = "default";
    row.appendChild(Object.assign(el("span","info"),{textContent:p}));
    m.appendChild(row);
  });
  m.appendChild(Object.assign(el("div","sec-label"),{textContent:t("gtCustom")}));
  gateDraft.forEach((p,idx)=>{
    const row = el("div","prof");
    row.style.cursor = "default";
    row.appendChild(Object.assign(el("span","info"),{textContent:p}));
    const del = el("button","btn btn-danger"); del.textContent = t("eDel");
    del.style.cssText = "margin-left:auto;padding:2px 10px;flex:none";
    del.onclick = ()=>{ gateDraft.splice(idx,1); render(); };
    row.appendChild(del);
    m.appendChild(row);
  });
  const addRow = el("div","ph-row");
  const inp = ptext("", ()=>{});
  inp.placeholder = t("gtPh");
  const add = el("button","btn"); add.textContent=t("gtAdd"); add.style.flex="none";
  const doAdd = ()=>{ const v = inp.value.trim(); if(v){ gateDraft.push(v); render(); } };
  add.onclick = doAdd;
  inp.onkeydown = e=>{ if(e.key==="Enter") doAdd(); };
  addRow.appendChild(inp); addRow.appendChild(add);
  m.appendChild(addRow);
  if(gateErr) m.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:gateErr}));
  const foot = el("div","mfoot");
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = ()=>{ gateModal=false; render(); };
  const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
  ok.onclick = ()=>{
    gateErr = "";
    api("/api/gate", { method:"POST", body:{ extra:gateDraft } }).then(ng=>{
      PREFS.gate = ng;   // 响应含 enabled/builtin/extra 全量（清洗去重后的服务端形）
      gateModal = false;
      flashPrefs("gt");
    }).catch(err=>{ gateErr = err.message; render(); });
  };
  foot.appendChild(cancel); foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask){ gateModal=false; render(); } };
  return mask;
}

/* ---------- embedding 服务管理弹窗：列表（设为使用中/编辑/删除）+ 按类型表单（字段对齐
   /api/setup/embedding/profile：builtin=模型下拉+下载源+下载状态条，ollama=地址+模型，
   openai=base_url+模型+key；类型保存后锁定，仅新增可改）+ 全局段（模型目录，空=恢复默认）。
   草稿制：弹窗内改动先落 embDraft，确定才按 diff 批量调端点生效；取消整盘放弃。 ---------- */
function openEmbModal(){
  const e = PREFS.emb || {};
  embDraft = { active:e.active||"",
    dir:e.models_dir||"",
    profiles:(e.profiles||[]).map(p=>({ name:p.name, type:p.type, base:p.base_url||"",
      model:p.model||"", key:"", has_key:!!p.has_key, mirror:p.mirror||"" })) };
  embEdit = -1; embForm = null; embErr = ""; embModal = true; render();
  embPoll();   // 打开时若已有下载任务（此前发起）即续轮
}
function closeEmbModal(){
  embModal = false; embDraft = null; embEdit = -1; embForm = null;
  if(embPollT){ clearTimeout(embPollT); embPollT = null; }
  render();
}
// 确定即生效：upserts → deletes → 模型目录 → 使用中切换；任一步失败中止并展示后端错误
async function embApply(){
  const draft = embDraft, old = PREFS.emb || {};
  for(const p of draft.profiles){
    await api("/api/setup/embedding/profile", { method:"POST", body:{
      name:p.name.trim(), type:p.type, base_url:String(p.base||"").trim(), model:String(p.model||"").trim(),
      api_key:p.key||"", mirror:p.mirror||"" } });   // api_key 空 = 同名保留旧 key（后端收口）
  }
  const keep = {}; draft.profiles.forEach(p=>{ keep[p.name]=true; });
  let serverActive = old.active || "";
  for(const sp of old.profiles||[]){
    if(!keep[sp.name]){
      await api("/api/setup/embedding/profile", { method:"DELETE", body:{ name:sp.name } });
      if(serverActive===sp.name) serverActive = "";   // 后端删使用中项自动置空
    }
  }
  const dir = String(draft.dir||"").trim();
  if(dir !== String(old.models_dir||"").trim()){
    await api("/api/setup/embedding/models-dir", { method:"POST", body:{ path:dir } });
  }
  if(draft.active !== serverActive){
    await api("/api/setup/embedding/active", { method:"POST", body:{ name:draft.active } });
  }
}
function fmtMB(n){ return (n/1048576).toFixed(0)+" MB"; }
// 下载状态条（旧 embRenderBiStatus 平移）：进行中=进度条+取消；已下载=就绪；未下载=下载按钮+上次错误
function paintEmbDl(){
  if(!embDlEl || !embForm || (embForm.type||"openai")!=="builtin") return;
  const e = PREFS.emb || {};
  const id = embForm.model || ((e.builtin_models||[])[0] && e.builtin_models[0].id) || "";
  const bm = (e.builtin_models||[]).find(x=>x.id===id);
  const box = embDlEl;
  box.innerHTML = "";
  if(!bm){ box.textContent = "…"; return; }
  const dl = e.download || {};
  if(dl.state==="downloading" && dl.model_id===id){
    const pct = dl.total ? Math.floor(dl.done*100/dl.total) : 0;
    box.appendChild(document.createTextNode(
      t("eDlDoing").replace("{done}",fmtMB(dl.done||0)).replace("{total}",fmtMB(dl.total||0))));
    const bar = el("div","emb-prog");
    const fill = el("i"); fill.style.width = pct+"%";
    bar.appendChild(fill); box.appendChild(bar);
    box.appendChild(Object.assign(el("span"),{textContent:pct+"%"}));
    const cancel = el("button","btn"); cancel.textContent = t("fCancel"); cancel.style.marginLeft = "8px";
    cancel.onclick = ()=>{
      api("/api/setup/embedding/download/cancel", { method:"POST", body:{ model_id:id } })
        .then(()=>embPoll());
    };
    box.appendChild(cancel);
    return;
  }
  if(bm.downloaded){
    box.appendChild(Object.assign(el("span","fb2"),{textContent:t("eDlReady").replace("{dim}",bm.dim)}));
    return;
  }
  if(!e.runtime_available){
    box.appendChild(Object.assign(el("span","fb2 err"),{textContent:t("eDlNoRt")}));
    return;
  }
  const btn = el("button","btn"); btn.textContent = t("eDlBtn").replace("{size}",fmtMB(bm.size));
  btn.onclick = ()=>{
    api("/api/setup/embedding/download", { method:"POST", body:{ model_id:id, mirror:embForm.mirror||"hf-mirror" } })
      .then(()=>embPoll())
      .catch(err=>{ embErr = err.message; render(); });
  };
  box.appendChild(btn);
  if(dl.state==="error" && dl.model_id===id){
    box.appendChild(Object.assign(el("span","fb2 err"),{textContent:" "+t("eDlErr")+(dl.error||"")}));
  }
}
// 下载进度轮询（旧 embRefresh(true)+embSchedulePoll 语义）：只重画状态条不整页重渲；
// state 仍为 downloading 时 1s 后续轮。失败静默（用户动作会再触发）
function embPoll(){
  if(embPollT){ clearTimeout(embPollT); embPollT = null; }
  if(!embModal) return;
  api("/api/setup/embedding"+(PREFS.project?"?project="+encodeURIComponent(PREFS.project):""))
    .then(d=>{
      PREFS.emb = d; PREFS.errs.emb = "";
      paintEmbDl();
      if(embModal && d.download && d.download.state==="downloading"){
        embPollT = setTimeout(embPoll, 1000);
      }
    }).catch(()=>{});
}
function renderEmbModal(){
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:t("eTitle")+" · "+t("eProfiles")}));
  // 使用中模型与当前项目索引模型不符警示（旧 GUI 语义，GET ?project= 的 index_model/active_identity）
  const e0 = PREFS.emb || {};
  if(e0.active_identity && e0.index_model && e0.active_identity!==e0.index_model){
    m.appendChild(Object.assign(el("div","pdesc fb2 err"),
      {textContent:t("eIdxWarn").replace("{a}",e0.active_identity).replace("{i}",e0.index_model)}));
  }

  embDraft.profiles.forEach((p,idx)=>{
    const row = el("div","prof"+(embDraft.active===p.name?" sel":""));
    row.style.cursor = "default";
    const info = el("span","info");
    const detail = p.type==="builtin" ? builtinLabel(p.model) : (p.model||"")+(p.base?" @ "+p.base:"");
    info.innerHTML = '<span class="badge-type t-'+(p.type==="builtin"?"reference":p.type==="ollama"?"note":"rule")+'">'
      + esc(p.type==="builtin"?t("tagBuiltin"):p.type==="ollama"?t("tagOllama"):t("tagCustom")) + '</span>'
      + ' <b>'+esc(p.name)+'</b> <span class="mono">'+esc(detail)+'</span>';
    row.appendChild(info);
    const acts = el("span");
    acts.style.cssText = "margin-left:auto;display:flex;gap:6px;flex:none";
    if(embDraft.active===p.name){
      acts.appendChild(Object.assign(el("span","chip on"),{textContent:t("eActive")}));
    } else {
      const sa = el("button","btn"); sa.textContent = t("eSetActive"); sa.style.padding="2px 10px";
      sa.onclick = ()=>{ embDraft.active = p.name; render(); };
      acts.appendChild(sa);
    }
    const ed = el("button","btn"); ed.textContent = t("eEdit"); ed.style.padding="2px 10px";
    ed.onclick = ()=>{ embEdit = idx; embForm = JSON.parse(JSON.stringify(p)); render(); };
    acts.appendChild(ed);
    const del = el("button","btn btn-danger"); del.textContent = t("eDel"); del.style.padding="2px 10px";
    del.onclick = ()=>{
      if(embDraft.active===p.name) embDraft.active = "";
      embDraft.profiles.splice(idx,1);
      if(embEdit===idx){ embEdit=-1; embForm=null; }
      render();
    };
    acts.appendChild(del);
    row.appendChild(acts);
    m.appendChild(row);
  });

  if(embForm){
    m.appendChild(Object.assign(el("div","sec-label"),
      {textContent:embEdit===-2?t("eAddTitle"):t("eEditTitle")}));
    m.appendChild(prow(t("fName"), ptext(embForm.name||"", v=>{ embForm.name=v; }, "300px")));
    // 类型：保存后锁定，仅新增可改（沿用旧 GUI 语义）
    const typeRow = el("div","prow");
    typeRow.appendChild(Object.assign(el("span","k"),{textContent:t("fType")}));
    const sel = el("select","pselect");
    [["builtin",t("typeBuiltin")],["ollama",t("typeOllama")],["openai",t("typeOpenai")]].forEach(([v,label])=>{
      const op=el("option"); op.value=v; op.textContent=label; sel.appendChild(op);
    });
    sel.value = embForm.type||"openai";
    sel.disabled = embEdit >= 0;
    sel.onchange = ()=>{ embForm.type = sel.value; render(); };
    typeRow.appendChild(sel);
    m.appendChild(typeRow);
    // 按类型切换字段组
    const ty = embForm.type||"openai";
    if(ty==="builtin"){
      if(!embForm.mirror) embForm.mirror = "hf-mirror";   // 显示默认同时落草稿（未触碰也按默认提交）
      const mRow = el("div","prow");
      mRow.appendChild(Object.assign(el("span","k"),{textContent:t("fModel")}));
      const mSel = el("select","pselect"); mSel.style.maxWidth="420px";
      (e0.builtin_models||[]).forEach(bm=>{
        const op=el("option"); op.value=bm.id;
        op.textContent = bm.label + (bm.downloaded?t("downloaded"):"");
        mSel.appendChild(op);
      });
      if(e0.builtin_models && e0.builtin_models.length) mSel.value = embForm.model||e0.builtin_models[0].id;
      embForm.model = mSel.value;
      mSel.onchange = ()=>{ embForm.model = mSel.value; paintEmbDl(); };
      mRow.appendChild(mSel);
      m.appendChild(mRow);
      const miRow = el("div","prow");
      miRow.appendChild(Object.assign(el("span","k"),{textContent:t("fMirror")}));
      const miSel = el("select","pselect");
      [["hf-mirror",t("mirrorHf")],["huggingface",t("mirrorOfficial")]].forEach(([v,label])=>{
        const op=el("option"); op.value=v; op.textContent=label; miSel.appendChild(op);
      });
      miSel.value = embForm.mirror||"hf-mirror";
      miSel.onchange = ()=>{ embForm.mirror = miSel.value; };
      miRow.appendChild(miSel);
      m.appendChild(miRow);
      // 下载状态条（paintEmbDl 原位重画，轮询不动表单其余部分）
      const stRow = el("div","prow");
      stRow.appendChild(el("span","k"));
      embDlEl = el("span");
      embDlEl.style.cssText = "flex:1;min-width:0;font-size:12.5px;color:var(--muted)";
      stRow.appendChild(embDlEl);
      m.appendChild(stRow);
      paintEmbDl();
    } else if(ty==="ollama"){
      if(!embForm.base) embForm.base = "http://localhost:11434";   // 同上：显示默认落草稿
      m.appendChild(prow(t("fOlUrl"), ptext(embForm.base||"http://localhost:11434", v=>{ embForm.base=v; }, "300px")));
      m.appendChild(prow(t("fModel"), ptext(embForm.model||"", v=>{ embForm.model=v; }, "300px")));
    } else {
      m.appendChild(prow(t("fBase"), ptext(embForm.base||"", v=>{ embForm.base=v; }, "300px")));
      m.appendChild(prow(t("fModel"), ptext(embForm.model||"", v=>{ embForm.model=v; }, "300px")));
      const keyIn = ptext(embForm.key||"", v=>{ embForm.key=v; }, "300px");
      keyIn.placeholder = embEdit>=0 ? t("keySaved") : "api_key";
      m.appendChild(prow(t("fKey"), keyIn));
    }
    const frow = el("div","prow");
    frow.appendChild(el("span","k"));
    const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
    ok.onclick = ()=>{
      const nm = (embForm.name||"").trim();
      if(!nm) return;
      embForm.name = nm;
      if(embEdit===-2){
        embDraft.profiles.push(JSON.parse(JSON.stringify(embForm)));
      } else {
        const oldName = embDraft.profiles[embEdit].name;
        embDraft.profiles[embEdit] = JSON.parse(JSON.stringify(embForm));
        if(embDraft.active===oldName && oldName!==nm) embDraft.active = nm;   // 改名联动使用中引用
      }
      embEdit=-1; embForm=null; render();
    };
    const cancel = el("button","btn"); cancel.textContent = t("fCancel");
    cancel.onclick = ()=>{ embEdit=-1; embForm=null; render(); };
    frow.appendChild(ok); frow.appendChild(cancel);
    m.appendChild(frow);
  } else {
    const add = el("button","btn"); add.textContent = t("eAdd"); add.style.marginTop="8px";
    add.onclick = ()=>{ embEdit=-2; embForm={type:"builtin"}; render(); };
    m.appendChild(add);
  }

  // 全局段：内置模型目录（空=恢复默认；属于 [embedding] 全局，不属于单个 profile）
  m.appendChild(Object.assign(el("div","sec-label"),{textContent:t("eGlobal")}));
  const dirRow = el("div","prow");
  dirRow.appendChild(Object.assign(el("span","k"),{textContent:t("eDir")}));
  const dirIn = ptext(embDraft.dir, v=>{ embDraft.dir=v; }, "300px");
  dirIn.placeholder = e0.models_dir_default || "";
  dirRow.appendChild(dirIn);
  const openDir = el("button","btn"); openDir.textContent = t("eDirOpen");
  openDir.onclick = ()=>{
    api("/api/setup/embedding/open-models-dir", { method:"POST" })
      .catch(err=>{ embErr = err.message; render(); });
  };
  dirRow.appendChild(openDir);
  m.appendChild(dirRow);

  if(embErr) m.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:embErr}));
  const foot = el("div","mfoot");
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = closeEmbModal;
  const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
  ok.onclick = ()=>{
    embErr = ""; ok.disabled = true;
    embApply().then(()=>refreshEmb()).then(()=>{
      closeEmbModal(); flashPrefs("e");
    }).catch(err=>{
      embErr = err.message; ok.disabled = false;
      refreshEmb().catch(()=>{}).then(()=>render());   // 部分步骤可能已生效：同步服务端真值，弹窗保持
    });
  };
  foot.appendChild(cancel); foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask) closeEmbModal(); };
  return mask;
}

/* ---------- LLM 服务管理弹窗：与 embedding 弹窗同构。kind 两档（OpenAI/Anthropic 兼容），
   temperature/max_tokens 为高级参数（留空/0 = 不传）；编辑表单内带「测试连接」（真实调
   /api/llm/test，成功显示实测耗时，失败显示后端错误）。确定即生效。 ---------- */
function openLlmModal(){
  const l = PREFS.llm || {};
  llmDraft = { active:l.active||"",
    profiles:(l.profiles||[]).map(p=>({ name:p.name, kind:p.kind, base:p.base_url||"",
      model:p.model||"", key:"", temperature:p.temperature||"", maxTokens:p.max_tokens||0 })) };
  llmEdit = -1; llmForm = null; llmErr = ""; llmModal = true; render();
}
function closeLlmModal(){
  llmModal = false; llmDraft = null; llmEdit = -1; llmForm = null;
  render();
}
async function llmApply(){
  const draft = llmDraft, old = PREFS.llm || {};
  for(const p of draft.profiles){
    await api("/api/llm/profile", { method:"POST", body:{
      name:p.name.trim(), kind:p.kind, base_url:String(p.base||"").trim(), model:String(p.model||"").trim(),
      api_key:p.key||"", temperature:String(p.temperature||"").trim(), max_tokens:p.maxTokens||0,
      activate:false } });   // api_key 空/掩码 = 同名保留旧 key（后端收口）
  }
  const keep = {}; draft.profiles.forEach(p=>{ keep[p.name]=true; });
  for(const sp of old.profiles||[]){
    if(!keep[sp.name]){
      await api("/api/llm/delete", { method:"POST", body:{ name:sp.name } });   // 删使用中项后端自动置空
    }
  }
  let serverActive = old.active || "";
  if(serverActive && !keep[serverActive]) serverActive = "";
  if(draft.active !== serverActive){
    await api("/api/llm/active", { method:"POST", body:{ name:draft.active } });   // 空串 = 停用
  }
}
function renderLlmModal(){
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:t("lTitle")+" · "+t("eProfiles")}));

  llmDraft.profiles.forEach((p,idx)=>{
    const row = el("div","prof"+(llmDraft.active===p.name?" sel":""));
    row.style.cursor = "default";
    const info = el("span","info");
    info.innerHTML = '<span class="badge-type t-'+(p.kind==="anthropic"?"pitfall":"rule")+'">'
      + esc(p.kind==="anthropic"?t("tagAnthropic"):t("tagOpenai")) + '</span>'
      + ' <b>'+esc(p.name)+'</b> <span class="mono">'+esc(p.model||"")+ (p.base?" @ "+esc(p.base):"") +'</span>';
    row.appendChild(info);
    const acts = el("span");
    acts.style.cssText = "margin-left:auto;display:flex;gap:6px;flex:none";
    if(llmDraft.active===p.name){
      acts.appendChild(Object.assign(el("span","chip on"),{textContent:t("eActive")}));
    } else {
      const sa = el("button","btn"); sa.textContent = t("eSetActive"); sa.style.padding="2px 10px";
      sa.onclick = ()=>{ llmDraft.active = p.name; render(); };
      acts.appendChild(sa);
    }
    const ed = el("button","btn"); ed.textContent = t("eEdit"); ed.style.padding="2px 10px";
    ed.onclick = ()=>{ llmEdit = idx; llmForm = JSON.parse(JSON.stringify(p)); llmDraft._testMsg=null; render(); };
    acts.appendChild(ed);
    const del = el("button","btn btn-danger"); del.textContent = t("eDel"); del.style.padding="2px 10px";
    del.onclick = ()=>{
      if(llmDraft.active===p.name) llmDraft.active = "";
      llmDraft.profiles.splice(idx,1);
      if(llmEdit===idx){ llmEdit=-1; llmForm=null; }
      render();
    };
    acts.appendChild(del);
    row.appendChild(acts);
    m.appendChild(row);
  });

  if(llmForm){
    m.appendChild(Object.assign(el("div","sec-label"),
      {textContent:llmEdit===-2?t("eAddTitle"):t("eEditTitle")}));
    m.appendChild(prow(t("fName"), ptext(llmForm.name||"", v=>{ llmForm.name=v; }, "300px")));
    const kindRow = el("div","prow");
    kindRow.appendChild(Object.assign(el("span","k"),{textContent:t("fType")}));
    const sel = el("select","pselect");
    [["openai",t("kindOpenai")],["anthropic",t("kindAnthropic")]].forEach(([v,label])=>{
      const op=el("option"); op.value=v; op.textContent=label; sel.appendChild(op);
    });
    sel.value = llmForm.kind||"openai";
    sel.onchange = ()=>{ llmForm.kind = sel.value; };
    kindRow.appendChild(sel);
    m.appendChild(kindRow);
    m.appendChild(prow(t("fBase"), ptext(llmForm.base||"", v=>{ llmForm.base=v; }, "300px")));
    m.appendChild(prow(t("fModel"), ptext(llmForm.model||"", v=>{ llmForm.model=v; }, "300px")));
    const keyIn = ptext(llmForm.key||"", v=>{ llmForm.key=v; }, "300px");
    keyIn.placeholder = llmEdit>=0 ? t("keySaved") : "api_key";
    m.appendChild(prow(t("fKey"), keyIn));
    m.appendChild(prow(t("fTemp"), ptext(llmForm.temperature||"", v=>{ llmForm.temperature=v; }, "120px")));
    m.appendChild(prow(t("fMaxTokens"), pnum(llmForm.maxTokens||0,0,128000,v=>{ llmForm.maxTokens=v; })));
    const frow = el("div","prow");
    frow.appendChild(el("span","k"));
    const test = el("button","btn");
    test.textContent = llmDraft._testing ? t("lTesting") : t("lTest");
    test.disabled = !!llmDraft._testing;
    test.onclick = ()=>{
      llmDraft._testing = true; llmDraft._testMsg = null; render();
      const started = Date.now();
      api("/api/llm/test", { method:"POST", body:{
        name:(llmForm.name||"").trim(), kind:llmForm.kind||"openai",
        base_url:String(llmForm.base||"").trim(), model:String(llmForm.model||"").trim(),
        api_key:llmForm.key||"", temperature:String(llmForm.temperature||"").trim(),
        max_tokens:llmForm.maxTokens||0 } }).then(()=>{
        llmDraft._testing = false;
        llmDraft._testMsg = { txt:t("lTestOk").replace("{ms}", String(Date.now()-started)), err:false };
        render();
      }).catch(err=>{
        llmDraft._testing = false;
        llmDraft._testMsg = { txt:err.message, err:true };
        render();
      });
    };
    frow.appendChild(test);
    if(llmDraft._testMsg)
      frow.appendChild(Object.assign(el("span","fb2"+(llmDraft._testMsg.err?" err":"")),
        {textContent:llmDraft._testMsg.txt}));
    const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
    ok.onclick = ()=>{
      const nm = (llmForm.name||"").trim();
      if(!nm) return;
      llmForm.name = nm;
      if(llmEdit===-2){
        llmDraft.profiles.push(JSON.parse(JSON.stringify(llmForm)));
      } else {
        const oldName = llmDraft.profiles[llmEdit].name;
        llmDraft.profiles[llmEdit] = JSON.parse(JSON.stringify(llmForm));
        if(llmDraft.active===oldName && oldName!==nm) llmDraft.active = nm;
      }
      llmEdit=-1; llmForm=null; render();
    };
    const cancel = el("button","btn"); cancel.textContent = t("fCancel");
    cancel.onclick = ()=>{ llmEdit=-1; llmForm=null; render(); };
    frow.appendChild(ok); frow.appendChild(cancel);
    m.appendChild(frow);
  } else {
    const add = el("button","btn"); add.textContent = t("eAdd"); add.style.marginTop="8px";
    add.onclick = ()=>{ llmEdit=-2; llmForm={kind:"openai"}; llmDraft._testMsg=null; render(); };
    m.appendChild(add);
  }

  if(llmErr) m.appendChild(Object.assign(el("div","pdesc fb2 err"),{textContent:llmErr}));
  const foot = el("div","mfoot");
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = closeLlmModal;
  const ok = el("button","btn btn-primary"); ok.textContent = t("fOk");
  ok.onclick = ()=>{
    llmErr = ""; ok.disabled = true;
    llmApply().then(()=>refreshLlm()).then(()=>{
      closeLlmModal(); flashPrefs("l");
    }).catch(err=>{
      llmErr = err.message; ok.disabled = false;
      refreshLlm().catch(()=>{}).then(()=>render());
    });
  };
  foot.appendChild(cancel); foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask) closeLlmModal(); };
  return mask;
}

/* ================= 日志页 ================= */
/* 三来源（ok=CLI/hook 客户端、daemon=守护进程、sidecar=embed 边车）实时日志查看器，只读。
   2s 轮询 GET /api/logs?tail=400&sig=…：签名不变时后端回 {unchanged:true}，跳过重绘；
   有变化则 lines 全量替换，按 state.logSrc/logSem/logQ 过滤渲染。仅菜单在 logs 且自动
   刷新开启时轮询；离开页面不清状态（数据/签名/过滤/开关全保留），回来自动续轮。
   结构/样式/交互照抄原型日志页（prototype-manager-v2.html 1475-1570），mock 换真。 */
let LOG_LINES = [], logSig = "", logLoaded = false;
let logBodyEl = null, logMetaEl = null;
async function fetchLogs(){
  let d;
  try {
    d = await api("/api/logs?tail=400&sig="+encodeURIComponent(logSig));
  } catch(err){ return; }   // 轮询失败静默、下轮重试（401 由 api() 自动刷新取新 token）
  if(!d || d.unchanged) return;
  LOG_LINES = d.lines || [];
  logSig = d.sig || "";
  paintLogs();
}
function paintLogs(){
  if(!logBodyEl) return;
  const q = state.logQ.trim().toLowerCase();
  let count = 0, html = "";
  LOG_LINES.forEach(l=>{
    if(!state.logSrc[l.src]) return;
    if(state.logSem && !l.semantic) return;
    if(q && l.text.toLowerCase().indexOf(q)<0) return;
    count++;
    html += '<span class="ls ls-'+l.src+'">'+l.src+'</span>'
          + '<span class="sem'+(l.semantic?"":" off")+'">'+(l.semantic?"◆":"◇")+'</span> '
          + esc(l.text)+"\n";
  });
  logBodyEl.innerHTML = count ? html : '<span class="empty">'+t("lgEmpty")+'</span>';
  if(state.logStick) logBodyEl.scrollTop = logBodyEl.scrollHeight;
  if(logMetaEl) logMetaEl.textContent =
    t("lgMeta").replace("{n}",LOG_LINES.length).replace("{m}",count);
}

function renderLogs(){
  const d = el("div","logs");
  const bar = el("div","logbar");
  [["ok","on-ok"],["daemon","on-daemon"],["sidecar","on-sidecar"]].forEach(([src,cls])=>{
    const c = el("span","logchip"+(state.logSrc[src]?" "+cls:""));
    c.textContent = src;
    c.onclick = ()=>{ state.logSrc[src]=!state.logSrc[src]; c.classList.toggle(cls, state.logSrc[src]); paintLogs(); };
    bar.appendChild(c);
  });
  const sem = el("span","logchip"+(state.logSem?" on-sem":""));
  sem.textContent = t("lgSemantic");
  sem.onclick = ()=>{ state.logSem=!state.logSem; sem.classList.toggle("on-sem", state.logSem); paintLogs(); };
  bar.appendChild(sem);
  const f = el("input","logfilter");
  f.placeholder = t("lgFilter"); f.value = state.logQ;
  f.oninput = ()=>{ state.logQ = f.value; paintLogs(); };
  bar.appendChild(f);
  const meta = el("span","logmeta");
  logMetaEl = el("span");
  meta.appendChild(logMetaEl);
  meta.appendChild(Object.assign(el("span"),{textContent:t("lgAuto")}));
  const sw = pswitch(state.logAuto, ()=>{
    state.logAuto = !state.logAuto; sw.classList.toggle("on", state.logAuto);
  });
  meta.appendChild(sw);
  bar.appendChild(meta);
  d.appendChild(bar);
  logBodyEl = el("div","logbody");
  logBodyEl.addEventListener("scroll", ()=>{
    state.logStick = logBodyEl.scrollHeight - logBodyEl.scrollTop - logBodyEl.clientHeight < 40;
  });
  d.appendChild(logBodyEl);
  paintLogs();
  if(!logLoaded){ logLoaded = true; fetchLogs(); }   // 首次进入立即拉取，不等首个轮询周期
  return d;
}

/* 2s 轮询：仅日志页可见且自动刷新开启时请求（unchanged 响应在 fetchLogs 内跳过重绘） */
setInterval(()=>{
  if(state.menu!=="logs" || !state.logAuto) return;
  fetchLogs();
}, 2000);

/* ================= 其他页 ================= */
/* 六卡照抄原型 renderMisc（prototype-manager-v2.html:1592-1688），mock 换真：
   导出 = GET /api/export?project=（带 token 裸 fetch → blob → a[download]，文件名取
   Content-Disposition，回退旧命名）；导入 = POST /api/import multipart——FormData file
   字段、只带 X-Ok-Token 头由浏览器自动补 boundary（旧 app.js:1487-1510 同款；api() helper
   强制 JSON Content-Type 不能用）；更新日志 = /api/changelog 的 all 降序 + <hr> 分隔
   （旧 openChangelogModal 语义，seen 只在升级首弹关闭时 POST——本页为常驻入口不标记，
   首弹由 Task 8 实现）；使用帮助 = /help.md 静态拉取；删除卡 = 三齐备解锁确认弹窗 →
   DELETE /api/project?project=（后端只认 query 参数，api.go:588 核实）；关于 = /api/status
   的 app_version/home/projects 计数。 */
let MISC = null;        // {projects, status} 缓存；loadMisc 惰性加载，refreshMisc 原位刷新
let miscDoc = null;     // 文档弹窗：{title, entries:[{log}]}（更新日志）或 {title, md}（帮助）
let delTarget = null;   // 删除确认弹窗目标项目名

function loadMisc(){
  if(MISC) return;
  MISC = { projects:[], status:null };
  refreshMisc();
}
function refreshMisc(){
  Promise.all([api("/api/projects"), api("/api/status")]).then(([ps, st])=>{
    MISC = { projects: ps || [], status: st || null };
  }).catch(err=>{
    MISC = { projects: [], status: null, loadErr: err.message };
  }).then(()=>{ if(state.menu==="misc" && !miscDoc && !delTarget) render(); });
}

function miscFbSpan(fb){
  return Object.assign(el("span","fb2"+(fb.err?" err":"")),{textContent:fb.txt});
}
// 卡片右端反馈：成功闪 1.5s；sticky（导入结果行/错误）驻留至下次操作覆盖
function flashMiscFb(key, txt, opts){
  opts = opts || {};
  state.miscFb = { key:key, txt:txt, err:!!opts.err };
  render();
  if(!opts.sticky) setTimeout(()=>{
    if(state.miscFb && state.miscFb.key===key){ state.miscFb = null; render(); }
  }, 1500);
}
// 导出/删除共用的 zip 下载（fetch 响应 → a[download]；Content-Disposition 文件名优先）
async function downloadZip(project){
  const res = await fetch("/api/export?project="+encodeURIComponent(project), {
    headers: { "X-Ok-Token": window.OK_TOKEN || "" },
  });
  if(!res.ok){
    const e = await res.json().catch(()=>({}));
    const err = new Error(e.error || ("HTTP "+res.status));
    err.status = res.status;
    throw err;
  }
  const blob = await res.blob();
  const cd = res.headers.get("Content-Disposition") || "";
  const m = cd.match(/filename="?([^";]+)"?/);
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = m ? m[1] : "openknowledge-backup-"+project+".zip";
  a.click();
  URL.revokeObjectURL(a.href);
}

function renderMisc(){
  loadMisc();
  const d = el("div","misc");
  const projects = MISC.projects, st = MISC.status;
  const fb = state.miscFb;
  const rightWrap = ()=>{
    const w = el("span");
    w.style.cssText = "margin-left:auto;display:flex;align-items:center;gap:10px;flex:none";
    return w;
  };

  if(MISC.loadErr){
    d.appendChild(Object.assign(el("div","pdesc fb2 err"),
      {textContent:t("xLoadFail")+MISC.loadErr}));
  }

  // 1. 数据导出：项目下拉（含全部）+ 导出
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("xExport")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("xExportDesc")}));
    const r = el("div","prow"); r.style.margin = "0";
    r.appendChild(Object.assign(el("span","k"),{textContent:t("xProject")}));
    const sel = el("select","pselect");
    const all = el("option"); all.value = "all"; all.textContent = t("xAllProjects");
    sel.appendChild(all);
    projects.forEach(p=>{
      const op = el("option"); op.value = p.name; op.textContent = p.name; sel.appendChild(op);
    });
    r.appendChild(sel);
    const right = rightWrap();
    if(fb && fb.key==="export") right.appendChild(miscFbSpan(fb));
    const btn = el("button","btn"); btn.textContent = t("xDoExport");
    btn.onclick = async ()=>{
      try {
        await downloadZip(sel.value || "all");   // 无项目时下拉只剩"全部"，与后端 all 语义对齐
        flashMiscFb("export", t("xExported"));
      } catch(err){
        flashMiscFb("export", t("xExportFail")+err.message, { err:true, sticky:true });
      }
    };
    right.appendChild(btn); r.appendChild(right); c.appendChild(r);
    d.appendChild(c);
  }
  // 2. 数据导入：选 zip 文件 + 导入（未选文件时禁用）
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("xImport")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("xImportDesc")}));
    const r = el("div","prow"); r.style.margin = "0";
    r.appendChild(Object.assign(el("span","k"),{textContent:t("xFile")}));
    const fi = el("input"); fi.type = "file"; fi.accept = ".zip";
    fi.style.cssText = "font-size:12px;color:var(--muted)";
    r.appendChild(fi);
    const right = rightWrap();
    if(fb && fb.key==="import") right.appendChild(miscFbSpan(fb));
    const btn = el("button","btn"); btn.textContent = t("xDoImport"); btn.disabled = true;
    fi.onchange = ()=>{ btn.disabled = !fi.files.length; };
    btn.onclick = async ()=>{
      const f = fi.files[0];
      if(!f){ flashMiscFb("import", t("xImportPick"), { err:true }); return; }
      const fd = new FormData();
      fd.append("file", f);
      try {
        const res = await fetch("/api/import", {
          method:"POST", headers:{ "X-Ok-Token": window.OK_TOKEN || "" }, body:fd,
        });
        const data = await res.json().catch(()=>({}));
        if(!res.ok){
          flashMiscFb("import", t("xImportFail")+(data.error || res.status), { err:true, sticky:true });
          return;
        }
        flashMiscFb("import", t("xImported")
          .replace("{n}", data.imported||0).replace("{s}", data.skipped||0)
          .replace("{ps}", (data.projects||[]).join("、")), { sticky:true });
        refreshMisc();   // 导入可能恢复出新项目：项目下拉/关于计数联动刷新
      } catch(err){
        flashMiscFb("import", t("xImportFail")+err.message, { err:true, sticky:true });
      }
    };
    right.appendChild(btn); r.appendChild(right); c.appendChild(r);
    d.appendChild(c);
  }
  // 3/4. 更新日志 / 使用帮助：详情行右端「查看」→ markdown 文档弹窗（共用）
  {
    const chlog = el("button","btn"); chlog.textContent = t("xView");
    chlog.onclick = async ()=>{
      try {
        const c = await api("/api/changelog");
        miscDoc = { title:t("xChlog"), entries:(c && c.all) || [] };
      } catch(err){
        miscDoc = { title:t("xChlog"), md:"**"+t("xLoadFail")+"** "+err.message };
      }
      render();
    };
    const help = el("button","btn"); help.textContent = t("xView");
    help.onclick = async ()=>{
      let md;
      try {
        const res = await fetch("/help.md");
        if(!res.ok) throw new Error("HTTP "+res.status);
        md = await res.text();
      } catch(err){ md = t("xHelpErr"); }
      miscDoc = { title:t("xHelp"), md:md };
      render();
    };
    [["xChlog","xChlogDesc",chlog],["xHelp","xHelpDesc",help]].forEach(([tk,dk,btn])=>{
      const c = el("div","pcard");
      c.appendChild(Object.assign(el("h3"),{textContent:t(tk)}));
      const r = el("div","prow"); r.style.margin = "0";
      const dsc = Object.assign(el("span","pdesc"),{textContent:t(dk)});
      dsc.style.margin = "0";
      r.appendChild(dsc);
      const right = rightWrap();
      right.appendChild(btn); r.appendChild(right); c.appendChild(r);
      d.appendChild(c);
    });
  }
  // 5. 删除项目知识库（危险卡）：项目下拉 + 删除… → 确认弹窗
  {
    const c = el("div","pcard danger");
    c.appendChild(Object.assign(el("h3"),{textContent:t("xDel")}));
    c.appendChild(Object.assign(el("div","pdesc"),{textContent:t("xDelDesc")}));
    const r = el("div","prow"); r.style.margin = "0";
    r.appendChild(Object.assign(el("span","k"),{textContent:t("xProject")}));
    const sel = el("select","pselect");
    projects.forEach(p=>{
      const op = el("option"); op.value = p.name; op.textContent = p.name; sel.appendChild(op);
    });
    r.appendChild(sel);
    const right = rightWrap();
    if(fb && fb.key.indexOf("del:")===0) right.appendChild(miscFbSpan(fb));
    const btn = el("button","btn btn-danger"); btn.textContent = t("xDelBtn");
    btn.disabled = !projects.length;
    btn.onclick = ()=>{ delTarget = sel.value; render(); };
    right.appendChild(btn); r.appendChild(right); c.appendChild(r);
    d.appendChild(c);
  }
  // 6. 关于（/api/status：版本 / 数据目录 / 已注册项目计数）
  {
    const c = el("div","pcard");
    c.appendChild(Object.assign(el("h3"),{textContent:t("xAbout")}));
    const v = el("div","about-line");
    v.textContent = t("xVer")+"：OkManager v"+(st ? st.app_version : "…");
    const h = el("div","about-line");
    h.appendChild(document.createTextNode(t("xHome")+"："));
    h.appendChild(Object.assign(el("span","mono"),{textContent:st ? st.home : "…"}));
    const n = el("div","about-line");
    n.textContent = t("xProjCount")+"："
      + (st && st.projects ? st.projects.length : projects.length) + t("xProjUnit");
    c.appendChild(v); c.appendChild(h); c.appendChild(n);
    d.appendChild(c);
  }

  if(miscDoc) d.appendChild(renderDocModal());
  if(delTarget) d.appendChild(renderDelModal(delTarget));
  return d;
}

/* 更新日志 / 使用帮助共用的 markdown 文档弹窗。entries 形（更新日志）按旧 GUI
   openChangelogModal 语义：API 升序 → 展示翻转为降序，版本间 <hr> 分隔。 */
function renderDocModal(){
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:miscDoc.title}));
  const body = el("div","md");
  if(miscDoc.entries){
    const es = miscDoc.entries;
    body.innerHTML = es.length
      ? es.slice().reverse().map(e=>renderMd(e.log)).join("<hr>")
      : "<p>"+esc(t("xChlogEmpty"))+"</p>";
  } else {
    body.innerHTML = renderMd(miscDoc.md || "");
  }
  m.appendChild(body);
  const foot = el("div","mfoot");
  const ok = el("button","btn btn-primary"); ok.textContent = t("xGotIt");
  ok.onclick = ()=>{ miscDoc = null; render(); };
  foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask){ miscDoc = null; render(); } };
  return mask;
}

/* ================= 升级首弹（旧 GUI checkChangelog 语义保留，Task 8 核对清单 1） =================
   启动时拉 /api/changelog：pending 非空 → 弹更新日志（API 升序 → 展示翻转为降序，版本间 <hr>）。
   弹窗挂 document.body、不进 render() 周期——任何菜单页启动都会弹且不被页面重渲打掉。
   仅升级弹窗关闭时 POST /api/changelog/seen；其他页「查看」入口只读 all，不影响 seen 状态。 */
function checkUpgrade(){
  api("/api/changelog").then(c=>{
    const p = c && c.pending;
    if(!p || !p.length) return;
    const latest = p[p.length-1].version;
    const title = p.length>1
      ? t("upTitleN").replace("{v}",latest).replace("{n}",p.length)
      : t("upTitle1").replace("{v}",latest);
    openUpgradeModal(title, p);
  }).catch(()=>{ /* 拉取失败不阻断主界面（旧语义） */ });
}
function openUpgradeModal(title, entries){
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:title}));
  const body = el("div","md");
  body.innerHTML = entries.slice().reverse().map(e=>renderMd(e.log)).join("<hr>");
  m.appendChild(body);
  const foot = el("div","mfoot");
  const ok = el("button","btn btn-primary"); ok.textContent = t("xGotIt");
  let closed = false;
  const close = ()=>{
    if(closed) return; closed = true;
    mask.remove();
    // seen 失败则下次启动再弹，自行愈合，不打扰本次使用
    api("/api/changelog/seen", { method:"POST" }).catch(()=>{});
  };
  ok.onclick = close;
  foot.appendChild(ok);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask) close(); };
  document.body.appendChild(mask);
}

/* 删除项目知识库确认弹窗：备份勾选 + ack 勾选 + 输入完整项目名，三齐备才解锁「永久删除」
   （计划/原型注释语义——原型代码漏查 backup，此处按注释与计划文本落实）。确认后先导出
   zip 备份（失败中止删除，旧 GUI 语义），再 DELETE；已注销但目录删除失败时按 warning
   提示手动清理。 */
function renderDelModal(name){
  const mask = el("div","mask");
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:t("xDel")}));
  const impact = el("div","pdesc");
  impact.textContent = t("xDelCounting");
  m.appendChild(impact);
  // 影响面统计失败不阻塞确认流程；直接改文本节点（不经 render，保住勾选与输入态）
  api("/api/entries?project="+encodeURIComponent(name)).then(list=>{
    list = list || [];
    impact.textContent = t("xDelImpact").replace("{p}",name).replace("{n}",list.length);
  }).catch(()=>{
    impact.textContent = t("xDelImpactFail").replace("{p}",name);
  });
  const backup = el("input"); backup.type = "checkbox"; backup.checked = true; backup.className = "radio";
  const lb = el("label","prow"); lb.style.margin = "6px 0";
  lb.appendChild(backup); lb.appendChild(document.createTextNode(t("xDelBackup")));
  m.appendChild(lb);
  const ack = el("input"); ack.type = "checkbox"; ack.className = "radio";
  const la = el("label","prow"); la.style.margin = "6px 0";
  la.appendChild(ack); la.appendChild(document.createTextNode(t("xDelAck")));
  m.appendChild(la);
  const nameRow = el("div","prow");
  nameRow.appendChild(Object.assign(el("span","k"),{textContent:t("xDelHint")}));
  const nameIn = ptext("", ()=>{}, "220px");
  nameIn.placeholder = name;
  nameRow.appendChild(nameIn);
  m.appendChild(nameRow);
  const errLine = el("div","pdesc fb2 err");
  m.appendChild(errLine);
  const foot = el("div","mfoot");
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = ()=>{ delTarget = null; render(); };
  const confirm = el("button","btn btn-danger"); confirm.textContent = t("xDelConfirm");
  confirm.disabled = true;
  const upd = ()=>{
    confirm.disabled = !(backup.checked && ack.checked && nameIn.value.trim()===name);
  };
  backup.onchange = upd; ack.onchange = upd; nameIn.oninput = upd;
  confirm.onclick = async ()=>{
    if(confirm.disabled) return;
    confirm.disabled = true; confirm.textContent = t("xDelDeleting");
    errLine.textContent = "";
    const restore = ()=>{ confirm.disabled = false; confirm.textContent = t("xDelConfirm"); upd(); };
    try {
      await downloadZip(name);   // 三齐备含备份勾选 → 备份必做，导出失败中止删除
    } catch(err){
      errLine.textContent = t("xDelBackupFail").replace("{s}", err.status || err.message);
      restore();
      return;
    }
    try {
      const res = await api("/api/project?project="+encodeURIComponent(name), { method:"DELETE" });
      delTarget = null;
      if(res && res.warning){
        flashMiscFb("del:"+name,
          t("xDelPartial").replace("{w}",res.warning).replace("{d}",res.dir||""),
          { err:true, sticky:true });
      } else {
        flashMiscFb("del:"+name, t("xDeleted").replace("{p}",name));
      }
      refreshMisc();   // 项目下拉/管理页（Task 5 接同一缓存）/关于计数联动少一个
    } catch(err){
      errLine.textContent = err.message;
      restore();
    }
  };
  foot.appendChild(cancel); foot.appendChild(confirm);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = e=>{ if(e.target===mask){ delTarget = null; render(); } };
  return mask;
}

/* ================= 渲染 ================= */
function el(tag, cls){ const n=document.createElement(tag); if(cls) n.className=cls; return n; }

function render(){
  document.documentElement.dataset.theme = state.theme;
  const app = document.getElementById("app");
  // 整页重渲不丢滚动：重渲前记住各滚动容器位置，重渲后同步恢复
  const keep = {};
  app.querySelectorAll(".prefs,.setup,.detail,.tree-scroll,.misc").forEach(n=>{
    keep[n.className.split(" ")[0]] = n.scrollTop;
  });
  renderBody(app);
  Object.entries(keep).forEach(([cls,top])=>{
    const n = app.querySelector("."+cls);
    if(n) n.scrollTop = top;
  });
}

function renderBody(app){
  app.innerHTML = "";

  // 顶栏：品牌在前，折叠按钮紧随其后（用户指定位置）
  const bar = el("div","topbar");
  const brand = el("div","brand");
  brand.innerHTML = '<span class="logo">ok</span>OkManager';
  bar.appendChild(brand);
  const collapse = el("button","collapse-btn");
  collapse.innerHTML = ICON.panel; collapse.title = t("collapseTip");
  collapse.onclick = ()=>{ state.collapsed=!state.collapsed; render(); };
  bar.appendChild(collapse);
  const lang = el("div","lang-seg");
  [["zh","中"],["en","EN"]].forEach(([k,label])=>{
    const b = el("button", state.lang===k?"on":"");
    b.textContent = label;
    b.onclick = ()=>{ state.lang=k; render(); };
    lang.appendChild(b);
  });
  bar.appendChild(lang);
  const theme = el("button","theme-btn");
  theme.innerHTML = state.theme==="light" ? ICON.moon : ICON.sun;
  theme.onclick = ()=>{ state.theme = state.theme==="light"?"dark":"light"; render(); };
  bar.appendChild(theme);
  app.appendChild(bar);

  // 主体
  const main = el("div","main");
  app.appendChild(main);

  // 侧栏（同一 DOM，collapsed class 切两态）
  const side = el("aside","side"+(state.collapsed?" collapsed":""));
  MENUS.forEach(m=>{
    const b = el("button","mi"+(state.menu===m.key?" active":""));
    b.innerHTML = '<span class="ico">'+m.ico+'</span><span class="txt">'+t(m.key)+'</span>'
                + '<span class="tip">'+t(m.key)+'</span>';
    b.onclick = ()=>{
      exitEdit();
      state.menu=m.key; location.hash=m.key;
      // 跨页缓存联动（Task 4 评审约定）：切到其他页重拉项目列表缓存；切到管理页重拉树数据
      if(m.key==="misc" && MISC) refreshMisc();
      if(m.key==="setup" && SETUP) refreshSetup();   // 引导页：重拉 agent 检测/接入状态
      if(m.key==="manage" && MGMT) refreshManage();
      render();
    };
    side.appendChild(b);
  });
  main.appendChild(side);

  // 五页均已接入真实数据：管理（Task 5）、引导（Task 7）、设置（Task 6）、日志（Task 3）、其他（Task 4）
  // （占位分支已随 Task 7 引导页接入移除；notImpl 键同步删除）
  if(state.menu==="manage"){
    loadManage();
    main.appendChild(renderTree());
    main.appendChild(renderTreeResizer());   // 树栏拖拽分隔条（反馈2）
    main.appendChild(renderDetail());
    return;
  }
  if(state.menu==="logs"){
    main.appendChild(renderLogs());
    return;
  }
  if(state.menu==="misc"){
    main.appendChild(renderMisc());
    return;
  }
  if(state.menu==="setup"){
    main.appendChild(renderSetup());
    return;
  }
  if(state.menu==="prefs"){
    main.appendChild(renderPrefs());
    return;
  }
  /* 五页已全部接入，无占位分支 */
}

/* 刷新恢复选中菜单：菜单点击时写入 location.hash，启动时读回（非法值回退 manage） */
(function(){
  const h = location.hash.replace(/^#/,"");
  if(MENUS.some(m=>m.key===h)) state.menu = h;
})();

render();



/* 启动横切（旧 GUI 语义，Task 8 核对清单 1/6）：
   1. 无项目 → 落 setup 引导页（旧：无项目隐藏管理 tab 只展示引导页；新 UI 树对空数据健壮，
      只落页不隐藏菜单，覆盖 hash 恢复——首次运行的引导意图优先）
   2. 升级后首次打开自动弹更新日志（pending 非空 → 弹窗，关闭时 POST seen） */
api("/api/projects").then(ps=>{
  if(ps && ps.length) return;
  state.menu = "setup"; location.hash = "setup"; render();
}).catch(()=>{ /* 拉取失败保持 hash/默认页，不阻断 */ });
checkUpgrade();
