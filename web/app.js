"use strict";
/* OkManager 配置中心外壳：五菜单左右栏 + 顶栏（侧栏折叠 / 中英 / 昼夜）+ hash 路由 + 整页重渲滚动保持。
   规范源 docs/prototypes/prototype-manager-v2.html——I18N/ICON/MENUS/el/esc/t/state/render/renderBody/
   设置卡助手（pswitch/pcard/prow/pnumLive/ptext/pDirtyLive 等）均为原型平移；日志页（Task 3）、其他页
   （Task 4）、管理页（Task 5）与设置页（Task 6）已接真后端，引导页挂占位，按
   docs/superpowers/plans/2026-08-21-config-center-ui.md 的 Task 7 接入。 */

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
    notImpl:"页尚未接入 —— 当前为占位页", modified:"修改于",
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
    detailRemove:"<b>卸载</b>：按标记移除 ok 写入的 hooks 段与技能目录条目，不动用户其他配置。",
    fbInstall:"✓ 已接入：hooks 已写入，技能已安装（下次会话生效）",
    fbUninstall:"✓ 已卸载：ok 写入的 hooks 与技能已移除",
    noneDetected:"未检测到任何已安装的 agent",
    save:"保存", saved:"✓ 已保存",
    gTitle:"全局开关", gDesc:"一键启停全部 agent 的 hooks 注入与强制检查（等同 CLI 的 ok on / ok off）", gLabel:"启用全部 hooks",
    gOnFb:"✓ 已开启全部 hooks", gOffFb:"✓ 已关闭全部 hooks",
    eManage:"管理配置", eProfiles:"服务列表", eAdd:"+ 新增服务", eEdit:"编辑", eDel:"删除", eSetActive:"设为使用中",
    fName:"名称", fType:"类型", fBase:"base_url", fModel:"模型", fKey:"api_key", fKeyEnv:"api_key_env（环境变量名）", fMirror:"下载源（仅 builtin）",
    eAddTitle:"新增服务", eEditTitle:"编辑服务", fOk:"确定", fCancel:"取消",
    typeBuiltin:"内置本地模型（ok 托管 · 无需联网）", typeOllama:"Ollama（本机/局域网服务）", typeOpenai:"自定义（OpenAI 兼容服务）",
    tagBuiltin:"内置", tagOllama:"Ollama", tagCustom:"自定义",
    mirrorHf:"hf-mirror 镜像（国内推荐）", mirrorOfficial:"huggingface 官方", downloaded:"（已下载）",
    fOlUrl:"服务地址", keySaved:"已保存（留空保持不变）", eGlobal:"全局",
    kindOpenai:"OpenAI 兼容（/chat/completions）", kindAnthropic:"Anthropic 兼容（/v1/messages）",
    tagOpenai:"OpenAI 兼容", tagAnthropic:"Anthropic 兼容",
    fTemp:"temperature（高级，留空 = 不传）", fMaxTokens:"max_tokens（高级，0 = 默认）",
    eTitle:"语义检索（embedding）", eDesc:"混合检索的语义通道；不配置任何服务时退化为纯关键词检索",
    eNone:"未配置（仅关键词检索）", eTimeout:"调用超时（秒）", eDir:"内置模型目录", eActive:"使用中",
    lTitle:"模型配置（LLM）", lDesc:"生成场景（条目优化等）调用的大模型服务；temperature 留空 = 不传",
    lTimeout:"生成超时（秒）", lTest:"测试连接", lTesting:"测试中…", lTestOk:"✓ 连通（{ms}ms）",
    hTitle:"Hook 超时", hDesc:"写入各 agent hooks 的超时秒数。2026-08-04 曾发生 Windows 高负载下 5s 超时致 PostToolUse 整会话静默丢失，故默认 10", hSec:"超时（秒）",
    gtTitle:"泛化门控", gtDesc:"命中内置/自定义短语的泛化 prompt 跳过检索注入与 embed 调用",
    gtOn:"启用门控", gtStatus:"内置 {b} 条 · 自定义 {n} 条", gtManage:"管理短语表",
    gtBuiltin:"内置短语（只读，随版本演进）", gtCustom:"自定义短语", gtAdd:"+ 添加", gtPh:"新短语…", gtClose:"关闭",
    noProject:"尚无已注册项目（先 ok init）——该项目级设置暂不可用",
    eDlReady:"✓ 模型已就绪（{dim} 维），sidecar 按需拉起、空闲自动退出",
    eDlBtn:"下载模型（{size}）", eDlDoing:"正在下载 — {done} / {total}", eDlErr:"上次下载失败：",
    eDlNoRt:"⚠ 推理运行时缺失——内置模式仅安装版可用（裸 exe 形态请用 Ollama/自定义）",
    eDirOpen:"打开",
    eIdxWarn:"⚠ 使用中模型（{a}）与当前项目索引（{i}）不符——切换后请运行 ok index 重建",
    cTitle:"跨轮注入冷却", cDesc:"同会话内已注入的检索条目冷却 N 个 prompt 轮不再注入（门控轮也计）；0 = 关闭（每轮都可注入）", cTurns:"冷却轮数",
    rTitle:"规则配置（强制检查）", rDesc:"AI 改动命中 code globs 的文件时，回合结束校验 changelog 是否同步更新",
    rType:"类型", rGlobs:"code globs", rCl:"changelog glob", rMsg:"提示语", rAdd:"+ 添加规则",
    capTitle:"经验沉淀", capDesc:"propose = AI 提议草稿、人批准后入库；auto = 按轮次间隔自动提取",
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
    opEdit:"编辑", opApprove:"批准", opArchive:"归档", opUnarchive:"取消归档", opDelete:"删除",
    cfmDelete:"确定删除条目「{t}」？",
    cfmArchive:"归档条目「{t}」？归档后退出 INDEX 与强制注入，仍可被检索命中。",
    emNew:"新建条目", emEdit:"编辑条目", emExists:"条目已存在", emNoProject:"尚无已注册项目，请先 ok init",
    fTitle:"标题", fType:"类型", fTags:"tags（逗号分隔）", fMand:"mandatory（每会话必注入）",
    fSummary:"摘要", fBody:"正文",
    typeRule:"规则", typePitfall:"踩坑", typeNote:"笔记", typeReference:"参考",
    optBtn:"✨ 优化", optBusy:"优化中…", optEmpty:"正文为空，无可优化内容",
    optTip:"结合项目真实代码与相关条目据实润色标题/标签/摘要/正文（类型与 mandatory 不动）；先出对照预览，确认回填后点保存才生效。",
    cmpTitle:"优化对照预览", cmpBasis:"依据：条目引用的真实代码 + 相关条目 + INDEX 摘录",
    cmpNotice:"模型判断：当前内容已足够简练准确，无需优化。如下仍有差异仅为排版/标点，可逐字段回填或直接放弃。",
    cmpApply:"回填表单", cmpDiscard:"放弃", cmpFill:"回填", cmpFilled:"已回填",
    cmpOld:"原数据", cmpNew:"优化后", cmpNote:"回填只改表单，点「保存」才写入 .md",
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
    notImpl:"is not wired up yet — placeholder page", modified:"Modified",
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
    detailRemove:"<b>Uninstall</b>: removes only the ok-marked hooks block and skill entries, leaving other config untouched.",
    fbInstall:"✓ Integrated: hooks written, skills installed (takes effect next session)",
    fbUninstall:"✓ Uninstalled: ok-written hooks and skills removed",
    noneDetected:"No installed agents detected",
    save:"Save", saved:"✓ Saved",
    gTitle:"Global switch", gDesc:"Enable/disable hooks injection and enforce checks for all agents (same as CLI ok on / ok off)", gLabel:"Enable all hooks",
    gOnFb:"✓ All hooks enabled", gOffFb:"✓ All hooks disabled",
    eManage:"Manage", eProfiles:"Services", eAdd:"+ Add service", eEdit:"Edit", eDel:"Delete", eSetActive:"Set active",
    fName:"Name", fType:"Type", fBase:"base_url", fModel:"Model", fKey:"api_key", fKeyEnv:"api_key_env (env var)", fMirror:"Mirror (builtin only)",
    eAddTitle:"Add service", eEditTitle:"Edit service", fOk:"OK", fCancel:"Cancel",
    typeBuiltin:"Builtin local model (ok-managed, offline)", typeOllama:"Ollama (local/LAN service)", typeOpenai:"Custom (OpenAI-compatible)",
    tagBuiltin:"Builtin", tagOllama:"Ollama", tagCustom:"Custom",
    mirrorHf:"hf-mirror (recommended in CN)", mirrorOfficial:"huggingface official", downloaded:" (downloaded)",
    fOlUrl:"Service URL", keySaved:"Saved (leave empty to keep)", eGlobal:"Global",
    kindOpenai:"OpenAI-compatible (/chat/completions)", kindAnthropic:"Anthropic-compatible (/v1/messages)",
    tagOpenai:"OpenAI-compat", tagAnthropic:"Anthropic-compat",
    fTemp:"temperature (advanced, empty = not sent)", fMaxTokens:"max_tokens (advanced, 0 = default)",
    eTitle:"Semantic retrieval (embedding)", eDesc:"The semantic channel of hybrid retrieval; degrades to keyword-only when no service is configured",
    eNone:"Not configured (keyword-only)", eTimeout:"Timeout (s)", eDir:"Builtin models dir", eActive:"Active",
    lTitle:"Model config (LLM)", lDesc:"LLM services for generation tasks (entry polishing etc.); empty temperature = not sent",
    lTimeout:"Generation timeout (s)", lTest:"Test connection", lTesting:"Testing…", lTestOk:"✓ Connected ({ms}ms)",
    hTitle:"Hook timeout", hDesc:"Timeout seconds written into each agent's hooks. On 2026-08-04 a 5s timeout under Windows load silently dropped PostToolUse for an entire session — hence default 10", hSec:"Timeout (s)",
    gtTitle:"Generalization gate", gtDesc:"Prompts matching builtin/custom phrases skip retrieval injection and embed calls",
    gtOn:"Enable gate", gtStatus:"{b} builtin · {n} custom", gtManage:"Manage phrases",
    gtBuiltin:"Builtin phrases (read-only, evolve with releases)", gtCustom:"Custom phrases", gtAdd:"+ Add", gtPh:"New phrase…", gtClose:"Close",
    noProject:"No registered project yet (run ok init) — this project-level setting is unavailable",
    eDlReady:"✓ Model ready ({dim} dim); sidecar starts on demand and exits when idle",
    eDlBtn:"Download model ({size})", eDlDoing:"Downloading — {done} / {total}", eDlErr:"Last download failed: ",
    eDlNoRt:"⚠ Inference runtime missing — builtin mode requires the installer edition (bare exe: use Ollama/custom)",
    eDirOpen:"Open",
    eIdxWarn:"⚠ Active model ({a}) differs from the project index ({i}) — run ok index to rebuild after switching",
    cTitle:"Cross-turn injection cooldown", cDesc:"Retrieved entries already injected in this session cool down for N prompt turns (gate turns count too); 0 = off", cTurns:"Cooldown turns",
    rTitle:"Rules (enforce checks)", rDesc:"When AI edits files matching code globs, session end verifies the changelog was updated",
    rType:"Type", rGlobs:"code globs", rCl:"changelog glob", rMsg:"Message", rAdd:"+ Add rule",
    capTitle:"Experience capture", capDesc:"propose = AI drafts, human approves; auto = extract every N turns",
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
    opEdit:"Edit", opApprove:"Approve", opArchive:"Archive", opUnarchive:"Unarchive", opDelete:"Delete",
    cfmDelete:"Delete entry \"{t}\"?",
    cfmArchive:"Archive entry \"{t}\"? It leaves INDEX and mandatory injection, but stays searchable.",
    emNew:"New entry", emEdit:"Edit entry", emExists:"Entry already exists", emNoProject:"No registered project yet; run ok init first",
    fTitle:"Title", fType:"Type", fTags:"tags (comma-separated)", fMand:"mandatory (injected every session)",
    fSummary:"Summary", fBody:"Body",
    typeRule:"Rule", typePitfall:"Pitfall", typeNote:"Note", typeReference:"Reference",
    optBtn:"✨ Optimize", optBusy:"Optimizing…", optEmpty:"Body is empty; nothing to optimize",
    optTip:"Polishes title/tags/summary/body against real code and related entries (type and mandatory untouched); shows a diff preview first — nothing is written until you fill back and save.",
    cmpTitle:"Optimize preview", cmpBasis:"Basis: real code referenced by the entry + related entries + INDEX excerpt",
    cmpNotice:"The model considers the content already concise and accurate — no optimization needed. Any difference below is layout/punctuation only; fill back per field or discard.",
    cmpApply:"Fill all back", cmpDiscard:"Discard", cmpFill:"Fill", cmpFilled:"Filled",
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

