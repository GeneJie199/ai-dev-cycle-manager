"use strict";

const $ = (selector) => document.querySelector(selector);
const esc = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
const fmtTime = (value) => { const parsed = new Date(value); return Number.isNaN(parsed.valueOf()) ? "-" : parsed.toLocaleString("zh-CN", { hour12: false }); };
const fmtDuration = (milliseconds) => {
  const seconds = Math.max(0, Math.round(Number(milliseconds || 0) / 1000));
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes} 分 ${seconds % 60} 秒`;
};
const empty = (message) => `<div class="empty">${esc(message)}</div>`;
const loadingHTML = '<div class="empty loading">加载中…</div>';

const state = {
  view: "workbench",
  workbenchMode: "plan",
  reqId: "",
  requirements: [],
  repos: [],
  gitRepoPath: "",
	gitStructured: null,
	aiProviders: [],
	aiPlan: null,
  rcData: null,
  reportData: null,
  criteria: [],
  openLogs: new Set(),
  pollTimer: null,
};
const taskStatus = { todo: "待办", in_progress: "进行中", done: "已完成" };
const evidenceStatus = { passed: "通过", failed: "未通过", informational: "参考" };
let toastTimer;

async function api(path, options) {
  const response = await fetch(path, options ? { method: options.method || "POST", headers: { "Content-Type": "application/json" }, body: options.body === undefined ? undefined : JSON.stringify(options.body) } : { cache: "no-store" });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
  if (!response.ok) throw new Error(data?.error || `HTTP ${response.status}`);
  return data;
}

function toast(message, isError = false) {
  const target = $("#toast");
  target.textContent = message;
  target.className = isError ? "error" : "";
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => target.classList.add("hidden"), 3500);
}

async function busy(button, action) {
  if (!button) return action();
  const text = button.textContent;
  button.disabled = true;
  button.textContent = "处理中";
  try { return await action(); } finally { button.disabled = false; button.textContent = text; }
}

// 渲染带重试按钮的错误态
function renderError(el, message, retry) {
  el.innerHTML = `<div class="empty error-box"><span>${esc(message)}</span><button type="button" class="small" data-retry>重试</button></div>`;
  el.querySelector("[data-retry]").addEventListener("click", retry);
}

function downloadJSON(data, filename) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2) + "\n"], { type: "application/json" }));
  const anchor = document.createElement("a"); anchor.href = url; anchor.download = filename; anchor.click(); URL.revokeObjectURL(url);
}

// --- 路由（hash 深链：#/workbench/<reqId>、#/git、#/repos） ---

function currentRoute() {
  const parts = (location.hash || "#/workbench").replace(/^#\/?/, "").split("/").filter(Boolean);
  const view = ["workbench", "git", "repos"].includes(parts[0]) ? parts[0] : "workbench";
  const mode = ["plan", "verify", "release"].includes(parts[2]) ? parts[2] : "plan";
  return { view, reqId: view === "workbench" ? (parts[1] || "") : "", mode };
}

function route() {
  const { view, reqId, mode } = currentRoute();
  const sameWorkbench = state.view === "workbench" && view === "workbench" && state.reqId === reqId && !$("#req-content").classList.contains("hidden");
  state.view = view;
  state.workbenchMode = mode;
  document.querySelectorAll("#mainnav a").forEach((link) => {
    const active = link.dataset.nav === view;
    link.classList.toggle("active", active);
    if (active) link.setAttribute("aria-current", "page"); else link.removeAttribute("aria-current");
  });
  document.querySelectorAll(".view").forEach((item) => item.classList.toggle("active", item.id === `view-${view}`));
  if (view === "workbench" && sameWorkbench) applyWorkbenchMode();
  else if (view === "workbench") loadWorkbench(reqId).catch((error) => toast(error.message, true));
  else if (view === "git") loadGitView().catch((error) => toast(error.message, true));
  else loadRepos().catch((error) => toast(error.message, true));
}
window.addEventListener("hashchange", route);

// --- 工作台：需求列表 ---

async function loadWorkbench(reqIdFromHash) {
  const listEl = $("#req-list");
  listEl.innerHTML = loadingHTML;
  try {
    state.requirements = await api("/api/requirements") || [];
  } catch (error) {
    renderError(listEl, `需求加载失败：${error.message}`, () => loadWorkbench(reqIdFromHash));
    showReqEmpty();
    return;
  }
  const exists = (id) => state.requirements.some((item) => item.id === id);
  const target = exists(reqIdFromHash) ? reqIdFromHash : exists(state.reqId) ? state.reqId : (state.requirements[0]?.id || "");
  state.reqId = target;
  renderReqList();
  if (target) {
    if (reqIdFromHash !== target) history.replaceState(null, "", `#/workbench/${target}/${state.workbenchMode}`);
    await loadReqDetail();
  } else {
    history.replaceState(null, "", "#/workbench");
    showReqEmpty();
  }
}

function renderReqList() {
  const listEl = $("#req-list");
  $("#req-count").textContent = `${state.requirements.length} 项`;
  listEl.innerHTML = state.requirements.length ? state.requirements.map((req) => `<button type="button" class="req-item${req.id === state.reqId ? " active" : ""}" data-select-req="${esc(req.id)}"><span class="title">${esc(req.title)}</span><span class="meta">更新于 ${fmtTime(req.updatedAt)}</span></button>`).join("") : empty("还没有需求，先在上方创建一个可验收的目标。");
}

function showReqEmpty() {
  $("#req-empty").classList.remove("hidden");
  $("#req-content").classList.add("hidden");
  stopPoll();
}

// --- 工作台：需求详情 ---

async function loadReqDetail() {
  const id = state.reqId;
  if (!id) { showReqEmpty(); return; }
  $("#req-empty").classList.add("hidden");
  const content = $("#req-content");
  content.classList.remove("hidden");
  $("#req-header").innerHTML = loadingHTML;
  $("#readiness-band").innerHTML = "";
	renderAIPlan(null);
  $("#criteria-list").innerHTML = $("#task-list").innerHTML = $("#run-list").innerHTML = $("#evidence-list").innerHTML = loadingHTML;
  resetReportArea();
  try {
    const [detail, evidence, runs, rc, sessions, repos, providers] = await Promise.all([
      api(`/api/requirements/${id}`),
      api(`/api/requirements/${id}/evidence`),
      api(`/api/requirements/${id}/runs`),
      api(`/api/requirements/${id}/release-candidate`),
      api(`/api/agent-sessions`),
      api("/api/repositories"),
		api("/api/ai/providers"),
    ]);
    state.repos = repos || [];
	state.criteria = detail.criteria || [];
	state.aiProviders = providers || [];
	renderAIProviders();
    renderReqHeader(detail.requirement);
    renderReadiness(rc.readiness, evidence || []);
    renderCriteria(detail.criteria || [], evidence || []);
    renderTasks(detail.tasks || [], sessions || []);
    renderRuns(runs || [], detail.criteria || []);
    renderEvidence(evidence || [], detail.criteria || []);
    fillCriterionSelect($("#run-criterion"), detail.criteria || []);
    fillCriterionSelect($("#ev-criterion"), detail.criteria || []);
    schedulePoll(sessions || [], runs || []);
    applyWorkbenchMode();
  } catch (error) {
    stopPoll();
    renderError($("#req-header"), `需求详情加载失败：${error.message}`, loadReqDetail);
    $("#readiness-band").innerHTML = "";
    $("#criteria-list").innerHTML = $("#task-list").innerHTML = $("#run-list").innerHTML = $("#evidence-list").innerHTML = "";
  }
}

