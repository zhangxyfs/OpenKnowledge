/* OpenKnowledge GUI — 两选项卡 SPA（管理 / 引导），原生 JS，无外部依赖。 */
(function () {
  "use strict";

  var TOKEN = window.OK_TOKEN || "";
  var state = {
    status: null,
    project: "",
    entries: [],
    editingFile: null, // null=新建；否则为正在编辑的条目 file（只读模式也用同一表单）
    readOnly: false,
    hitFiles: null, // 搜索命中的条目 file 集合；null 表示无搜索高亮
    typeFilter: "", // "" = 全部；"draft" = 仅草稿；其余按条目类型过滤
    branchFilter: "", // "" = 全部；选中后 = born==X ∪ scope==X ∪ 无 born 无 scope 的条目
    sortDir: "desc", // 时间排序方向：desc 新→旧 / asc 旧→新
    page: 1,
    pageSize: 12,
    lastVersion: 0 // 最近一次自动刷新见到的 kb.db 版本（mtime）
  };
  state.agent = localStorage.getItem("ok.agent") || "";

  // ---------- 工具 ----------

  function $(id) { return document.getElementById(id); }

  function showError(msg) {
    $("banner-text").textContent = msg;
    $("banner").classList.remove("hidden");
  }
  $("banner-close").addEventListener("click", function () {
    $("banner").classList.add("hidden");
  });

  function api(path, opts) {
    opts = opts || {};
    opts.headers = Object.assign({ "X-Ok-Token": TOKEN }, opts.headers || {});
    if (opts.body && typeof opts.body !== "string") {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(opts.body);
    }
    return fetch(path, opts).then(function (res) {
      if (res.ok) sessionStorage.removeItem("ok-401-reload");
      if (res.status === 204) return null;
      return res.json().then(function (data) {
        if (!res.ok) {
          // daemon 被替换（多 exe 共存/重启）后页面 token 过期：自动刷新一次取新 token
          // （sessionStorage 标志防刷新循环；任一成功响应后清除，见上）
          if (res.status === 401 && !sessionStorage.getItem("ok-401-reload")) {
            sessionStorage.setItem("ok-401-reload", "1");
            showError("服务已重启，正在刷新…");
            setTimeout(function () { location.reload(); }, 800);
            return new Promise(function () {});
          }
          var err = new Error((data && data.error) || ("请求失败: " + res.status));
          err.status = res.status;
          throw err;
        }
        return data;
      });
    }).catch(function (err) {
      if (!err.status) throw new Error("网络错误: " + err.message);
      throw err;
    });
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // 条目类型显示名：存储值固定英文（frontmatter/API 不变），界面显示中文。
  var TYPE_LABELS = { rule: "规则", pitfall: "踩坑", note: "笔记", reference: "参考" };
  function typeLabel(t) { return TYPE_LABELS[t] || t || ""; }

  // ---------- 摘要浮窗（悬停显示完整内容） ----------

  var tipEl = null;
  function ensureTip() {
    if (!tipEl) {
      tipEl = document.createElement("div");
      tipEl.id = "summary-tip";
      tipEl.className = "hidden";
      document.body.appendChild(tipEl);
    }
    return tipEl;
  }
  function showTip(text, x, y) {
    if (!text) return;
    var tip = ensureTip();
    tip.textContent = text;
    tip.classList.remove("hidden");
    moveTip(x, y);
  }
  function moveTip(x, y) {
    if (!tipEl || tipEl.classList.contains("hidden")) return;
    var pad = 12;
    var left = x + 14, top = y + 16;
    // 溢出视口右侧/底部时翻到另一侧/贴底，保证完整可见
    if (left + tipEl.offsetWidth + pad > window.innerWidth) left = Math.max(pad, x - tipEl.offsetWidth - 14);
    if (top + tipEl.offsetHeight + pad > window.innerHeight) top = Math.max(pad, window.innerHeight - tipEl.offsetHeight - pad);
    tipEl.style.left = left + "px";
    tipEl.style.top = top + "px";
  }
  function hideTip() {
    if (tipEl) tipEl.classList.add("hidden");
  }
  // 滚动（含表格横滑容器）时收起浮窗，避免浮窗与行错位
  window.addEventListener("scroll", hideTip, true);

  // ---------- 选项卡 ----------

  function switchTab(name) {
    $("tab-manage").classList.toggle("active", name === "manage");
    $("tab-guide").classList.toggle("active", name === "guide");
    $("tab-misc").classList.toggle("active", name === "misc");
    $("page-manage").classList.toggle("hidden", name !== "manage");
    $("page-guide").classList.toggle("hidden", name !== "guide");
    $("page-misc").classList.toggle("hidden", name !== "misc");
  }
  $("tab-manage").addEventListener("click", function () { switchTab("manage"); });
  $("tab-guide").addEventListener("click", function () { switchTab("guide"); });
  $("tab-misc").addEventListener("click", function () { switchTab("misc"); });

  // ---------- 启动与状态 ----------

  function refreshStatus() {
    return api("/api/status").then(function (s) {
      state.status = s;
      renderGuide(s);
      $("misc-version").textContent = "OpenKnowledge v" + (s.app_version || "dev");
      $("misc-home").textContent = "知识库目录：" + (s.home || "");
      $("misc-project-count").textContent = "已注册项目：" + ((s.projects || []).length) + " 个";
      if (!s.projects || s.projects.length === 0) {
        // 首次运行：隐藏管理 tab，只展示引导页
        $("tab-manage").classList.add("hidden");
        switchTab("guide");
      } else {
        $("tab-manage").classList.remove("hidden");
        renderProjectSelect(s.projects);
        if ($("page-manage").classList.contains("hidden") &&
            $("page-guide").classList.contains("hidden")) {
          switchTab("manage");
        }
      }
      return s;
    }).catch(function (err) { showError(err.message); });
  }

  function renderProjectSelect(projects) {
    var sel = $("project-select");
    var prev = state.project;
    sel.innerHTML = "";
    projects.forEach(function (p) {
      var opt = document.createElement("option");
      opt.value = p.name;
      opt.textContent = p.name;
      sel.appendChild(opt);
    });
    var names = projects.map(function (p) { return p.name; });
    state.project = names.indexOf(prev) >= 0 ? prev : names[0];
    sel.value = state.project;
    // "其他"页的导出项目下拉：与管理页项目列表保持同步
    var exp = $("misc-export-project");
    if (exp) {
      exp.innerHTML = "";
      var all = document.createElement("option");
      all.value = "all";
      all.textContent = "全部项目";
      exp.appendChild(all);
      (projects || []).forEach(function (p) {
        var o = document.createElement("option");
        o.value = p.name;
        o.textContent = p.name;
        exp.appendChild(o);
      });
    }
    // 删除项目卡下拉：与管理页项目列表同序同步（无"全部"项）
    var del = $("del-project-select");
    if (del) {
      del.innerHTML = "";
      (projects || []).forEach(function (p) {
        var o = document.createElement("option");
        o.value = p.name;
        o.textContent = p.name;
        del.appendChild(o);
      });
    }
    loadEntries();
  }

  $("project-select").addEventListener("change", function () {
    state.project = this.value;
    state.page = 1;
    state.lastVersion = 0;
    renderBranchFilter(); // 项目切换：先按现有条目重聚合分支选项（loadEntries 完成后会再次聚合）
    renderBranchInfo(); // 分支上下文随项目联动（loadEntries 完成后会再刷一次）
    loadEntries();
    runSearch();
    refreshCapture();
  });

  $("branch-filter").addEventListener("change", function () {
    state.branchFilter = this.value;
    state.page = 1;
    renderEntries();
  });

  $("type-filter").addEventListener("change", function () {
    state.typeFilter = this.value;
    state.page = 1;
    renderEntries();
  });

  $("th-time").addEventListener("click", function () {
    state.sortDir = state.sortDir === "desc" ? "asc" : "desc";
    $("time-arrow").textContent = state.sortDir === "desc" ? "↓" : "↑";
    renderEntries();
  });

  $("btn-refresh").addEventListener("click", function () {
    var btn = this;
    if (btn.disabled) return;
    btn.disabled = true;
    btn.textContent = "刷新中…";
    state.lastVersion = 0; // 手动刷新后重新记录版本，避免下一次心跳重复拉取
    // 全量刷新：状态（含项目下拉/排序）+ 条目列表（renderProjectSelect 内部触发 loadEntries）；
    // refreshStatus 内部已 catch（错误走横幅），then 必定到达，按钮态必恢复
    refreshStatus().then(function () {
      btn.textContent = "已刷新 ✓";
      setTimeout(function () { btn.disabled = false; btn.textContent = "刷新"; }, 1200);
    });
  });

  $("btn-prev").addEventListener("click", function () {
    if (state.page > 1) { state.page--; renderEntries(); }
  });
  $("btn-next").addEventListener("click", function () {
    state.page++;
    renderEntries();
  });

  // ---------- 管理页：条目列表 ----------

  function loadEntries() {
    if (!state.project) return;
    api("/api/entries?project=" + encodeURIComponent(state.project)).then(function (list) {
      state.entries = list || [];
      renderBranchFilter(); // 分支选项随条目（含项目切换）重新聚合
      renderEntries();
      renderBranchInfo(); // 分支上下文/谱系随条目加载完成刷新
    }).catch(function (err) { showError(err.message); });
  }

  function fmtTime(unix) {
    if (!unix) return "";
    var d = new Date(unix * 1000);
    var p = function (n) { return String(n).padStart(2, "0"); };
    return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) +
      " " + p(d.getHours()) + ":" + p(d.getMinutes());
  }

  // entryBranch 提取条目的分支标签（branch:<名>，第一个）；无则空串
  function entryBranch(e) {
    var tags = e.tags || [];
    for (var i = 0; i < tags.length; i++) {
      if (tags[i].indexOf("branch:") === 0) return tags[i].slice(7);
    }
    return "";
  }

  // bornOf 取条目出生分支（born:<名>，第一个）；无则空串
  function bornOf(e) {
    var tags = e.tags || [];
    for (var i = 0; i < tags.length; i++) {
      if (tags[i].indexOf("born:") === 0) return tags[i].slice(5);
    }
    return "";
  }

  // renderBranchInfo 拉取并渲染分支上下文与合并谱系（随项目联动）
  function renderBranchInfo() {
    var el = $("branch-context");
    if (!el || !state.project) return;
    api("/api/project/branch-info?project=" + encodeURIComponent(state.project)).then(function (info) {
      var base = info.base_branch || "—";
      var cur = info.current_branch || "—";
      el.innerHTML = "";
      var b = document.createElement("span");
      b.textContent = "基准分支: " + base + " · 当前分支: ";
      var c = document.createElement("span");
      c.textContent = cur;
      if (info.base_branch && info.current_branch && info.base_branch !== info.current_branch) {
        c.className = "branch-warn";
      }
      el.appendChild(b); el.appendChild(c);
      var lineage = $("merge-lineage");
      var ms = info.merges || [];
      if (ms.length > 0) {
        var last = ms[ms.length - 1];
        lineage.textContent = "合并谱系: " + last.from + " → " + last.to +
          "（" + String(last.time || "").slice(0, 10) + "，共 " + ms.length + " 条）";
        lineage.classList.remove("hidden");
      } else {
        lineage.classList.add("hidden");
      }
    }).catch(function () {});
  }

  // renderBranchFilter 按当前项目条目聚合分支选项（born ∪ scope；项目切换时重聚合，联动）
  function renderBranchFilter() {
    var sel = $("branch-filter");
    if (!sel) return;
    var seen = {};
    (state.entries || []).forEach(function (e) {
      var b = entryBranch(e);
      if (b) seen[b] = true;
      var bo = bornOf(e);
      if (bo) seen[bo] = true;
    });
    var cur = state.branchFilter || "";
    sel.innerHTML = '<option value="">全部</option>';
    Object.keys(seen).sort().forEach(function (b) {
      var o = document.createElement("option");
      o.value = b;
      o.textContent = b;
      sel.appendChild(o);
    });
    if (cur && seen[cur]) sel.value = cur; else { state.branchFilter = ""; sel.value = ""; }
  }

  function renderEntries() {
    var tbody = $("entries-body");
    tbody.innerHTML = "";
    // 类型过滤（draft 选项只看草稿）+ 分支过滤（选中 X = born==X ∪ scope==X ∪ 无 born 无 scope）
    var list = state.entries.filter(function (e) {
      if (state.typeFilter === "draft" && !e.draft) return false;
      if (state.typeFilter && state.typeFilter !== "draft" && e.type !== state.typeFilter) return false;
      if (state.branchFilter) {
        var bo = bornOf(e), sc = entryBranch(e);
        if (bo !== state.branchFilter && sc !== state.branchFilter && (bo !== "" || sc !== "")) return false;
      }
      return true;
    });
    $("entries-empty").classList.toggle("hidden", list.length > 0);
    // 默认按时间排序（方向见 state.sortDir）；搜索命中时命中项置顶（组内仍按时间排）
    list = list.slice().sort(function (a, b) {
      var ha = state.hitFiles && state.hitFiles[a.file] ? 1 : 0;
      var hb = state.hitFiles && state.hitFiles[b.file] ? 1 : 0;
      if (ha !== hb) return hb - ha;
      var d = (b.mtime || 0) - (a.mtime || 0);
      return state.sortDir === "asc" ? -d : d;
    });
    // 分页
    var total = list.length;
    var pages = Math.max(1, Math.ceil(total / state.pageSize));
    if (state.page > pages) state.page = pages;
    var start = (state.page - 1) * state.pageSize;
    var pageItems = list.slice(start, start + state.pageSize);
    $("entries-total").textContent = "共 " + total + " 条";
    $("entries-page").textContent = "第 " + state.page + " / " + pages + " 页";
    $("btn-prev").disabled = state.page <= 1;
    $("btn-next").disabled = state.page >= pages;
    pageItems.forEach(function (e) {
      var tr = document.createElement("tr");
      if (state.hitFiles && state.hitFiles[e.file]) tr.classList.add("hit-row");
      // 分支格双徽标：born（出生分支，溯源）+ scope（branch: 作用域标签）
      var born = bornOf(e), scope = entryBranch(e);
      var branchCell = "";
      if (born) branchCell += '<span class="badge badge-born">⎇ ' + esc(born) + "</span> ";
      if (scope) branchCell += '<span class="badge badge-branch">⇢ ' + esc(scope) + "</span>";
      tr.innerHTML =
        '<td class="muted">' + fmtTime(e.mtime) + "</td>" +
        '<td>' + branchCell + "</td>" +
        "<td>" + esc(e.title) + (e.draft ? ' <span class="badge badge-draft">草稿</span>' : "") + "</td>" +
        "<td>" + esc(typeLabel(e.type)) + "</td>" +
        "<td>" + esc((e.tags || []).join(", ")) + "</td>" +
        "<td>" + (e.mandatory ? "✓" : "") + "</td>" +
        '<td><span class="summary-clip">' + esc(e.summary) + "</span></td>" +
        '<td class="ops">' +
        '<button type="button" data-act="view">查看</button> ' +
        '<button type="button" data-act="edit">编辑</button> ' +
        (e.draft ? '<button type="button" data-act="approve">采纳</button> ' : "") +
        '<button type="button" data-act="del" class="danger-link">删除</button>' +
        "</td>";
      var sumEl = tr.querySelector(".summary-clip");
      sumEl.addEventListener("mouseenter", function (ev) { showTip(e.summary, ev.clientX, ev.clientY); });
      sumEl.addEventListener("mousemove", function (ev) { moveTip(ev.clientX, ev.clientY); });
      sumEl.addEventListener("mouseleave", hideTip);
      tr.querySelectorAll("button").forEach(function (btn) {
        btn.addEventListener("click", function () {
          var act = btn.getAttribute("data-act");
          if (act === "view") openForm(e, true);
          else if (act === "edit") openForm(e, false);
          else if (act === "approve") approveEntry(e);
          else delEntry(e);
        });
      });
      tbody.appendChild(tr);
    });
  }

  // ---------- 管理页：搜索（300ms 防抖） ----------

  var searchTimer = null;
  var searchSeq = 0; // 单调递增请求序号，用于丢弃过期响应
  $("search-input").addEventListener("input", function () {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(runSearch, 300);
  });

  function runSearch() {
    var q = $("search-input").value.trim();
    var box = $("search-results");
    var seq = ++searchSeq;
    state.page = 1; // 新搜索回到第一页
    if (!q || !state.project) {
      box.classList.add("hidden");
      box.innerHTML = "";
      state.hitFiles = null;
      renderEntries();
      return;
    }
    api("/api/search?project=" + encodeURIComponent(state.project) +
        "&q=" + encodeURIComponent(q)).then(function (hits) {
      // 竞态防护：响应到达时若已有更新的请求或输入已被清空/修改，丢弃本次结果
      if (seq !== searchSeq || $("search-input").value.trim() !== q) return;
      if (!hits || hits.length === 0) {
        box.innerHTML = '<span class="muted">无匹配结果</span>';
      } else {
        box.innerHTML = hits.map(function (h) {
          return '<div class="hit"><span class="hit-title">' + esc(h.title) +
            '</span> <span class="hit-score">' + Number(h.score).toFixed(3) + "</span></div>";
        }).join("");
      }
      box.classList.remove("hidden");
      // 命中条目在表格中高亮并置顶
      var files = {};
      (hits || []).forEach(function (h) { files[h.file] = true; });
      state.hitFiles = files;
      renderEntries();
    }).catch(function (err) { showError(err.message); });
  }

  // ---------- 管理页：条目表单（新建 / 查看 / 编辑） ----------

  function openForm(entry, readOnly) {
    state.readOnly = !!readOnly;
    state.editingFile = entry ? entry.file : null;
    $("entry-modal-title").textContent = entry ? (readOnly ? "查看条目" : "编辑条目") : "新建条目";
    var fill = function (d) {
      $("f-title").value = d.title || "";
      $("f-type").value = d.type || "note";
      $("f-tags").value = (d.tags || []).join(", ");
      $("f-mandatory").checked = !!d.mandatory;
      $("f-summary").value = d.summary || "";
      $("f-body").value = d.body || "";
      setFormReadOnly(state.readOnly);
      $("entry-modal").classList.remove("hidden");
    };
    if (entry) {
      api("/api/entry?project=" + encodeURIComponent(state.project) +
          "&file=" + encodeURIComponent(entry.file)).then(fill)
        .catch(function (err) { showError(err.message); });
    } else {
      fill({});
    }
  }

  function setFormReadOnly(ro) {
    ["f-title", "f-type", "f-tags", "f-summary", "f-body"].forEach(function (id) {
      $(id).disabled = ro;
    });
    $("f-mandatory").disabled = ro;
    $("f-save").classList.toggle("hidden", ro);
  }

  function closeForm() { $("entry-modal").classList.add("hidden"); }

  // ---------- 更新日志 ----------

  // renderMd 极简 markdown 渲染：# / ## 标题、- 列表、**粗体**、`行内代码`；先 esc 转义防注入。
  function renderMd(md) {
    function inline(s) {
      return esc(s)
        .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
        .replace(/`([^`]+)`/g, "<code>$1</code>");
    }
    var html = [];
    var inList = false;
    function closeList() { if (inList) { html.push("</ul>"); inList = false; } }
    md.split(/\r?\n/).forEach(function (line) {
      var t = line.trim();
      if (t.indexOf("## ") === 0) { closeList(); html.push("<h4>" + inline(t.slice(3)) + "</h4>"); }
      else if (t.indexOf("# ") === 0) { closeList(); html.push("<h3>" + inline(t.slice(2)) + "</h3>"); }
      else if (t.indexOf("- ") === 0) { if (!inList) { html.push("<ul>"); inList = true; } html.push("<li>" + inline(t.slice(2)) + "</li>"); }
      else if (t === "") { closeList(); }
      else { closeList(); html.push("<p>" + inline(t) + "</p>"); }
    });
    closeList();
    return html.join("");
  }

  // changelogFromPending 标记当前弹窗是否由升级弹窗（pending）打开：仅此时关闭才 POST seen；常驻入口只查看、不影响 seen 状态。
  var changelogFromPending = false;

  function openChangelogModal(title, entries) {
    $("changelog-modal-title").textContent = title;
    var content = $("changelog-content");
    if (!entries || entries.length === 0) {
      content.innerHTML = '<p class="muted">暂无更新日志</p>';
    } else {
      // 最新版本在最前（API 返回升序，展示层翻转为降序；标题取 latest 的逻辑不受影响）
      content.innerHTML = entries.slice().reverse().map(function (e) { return renderMd(e.log); }).join("<hr>");
    }
    $("changelog-modal").classList.remove("hidden");
  }

  // checkChangelog 启动时拉取：pending 非空弹升级日志；结果缓存供常驻入口使用。
  function checkChangelog() {
    api("/api/changelog").then(function (c) {
      state.changelog = c;
      if (c.pending && c.pending.length > 0) {
        var latest = c.pending[c.pending.length - 1].version;
        var title = c.pending.length > 1
          ? ("已更新到 v" + latest + "（含最近 " + c.pending.length + " 个版本）")
          : ("新版本 v" + latest + " 更新内容");
        changelogFromPending = true;
        openChangelogModal(title, c.pending);
      }
    }).catch(function () { /* 拉取失败不阻断主界面 */ });
  }

  $("changelog-close").addEventListener("click", function () {
    $("changelog-modal").classList.add("hidden");
    if (!changelogFromPending) return;
    changelogFromPending = false;
    api("/api/changelog/seen", { method: "POST" }).catch(function (err) { showError(err.message); });
  });

  $("btn-changelog").addEventListener("click", function () {
    changelogFromPending = false;
    openChangelogModal("更新日志", state.changelog ? state.changelog.all : null);
  });

  // 使用帮助：拉取 help.md 复用 changelog 弹窗渲染；不属于升级弹窗，不影响 seen
  $("btn-help").addEventListener("click", function () {
    changelogFromPending = false;
    fetch("/help.md").then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.text();
    }).then(function (md) {
      openChangelogModal("使用帮助", [{ log: md }]);
    }).catch(function () {
      openChangelogModal("使用帮助", [{ log: "帮助文档加载失败，请检查安装是否完整。" }]);
    });
  });

  $("f-cancel").addEventListener("click", closeForm);
  $("btn-new").addEventListener("click", function () { openForm(null, false); });

  $("entry-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    var tags = $("f-tags").value.split(",").map(function (t) { return t.trim(); })
      .filter(function (t) { return t; });
    var payload = {
      project: state.project,
      title: $("f-title").value,
      type: $("f-type").value,
      tags: tags,
      mandatory: $("f-mandatory").checked,
      summary: $("f-summary").value,
      body: $("f-body").value
    };
    var req;
    if (state.editingFile) {
      payload.file = state.editingFile; // 以原文件名作为身份，不支持改名
      req = api("/api/entry", { method: "PUT", body: payload });
    } else {
      req = api("/api/entry", { method: "POST", body: payload });
    }
    req.then(function () {
      closeForm();
      loadEntries();
    }).catch(function (err) {
      showError(err.status === 409 ? "条目已存在" : err.message);
    });
  });

  function delEntry(e) {
    if (!confirm("确定删除条目「" + e.title + "」？")) return;
    api("/api/entry?project=" + encodeURIComponent(state.project) +
        "&file=" + encodeURIComponent(e.file), { method: "DELETE" })
      .then(loadEntries)
      .catch(function (err) { showError(err.message); });
  }

  // 采纳草稿：draft 翻正并同步索引与向量，随后刷新列表
  function approveEntry(e) {
    api("/api/approve", {
      method: "POST",
      body: { project: state.project, file: e.file }
    }).then(loadEntries)
      .catch(function (err) { showError(err.message); });
  }

  // ---------- 引导页 ----------

  function setBadge(id, ok, okText, offText) {
    var el = $(id);
    el.textContent = ok ? okText : offText;
    el.classList.toggle("badge-on", ok);
    el.classList.toggle("badge-off", !ok);
  }

  function currentAgent() {
    var agents = (state.status && state.status.agents) || [];
    for (var i = 0; i < agents.length; i++) {
      if (agents[i].id === state.agent) return agents[i];
    }
    return null;
  }

  function renderAgentSelect(agents) {
    var sel = $("agent-select");
    sel.innerHTML = "";
    agents.forEach(function (a) {
      var opt = document.createElement("option");
      opt.value = a.id;
      opt.textContent = a.name + (a.detected ? "" : "（未安装）");
      opt.disabled = !a.detected;
      sel.appendChild(opt);
    });
    var ids = agents.map(function (a) { return a.id; });
    if (ids.indexOf(state.agent) < 0) {
      var first = agents.filter(function (a) { return a.detected; })[0] || agents[0];
      state.agent = first ? first.id : "";
    }
    sel.value = state.agent;
  }

  function renderGuide(s) {
    var agents = s.agents || [];
    renderAgentSelect(agents);
    var cur = currentAgent();
    setBadge("badge-hooks", !!(cur && cur.hooksInstalled), "已安装", "未配置");
    $("hooks-agent-name").textContent = cur ? cur.name : "agent";
    $("btn-hooks").textContent = cur ? ("写入 " + cur.name + " hooks 配置") : "写入 hooks 配置";
    $("hooks-timeout").value = s.hooksTimeout || 10;
    renderRxEnforce();
    setBadge("badge-skills", s.skillsInstalled, "已安装", "未配置");
    setBadge("badge-embedding", s.embeddingConfigured, "已配置", "未配置");
    setBadge("badge-toggle", !s.disabled, "已开启", "已关闭");
    $("btn-toggle").textContent = s.disabled ? "开启" : "关闭";
    if (s.embedding) {
      if (s.embedding.base_url) $("emb-base-url").value = s.embedding.base_url;
      if (s.embedding.model) $("emb-model").value = s.embedding.model;
      $("emb-api-key").placeholder = s.embedding.has_key ? "已保存（留空保持不变）" : "api_key";
    }
    refreshCapture();
  }

  // renderRxEnforce 三档卡仅 agent=reasonix 时显示，并回填当前保存的档位。
  // 由 renderGuide 调用（状态刷新与 agent 下拉切换都会经过 renderGuide）。
  function renderRxEnforce() {
    var card = $("rx-enforce-card");
    if (!card) return;
    var isRx = state.agent === "reasonix";
    card.classList.toggle("hidden", !isRx);
    if (isRx) {
      var mode = (state.status && state.status.rxEnforceMode) || "mixed";
      var radios = document.getElementsByName("rx-enforce");
      for (var i = 0; i < radios.length; i++) {
        radios[i].checked = radios[i].value === mode;
      }
    }
  }

  // ---------- 引导页：经验沉淀卡片 ----------

  // captureProject 取管理页当前选中项目，缺省取第一个项目。
  function captureProject() {
    if (state.project) return state.project;
    var ps = state.status && state.status.projects;
    return ps && ps.length > 0 ? ps[0].name : "";
  }

  function refreshCapture() {
    var project = captureProject();
    var statusEl = $("capture-status");
    if (!project) {
      statusEl.textContent = "当前模式：尚无已注册项目（先 ok init）";
      return;
    }
    api("/api/capture?project=" + encodeURIComponent(project)).then(function (c) {
      statusEl.textContent = "当前模式：" + c.mode +
        "（turn_interval=" + c.turn_interval + "，项目 " + project + "）";
      $("capture-interval").value = c.turn_interval;
      $("auto-born").checked = !!c.auto_born;
      $("capture-mode-note").textContent = c.mode === "auto"
        ? "auto 模式：每 " + c.turn_interval + " 个回合结束强制自省一次"
        : "propose 模式：由 AI 自主判断，无轮次限制";
    }).catch(function (err) { showError(err.message); });
  }

  // provenance 开关：勾选变更即随 capture 保存路径落盘（nil=不变，显式布尔才改写）
  $("auto-born").addEventListener("change", function () {
    var project = captureProject();
    if (!project) {
      showError("尚无已注册项目，请先 ok init");
      this.checked = !this.checked;
      return;
    }
    api("/api/capture", {
      method: "POST",
      body: { project: project, auto_born: this.checked }
    }).then(refreshCapture)
      .catch(function (err) { showError(err.message); refreshCapture(); });
  });

  function setCaptureMode(mode) {
    var project = captureProject();
    if (!project) {
      showError("尚无已注册项目，请先 ok init");
      return;
    }
    api("/api/capture", {
      method: "POST",
      body: { project: project, mode: mode }
    }).then(refreshCapture)
      .catch(function (err) { showError(err.message); });
  }

  $("btn-capture-propose").addEventListener("click", function () { setCaptureMode("propose"); });
  $("btn-capture-auto").addEventListener("click", function () { setCaptureMode("auto"); });

  $("btn-capture-interval").addEventListener("click", function () {
    var project = captureProject();
    if (!project) {
      showError("尚无已注册项目，请先 ok init");
      return;
    }
    var n = parseInt($("capture-interval").value, 10);
    if (!n || n < 1 || n > 100) {
      showError("轮次间隔必须是 1~100 的整数");
      return;
    }
    api("/api/capture", {
      method: "POST",
      body: { project: project, turn_interval: n }
    }).then(refreshCapture)
      .catch(function (err) { showError(err.message); });
  });

  $("agent-select").addEventListener("change", function () {
    state.agent = this.value;
    localStorage.setItem("ok.agent", state.agent);
    if (state.status) renderGuide(state.status);
  });

  $("btn-hooks").addEventListener("click", function () {
    api("/api/setup/hooks", { method: "POST", body: { agent: state.agent } })
      .then(function () { refreshStatus(); })
      .catch(function (err) { showError(err.message); });
  });

  // hook 超时为全局统一设置：保存后对所有已检测 agent 重写 hooks（不传 agent）。
  $("btn-hooks-timeout").addEventListener("click", function () {
    var timeout = parseInt($("hooks-timeout").value, 10);
    if (!timeout || timeout < 1 || timeout > 60) {
      showError("hook 超时必须是 1~60 的整数");
      return;
    }
    api("/api/setup/hooks", { method: "POST", body: { timeout_sec: timeout } })
      .then(function () { refreshStatus(); })
      .catch(function (err) { showError(err.message); });
  });

  // reasonix 三档：radio 变更即保存（sidecar 实时读配置，即时生效）；
  // 失败时回退 radio 到已保存档位。
  document.getElementsByName("rx-enforce").forEach(function (r) {
    r.addEventListener("change", function () {
      api("/api/reasonix/enforce-mode", { method: "POST", body: { mode: r.value } })
        .then(function () {
          if (state.status) state.status.rxEnforceMode = r.value;
        })
        .catch(function (err) {
          showError("强制检查方式保存失败：" + err.message);
          renderRxEnforce();
        });
    });
  });

  $("btn-skills").addEventListener("click", function () {
    api("/api/setup/skills", { method: "POST" })
      .then(function () { refreshStatus(); })
      .catch(function (err) { showError(err.message); });
  });

  $("btn-embedding").addEventListener("click", function () {
    var out = $("emb-result");
    out.textContent = "保存并测试中…";
    api("/api/setup/embedding", {
      method: "POST",
      body: {
        base_url: $("emb-base-url").value.trim(),
        model: $("emb-model").value.trim(),
        api_key: $("emb-api-key").value
      }
    }).then(function (res) {
      out.textContent = res.ok ? "连通性验证通过" : ("验证失败: " + (res.error || "未知错误"));
      refreshStatus();
    }).catch(function (err) {
      out.textContent = "";
      showError(err.message);
    });
  });

  $("btn-toggle").addEventListener("click", function () {
    var on = state.status ? state.status.disabled : false;
    api("/api/toggle", { method: "POST", body: { on: on } })
      .then(function () { refreshStatus(); })
      .catch(function (err) { showError(err.message); });
  });

  $("btn-uninstall").addEventListener("click", function () {
    if (!confirm("确认卸载？将移除 hooks 配置、技能与全局 embedding 配置。\n知识库数据（条目、索引、注册表）全部保留。")) return;
    api("/api/uninstall", { method: "POST" })
      .then(function (r) {
        showError("已卸载：hooks " + (r.hooks_removed ? "已移除" : "无") +
          "，技能移除 " + r.skills_removed + " 个，embedding " +
          (r.embedding_removed ? "已移除" : "无") + "。知识库数据已保留。");
        refreshStatus();
      })
      .catch(function (err) { showError(err.message); });
  });

  // ---------- 其他页：导出 / 导入 ----------

  $("btn-export").addEventListener("click", function () {
    var p = $("misc-export-project").value || "all"; // 无项目时下拉为空，回退到全部
    fetch("/api/export?project=" + encodeURIComponent(p), {
      headers: { "X-Ok-Token": TOKEN },
    }).then(function (res) {
      if (!res.ok) {
        return res.json().catch(function () { return {}; }).then(function (e) {
          showError("导出失败：" + (e.error || res.status));
        });
      }
      return res.blob().then(function (blob) {
        var a = document.createElement("a");
        a.href = URL.createObjectURL(blob);
        a.download = "openknowledge-backup-" + p + ".zip";
        a.click();
        URL.revokeObjectURL(a.href);
      });
    }).catch(function (err) { showError("网络错误: " + err.message); });
  });

  $("btn-import").addEventListener("click", function () {
    var out = $("misc-import-result");
    var f = $("misc-import-file").files[0];
    if (!f) {
      out.textContent = "请先选择 zip 文件";
      return;
    }
    var fd = new FormData();
    fd.append("file", f);
    fetch("/api/import", { method: "POST", headers: { "X-Ok-Token": TOKEN }, body: fd })
      .then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (data) {
          if (!res.ok) {
            out.textContent = "导入失败：" + (data.error || res.status);
            return;
          }
          out.textContent = "导入 " + data.imported + " 条，跳过 " + data.skipped +
            " 条（格式损坏），涉及项目：" + (data.projects || []).join("、") + "；同名条目已覆盖";
          state.lastVersion = 0;
          refreshStatus();
          loadEntries();
        });
      }).catch(function (err) { showError("网络错误: " + err.message); });
  });

  // ---------- 其他页：删除项目知识库（三重确认：备份可选 + 勾选了解 + 输入项目名） ----------

  var delTarget = ""; // 当前弹窗要删的项目名

  function updateDelConfirm() {
    var ok = $("del-ack").checked && $("del-name").value.trim() === delTarget && delTarget !== "";
    $("btn-del-confirm").disabled = !ok;
  }

  $("btn-del-project").addEventListener("click", function () {
    delTarget = $("del-project-select").value || "";
    if (!delTarget) return;
    $("del-impact").textContent = "正在统计条目…";
    $("del-backup").checked = true;
    $("del-ack").checked = false;
    $("del-name").value = "";
    $("del-name-hint").firstChild.textContent = "请输入完整项目名 " + delTarget + " 以确认 ";
    $("btn-del-confirm").textContent = "永久删除";
    $("del-modal").classList.remove("hidden");
    updateDelConfirm();
    // 影响面统计失败不阻塞确认流程
    api("/api/entries?project=" + encodeURIComponent(delTarget)).then(function (list) {
      list = list || [];
      var drafts = list.filter(function (e) { return e.draft; }).length;
      $("del-impact").textContent = "将永久删除项目「" + delTarget + "」的知识库：共 " +
        list.length + " 条知识（含 " + drafts + " 条草稿）、索引与项目配置，并注销注册表" +
        "（hooks 不再注入）。项目源码目录不受影响。";
    }).catch(function () {
      $("del-impact").textContent = "条目统计失败（不影响删除操作）。将删除项目「" + delTarget +
        "」的知识库、索引与项目配置，并注销注册表。项目源码目录不受影响。";
    });
  });

  ["del-ack", "del-name"].forEach(function (id) {
    $(id).addEventListener("input", updateDelConfirm);
    $(id).addEventListener("change", updateDelConfirm);
  });
  $("btn-del-cancel").addEventListener("click", function () {
    $("del-modal").classList.add("hidden");
  });

  $("btn-del-confirm").addEventListener("click", function () {
    var btn = this;
    if (btn.disabled) return;
    btn.disabled = true;
    btn.textContent = "删除中…";
    var name = delTarget;
    // 可选备份：复用导出卡的 blob 下载写法；导出失败中止删除
    var backup = Promise.resolve();
    if ($("del-backup").checked) {
      backup = fetch("/api/export?project=" + encodeURIComponent(name), {
        headers: { "X-Ok-Token": TOKEN },
      }).then(function (res) {
        if (!res.ok) throw new Error("备份导出失败（" + res.status + "），已中止删除");
        return res.blob().then(function (blob) {
          var a = document.createElement("a");
          a.href = URL.createObjectURL(blob);
          a.download = "openknowledge-backup-" + name + ".zip";
          a.click();
          URL.revokeObjectURL(a.href);
        });
      });
    }
    backup.then(function () {
      return api("/api/project?project=" + encodeURIComponent(name), { method: "DELETE" });
    }).then(function (res) {
      $("del-modal").classList.add("hidden");
      state.lastVersion = 0;
      if (state.project === name) state.project = ""; // 删的是当前选中项目：先清空，避免 refreshCapture 拿着已删项目名 404 误报
      refreshStatus();
      if (res && res.warning) {
        showError("项目已注销，但" + res.warning + "，请手动清理 " + (res.dir || ""));
      }
    }).catch(function (err) {
      showError(err.message);
    }).then(function () {
      btn.disabled = false;
      btn.textContent = "永久删除";
      updateDelConfirm();
    });
  });

  // ---------- 心跳（5s 轮询，变更才重拉；进程由 daemon 托管，页面关闭不退出） ----------

  setInterval(function () {
    // 心跳顺带做"变更才重拉"的自动刷新：版本（kb.db mtime）变化才重载列表
    var url = "/api/heartbeat" + (state.project ? "?project=" + encodeURIComponent(state.project) : "");
    api(url, { method: "POST" }).then(function (res) {
      var v = res && res.version ? res.version : 0;
      if (state.lastVersion !== 0 && v !== 0 && v !== state.lastVersion) {
        loadEntries(); // 数据库有变更，自动刷新（仅此时才重拉）
      }
      if (v !== 0) state.lastVersion = v;
    }).catch(function () { /* 心跳失败不打扰用户 */ });
  }, 5000);

  // ---------- 启动 ----------

  refreshStatus().then(function () {
    if (state.status && state.status.projects && state.status.projects.length > 0) {
      switchTab("manage");
    }
  }).then(checkChangelog);
})();