/* ================= 状态 ================= */
const state = { menu:"manage", lang:"zh", theme:"light", collapsed:false,
                open:{}, sel:null, q:"", mgmtFb:null,
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

function pDirty(k){ prefsDirty[k]=true; render(); }
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
function esc(s){ return s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;"); }
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

/* ================= 管理页 ================= */
/* 两级树（项目→条目）+ markdown 详情，结构/交互照抄原型 renderTree/fillTree/renderDetail
   （prototype-manager-v2.html:1414-1473），mock 换真：项目=GET /api/projects，
   条目=GET /api/entries?project=（逐项目拉取），详情正文=GET /api/entry?project=&file=。
   响应字段已核 api.go:302-352（entrySummaryJSON：file/title/type/tags/mandatory/draft/
   archived/summary/mtime[unix 秒]；无 size/born——born 从 tags 的 born:<名> 提取，
   大小后端不返回故详情不展示）。条目操作沿用旧 GUI 语义（旧 app.js:638-825）：
   操作组在详情页右上（编辑/归档或取消归档/删除，草稿额外批准）；新建按钮在树头部
   搜索框旁；新建/编辑复用同一弹窗 + ✨优化走 /api/entry/optimize（对照预览回填，
   保存才落盘）。born/继承徽标 hover 浮动窗沿用 v2.18.2 行为（branch-info，旧
   app.js:321-374）。弹窗挂 document.body（position:fixed），不进 #app 渲染周期——
   后台刷新（refreshManage/loadDetail/loadBranchInfo 完成）整页重渲不会打掉表单输入态。 */
const TYPE_LABEL = { pitfall:"坑", note:"注", rule:"规", reference:"参" };
let MGMT = null;        // {list:[{name,paths,entries,err}], loadErr} 缓存；loadManage 惰性加载
let DETAIL = null;      // {key(project\nfile), data, err} 当前选中条目全文缓存
const BRANCH = {};      // project → branch-info（会话级缓存；null=已拉取但失败/无数据，占位防重拉）
let entryMask = null, cmpMask = null, llmMask = null;   // 管理页弹窗节点（body 级，同时只各存一个）
let optAbort = null;    // 优化进行中的 AbortController；关闭编辑弹窗即取消

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
// refreshManage 全量重拉项目+条目；完成时原位刷新（过滤框聚焦中只重填树、不整页重渲，保焦点）
function refreshManage(){
  api("/api/projects").then(ps=>{
    return Promise.all((ps||[]).map(p=>
      api("/api/entries?project="+encodeURIComponent(p.name))
        .then(es=>({ name:p.name, paths:p.paths||[], entries:es||[] }))
        .catch(err=>({ name:p.name, paths:p.paths||[], entries:[], err:err.message }))));
  }).then(list=>{
    MGMT = { list:list };
    // 选中条目已消失（外部删除/项目删除）→ 清选择与详情缓存
    if(state.sel && !findEntry(state.sel.project, state.sel.file)){ state.sel = null; DETAIL = null; }
  }).catch(err=>{
    MGMT = { list:[], loadErr: err.message };
  }).then(()=>{
    if(state.menu!=="manage") return;
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
    .then(()=>{ if(state.menu==="manage") render(); });
}
// loadBranchInfo 惰性拉项目分支上下文（继承徽标 hover 数据，v2.18.2 既有端点）
function loadBranchInfo(project){
  if(!project || project in BRANCH) return;
  BRANCH[project] = null;
  api("/api/project/branch-info?project="+encodeURIComponent(project))
    .then(info=>{ BRANCH[project] = info || null; })
    .catch(()=>{ BRANCH[project] = null; })
    .then(()=>{ if(state.menu==="manage") render(); });
}

function badges(e){
  const born = bornOf(e);
  return (born ? '<span class="badge-born" title="born">'+ICON.branch+esc(born)+'</span>' : "")
       + '<span class="badge-type t-'+e.type+'" title="'+esc(e.type)+'">'+(TYPE_LABEL[e.type]||"?")+'</span>';
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
      || (MGMT.list[0] && MGMT.list[0].name) || "";
    openEntryModal(def, null);
  };
  tools.appendChild(add);
  tree.appendChild(tools);
  const scroll = el("div","tree-scroll");
  tree.appendChild(scroll);
  fillTree(scroll);
  return tree;
}
function fillTree(scroll){
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
    const open = q ? true : state.open[p.name] !== false;   // 默认展开；点项目名折叠/展开
    const pj = el("button","tn-proj"+(open?" open":""));
    pj.innerHTML = '<span class="caret">▶</span><span class="folder">'+ICON.folder+'</span>'+esc(p.name)
      +'<span class="cnt">'+(p.err?"!":list.length)+'</span>';
    pj.title = p.err ? t("mgTreeErr")+p.err : (p.paths||[]).join("\n");
    pj.onclick = ()=>{ state.open[p.name]=!open; render(); };
    scroll.appendChild(pj);
    if(open){
      const kids = el("div","tn-kids");
      list.forEach(e=>{
        const sel = state.sel && state.sel.project===p.name && state.sel.file===e.file;
        const leaf = el("button","leaf"+(sel?" sel":"")+(e.archived?" archived":""));
        leaf.innerHTML = '<span class="l1">'+badges(e)
          +(e.mandatory?'<span class="badge-mand">★</span>':"")
          +(e.draft?'<span class="badge-draft">'+t("draft")+'</span>':"")+'</span>'
          +'<span class="t2">'+esc(e.title)+'</span>';
        leaf.onclick = ()=>{ state.sel={ project:p.name, file:e.file }; state.mgmtFb=null; loadDetail(); render(); };
        kids.appendChild(leaf);
      });
      scroll.appendChild(kids);
    }
  });
}

function renderDetail(){
  const d = el("div","detail");
  const sel = state.sel && findEntry(state.sel.project, state.sel.file);
  if(!sel){
    d.appendChild(Object.assign(el("div","placeholder"),{textContent:t("pickEntry")}));
    return d;
  }
  const e = sel.entry, proj = sel.project;
  // 右上操作组（方案决策位置）：编辑 /（草稿额外）批准 / 归档或取消归档 / 删除；左侧为操作反馈行
  const ops = el("div","d-ops");
  if(state.mgmtFb)
    ops.appendChild(Object.assign(el("span","fb2"+(state.mgmtFb.err?" err":"")),{textContent:state.mgmtFb.txt}));
  const mk = (label, cls, fn)=>{ const b=el("button",cls); b.textContent=label; b.onclick=fn; return b; };
  ops.appendChild(mk(t("opEdit"), "btn", ()=>openEntryModal(proj, e.file)));
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
  const at = el("div","d-attrs"); at.innerHTML = detailAttrs(e, proj); d.appendChild(at);
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
  refreshManage();               // 树刷新（选中项消失则自动清选择）
  if(state.sel) loadDetail(true);
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

/* ---------- 新建/编辑复用弹窗（含 ✨优化） ----------
   表单值由 DOM 直读（弹窗期间不整页重渲）；保存成功才落盘——新建 POST /api/entry，
   编辑 PUT /api/entry（file 为身份，不可改名；created/draft/archived 由后端继承）。 */
function openEntryModal(project, file){
  if(!project){
    state.mgmtFb = { txt:t("emNoProject"), err:true }; render(); return;
  }
  if(!file){
    editModal_open(project, null, null);
    return;
  }
  api("/api/entry?project="+encodeURIComponent(project)+"&file="+encodeURIComponent(file))
    .then(d=>{ editModal_open(project, file, d); })
    .catch(opFail);
}
function editModal_open(project, file, init){
  init = init || {};
  const isEdit = !!file;
  const mask = el("div","mask");
  entryMask = mask;
  const m = el("div","modal");
  m.appendChild(Object.assign(el("h3"),{textContent:isEdit?t("emEdit"):t("emNew")}));
  // 字段行构造器
  const row = (label, input)=>{
    const r = el("div","efield");
    r.appendChild(Object.assign(el("span","k"),{textContent:label}));
    r.appendChild(input);
    m.appendChild(r);
    return input;
  };
  // 项目：新建可选（条目身份=项目+文件名）；编辑只读
  if(isEdit){
    row(t("xProject"), Object.assign(el("span"),{textContent:project}));
  }
  let projSel = null;
  if(!isEdit){
    projSel = el("select","pselect");
    MGMT.list.forEach(p=>{ const o=el("option"); o.value=p.name; o.textContent=p.name; projSel.appendChild(o); });
    projSel.value = project;
    row(t("xProject"), projSel);
  }
  const fTitle = row(t("fTitle"), el("input","pinput")); fTitle.value = init.title||"";
  const fType = el("select","pselect");
  [["rule","typeRule"],["pitfall","typePitfall"],["note","typeNote"],["reference","typeReference"]]
    .forEach(([v,k])=>{ const o=el("option"); o.value=v; o.textContent=t(k); fType.appendChild(o); });
  fType.value = init.type||"note";
  row(t("fType"), fType);
  const fTags = row(t("fTags"), el("input","pinput")); fTags.value = (init.tags||[]).join(", ");
  const fMand = el("input"); fMand.type="checkbox"; fMand.className="radio"; fMand.checked = !!init.mandatory;
  {
    const r = el("label","efield");
    r.appendChild(fMand);
    r.appendChild(document.createTextNode(t("fMand")));
    m.appendChild(r);
  }
  const fSummary = row(t("fSummary"), el("input","pinput")); fSummary.value = init.summary||"";
  const fBody = el("textarea","pinput"); fBody.rows = 10; fBody.value = init.body||"";
  row(t("fBody"), fBody);
  const errLine = el("div","pdesc fb2 err");
  m.appendChild(errLine);

  const curProject = ()=>isEdit ? project : projSel.value;
  const fields = { title:fTitle, tags:fTags, summary:fSummary, body:fBody };
  const close = ()=>{
    if(optAbort){ optAbort.abort(); optAbort=null; }   // 关闭弹窗 = 取消进行中的优化
    if(cmpMask){ cmpMask.remove(); cmpMask=null; }
    if(llmMask){ llmMask.remove(); llmMask=null; }
    mask.remove();
    if(entryMask===mask) entryMask=null;
  };

  const foot = el("div","mfoot");
  const save = el("button","btn btn-primary"); save.textContent = t("save");
  save.onclick = ()=>{
    const payload = { project:curProject(), title:fTitle.value, type:fType.value,
      tags:fTags.value.split(",").map(s=>s.trim()).filter(Boolean),
      mandatory:fMand.checked, summary:fSummary.value, body:fBody.value };
    save.disabled = true;
    if(isEdit) payload.file = file;
    api("/api/entry", { method:isEdit?"PUT":"POST", body:payload })
      .then(()=>{ close(); afterEntryOp(); })
      .catch(err=>{
        errLine.textContent = err.status===409 ? t("emExists") : err.message;
        save.disabled = false;
      });
  };
  const cancel = el("button","btn"); cancel.textContent = t("fCancel");
  cancel.onclick = close;
  // ✨优化：loading（禁用+文案）→ 对照预览弹窗；409=未配置模型弹窗；关闭编辑弹窗取消请求
  const opt = el("button","btn"); opt.textContent = t("optBtn"); opt.title = t("optTip");
  opt.onclick = ()=>{
    if(!fBody.value.trim()){ errLine.textContent = t("optEmpty"); return; }
    errLine.textContent = "";
    opt.disabled = true;
    const oldText = opt.textContent;
    opt.textContent = t("optBusy");
    optAbort = new AbortController();
    api("/api/entry/optimize", {
      method:"POST", signal:optAbort.signal,
      body:{ project:curProject(), file:file||"", title:fTitle.value,
             tags:fTags.value, summary:fSummary.value, body:fBody.value },
    }).then(out=>{
      // 一律打开对照预览（no_change 提示在弹窗内展示）
      openCmpModal({ title:fTitle.value, tags:fTags.value, summary:fSummary.value, body:fBody.value },
                   out||{}, fields);
    }).catch(err=>{
      if(err && err.name==="AbortError") return;   // 关弹窗主动取消，静默
      if(err.status===409) openLlmNeededModal();
      else errLine.textContent = err.message;
    }).finally(()=>{
      optAbort = null;
      opt.disabled = false;
      opt.textContent = oldText;
    });
  };
  foot.appendChild(save); foot.appendChild(cancel); foot.appendChild(opt);
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = ev=>{ if(ev.target===mask) close(); };
  document.body.appendChild(mask);
}

/* ---------- 优化对照预览弹窗（旧 app.js:689-748 平移；回填只改表单，保存才落盘） ---------- */
const CMP_FIELDS = [["title","fTitle"],["tags","fTags"],["summary","fSummary"],["body","fBody"]];
function usageText(u){
  if(!u || (!u.prompt && !u.completion)) return "";
  return t("cmpUsage").replace("{t}", u.prompt+u.completion)
    .replace("{p}", u.prompt).replace("{c}", u.completion);
}
function openCmpModal(oldV, newV, fields){
  const mask = el("div","mask");
  cmpMask = mask;
  const m = el("div","modal cmp-box");
  const h = el("h3"); h.textContent = t("cmpTitle");
  const basis = el("span","cmp-basis"); basis.textContent = " "+t("cmpBasis");
  h.appendChild(basis);
  m.appendChild(h);
  const usage = el("span","cmp-note"); usage.textContent = usageText(newV.usage);
  if(newV.no_change){
    m.appendChild(Object.assign(el("div","cmp-notice"),{textContent:t("cmpNotice")}));
  }
  const box = el("div");
  // 单字段回填值：no_change 场景模型可能不回字段，空值回退原值（不得清空表单）
  const fillVal = key=>{
    if(key==="tags") return (newV.tags||[]).join(", ") || oldV.tags || "";
    return newV[key] || oldV[key] || "";
  };
  CMP_FIELDS.forEach(([key,labelKey])=>{
    const oldText = oldV[key] || "";
    const newText = fillVal(key);
    const div = el("div","cmp-field");
    const name = el("div","cmp-name");
    name.appendChild(Object.assign(el("span"),{textContent:t(labelKey)}));
    const fill = el("button","btn cmp-fill"); fill.textContent = t("cmpFill");
    fill.onclick = ()=>{ fields[key].value = fillVal(key); fill.textContent = t("cmpFilled"); };
    name.appendChild(fill);
    div.appendChild(name);
    const oldD = el("div","cmp-old");
    oldD.appendChild(Object.assign(el("span","cmp-tag old"),{textContent:t("cmpOld")}));
    oldD.appendChild(document.createTextNode(oldText));
    div.appendChild(oldD);
    const newD = el("div","cmp-new");
    newD.appendChild(Object.assign(el("span","cmp-tag new"),{textContent:t("cmpNew")}));
    newD.appendChild(document.createTextNode(newText));
    div.appendChild(newD);
    box.appendChild(div);
  });
  m.appendChild(box);
  const foot = el("div","mfoot");
  const apply = el("button","btn btn-primary"); apply.textContent = t("cmpApply");
  apply.onclick = ()=>{ CMP_FIELDS.forEach(([key])=>{ fields[key].value = fillVal(key); }); close(); };
  const discard = el("button","btn"); discard.textContent = t("cmpDiscard");
  const close = ()=>{ mask.remove(); if(cmpMask===mask) cmpMask=null; };
  discard.onclick = close;
  foot.appendChild(apply); foot.appendChild(discard); foot.appendChild(usage);
  foot.appendChild(Object.assign(el("span","cmp-note"),{textContent:t("cmpNote")}));
  m.appendChild(foot);
  mask.appendChild(m);
  mask.onclick = ev=>{ if(ev.target===mask) close(); };
  document.body.appendChild(mask);
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
    if(entryMask){ entryMask.remove(); entryMask=null; }
    if(cmpMask){ cmpMask.remove(); cmpMask=null; }
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
   初始聚合拉取：status 先行（取项目名 + hooksTimeout/disabled），随后 embedding/llm 全局两件与
   retrieve/capture/gate/enforce-rules 项目级四件并行；单件失败只记 PREFS.errs[key]，不拖垮整页。
   项目级卡（冷却/沉淀/门控/规则）作用项目 = 第一个注册项目（旧 GUI captureProject 缺省语义），
   无项目时该卡退化为只读提示。保存语义（Global Constraints 范式 2）：
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
// 聚合拉取（多请求并行，非新聚合端点）；项目级四件在无项目时跳过（后端缺 project 400）
function refreshPrefs(){
  api("/api/status").then(st=>{
    const project = (st.projects && st.projects[0] && st.projects[0].name) || "";
    const jobs = {
      emb: api("/api/setup/embedding"+(project?"?project="+encodeURIComponent(project):"")),
      llm: api("/api/llm"),
    };
    if(project){
      const urls = { retr:"/api/retrieve", cap:"/api/capture", gate:"/api/gate", rules:"/api/enforce/rules" };
      Object.keys(urls).forEach(k=>{
        jobs[k] = api(urls[k]+"?project="+encodeURIComponent(project));
      });
    }
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
  api("/api/enforce/rules", { method:"POST", body:{ project:PREFS.project, rules:payload } })
    .then(()=>api("/api/enforce/rules?project="+encodeURIComponent(PREFS.project)))   // 复读核对
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
  const noProj = !PREFS.project;

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
        sumBody.innerHTML = '<span class="muted">'+t("eNone")+'</span>';
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
  // 5. 跨轮注入冷却（项目级）
  if(noProj){
    d.appendChild(prefsNoteCard(t("cTitle"), t("cDesc"), t("noProject")));
  } else if(PREFS.errs.retr){
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
      api("/api/retrieve", { method:"POST", body:{ project:PREFS.project, dedup_turns:v } }).then(()=>{
        PREFS.retr.dedup_turns = v; pSave("c");
      }).catch(err=>{ prefsErr.c = err.message; render(); });
    });
    d.appendChild(card);
  }
  // 6. 经验沉淀（项目级）：保存按钮收进「轮次间隔」行右端；模式+间隔合并判脏
  if(noProj){
    d.appendChild(prefsNoteCard(t("capTitle"), t("capDesc"), t("noProject")));
  } else if(PREFS.errs.cap){
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
      api("/api/capture", { method:"POST", body:{ project:PREFS.project,
        mode:PREFS_D.capMode, turn_interval:PREFS_D.capInterval } }).then(r=>{
        PREFS.cap.mode = r.mode; PREFS.cap.turn_interval = r.turn_interval; pSave("cap");
      }).catch(err=>{ prefsErr.cap = err.message; render(); });
    });
    d.appendChild(card);
  }
  // 7. 泛化门控（项目级）：单行布局（开关即开即存，短语表弹窗内编辑、确定即生效），无保存按钮
  if(noProj){
    d.appendChild(prefsNoteCard(t("gtTitle"), t("gtDesc"), t("noProject")));
  } else if(PREFS.errs.gate){
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
      api("/api/gate", { method:"POST", body:{ project:PREFS.project, enabled:want } }).then(ng=>{
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
  // 8. 规则配置（项目级）：保存按钮收进「添加规则」行右端；后端 400 信息经 prefsErr 展示
  if(noProj){
    d.appendChild(prefsNoteCard(t("rTitle"), t("rDesc"), t("noProject")));
  } else if(PREFS.errs.rules){
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
    api("/api/gate", { method:"POST", body:{ project:PREFS.project, extra:gateDraft } }).then(ng=>{
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
      state.menu=m.key; location.hash=m.key;
      // 跨页缓存联动（Task 4 评审约定）：切到其他页重拉项目列表缓存；切到管理页重拉树数据
      if(m.key==="misc" && MISC) refreshMisc();
      if(m.key==="manage" && MGMT) refreshManage();
      render();
    };
    side.appendChild(b);
  });
  main.appendChild(side);

  // 管理页（Task 5）、日志页（Task 3）、其他页（Task 4）与设置页（Task 6）已接入真实数据；
  // 引导页仍为占位：真实内容（agent 卡）按实施计划 Task 7 接入，占位文案走 i18n（notImpl 键）
  if(state.menu==="manage"){
    loadManage();
    main.appendChild(renderTree());
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
  if(state.menu==="prefs"){
    main.appendChild(renderPrefs());
    return;
  }
  const ph = el("div","placeholder");
  ph.textContent = "「"+t(state.menu)+"」"+t("notImpl");
  main.appendChild(ph);
}

/* 刷新恢复选中菜单：菜单点击时写入 location.hash，启动时读回（非法值回退 manage） */
(function(){
  const h = location.hash.replace(/^#/,"");
  if(MENUS.some(m=>m.key===h)) state.menu = h;
})();

render();