function renderAIProviders() {
	const select = $("#ai-provider");
	const available = state.aiProviders.filter((provider) => provider.available);
	select.innerHTML = state.aiProviders.map((provider) => `<option value="${esc(provider.id)}" ${provider.available ? "" : "disabled"}>${esc(provider.name)}${provider.available ? "" : "（未安装）"}</option>`).join("");
	const preferred = available.find((provider) => provider.id === "kimi") || available[0];
	if (preferred) select.value = preferred.id;
	select.disabled = !preferred;
	const status = $("#ai-provider-status");
	status.textContent = preferred ? `${available.length} 个可用` : "没有可用模型";
	status.classList.toggle("unavailable", !preferred);
	const gitSelect = $("#git-ai-provider");
	if (gitSelect) {
		gitSelect.innerHTML = select.innerHTML;
		if (preferred) gitSelect.value = preferred.id;
		gitSelect.disabled = !preferred;
	}
}

function renderAIPlan(plan) {
	const target = $("#ai-plan-preview");
	state.aiPlan = plan;
	if (!plan) { target.innerHTML = ""; return; }
	const criteria = (plan.criteria || []).map((item, index) => `<label class="ai-choice"><input type="checkbox" data-ai-criterion="${index}" checked><span><b>${esc(item.description)}</b>${item.rationale ? `<small>${esc(item.rationale)}</small>` : ""}</span></label>`).join("");
	const tasks = (plan.tasks || []).map((item, index) => `<label class="ai-choice"><input type="checkbox" data-ai-task="${index}" checked><span><b>${esc(item.title)}</b>${item.description ? `<small>${esc(item.description)}</small>` : ""}${item.dependsOn?.length ? `<small>依赖：${item.dependsOn.map(esc).join("、")}</small>` : ""}</span></label>`).join("");
	const notes = [...(plan.assumptions || []).map((value) => ["假设", value]), ...(plan.risks || []).map((value) => ["风险", value])];
	target.innerHTML = `<div class="ai-meta"><b>${esc(plan.meta?.provider || "AI")} 建议</b><span>${fmtTime(plan.meta?.generatedAt)} · ${fmtDuration(plan.meta?.durationMilliseconds)} · 输入 ${plan.meta?.inputChars || 0} 字符 · 输出 ${plan.meta?.outputBytes || 0} 字节 · 脱敏 ${plan.meta?.redactionCount || 0} 项 · 费用由本地 CLI 管理</span></div><div class="ai-grid"><div><h4>验收标准（${plan.criteria?.length || 0}）</h4>${criteria || empty("没有新增建议")}</div><div><h4>工程任务（${plan.tasks?.length || 0}）</h4>${tasks || empty("没有新增建议")}</div></div>${notes.length ? `<div class="ai-notes">${notes.map(([kind, value]) => `<p><b>${esc(kind)}</b>${esc(value)}</p>`).join("")}</div>` : ""}<div class="actions"><button type="button" class="btn" data-apply-ai-plan>应用所选条目</button><button type="button" class="btn ghost" data-discard-ai-plan>放弃建议</button><span class="form-note">应用后仍可逐项编辑或删除</span></div>`;
}

function applyWorkbenchMode() {
  document.querySelectorAll("[data-wb-mode]").forEach((link) => {
    const active = link.dataset.wbMode === state.workbenchMode;
    link.classList.toggle("active", active);
    if (active) link.setAttribute("aria-current", "page"); else link.removeAttribute("aria-current");
  });
  document.querySelectorAll("[data-wb-panel]").forEach((panel) => panel.classList.toggle("hidden", panel.dataset.wbPanel !== state.workbenchMode));
}

function askConfirmation(title, message, confirmLabel = "确认") {
  const dialog = $("#confirm-dialog");
  $("#confirm-title").textContent = title;
  $("#confirm-message").textContent = message;
  $("#confirm-submit").textContent = confirmLabel;
  dialog.showModal();
  return new Promise((resolve) => dialog.addEventListener("close", () => resolve(dialog.returnValue === "confirm"), { once: true }));
}

function renderReqHeader(req) {
  $("#req-header").innerHTML = `<header class="req-header card"><div class="req-head-main"><h2>${esc(req.title)}</h2><div class="meta">${esc(req.id)} · 更新于 ${fmtTime(req.updatedAt)}</div>${req.description ? `<div class="desc">${esc(req.description)}</div>` : ""}</div><div class="item-actions"><button class="small" type="button" data-edit-req>编辑</button><button class="small danger" type="button" data-delete-req="${esc(req.id)}">删除需求</button></div><form class="inline-editor hidden" id="req-edit-form"><label>标题<input name="title" value="${esc(req.title)}" required></label><label>描述<textarea name="description" rows="2">${esc(req.description || "")}</textarea></label><div class="actions"><button class="btn" type="submit">保存</button><button class="btn ghost" type="button" data-cancel-edit>取消</button></div></form></header>`;
}

function progressCell(value, total, label) {
  return `<div><b>${value}/${total}</b><span>${label}</span><progress value="${value}" max="${total || 1}" aria-label="${label}"></progress></div>`;
}

function renderReadiness(readiness, evidence) {
  const passed = evidence.filter((item) => item.status === "passed").length;
  const failed = evidence.filter((item) => item.status === "failed").length;
  $("#readiness-band").innerHTML = `<div class="summary-band readiness-summary"><div><b class="${readiness.ready ? "text-ok" : "text-warn"}">${readiness.ready ? "可发布" : "未就绪"}</b><span>发布就绪</span></div>${progressCell(readiness.criteriaSatisfied, readiness.criteriaTotal, "验收通过")}${progressCell(readiness.criteriaWithEvidence, readiness.criteriaTotal, "证据覆盖")}${progressCell(readiness.tasksDone, readiness.tasksTotal, "任务完成")}${progressCell(readiness.sourcesClean || 0, readiness.sourcesTotal || 0, "代码源干净")}<div><b>${passed} 通过 / ${failed} 未过</b><span>证据共 ${evidence.length} 条</span></div></div>`;
}

