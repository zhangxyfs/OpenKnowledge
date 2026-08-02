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
    sortDir: "desc", // 时间排序方向：desc 新→旧 / asc 旧→新
    page: 1,
    pageSize: 20,
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
      if (res.status === 204) return null;
      return res.json().then(function (data) {
        if (!res.ok) {
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
    loadEntries();
  }

  $("project-select").addEventListener("change", function () {
    state.project = this.value;
    state.page = 1;
    state.lastVersion = 0;
    loadEntries();
    runSearch();
    refreshCapture();
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
    state.lastVersion = 0; // 手动刷新后重新记录版本，避免下一次心跳重复拉取
    loadEntries();
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
      renderEntries();
    }).catch(function (err) { showError(err.message); });
  }

  function fmtTime(unix) {
    if (!unix) return "";
    var d = new Date(unix * 1000);
    var p = function (n) { return String(n).padStart(2, "0"); };
    return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) +
      " " + p(d.getHours()) + ":" + p(d.getMinutes());
  }

  function renderEntries() {
    var tbody = $("entries-body");
    tbody.innerHTML = "";
    // 类型过滤（draft 选项只看草稿）
    var list = state.entries.filter(function (e) {
      if (state.typeFilter === "draft") return e.draft;
      if (state.typeFilter) return e.type === state.typeFilter;
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
      tr.innerHTML =
        '<td class="muted">' + fmtTime(e.mtime) + "</td>" +
        "<td>" + esc(e.title) + (e.draft ? ' <span class="badge badge-draft">草稿</span>' : "") + "</td>" +
        "<td>" + esc(e.type) + "</td>" +
        "<td>" + esc((e.tags || []).join(", ")) + "</td>" +
        "<td>" + (e.mandatory ? "✓" : "") + "</td>" +
        "<td>" + esc(e.summary) + "</td>" +
        '<td class="ops">' +
        '<button type="button" data-act="view">查看</button> ' +
        '<button type="button" data-act="edit">编辑</button> ' +
        (e.draft ? '<button type="button" data-act="approve">采纳</button> ' : "") +
        '<button type="button" data-act="del" class="danger-link">删除</button>' +
        "</td>";
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
      $("capture-mode-note").textContent = c.mode === "auto"
        ? "auto 模式：每 " + c.turn_interval + " 个回合结束强制自省一次"
        : "propose 模式：由 AI 自主判断，无轮次限制";
    }).catch(function (err) { showError(err.message); });
  }

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
  });
})();
