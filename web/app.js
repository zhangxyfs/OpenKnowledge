"use strict";
/* OkManager 配置中心外壳：五菜单左右栏 + 顶栏（侧栏折叠 / 中英 / 昼夜）+ hash 路由 + 整页重渲滚动保持。
   规范源 docs/prototypes/prototype-manager-v2.html——I18N/ICON/MENUS/el/esc/t/state/render/renderBody/
   设置卡助手（pswitch/pcard/prow/pnumLive/ptext/pDirtyLive 等）均为原型平移；日志页（Task 3）与其他页
   （Task 4）已接真后端，其余三页挂占位，按 docs/superpowers/plans/2026-08-21-config-center-ui.md 的
   Task 5-7 逐页接入。 */

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
    res = await fetch(path, { method: opts.method || "GET", headers: headers, body: body });
  } catch(err){
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
    lTimeout:"生成超时（秒）", lTest:"测试连接", lTesting:"测试中…", lTestOk:"✓ 连通（231ms）",
    hTitle:"Hook 超时", hDesc:"写入各 agent hooks 的超时秒数。2026-08-04 曾发生 Windows 高负载下 5s 超时致 PostToolUse 整会话静默丢失，故默认 10", hSec:"超时（秒）",
    gtTitle:"泛化门控", gtDesc:"命中内置/自定义短语的泛化 prompt 跳过检索注入与 embed 调用",
    gtOn:"启用门控", gtStatus:"内置 21 条 · 自定义 {n} 条", gtManage:"管理短语表",
    gtBuiltin:"内置短语（只读，随版本演进）", gtCustom:"自定义短语", gtAdd:"+ 添加", gtPh:"新短语…", gtClose:"关闭",
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
    lTimeout:"Generation timeout (s)", lTest:"Test connection", lTesting:"Testing…", lTestOk:"✓ Connected (231ms)",
    hTitle:"Hook timeout", hDesc:"Timeout seconds written into each agent's hooks. On 2026-08-04 a 5s timeout under Windows load silently dropped PostToolUse for an entire session — hence default 10", hSec:"Timeout (s)",
    gtTitle:"Generalization gate", gtDesc:"Prompts matching builtin/custom phrases skip retrieval injection and embed calls",
    gtOn:"Enable gate", gtStatus:"21 builtin · {n} custom", gtManage:"Manage phrases",
    gtBuiltin:"Builtin phrases (read-only, evolve with releases)", gtCustom:"Custom phrases", gtAdd:"+ Add", gtPh:"New phrase…", gtClose:"Close",
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
                logSrc:{ ok:true, daemon:true, sidecar:true }, logSem:false, logQ:"",
                logAuto:true, logStick:true, miscFb:null };
const t = k => I18N[state.lang][k];

/* 设置卡脏态/已保存反馈（pcard/pDirtyLive/pSave 用；各页内容接入时复用） */
const prefsDirty = {}, prefsSaved = {};

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
  prefsDirty[k]=false; prefsSaved[k]=true; render();
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
    wrap.appendChild(sv);
    inlineRow.appendChild(wrap);
  } else {
    const f = el("div","pfoot");
    f.appendChild(sv);
    if(prefsSaved[key]) f.appendChild(Object.assign(el("span","fb2"),{textContent:t("saved")}));
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
    b.onclick = ()=>{ state.menu=m.key; location.hash=m.key; render(); };
    side.appendChild(b);
  });
  main.appendChild(side);

  // 日志页（Task 3）与其他页（Task 4）已接入真实数据；管理/引导/设置三页仍为占位：
  // 真实内容（管理树+详情 / 引导卡 / 设置卡）按实施计划 Task 5-7 逐页接入，占位文案走 i18n（notImpl 键）
  if(state.menu==="logs"){
    main.appendChild(renderLogs());
    return;
  }
  if(state.menu==="misc"){
    main.appendChild(renderMisc());
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