function fillCriterionSelect(select, criteria) {
  select.innerHTML = '<option value="">不关联</option>' + criteria.map((item) => `<option value="${esc(item.id)}">${esc(item.description)}</option>`).join("");
}

// --- 验收标准 ---

function renderCriteria(criteria, evidence) {
  $("#criteria-list").innerHTML = criteria.length ? criteria.map((criterion) => {
    const linked = evidence.filter((item) => item.criterionId === criterion.id);
    const latest = linked[0];
    const latestPassed = latest?.status === "passed";
    return `<article class="item"><div class="item-head"><div><div class="title">${esc(criterion.description)} <span class="badge ${criterion.satisfied && latestPassed ? "done" : "warn"}">${criterion.satisfied && latestPassed ? "已满足" : "未满足"}</span></div><div class="meta">${linked.length ? `最新证据：${esc(evidenceStatus[latest.status] || latest.status)} · ${fmtTime(latest.createdAt)}` : "还没有证据"}</div></div><div class="item-actions"><button class="small" data-edit-criterion="${esc(criterion.id)}" type="button">编辑</button><button class="icon-btn danger" data-delete-criterion="${esc(criterion.id)}" type="button" title="删除验收标准" aria-label="删除验收标准">×</button></div></div><form class="inline-editor hidden" data-criterion-edit-form="${esc(criterion.id)}"><label>验收标准<textarea name="description" rows="2" minlength="5" maxlength="2000" required>${esc(criterion.description)}</textarea></label><div class="actions"><button class="btn" type="submit">保存</button><button class="btn ghost" data-cancel-criterion-edit type="button">取消</button></div></form><div class="item-actions criterion-actions">${criterion.satisfied && latestPassed ? `<button class="small" data-unsatisfy="${esc(criterion.id)}" type="button">撤回通过</button>` : latestPassed ? `<button class="small" data-satisfy="${esc(criterion.id)}" type="button">标记通过</button>` : `<button class="small" data-show-proof="${esc(criterion.id)}" type="button">登记证据并通过</button>`}</div><form class="proof-form hidden" data-proof-form="${esc(criterion.id)}"><label>证据标题<input name="title" required placeholder="例如：自动化测试通过"></label><label>结果说明<textarea name="note" rows="2" placeholder="关键输出或人工核对结论"></textarea></label><div class="actions"><button class="btn" type="submit">登记并通过</button><button class="btn ghost" data-cancel-proof type="button">取消</button></div></form></article>`;
  }).join("") : empty("这个需求还没有验收标准。");
}

// --- 任务与 Agent ---

function repoOptions() {
  return state.repos.map((repo) => `<option value="${esc(repo.path)}">${esc(repo.name)} · ${esc(repo.path)}</option>`).join("") || '<option value="">请先在“仓库”页导入仓库</option>';
}

function renderTasks(tasks, sessions) {
	const taskTitles = new Map(tasks.map((task) => [task.id, task.title]));
	const taskByID = new Map(tasks.map((task) => [task.id, task]));
  $("#task-list").innerHTML = tasks.length ? tasks.map((task) => {
    const taskSessions = sessions.filter((session) => session.taskId === task.id);
    const workspace = task.worktreePath ? `<div class="workspace"><span>分支 <code>${esc(task.branch)}</code></span><span>工作区 <code>${esc(task.worktreePath)}</code></span></div>` : `<details class="tool-panel"><summary>创建隔离工作区</summary><form data-worktree-form="${esc(task.id)}"><label>仓库<select name="repositoryPath" required>${repoOptions()}</select></label><div class="grid-2"><label>分支<input name="branch" value="devcycle/${esc(task.id.slice(0, 8))}" required></label><label>工作区路径<input name="worktreePath" required placeholder="仓库同级的独立目录"></label></div><div class="actions"><button class="btn" type="submit">创建并关联</button></div></form></details>`;
    const agent = task.worktreePath ? `<details class="tool-panel"><summary>Agent 会话（${taskSessions.length}）</summary><form data-session-form="${esc(task.id)}"><label>提供方<select name="provider"><option value="codex">Codex</option><option value="kimi">KIMI</option><option value="claude">Claude</option></select></label><label>任务说明<textarea name="prompt" rows="3" required placeholder="说明目标、约束和验收方式"></textarea></label><div class="actions"><button class="btn" type="submit">启动会话</button></div></form><div class="sessions" data-sessions-for="${esc(task.id)}">${renderSessions(taskSessions)}</div></details>` : "";
	const dependencyTasks = (task.dependsOn || []).map((id) => taskByID.get(id)).filter(Boolean);
	const blocked = dependencyTasks.some((item) => item.status !== "done");
	const dependencies = dependencyTasks.length ? `<div class="dependency-line ${blocked ? "blocked" : "ready"}"><span>${blocked ? "被前置任务阻塞" : "前置任务已完成"}</span>${dependencyTasks.map((item) => `<code>${esc(item.title)}</code>`).join("")}</div>` : "";
	const dependencyChoices = tasks.filter((candidate) => candidate.id !== task.id).map((candidate) => `<label class="check-row"><input type="checkbox" name="dependsOn" value="${esc(candidate.id)}" ${(task.dependsOn || []).includes(candidate.id) ? "checked" : ""}><span>${esc(candidate.title)} <small>${esc(taskStatus[candidate.status] || candidate.status)}</small></span></label>`).join("") || `<span class="meta">没有其他可依赖任务</span>`;
	const editor = `<form class="task-editor hidden" data-task-edit-form="${esc(task.id)}"><label>标题<input name="title" value="${esc(task.title)}" minlength="2" maxlength="300" required></label><label>描述<textarea name="description" rows="3" maxlength="8000">${esc(task.description || "")}</textarea></label><fieldset><legend>前置任务</legend><div class="dependency-picker">${dependencyChoices}</div></fieldset><div class="actions"><button class="btn" type="submit">保存任务</button><button class="btn ghost" data-cancel-task-edit type="button">取消</button></div></form>`;
    return `<article class="item task-item${blocked ? " is-blocked" : ""}"><div class="item-head"><div><div class="title">${esc(task.title)} <span class="badge ${esc(task.status)}">${esc(taskStatus[task.status] || task.status)}</span>${blocked ? `<span class="badge warn">阻塞</span>` : ""}</div>${dependencies}</div><div class="item-actions"><button class="small" data-edit-task="${esc(task.id)}" type="button">编辑</button><button class="icon-btn danger" data-delete-task="${esc(task.id)}" type="button" title="删除任务" aria-label="删除任务">×</button></div></div>${task.description ? `<div class="desc">${esc(task.description)}</div>` : ""}${editor}<div class="task-status"><label>状态<select data-task-status="${esc(task.id)}">${Object.entries(taskStatus).map(([value, label]) => `<option value="${value}" ${value === task.status ? "selected" : ""} ${(blocked && value !== "todo") ? "disabled" : ""}>${label}</option>`).join("")}</select></label></div>${workspace}${agent}</article>`;
  }).join("") : empty("还没有任务。每个可发布需求至少应有一个明确任务。");
}

function renderSessions(sessions) {
  return sessions.length ? sessions.map((session) => `<div class="session"><div><b>${esc(session.provider)}</b> <span class="badge ${esc(session.status)}">${esc(session.status)}</span><span class="meta">${fmtTime(session.startedAt)} · 已运行 ${fmtDuration(session.durationMilliseconds)}${session.pid ? ` · PID ${session.pid}` : ""} · 日志上限 ${Math.round((session.logLimitBytes || 0) / 1048576)} MB</span></div><div class="item-actions"><button class="small" data-session-log="${esc(session.id)}" type="button">${state.openLogs.has(session.id) ? "收起日志" : "日志"}</button>${session.status === "running" ? `<button class="small danger" data-stop-session="${esc(session.id)}" type="button">停止</button>` : ""}</div><pre class="session-log${state.openLogs.has(session.id) ? "" : " hidden"}" data-log-target="${esc(session.id)}"></pre></div>`).join("") : empty("还没有 Agent 会话。");
}

// --- Agent 与验证任务自动刷新 ---

function stopPoll() {
  if (state.pollTimer) { clearInterval(state.pollTimer); state.pollTimer = null; }
}

function schedulePoll(sessions, runs = []) {
  const running = sessions.some((session) => session.status === "running") || runs.some((run) => ["running", "stopping"].includes(run.status));
  if (running && !state.pollTimer) state.pollTimer = setInterval(pollLiveWork, 2000);
  if (!running) stopPoll();
}

async function pollLiveWork() {
  if (state.view !== "workbench" || !state.reqId) { stopPoll(); return; }
  if (document.hidden) return;
  try {
    const [sessions, runs, evidence] = await Promise.all([
      api("/api/agent-sessions"),
      api(`/api/requirements/${state.reqId}/runs`),
      api(`/api/requirements/${state.reqId}/evidence`),
    ]);
    document.querySelectorAll("[data-sessions-for]").forEach((box) => {
      const taskId = box.dataset.sessionsFor;
      box.innerHTML = renderSessions(sessions.filter((session) => session.taskId === taskId));
    });
    renderRuns(runs || [], state.criteria);
    renderEvidence(evidence || [], state.criteria);
    for (const id of state.openLogs) {
      const pre = document.querySelector(`[data-log-target="${id}"]`);
      if (!pre) continue;
      try {
        const data = await api(`/api/agent-sessions/${id}/log`);
        pre.textContent = data.log || "（暂无输出）";
        pre.scrollTop = pre.scrollHeight;
      } catch (_) { /* 保留旧内容，下次再试 */ }
    }
    schedulePoll(sessions || [], runs || []);
  } catch (_) { /* 网络抖动时保持现有界面 */ }
}

// --- 验证运行与证据 ---

function renderRuns(runs, criteria) {
  const criterionNames = Object.fromEntries(criteria.map((item) => [item.id, item.description]));
  const labels = { running: "运行中", stopping: "正在停止", passed: "通过", failed: "未通过", timed_out: "超时", stopped: "已停止", interrupted: "已中断" };
  $("#run-list").innerHTML = runs.length ? runs.map((run) => {
    const active = ["running", "stopping"].includes(run.status);
    const timing = active ? `已运行 ${fmtDuration(run.durationMilliseconds)}` : `${fmtDuration(run.durationMilliseconds)} · ${fmtTime(run.completedAt)}`;
    const result = active ? timing : `退出码 ${run.exitCode} · ${timing}`;
    return `<article class="item run-item ${active ? "is-running" : ""}"><div class="item-head"><div><div class="title">${esc(run.name)} <span class="badge ${esc(run.status)}">${esc(labels[run.status] || run.status)}</span></div><div class="meta"><code>${esc(run.command)}</code> · ${result}</div></div>${active ? `<button class="small danger" type="button" data-stop-run="${esc(run.id)}" ${run.status === "stopping" ? "disabled" : ""}>${run.status === "stopping" ? "停止中" : "停止"}</button>` : ""}</div>${run.criterionId ? `<div class="desc">关联：${esc(criterionNames[run.criterionId] || run.criterionId)}</div>` : ""}<details ${active ? "open" : ""}><summary>${active ? "实时输出" : "查看输出"}</summary><pre class="code run-output">${esc(run.output || (active ? "（等待命令输出）" : "（无输出）"))}</pre></details></article>`;
  }).join("") : empty("尚未运行验证命令。");
}

function renderEvidence(evidence, criteria) {
  const criterionNames = Object.fromEntries(criteria.map((item) => [item.id, item.description]));
  $("#evidence-list").innerHTML = evidence.length ? evidence.map((item) => `<article class="item"><div class="item-head"><div><div class="title">${esc(item.title)} <span class="badge ${esc(item.status)}">${esc(evidenceStatus[item.status] || item.status)}</span></div><div class="meta">${esc(item.kind)} · ${fmtTime(item.createdAt)}${item.criterionId ? ` · ${esc(criterionNames[item.criterionId] || item.criterionId)}` : ""}</div></div></div>${item.inline ? `<details><summary>查看内容</summary><pre class="code evidence-output">${esc(item.inline)}</pre></details>` : ""}${safeEvidenceLink(item.uri)}</article>`).join("") : empty("尚未登记证据。");
}

function safeEvidenceLink(uri) {
  if (!uri) return "";
  try {
    const url = new URL(uri, window.location.href);
    if (!["http:", "https:"].includes(url.protocol)) return `<div class="meta">位置：${esc(uri)}</div>`;
    return `<a class="evidence-link" href="${esc(url.href)}" target="_blank" rel="noreferrer">打开证据链接</a>`;
  } catch (_) { return `<div class="meta">位置：${esc(uri)}</div>`; }
}

// --- 报告与发布候选 ---

function resetReportArea() {
  state.rcData = null;
  state.reportData = null;
  $("#report-output").innerHTML = "";
  $("#rc-output").innerHTML = "";
  $("#btn-download-report").disabled = true;
  $("#btn-download-rc").disabled = true;
}

function renderReportStructured(report) {
  const ready = report.candidate.readiness;
  const runsList = report.verificationRuns.length ? report.verificationRuns.map((run) => `<div class="mini-row"><span>${esc(run.name)}</span><span class="badge ${esc(run.status)}">${run.status === "passed" ? "通过" : "失败"}</span></div>`).join("") : '<div class="meta">无验证运行</div>';
  const evList = report.evidence.length ? report.evidence.map((item) => `<div class="mini-row"><span>${esc(item.title)}</span><span class="badge ${esc(item.status)}">${esc(evidenceStatus[item.status] || item.status)}</span></div>`).join("") : '<div class="meta">无证据</div>';
  const sessionList = report.agentSessions.length ? report.agentSessions.map((session) => `<div class="mini-row"><span>${esc(session.provider)} · ${fmtDuration(session.durationMilliseconds)}</span><span class="badge ${esc(session.status)}">${esc(session.status)}</span></div>`).join("") : '<div class="meta">无 Agent 会话</div>';
  const sourceList = report.candidate.sources?.length ? report.candidate.sources.map((source) => `<div class="source-row ${source.clean ? "clean" : "dirty"}"><div><b>${esc(source.taskTitle)}</b><code>${esc(source.headCommit)}</code><span>${esc(source.branch)} · ${esc(source.repositoryPath)}</span></div><span class="badge ${source.clean ? "done" : "failed"}">${source.clean ? "已钉住" : "有未提交改动"}</span></div>`).join("") : '<div class="meta">该候选没有关联代码工作区</div>';
  const warnings = report.warnings?.length ? `<div class="release-blocker"><b>报告存在未完成分析</b><span>${report.warnings.map(esc).join("；")}</span></div>` : "";
  return `<div class="report-block"><div class="summary-band"><div><b class="${ready.ready ? "text-ok" : "text-warn"}">${ready.ready ? "可进入发布验证" : "尚未就绪"}</b><span>${esc(report.candidate.requirement.title)}</span></div><div><b>${report.evidence.length}</b><span>证据</span></div><div><b>${report.verificationRuns.length}</b><span>验证运行</span></div><div><b>${report.agentSessions.length}</b><span>Agent 会话</span></div><div><b class="${report.evidenceComplete ? "text-ok" : "text-warn"}">${report.evidenceComplete ? "完整" : "缺失"}</b><span>证据覆盖</span></div></div>${warnings}<div class="source-proof"><h4>代码来源</h4>${sourceList}</div><div class="report-cols"><div><h4>验证运行</h4>${runsList}</div><div><h4>证据</h4>${evList}</div><div><h4>Agent 会话</h4>${sessionList}</div></div><div class="suite-handoff"><span>开发证据已汇总</span><a data-suite="releaseguard">进入 ReleaseGuard 验证发布 →</a></div><details class="json-details"><summary>查看原始 JSON</summary><pre class="code" id="report-json" tabindex="0">${esc(JSON.stringify(report, null, 2))}</pre></details></div>`;
}

function renderRcStructured(candidate) {
  const readiness = candidate.readiness;
  const dirty = (candidate.sources || []).filter((source) => !source.clean);
  return `<div class="report-block"><div class="summary-band"><div><b class="${readiness.ready ? "text-ok" : "text-warn"}">${readiness.ready ? "可发布" : "未就绪"}</b><span>${esc(candidate.requirement.title)}</span></div>${progressCell(readiness.criteriaSatisfied, readiness.criteriaTotal, "验收通过")}${progressCell(readiness.criteriaWithEvidence, readiness.criteriaTotal, "证据覆盖")}${progressCell(readiness.tasksDone, readiness.tasksTotal, "任务完成")}${progressCell(readiness.sourcesClean || 0, readiness.sourcesTotal || 0, "代码源干净")}</div>${dirty.length ? `<div class="release-blocker"><b>代码来源尚未钉住</b><span>${dirty.map((source) => esc(source.taskTitle)).join("、")} 仍有未提交改动</span></div>` : ""}<div class="suite-handoff"><span>候选清单已生成</span><a data-suite="releaseguard">进入 ReleaseGuard 验证发布 →</a></div><details class="json-details"><summary>查看原始 JSON</summary><pre class="code" id="rc-json" tabindex="0">${esc(JSON.stringify(candidate, null, 2))}</pre></details></div>`;
}

$("#btn-load-report").addEventListener("click", async (event) => {
  if (!state.reqId) return toast("请先选择需求", true);
  await busy(event.currentTarget, async () => {
    $("#report-output").innerHTML = loadingHTML;
    try {
      const report = await api(`/api/requirements/${state.reqId}/development-report`);
      state.reportData = report;
      $("#report-output").innerHTML = renderReportStructured(report);
      $("#btn-download-report").disabled = false;
    } catch (error) {
      renderError($("#report-output"), `报告生成失败：${error.message}`, () => $("#btn-load-report").click());
    }
  });
});
$("#btn-download-report").addEventListener("click", () => state.reportData && downloadJSON(state.reportData, `development-report-${state.reqId}.json`));

$("#btn-load-rc").addEventListener("click", async (event) => {
  if (!state.reqId) return toast("请先选择需求", true);
  await busy(event.currentTarget, async () => {
    $("#rc-output").innerHTML = loadingHTML;
    try {
      const candidate = await api(`/api/requirements/${state.reqId}/release-candidate`);
      state.rcData = candidate;
      $("#rc-output").innerHTML = renderRcStructured(candidate);
      $("#btn-download-rc").disabled = false;
    } catch (error) {
      renderError($("#rc-output"), `发布候选生成失败：${error.message}`, () => $("#btn-load-rc").click());
    }
  });
});
$("#btn-download-rc").addEventListener("click", () => state.rcData && downloadJSON(state.rcData, `release-candidate-${state.reqId}.json`));

// --- Git 工具 ---

async function loadGitView() {
  try {
	const [repos, providers] = await Promise.all([api("/api/repositories"), api("/api/ai/providers")]);
	state.repos = repos || [];
	state.aiProviders = providers || [];
	renderAIProviders();
  } catch (error) {
    renderError($("#git-status"), `仓库列表加载失败：${error.message}`, loadGitView);
    return;
  }
  const select = $("#git-repo-select");
  select.innerHTML = repoOptions();
  if (state.gitRepoPath && state.repos.some((repo) => repo.path === state.gitRepoPath)) select.value = state.gitRepoPath;
  state.gitRepoPath = select.value;
  await refreshGit();
}
$("#git-repo-select").addEventListener("change", (event) => { state.gitRepoPath = event.target.value; refreshGit(); });
$("#git-stat").addEventListener("change", refreshGit);
$("#git-staged").addEventListener("change", refreshGit);
$("#btn-refresh-git").addEventListener("click", refreshGit);

function gitRangeParams() {
	const from = $("#git-from").value.trim();
	const to = $("#git-to").value.trim();
	return `&staged=${$("#git-staged").checked ? 1 : 0}&from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;
}

function renderStructuredDiff(diff) {
	state.gitStructured = diff;
	const files = diff.files || [];
	$("#git-diff-summary").textContent = `${files.length} 个文件 · +${diff.totalAdditions || 0} / −${diff.totalDeletions || 0}${diff.untrackedFiles ? ` · ${diff.untrackedFiles} 个未跟踪` : ""}${diff.binaryFiles ? ` · ${diff.binaryFiles} 个二进制` : ""}`;
	$("#git-files").innerHTML = files.length ? `<div class="table-scroll"><table><thead><tr><th>状态</th><th>文件</th><th class="number">新增</th><th class="number">删除</th></tr></thead><tbody>${files.map((file) => `<tr><td><span class="change-status">${esc(file.status)}</span></td><td><b>${esc(file.path)}</b>${file.oldPath ? `<small>原路径：${esc(file.oldPath)}</small>` : ""}</td><td class="number add">${file.untracked ? "未统计" : file.binary ? "二进制" : `+${file.additions}`}</td><td class="number delete">${file.untracked || file.binary ? "—" : `−${file.deletions}`}</td></tr>`).join("")}</tbody></table></div>` : empty("当前比较范围没有文件改动。")
}

async function refreshGit() {
  const repo = state.gitRepoPath;
  if (!repo) {
    $("#git-status").innerHTML = empty("请先在“仓库”页导入并选择仓库。");
    $("#git-diff").textContent = "";
	$("#git-files").innerHTML = "";
    $("#git-log").innerHTML = $("#git-branches").innerHTML = "";
    return;
  }
  $("#git-status").innerHTML = loadingHTML;
	const from = $("#git-from").value.trim();
	const to = $("#git-to").value.trim();
	if (to && !from) {
		renderError($("#git-status"), "填写终点时必须同时填写起点。", refreshGit);
		return;
	}
  const query = encodeURIComponent(repo);
	const range = gitRangeParams();
  try {
	const [status, diff, structured, commits, branches] = await Promise.all([api(`/api/git/status?repo=${query}`), api(`/api/git/diff?repo=${query}&stat=${$("#git-stat").checked ? 1 : 0}${range}`), api(`/api/git/structured-diff?repo=${query}${range}`), api(`/api/git/log?repo=${query}`), api(`/api/git/branches?repo=${query}&remote=1`)]);
    $("#git-status").innerHTML = `<article class="item"><div class="title">当前分支：${esc(status.branch)} <span class="badge ${status.clean ? "ok" : "warn"}">${status.clean ? "工作区干净" : "有未提交改动"}</span></div>${(status.files || []).map((file) => `<div class="meta">[${esc(file.xy)}] ${esc(file.path)}</div>`).join("")}</article>`;
    $("#git-diff").textContent = diff.content || "（无改动）";
	renderStructuredDiff(structured);
    $("#git-branches").innerHTML = branches.length ? `<div class="branch-grid">${branches.map((branch) => `<span class="branch ${branch.current ? "current" : ""}">${branch.current ? "当前 · " : ""}${esc(branch.name)}</span>`).join("")}</div>` : empty("暂无分支");
    $("#git-log").innerHTML = commits.length ? commits.map((commit) => `<article class="item compact"><div class="title">${esc(commit.subject)}</div><div class="meta">${esc(commit.shortHash)} · ${esc(commit.author)} · ${fmtTime(commit.when)}</div></article>`).join("") : empty("暂无提交记录。");
  } catch (error) {
    renderError($("#git-status"), `读取失败：${error.message}`, refreshGit);
    $("#git-diff").textContent = "";
	$("#git-files").innerHTML = "";
  }
}

$("#btn-impact").addEventListener("click", async (event) => {
  if (!state.gitRepoPath) return toast("请先选择仓库", true);
  await busy(event.currentTarget, async () => {
    $("#git-impact").innerHTML = loadingHTML;
    try {
	  const impact = await api(`/api/git/impact?repo=${encodeURIComponent(state.gitRepoPath)}${gitRangeParams()}`);
      const flags = [[impact.userImpact, "用户界面"], [impact.apiImpact, "API"], [impact.databaseImpact, "数据库"], [impact.configurationImpact, "配置"]].filter(([active]) => active);
      $("#git-impact").innerHTML = `<div class="impact ${esc(impact.risk)}"><div class="item-head"><strong>风险：${esc(impact.risk.toUpperCase())}</strong><span>${impact.files.length} 个文件</span></div><div class="impact-flags">${flags.map(([, label]) => `<span>${label}</span>`).join("") || "未识别到特殊影响面"}</div>${impact.summary.map((item) => `<p>${esc(item)}</p>`).join("")}</div>`;
    } catch (error) {
      renderError($("#git-impact"), `影响分析失败：${error.message}`, () => $("#btn-impact").click());
    }
  });
});

function renderFindingGroup(title, items) {
	if (!items?.length) return "";
	return `<section><h4>${esc(title)}</h4>${items.map((item) => `<div class="ai-finding"><p>${esc(item.text)}</p><div>${(item.paths || []).map((path) => `<code>${esc(path)}</code>`).join("")}</div></div>`).join("")}</section>`;
}

$("#btn-explain-diff").addEventListener("click", async (event) => {
	if (!state.gitRepoPath) return toast("请先选择仓库", true);
	if (!state.gitStructured?.files?.length) return toast("当前范围没有可解释的改动", true);
	await busy(event.currentTarget, async () => {
		const target = $("#git-ai-explanation");
		target.innerHTML = loadingHTML;
		try {
			const explanation = await api("/api/git/explain", { body: { repositoryPath: state.gitRepoPath, provider: $("#git-ai-provider").value, from: $("#git-from").value.trim(), to: $("#git-to").value.trim(), staged: $("#git-staged").checked } });
			target.innerHTML = `<article class="ai-explanation"><header><div><span>AI 评审摘要</span><h3>${esc(explanation.summary)}</h3></div><small>${esc(explanation.meta?.provider)} · ${fmtTime(explanation.meta?.generatedAt)}</small></header><div class="explanation-grid">${renderFindingGroup("用户影响", explanation.userImpact)}${renderFindingGroup("工程影响", explanation.engineeringImpact)}${renderFindingGroup("风险", explanation.risks)}${renderFindingGroup("测试重点", explanation.testFocus)}</div>${explanation.uncertainties?.length ? `<div class="uncertainties"><b>待确认</b>${explanation.uncertainties.map((item) => `<p>${esc(item)}</p>`).join("")}</div>` : ""}</article>`;
		} catch (error) { renderError(target, `AI 解释失败：${error.message}`, () => $("#btn-explain-diff").click()); }
	});
});

// --- 仓库管理 ---

async function loadRepos() {
  const listEl = $("#repo-list");
  listEl.innerHTML = loadingHTML;
  try {
    state.repos = await api("/api/repositories") || [];
  } catch (error) {
    renderError(listEl, `仓库列表加载失败：${error.message}`, loadRepos);
    return;
  }
  listEl.innerHTML = state.repos.length ? state.repos.map((repo) => `<article class="item"><div class="item-head"><div><div class="title">${esc(repo.name)}</div><div class="meta">${esc(repo.path)} · 导入于 ${fmtTime(repo.createdAt)}</div></div><button class="icon-btn danger" data-delete-repo="${esc(repo.id)}" type="button" title="移除仓库记录" aria-label="移除仓库记录">×</button></div></article>`).join("") : empty("还没有导入仓库。导入只登记路径，不会修改仓库内容。");
}

$("#form-import-repo").addEventListener("submit", async (event) => {
  event.preventDefault(); const form = event.currentTarget;
  await busy(event.submitter, async () => { try { await api("/api/repositories", { body: { path: form.path.value.trim() } }); form.reset(); toast("仓库已导入"); await loadRepos(); } catch (error) { toast(error.message, true); } });
});

// --- 表单提交 ---

$("#form-create-req").addEventListener("submit", async (event) => {
  event.preventDefault(); const form = event.currentTarget;
  await busy(event.submitter, async () => {
    try {
      const created = await api("/api/requirements", { body: { title: form.title.value.trim(), description: form.description.value.trim() } });
	  form.reset(); form.closest("details").open = false; toast("需求已创建");
      location.hash = `#/workbench/${created.id}`;
      if (currentRoute().reqId === created.id) await loadWorkbench(created.id);
    } catch (error) { toast(error.message, true); }
  });
});

$("#form-create-criterion").addEventListener("submit", async (event) => {
  event.preventDefault(); if (!state.reqId) return toast("请先选择需求", true);
	await busy(event.submitter, async () => { try { await api(`/api/requirements/${state.reqId}/criteria`, { body: { description: event.currentTarget.description.value.trim() } }); event.currentTarget.reset(); event.currentTarget.closest("details").open = false; toast("验收标准已添加"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
});

$("#form-create-task").addEventListener("submit", async (event) => {
  event.preventDefault(); if (!state.reqId) return toast("请先选择需求", true);
  const form = event.currentTarget;
	await busy(event.submitter, async () => { try { await api("/api/tasks", { body: { requirementId: state.reqId, title: form.title.value.trim(), description: form.description.value.trim() } }); form.reset(); form.closest("details").open = false; toast("任务已创建"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
});

$("#form-ai-plan").addEventListener("submit", async (event) => {
	event.preventDefault();
	if (!state.reqId) return toast("请先选择需求", true);
	const form = event.currentTarget;
	await busy(event.submitter, async () => {
		try {
			$("#ai-plan-preview").innerHTML = loadingHTML;
			const plan = await api(`/api/requirements/${state.reqId}/ai-plan`, { body: { provider: form.provider.value, additionalContext: form.additionalContext.value.trim() } });
			renderAIPlan(plan);
			toast("规划建议已生成，请审阅后应用");
		} catch (error) {
			renderError($("#ai-plan-preview"), `生成失败：${error.message}`, () => form.requestSubmit());
			toast(error.message, true);
		}
	});
});

$("#form-run").addEventListener("submit", async (event) => {
  event.preventDefault(); if (!state.reqId) return toast("请先选择需求", true);
  const form = event.currentTarget;
  await busy(event.submitter, async () => {
    try {
      await api(`/api/requirements/${state.reqId}/runs`, { body: { name: form.name.value.trim(), command: form.command.value.trim(), workingDir: form.workingDir.value.trim(), criterionId: form.criterionId.value, timeoutSeconds: Number(form.timeoutSeconds.value) || 600 } });
      form.reset(); toast("验证任务已启动，可在运行列表查看实时输出");
    } catch (error) {
      toast(`验证任务启动失败：${error.message}`, true);
    } finally { await loadReqDetail(); }
  });
});

$("#form-evidence").addEventListener("submit", async (event) => {
  event.preventDefault(); if (!state.reqId) return toast("请先选择需求", true);
  const form = event.currentTarget;
  await busy(event.submitter, async () => { try { await api(`/api/requirements/${state.reqId}/evidence`, { body: { kind: form.kind.value, status: form.status.value, title: form.title.value.trim(), criterionId: form.criterionId.value, uri: form.uri.value.trim(), inline: form.inline.value.trim() } }); form.reset(); toast("证据已登记"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
});

// --- 委托事件：点击 ---

document.addEventListener("click", async (event) => {
  const modeLink = event.target.closest("[data-wb-mode]");
  if (modeLink) {
    event.preventDefault();
    if (state.reqId) location.hash = `#/workbench/${state.reqId}/${modeLink.dataset.wbMode}`;
    return;
  }
  const target = event.target.closest("button");
  if (!target) return;

  if (target.matches("[data-select-req]")) {
    const id = target.dataset.selectReq;
    if (id && id !== state.reqId) { location.hash = `#/workbench/${id}/${state.workbenchMode}`; }
    return;
  }
	if (target.matches("[data-discard-ai-plan]")) { renderAIPlan(null); return; }
	if (target.matches("[data-apply-ai-plan]")) {
		const plan = state.aiPlan;
		if (!plan) return;
		const criteria = [...document.querySelectorAll("[data-ai-criterion]:checked")].map((input) => plan.criteria[Number(input.dataset.aiCriterion)]);
		const tasks = [...document.querySelectorAll("[data-ai-task]:checked")].map((input) => plan.tasks[Number(input.dataset.aiTask)]);
		const selectedTitles = new Set(tasks.map((task) => task.title.trim().toLowerCase()));
		const generatedTitles = new Set((plan.tasks || []).map((task) => task.title.trim().toLowerCase()));
		const missing = tasks.flatMap((task) => (task.dependsOn || []).filter((dependency) => generatedTitles.has(dependency.trim().toLowerCase()) && !selectedTitles.has(dependency.trim().toLowerCase())));
		if (missing.length) return toast(`请同时选择前置任务：${missing.join("、")}`, true);
		if (!criteria.length && !tasks.length) return toast("至少选择一条建议", true);
		await busy(target, async () => {
			try {
				await api(`/api/requirements/${state.reqId}/ai-plan/apply`, { body: { criteria, tasks } });
				renderAIPlan(null);
				toast("所选规划已写入");
				await loadReqDetail();
			} catch (error) { toast(error.message, true); }
		});
		return;
	}
  if (target.matches("[data-delete-repo]")) {
    if (await askConfirmation("移除仓库登记", "只会移除 DevCycle 中的登记，不会删除磁盘上的仓库或文件。", "移除登记")) { try { await api(`/api/repositories/${target.dataset.deleteRepo}`, { method: "DELETE" }); toast("仓库记录已移除"); await loadRepos(); } catch (error) { toast(error.message, true); } }
    return;
  }
  if (target.matches("[data-edit-req]")) { $("#req-edit-form")?.classList.remove("hidden"); return; }
  if (target.matches("[data-cancel-edit]")) { $("#req-edit-form")?.classList.add("hidden"); return; }
  if (target.matches("[data-delete-req]")) {
    if (await askConfirmation("删除整个需求", "任务、验收、验证、证据和 Agent 会话记录都会一起删除。此操作无法撤销。", "删除需求")) {
      try {
        await api(`/api/requirements/${target.dataset.deleteReq}`, { method: "DELETE" });
        toast("需求已删除");
        state.reqId = "";
        history.replaceState(null, "", "#/workbench");
        await loadWorkbench("");
      } catch (error) { toast(error.message, true); }
    }
    return;
  }
  if (target.matches("[data-show-proof]")) { target.closest(".item").querySelector(".proof-form").classList.remove("hidden"); return; }
  if (target.matches("[data-cancel-proof]")) { target.closest(".proof-form").classList.add("hidden"); return; }
  if (target.matches("[data-edit-criterion]")) { target.closest(".item").querySelector("[data-criterion-edit-form]").classList.remove("hidden"); return; }
  if (target.matches("[data-cancel-criterion-edit]")) { target.closest("[data-criterion-edit-form]").classList.add("hidden"); return; }
  if (target.matches("[data-edit-task]")) { target.closest(".item").querySelector("[data-task-edit-form]").classList.remove("hidden"); return; }
  if (target.matches("[data-cancel-task-edit]")) { target.closest("[data-task-edit-form]").classList.add("hidden"); return; }
  if (target.matches("[data-satisfy]")) {
    try { await api(`/api/criteria/${target.dataset.satisfy}`, { method: "PATCH", body: { satisfied: true } }); toast("验收标准已通过"); await loadReqDetail(); } catch (error) { toast(error.message, true); }
    return;
  }
  if (target.matches("[data-unsatisfy]")) {
    try { await api(`/api/criteria/${target.dataset.unsatisfy}`, { method: "PATCH", body: { satisfied: false } }); toast("验收状态已撤回"); await loadReqDetail(); } catch (error) { toast(error.message, true); }
    return;
  }
  if (target.matches("[data-delete-criterion]")) {
    if (await askConfirmation("删除验收标准", "标准会被删除，已有历史证据仍保留但不再关联到该标准。", "删除标准")) { try { await api(`/api/criteria/${target.dataset.deleteCriterion}`, { method: "DELETE" }); toast("验收标准已删除"); await loadReqDetail(); } catch (error) { toast(error.message, true); } }
    return;
  }
  if (target.matches("[data-delete-task]")) {
    if (await askConfirmation("删除任务记录", "Agent 会话记录会随任务删除；历史证据保留。已创建的 Git worktree 不会从磁盘移除。", "删除任务")) { try { await api(`/api/tasks/${target.dataset.deleteTask}`, { method: "DELETE" }); toast("任务已删除"); await loadReqDetail(); } catch (error) { toast(error.message, true); } }
    return;
  }
  if (target.matches("[data-session-log]")) {
    const id = target.dataset.sessionLog;
    if (state.openLogs.has(id)) { state.openLogs.delete(id); }
    else { state.openLogs.add(id); }
    const pre = target.closest(".session").querySelector(".session-log");
    const opening = state.openLogs.has(id);
    target.textContent = opening ? "收起日志" : "日志";
    pre.classList.toggle("hidden", !opening);
    if (opening) {
      pre.textContent = "加载中…";
      try {
        const data = await api(`/api/agent-sessions/${id}/log`);
        pre.textContent = data.log || "（暂无输出）";
        pre.scrollTop = pre.scrollHeight;
      } catch (error) { pre.textContent = `日志读取失败：${error.message}`; }
    }
    return;
  }
  if (target.matches("[data-stop-session]")) {
    if (await askConfirmation("停止 Agent 会话", "正在运行的进程及其子进程将被终止，已有日志和会话记录会保留。", "停止会话")) {
      try { await api(`/api/agent-sessions/${target.dataset.stopSession}/stop`, { body: {} }); toast("Agent 会话已停止"); await loadReqDetail(); } catch (error) { toast(error.message, true); }
    }
    return;
  }
  if (target.matches("[data-stop-run]")) {
    if (await askConfirmation("停止验证任务", "正在运行的命令及其子进程将被终止，已有输出会保留并登记为失败证据。", "停止任务")) {
      try { await api(`/api/verification-runs/${target.dataset.stopRun}/stop`, { body: {} }); toast("正在停止验证任务"); await pollLiveWork(); } catch (error) { toast(`停止失败：${error.message}`, true); }
    }
    return;
  }
});

// --- 委托事件：变更与提交 ---

document.addEventListener("change", async (event) => {
  if (!event.target.matches("[data-task-status]")) return;
  try { await api(`/api/tasks/${event.target.dataset.taskStatus}`, { method: "PATCH", body: { status: event.target.value } }); toast("任务状态已更新"); await loadReqDetail(); } catch (error) { toast(error.message, true); await loadReqDetail(); }
});

document.addEventListener("submit", async (event) => {
  const form = event.target;
  if (form.matches("#req-edit-form")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api(`/api/requirements/${state.reqId}`, { method: "PATCH", body: { title: form.title.value.trim(), description: form.description.value.trim() } }); toast("需求已更新"); await loadWorkbench(state.reqId); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-proof-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api(`/api/criteria/${form.dataset.proofForm}`, { method: "PATCH", body: { satisfied: true, evidenceTitle: form.title.value.trim(), evidenceNote: form.note.value.trim() } }); toast("证据已登记，验收标准已通过"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-criterion-edit-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api(`/api/criteria/${form.dataset.criterionEditForm}`, { method: "PATCH", body: { description: form.description.value.trim() } }); toast("验收标准已更新"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-task-edit-form]")) {
    event.preventDefault();
    const dependsOn = [...form.querySelectorAll('input[name="dependsOn"]:checked')].map((input) => input.value);
    await busy(event.submitter, async () => { try { await api(`/api/tasks/${form.dataset.taskEditForm}`, { method: "PATCH", body: { title: form.title.value.trim(), description: form.description.value.trim(), dependsOn } }); toast("任务与依赖已更新"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-worktree-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api(`/api/tasks/${form.dataset.worktreeForm}/worktree`, { body: { repositoryPath: form.repositoryPath.value, branch: form.branch.value.trim(), worktreePath: form.worktreePath.value.trim() } }); toast("隔离工作区已创建并关联"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-session-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api("/api/agent-sessions", { body: { taskId: form.dataset.sessionForm, provider: form.provider.value, prompt: form.prompt.value.trim() } }); form.prompt.value = ""; toast("Agent 会话已启动"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
});

route();
