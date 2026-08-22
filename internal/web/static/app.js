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
const lines = (value) => String(value || "").split(/\r?\n/).map((item) => item.trim()).filter(Boolean);
const lineText = (values) => (values || []).join("\n");

function refreshIcons(root = document) {
  if (!window.lucide?.createIcons) return;
  // Lucide replaces every matching element in the document. Keeping this helper
  // centralized makes icons added by live updates behave like the static shell.
  window.lucide.createIcons({ attrs: { "aria-hidden": "true" } });
}

const state = {
  view: "workbench",
  workbenchMode: "intent",
  reqId: "",
  requirements: [],
  repos: [],
  gitRepoPath: "",
	gitStructured: null,
	aiProviders: [],
	aiPlan: null,
  agentAdapters: [],
  handoffs: new Map(),
  planDraft: null,
  requirementDetail: null,
  rcData: null,
  reportData: null,
  criteria: [],
  openLogs: new Set(),
  pollTimer: null,
};
const taskStatus = { todo: "待办", in_progress: "进行中", done: "已完成" };
const evidenceStatus = { passed: "通过", failed: "未通过", informational: "参考" };
let toastTimer;

async function api(path, options = {}) {
  const timeoutMs = options.timeoutMs || 30000;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(path, {
      method: options.method || (options.body === undefined ? "GET" : "POST"),
      headers: options.body === undefined ? undefined : { "Content-Type": "application/json" },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      cache: options.body === undefined ? "no-store" : undefined,
      signal: controller.signal,
    });
    const text = await response.text();
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
    if (!response.ok) throw new Error(data?.error || `HTTP ${response.status}`);
    return data;
  } catch (error) {
    if (error?.name === "AbortError") throw new Error(`请求超过 ${Math.round(timeoutMs / 1000)} 秒，已停止等待`);
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

function toast(message, isError = false) {
  const target = $("#toast");
  target.textContent = message;
  target.className = isError ? "error" : "";
  target.setAttribute("role", isError ? "alert" : "status");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => target.classList.add("hidden"), 3500);
}

async function busy(button, action) {
  if (!button) return action();
  button.disabled = true;
  button.classList.add("is-busy");
  button.setAttribute("aria-busy", "true");
  try { return await action(); } finally { button.disabled = false; button.classList.remove("is-busy"); button.removeAttribute("aria-busy"); refreshIcons(button); }
}

// 渲染带重试按钮的错误态
function renderError(el, message, retry) {
  el.innerHTML = `<div class="empty error-box"><span>${esc(message)}</span><button type="button" class="small" data-retry><i data-lucide="refresh-cw"></i>重试</button></div>`;
  el.querySelector("[data-retry]").addEventListener("click", retry);
  refreshIcons(el);
}

function downloadJSON(data, filename) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2) + "\n"], { type: "application/json" }));
  const anchor = document.createElement("a"); anchor.href = url; anchor.download = filename; anchor.click(); URL.revokeObjectURL(url);
}

// --- 路由 ---

function currentRoute() {
  const parts = (location.hash || "#/workbench").replace(/^#\/?/, "").split("/").filter(Boolean);
  let view = ["workbench", "activity", "git", "settings"].includes(parts[0]) ? parts[0] : "workbench";
  if (parts[0] === "repos") view = "settings";
  const legacyMode = parts[2] === "plan" ? "intent" : parts[2];
  const mode = ["intent", "acceptance", "execute", "verify", "release"].includes(legacyMode) ? legacyMode : "intent";
  const settingsTab = parts[0] === "repos" ? "repos" : ["ai", "agents", "repos"].includes(parts[1]) ? parts[1] : "ai";
  return { view, reqId: view === "workbench" ? (parts[1] || "") : "", mode, settingsTab };
}

function route(event) {
  const { view, reqId, mode, settingsTab } = currentRoute();
  const sameWorkbench = state.view === "workbench" && view === "workbench" && state.reqId === reqId && !$("#req-content").classList.contains("hidden");
  state.view = view;
  state.workbenchMode = mode;
  document.querySelectorAll("#mainnav a").forEach((link) => {
    const active = link.dataset.nav === view;
    link.classList.toggle("active", active);
    if (active) link.setAttribute("aria-current", "page"); else link.removeAttribute("aria-current");
  });
  document.querySelectorAll(".view").forEach((item) => item.classList.toggle("active", item.id === `view-${view}`));
  document.title = `${({ workbench: "工作台", activity: "活动中心", git: "代码变更", settings: "设置" })[view]} · DevCycle`;
  if (view === "workbench" && sameWorkbench) applyWorkbenchMode();
  else if (view === "workbench") loadWorkbench(reqId).catch((error) => toast(error.message, true));
  else if (view === "activity") loadActivity().catch((error) => toast(error.message, true));
  else if (view === "git") loadGitView().catch((error) => toast(error.message, true));
  else loadSettings(settingsTab).catch((error) => toast(error.message, true));
  if (event) {
    const heading = document.querySelector(`#view-${view} h1, #view-${view} h2`);
    if (heading) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); }
  }
  refreshIcons();
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
  const mobileListOnly = matchMedia("(max-width: 640px)").matches && !reqIdFromHash;
  const target = mobileListOnly ? "" : exists(reqIdFromHash) ? reqIdFromHash : exists(state.reqId) ? state.reqId : (state.requirements[0]?.id || "");
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
  refreshIcons(listEl);
}

function showReqEmpty() {
  $("#req-empty").classList.remove("hidden");
  $("#req-content").classList.add("hidden");
  const first = state.requirements.length === 0;
  $("#start-first-requirement").innerHTML = `<i data-lucide="${first ? "plus" : "list-tree"}"></i>${first ? "新建第一个需求" : "选择一个需求"}`;
  refreshIcons($("#start-first-requirement"));
  document.body.classList.remove("has-selected-requirement");
  stopPoll();
}

// --- 工作台：需求详情 ---

async function loadReqDetail() {
  const id = state.reqId;
  if (!id) { showReqEmpty(); return; }
  $("#req-empty").classList.add("hidden");
  document.body.classList.add("has-selected-requirement");
  const content = $("#req-content");
  content.classList.remove("hidden");
  $("#req-header").innerHTML = loadingHTML;
  $("#readiness-band").innerHTML = "";
	renderAIPlan(null);
  $("#criteria-list").innerHTML = $("#task-list").innerHTML = $("#run-list").innerHTML = $("#evidence-list").innerHTML = loadingHTML;
  resetReportArea();
  try {
    const [detail, evidence, runs, rc, sessions, repos, providers, adapters] = await Promise.all([
      api(`/api/requirements/${id}`),
      api(`/api/requirements/${id}/evidence`),
      api(`/api/requirements/${id}/runs`),
      api(`/api/requirements/${id}/release-candidate`),
      api(`/api/agent-sessions`),
      api("/api/repositories"),
		api("/api/ai/providers"),
      api("/api/agent-adapters"),
    ]);
    const handoffEntries = await Promise.all((detail.tasks || []).map(async (task) => [task.id, await api(`/api/tasks/${task.id}/handoffs`)]));
    state.repos = repos || [];
	state.criteria = detail.criteria || [];
	state.aiProviders = providers || [];
	state.agentAdapters = adapters || [];
    state.handoffs = new Map(handoffEntries);
    state.requirementDetail = detail;
	renderAIProviders();
    renderReqHeader(detail.requirement);
    renderReadiness(rc.readiness, evidence || []);
    setPlanDraft(detail.plan || null);
    renderCriteria(detail.criteria || [], evidence || []);
    renderTasks(detail.tasks || [], sessions || [], state.handoffs);
    renderRuns(runs || [], detail.criteria || []);
    renderEvidence(evidence || [], detail.criteria || []);
    fillCriterionSelect($("#run-criterion"), detail.criteria || []);
    fillCriterionSelect($("#ev-criterion"), detail.criteria || []);
    schedulePoll(sessions || [], runs || []);
    applyWorkbenchMode();
    refreshIcons(content);
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
	select.innerHTML = state.aiProviders.map((provider) => `<option value="${esc(provider.id)}" ${provider.available ? "" : "disabled"}>${esc(provider.name)}${provider.available ? "" : "（不可用）"}</option>`).join("");
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
	target.innerHTML = `<div class="ai-preview-summary"><div class="item-head"><div><b>${esc(plan.meta?.provider || plan.provider || "AI")} 已生成完整草稿</b><div class="meta">${fmtTime(plan.meta?.generatedAt)} · ${plan.criteria?.length || 0} 条验收 · ${plan.testCases?.length || 0} 个用例 · ${plan.tasks?.length || 0} 项任务 · 脱敏 ${plan.meta?.redactionCount || 0} 项</div></div><button type="button" class="small" data-discard-ai-plan><i data-lucide="undo-2"></i>恢复已保存计划</button></div></div>`;
	setPlanDraft(plan);
	refreshIcons(target);
}

function blankPlan() {
  const requirement = state.requirementDetail?.requirement || {};
  return { schemaVersion: "devcycle.planning-document/v1", requirementId: state.reqId, understanding: requirement.description || requirement.title || "", scope: { included: [], excluded: [] }, assumptions: [], openQuestions: [], criteria: [], testCases: [], testStrategy: { summary: "", environments: [], commands: [] }, tasks: [], risks: [], rollbackConcerns: [], candidateNotes: "", source: "manual", provider: "", status: "draft", revision: 0 };
}

function normalizePlan(plan) {
  const value = JSON.parse(JSON.stringify(plan || blankPlan()));
  value.scope ||= { included: [], excluded: [] };
  value.assumptions ||= []; value.openQuestions ||= []; value.criteria ||= []; value.testCases ||= [];
  value.testStrategy ||= { summary: "", environments: [], commands: [] };
  value.testStrategy.environments ||= []; value.testStrategy.commands ||= [];
  value.tasks ||= []; value.risks ||= []; value.rollbackConcerns ||= [];
  value.source ||= "manual"; value.status ||= "draft"; value.revision ||= 0;
  return value;
}

function setPlanDraft(plan) {
  state.planDraft = plan ? normalizePlan(plan) : null;
  renderPlanEditor();
}

function planRemoveButton(group, index) {
  return `<button type="button" class="icon-btn danger" data-plan-remove="${group}" data-plan-index="${index}" title="移除" aria-label="移除"><i data-lucide="trash-2"></i></button>`;
}

function renderPlanEditor() {
  const target = $("#plan-editor");
  const plan = state.planDraft;
  $("#btn-save-plan").disabled = !plan || plan.status === "applied";
  $("#btn-apply-plan").disabled = !plan || plan.status === "applied";
  $("#plan-state").textContent = !plan ? "尚未建立" : plan.status === "applied" ? `已拆分 · 修订 ${plan.revision}` : `${plan.source === "ai" ? "AI 草稿" : "手工草稿"} · 修订 ${plan.revision || 0}`;
  if (!plan) {
    target.innerHTML = `<div class="plan-empty"><b>还没有交付计划</b><p>可以手动建立，也可以让已配置的 AI 生成完整草稿。</p></div>`;
    return;
  }
  const questions = plan.openQuestions.map((item, index) => `<div class="plan-row" data-plan-row="question"><div class="plan-row-head"><b>待确认 ${index + 1}</b>${planRemoveButton("question", index)}</div><label>问题<input name="question" value="${esc(item.question)}"></label><div class="grid-2"><label>建议默认值<input name="suggestedDefault" value="${esc(item.suggestedDefault || "")}"></label><label class="toggle"><input name="blocking" type="checkbox" ${item.blocking ? "checked" : ""}><span>阻塞执行</span></label></div></div>`).join("");
  const criteria = plan.criteria.map((item, index) => `<div class="plan-row" data-plan-row="criterion"><div class="plan-row-head"><b>标准 ${index + 1}</b>${planRemoveButton("criterion", index)}</div><label>可核验条件<textarea name="criterionDescription" rows="2">${esc(item.description)}</textarea></label><label>设计理由<input name="criterionRationale" value="${esc(item.rationale || "")}"></label></div>`).join("");
  const tests = plan.testCases.map((item, index) => `<div class="plan-row" data-plan-row="test"><div class="plan-row-head"><b>测试用例 ${index + 1}</b>${planRemoveButton("test", index)}</div><div class="grid-2"><label>标题<input name="testTitle" value="${esc(item.title)}"></label><label>类型<select name="testKind">${["unit", "integration", "e2e", "manual", "security", "performance"].map((kind) => `<option value="${kind}" ${item.kind === kind ? "selected" : ""}>${kind}</option>`).join("")}</select></label></div><label>关联验收标准<input name="testCriterion" value="${esc(item.criterion)}" list="plan-criteria-options"></label><div class="grid-3"><label>准备（每行一项）<textarea name="testSetup" rows="3">${esc(lineText(item.setup))}</textarea></label><label>步骤（每行一项）<textarea name="testSteps" rows="3">${esc(lineText(item.steps))}</textarea></label><label>预期（每行一项）<textarea name="testExpected" rows="3">${esc(lineText(item.expected))}</textarea></label></div></div>`).join("");
  const tasks = plan.tasks.map((item, index) => `<div class="plan-row" data-plan-row="task"><div class="plan-row-head"><b>任务 ${index + 1}</b>${planRemoveButton("task", index)}</div><div class="grid-2"><label>标题<input name="planTaskTitle" value="${esc(item.title)}"></label><label>建议执行者<select name="suggestedAdapter">${["human", "codex", "claude", "gemini", "kimi", "opencode", "custom"].map((adapter) => `<option value="${adapter}" ${item.suggestedAdapter === adapter ? "selected" : ""}>${adapter}</option>`).join("")}</select></label></div><label>描述<textarea name="planTaskDescription" rows="2">${esc(item.description || "")}</textarea></label><div class="grid-3"><label>前置任务标题（每行一项）<textarea name="planTaskDependencies" rows="3">${esc(lineText(item.dependsOn))}</textarea></label><label>交付物（每行一项）<textarea name="planTaskDeliverables" rows="3">${esc(lineText(item.expectedDeliverables))}</textarea></label><label>安排理由<textarea name="planTaskRationale" rows="3">${esc(item.rationale || "")}</textarea></label></div></div>`).join("");
  const risks = plan.risks.map((item, index) => `<div class="plan-row" data-plan-row="risk"><div class="plan-row-head"><b>风险 ${index + 1}</b>${planRemoveButton("risk", index)}</div><div class="grid-2"><label>风险<input name="riskText" value="${esc(item.risk)}"></label><label>等级<select name="riskSeverity">${["low", "medium", "high", "critical"].map((severity) => `<option value="${severity}" ${item.severity === severity ? "selected" : ""}>${severity}</option>`).join("")}</select></label></div><label>缓解措施<textarea name="riskMitigation" rows="2">${esc(item.mitigation || "")}</textarea></label></div>`).join("");
  target.innerHTML = `<form id="plan-form" class="plan-form"><datalist id="plan-criteria-options">${plan.criteria.map((item) => `<option value="${esc(item.description)}"></option>`).join("")}</datalist>
    <section class="plan-section"><header><h4>目标与边界</h4></header><label>需求理解<textarea name="understanding" rows="4">${esc(plan.understanding)}</textarea></label><div class="grid-2"><label>包含范围（每行一项）<textarea name="scopeIncluded" rows="4">${esc(lineText(plan.scope.included))}</textarea></label><label>不包含范围（每行一项）<textarea name="scopeExcluded" rows="4">${esc(lineText(plan.scope.excluded))}</textarea></label></div><label>合理假设（每行一项）<textarea name="assumptions" rows="3">${esc(lineText(plan.assumptions))}</textarea></label></section>
    <section class="plan-section"><header><h4>待确认信息</h4><button type="button" class="small" data-plan-add="question"><i data-lucide="plus"></i>添加</button></header><div class="plan-repeat">${questions || empty("没有待确认问题")}</div></section>
    <section class="plan-section"><header><h4>验收标准</h4><button type="button" class="small" data-plan-add="criterion"><i data-lucide="plus"></i>添加</button></header><div class="plan-repeat">${criteria || empty("尚未规划验收标准")}</div></section>
    <section class="plan-section"><header><h4>测试用例与策略</h4><button type="button" class="small" data-plan-add="test"><i data-lucide="plus"></i>添加用例</button></header><label>测试策略<textarea name="testSummary" rows="3">${esc(plan.testStrategy.summary || "")}</textarea></label><div class="grid-2"><label>测试环境（每行一项）<textarea name="testEnvironments" rows="3">${esc(lineText(plan.testStrategy.environments))}</textarea></label><label>验证命令（每行一项）<textarea name="testCommands" rows="3">${esc(lineText(plan.testStrategy.commands))}</textarea></label></div><div class="plan-repeat">${tests || empty("尚未规划测试用例")}</div></section>
    <section class="plan-section"><header><h4>开发任务</h4><button type="button" class="small" data-plan-add="task"><i data-lucide="plus"></i>添加任务</button></header><div class="plan-repeat">${tasks || empty("尚未规划开发任务")}</div></section>
    <section class="plan-section"><header><h4>风险、回滚与候选说明</h4><button type="button" class="small" data-plan-add="risk"><i data-lucide="plus"></i>添加风险</button></header><div class="plan-repeat">${risks || empty("尚未记录风险")}</div><div class="grid-2"><label>回滚关注点（每行一项）<textarea name="rollbackConcerns" rows="4">${esc(lineText(plan.rollbackConcerns))}</textarea></label><label>发布候选说明<textarea name="candidateNotes" rows="4">${esc(plan.candidateNotes || "")}</textarea></label></div></section>
  </form>`;
  refreshIcons(target);
}

function syncPlanFromForm() {
  const form = $("#plan-form");
  if (!form || !state.planDraft) return state.planDraft;
  const plan = normalizePlan(state.planDraft);
  plan.understanding = form.elements.understanding.value.trim();
  plan.scope = { included: lines(form.elements.scopeIncluded.value), excluded: lines(form.elements.scopeExcluded.value) };
  plan.assumptions = lines(form.elements.assumptions.value);
  plan.openQuestions = [...form.querySelectorAll('[data-plan-row="question"]')].map((row) => ({ question: row.querySelector('[name="question"]').value.trim(), blocking: row.querySelector('[name="blocking"]').checked, suggestedDefault: row.querySelector('[name="suggestedDefault"]').value.trim() }));
  plan.criteria = [...form.querySelectorAll('[data-plan-row="criterion"]')].map((row) => ({ description: row.querySelector('[name="criterionDescription"]').value.trim(), rationale: row.querySelector('[name="criterionRationale"]').value.trim() }));
  plan.testCases = [...form.querySelectorAll('[data-plan-row="test"]')].map((row) => ({ title: row.querySelector('[name="testTitle"]').value.trim(), criterion: row.querySelector('[name="testCriterion"]').value.trim(), kind: row.querySelector('[name="testKind"]').value, setup: lines(row.querySelector('[name="testSetup"]').value), steps: lines(row.querySelector('[name="testSteps"]').value), expected: lines(row.querySelector('[name="testExpected"]').value) }));
  plan.testStrategy = { summary: form.elements.testSummary.value.trim(), environments: lines(form.elements.testEnvironments.value), commands: lines(form.elements.testCommands.value) };
  plan.tasks = [...form.querySelectorAll('[data-plan-row="task"]')].map((row, index) => ({ title: row.querySelector('[name="planTaskTitle"]').value.trim(), description: row.querySelector('[name="planTaskDescription"]').value.trim(), dependsOn: lines(row.querySelector('[name="planTaskDependencies"]').value), rationale: row.querySelector('[name="planTaskRationale"]').value.trim(), order: index + 1, suggestedAdapter: row.querySelector('[name="suggestedAdapter"]').value, expectedDeliverables: lines(row.querySelector('[name="planTaskDeliverables"]').value) }));
  plan.risks = [...form.querySelectorAll('[data-plan-row="risk"]')].map((row) => ({ risk: row.querySelector('[name="riskText"]').value.trim(), severity: row.querySelector('[name="riskSeverity"]').value, mitigation: row.querySelector('[name="riskMitigation"]').value.trim() }));
  plan.rollbackConcerns = lines(form.elements.rollbackConcerns.value);
  plan.candidateNotes = form.elements.candidateNotes.value.trim();
  state.planDraft = plan;
  return plan;
}

async function confirmAIRequest(providerID, input) {
  const provider = state.aiProviders.find((item) => item.id === providerID);
  const dialog = $("#ai-request-dialog");
  const target = $("#ai-request-preview");
  let preview;
  if (provider?.transport === "api") {
    preview = await api("/api/ai/request-preview", { body: { provider: providerID, input: input || state.requirementDetail?.requirement?.description || "生成交付计划" } });
    target.innerHTML = `<div class="request-preview"><div class="preview-facts"><div><b>${esc(preview.model)}</b><span>模型</span></div><div><b>${preview.redactionCount || 0}</b><span>已脱敏项</span></div><div><b>${esc(preview.kind)}</b><span>协议</span></div></div><label>发送到<input value="${esc(preview.endpoint)}" readonly></label><label>脱敏后的补充内容<textarea rows="5" readonly>${esc(preview.redactedInput)}</textarea></label><div class="grid-2"><div><h4>会发送</h4>${(preview.sends || []).map((item) => `<div class="meta">✓ ${esc(item)}</div>`).join("")}</div><div><h4>不会发送</h4>${(preview.excludes || []).map((item) => `<div class="meta">× ${esc(item)}</div>`).join("")}</div></div></div>`;
  } else {
    target.innerHTML = `<div class="request-preview"><div class="preview-facts"><div><b>${esc(provider?.name || providerID)}</b><span>本机 CLI</span></div><div><b>本机</b><span>执行位置</span></div><div><b>只读临时目录</b><span>工作区</span></div></div><div class="meta">需求和补充内容会在后端脱敏后交给本机命令行，计划生成过程不读取项目工作区。</div></div>`;
  }
  refreshIcons(dialog);
  dialog.showModal();
  return new Promise((resolve) => dialog.addEventListener("close", () => resolve(dialog.returnValue === "confirm"), { once: true }));
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
  const target = $("#req-header");
  target.innerHTML = `<header class="req-header card"><div class="req-head-main"><h2>${esc(req.title)}</h2><div class="meta">${esc(req.id)} · 更新于 ${fmtTime(req.updatedAt)}</div>${req.description ? `<div class="desc">${esc(req.description)}</div>` : ""}</div><div class="item-actions"><button class="small" type="button" data-edit-req><i data-lucide="pencil"></i>编辑</button><button class="small danger" type="button" data-delete-req="${esc(req.id)}"><i data-lucide="trash-2"></i>删除需求</button></div><form class="inline-editor hidden" id="req-edit-form"><label>标题<input name="title" value="${esc(req.title)}" required></label><label>描述<textarea name="description" rows="2">${esc(req.description || "")}</textarea></label><div class="actions"><button class="btn" type="submit"><i data-lucide="save"></i>保存</button><button class="btn ghost" type="button" data-cancel-edit>取消</button></div></form></header>`;
  refreshIcons(target);
}

function progressCell(value, total, label) {
  return `<div><b>${value}/${total}</b><span>${label}</span><progress value="${value}" max="${total || 1}" aria-label="${label}"></progress></div>`;
}

function renderReadiness(readiness, evidence) {
  const latestByCriterion = new Map();
  evidence.forEach((item) => {
    if (!item.criterionId) return;
    const existing = latestByCriterion.get(item.criterionId);
    if (!existing || new Date(item.createdAt || 0) > new Date(existing.createdAt || 0)) latestByCriterion.set(item.criterionId, item);
  });
  const current = [...latestByCriterion.values()];
  const passed = current.filter((item) => item.status === "passed").length;
  const failed = current.filter((item) => item.status === "failed").length;
  $("#readiness-band").innerHTML = `<div class="summary-band readiness-summary"><div><b class="${readiness.ready ? "text-ok" : "text-warn"}">${readiness.ready ? "可发布" : "未就绪"}</b><span>发布就绪</span></div>${progressCell(readiness.criteriaSatisfied, readiness.criteriaTotal, "验收通过")}${progressCell(readiness.criteriaWithEvidence, readiness.criteriaTotal, "证据覆盖")}${progressCell(readiness.tasksDone, readiness.tasksTotal, "任务完成")}${progressCell(readiness.sourcesClean || 0, readiness.sourcesTotal || 0, "代码源干净")}<div><b>${passed} 当前通过 / ${failed} 当前未过</b><span>${current.length} 项标准有最新证据 · 历史 ${evidence.length} 条</span></div></div>`;
}

function fillCriterionSelect(select, criteria) {
  select.innerHTML = '<option value="">不关联</option>' + criteria.map((item) => `<option value="${esc(item.id)}">${esc(item.description)}</option>`).join("");
}

// --- 验收标准 ---

function renderCriteria(criteria, evidence) {
  const target = $("#criteria-list");
  target.innerHTML = criteria.length ? criteria.map((criterion) => {
    const linked = evidence.filter((item) => item.criterionId === criterion.id);
    const latest = linked[0];
    const latestPassed = latest?.status === "passed";
    return `<article class="item"><div class="item-head"><div><div class="title">${esc(criterion.description)} <span class="badge ${criterion.satisfied && latestPassed ? "done" : "warn"}">${criterion.satisfied && latestPassed ? "已满足" : "未满足"}</span></div><div class="meta">${linked.length ? `最新证据：${esc(evidenceStatus[latest.status] || latest.status)} · ${fmtTime(latest.createdAt)}` : "还没有证据"}</div></div><div class="item-actions"><button class="small" data-edit-criterion="${esc(criterion.id)}" type="button"><i data-lucide="pencil"></i>编辑</button><button class="icon-btn danger" data-delete-criterion="${esc(criterion.id)}" type="button" title="删除验收标准" aria-label="删除验收标准"><i data-lucide="trash-2"></i></button></div></div><form class="inline-editor hidden" data-criterion-edit-form="${esc(criterion.id)}"><label>验收标准<textarea name="description" rows="2" minlength="5" maxlength="2000" required>${esc(criterion.description)}</textarea></label><div class="actions"><button class="btn" type="submit"><i data-lucide="save"></i>保存</button><button class="btn ghost" data-cancel-criterion-edit type="button">取消</button></div></form><div class="item-actions criterion-actions">${criterion.satisfied && latestPassed ? `<button class="small" data-unsatisfy="${esc(criterion.id)}" type="button"><i data-lucide="undo-2"></i>撤回通过</button>` : latestPassed ? `<button class="small" data-satisfy="${esc(criterion.id)}" type="button"><i data-lucide="check"></i>标记通过</button>` : `<button class="small" data-show-proof="${esc(criterion.id)}" type="button"><i data-lucide="badge-check"></i>登记证据并通过</button>`}</div><form class="proof-form hidden" data-proof-form="${esc(criterion.id)}"><label>证据标题<input name="title" required placeholder="例如：自动化测试通过"></label><label>结果说明<textarea name="note" rows="2" placeholder="关键输出或人工核对结论"></textarea></label><div class="actions"><button class="btn" type="submit"><i data-lucide="badge-check"></i>登记并通过</button><button class="btn ghost" data-cancel-proof type="button">取消</button></div></form></article>`;
  }).join("") : empty("这个需求还没有验收标准。");
  refreshIcons(target);
}

// --- 任务与 Agent ---

function repoOptions() {
  return state.repos.map((repo) => `<option value="${esc(repo.path)}">${esc(repo.name)} · ${esc(repo.path)}</option>`).join("") || '<option value="">请先在设置中导入仓库</option>';
}

function adapterOptions(selected = "") {
  return state.agentAdapters.map((adapter) => `<option value="${esc(adapter.id)}" ${adapter.id === selected ? "selected" : ""} ${adapter.available && adapter.enabled ? "" : "disabled"}>${esc(adapter.name)}${adapter.available ? "" : "（不可用）"}</option>`).join("") || '<option value="">没有可用适配器</option>';
}

function renderHandoffs(items) {
  return items.length ? items.map((handoff) => `<article class="handoff"><div><div class="title"><span class="badge ${esc(handoff.status)}">${handoff.status === "accepted" ? "已接收" : "待接收"}</span> ${esc(handoff.fromAdapter || "人工")} → ${esc(handoff.toAdapter)}</div><div class="meta">${fmtTime(handoff.createdAt)}${handoff.changedFiles?.length ? ` · ${handoff.changedFiles.length} 个变更文件` : ""}</div><div class="desc">${esc(handoff.summary)}</div>${handoff.remainingWork?.length ? `<div class="meta">待完成：${handoff.remainingWork.map(esc).join("；")}</div>` : ""}${handoff.risks?.length ? `<div class="meta text-warn">风险：${handoff.risks.map(esc).join("；")}</div>` : ""}</div>${handoff.status === "open" ? `<button type="button" class="small" data-accept-handoff="${esc(handoff.id)}"><i data-lucide="handshake"></i>接收</button>` : ""}</article>`).join("") : empty("还没有任务交接记录。");
}

function renderTasks(tasks, sessions, handoffs = new Map()) {
	const taskByID = new Map(tasks.map((task) => [task.id, task]));
  $("#task-list").innerHTML = tasks.length ? tasks.map((task) => {
    const taskSessions = sessions.filter((session) => session.taskId === task.id);
	const workspace = task.worktreePath ? `<div class="workspace"><span><i data-lucide="git-branch"></i> <code>${esc(task.branch)}</code></span><span><i data-lucide="folder-git-2"></i> <code>${esc(task.worktreePath)}</code></span></div>` : `<details class="tool-panel"><summary><i data-lucide="folder-git-2"></i>创建隔离工作区</summary><form data-worktree-form="${esc(task.id)}"><label>仓库<select name="repositoryPath" required>${repoOptions()}</select></label><div class="grid-2"><label>分支<input name="branch" value="devcycle/${esc(task.id.slice(0, 8))}" required></label><label>工作区路径<input name="worktreePath" required placeholder="仓库同级的独立目录"></label></div><div class="actions"><button class="btn" type="submit">创建并关联</button></div></form></details>`;
    const agent = task.worktreePath ? `<details class="tool-panel"><summary><i data-lucide="terminal-square"></i>Agent 会话（${taskSessions.length}）</summary><form data-session-form="${esc(task.id)}"><label>Agent 适配器<select name="provider" required>${adapterOptions()}</select></label><label>任务说明<textarea name="prompt" rows="4" required placeholder="目标、约束、已有上下文和验收方式"></textarea></label><div class="actions"><button class="btn" type="submit"><i data-lucide="play"></i>启动会话</button><a class="text-link" href="#/settings/agents">管理适配器</a></div></form><div class="sessions" data-sessions-for="${esc(task.id)}">${renderSessions(taskSessions)}</div></details>` : "";
	const terminalSessions = taskSessions.filter((session) => session.status !== "running");
    const handoff = `<details class="tool-panel"><summary><i data-lucide="handshake"></i>任务交接（${(handoffs.get(task.id) || []).length}）</summary><form data-handoff-form="${esc(task.id)}"><div class="grid-2"><label>来源会话<select name="fromSessionId"><option value="">人工整理</option>${terminalSessions.map((session) => `<option value="${esc(session.id)}">${esc(session.provider)} · ${fmtTime(session.startedAt)}</option>`).join("")}</select></label><label>交给<select name="toAdapter" required>${adapterOptions()}</select></label></div><label>交接摘要<textarea name="summary" rows="3" minlength="5" maxlength="4000" required></textarea></label><div class="grid-2"><label>已完成（每行一项）<textarea name="completedWork" rows="3"></textarea></label><label>待完成（每行一项）<textarea name="remainingWork" rows="3"></textarea></label><label>风险（每行一项）<textarea name="risks" rows="3"></textarea></label><label>验证事项（每行一项）<textarea name="validation" rows="3"></textarea></label></div><div class="actions"><button class="btn" type="submit"><i data-lucide="send"></i>创建交接</button></div></form><div class="handoffs" data-handoffs-for="${esc(task.id)}">${renderHandoffs(handoffs.get(task.id) || [])}</div></details>`;
	const dependencyTasks = (task.dependsOn || []).map((id) => taskByID.get(id)).filter(Boolean);
	const blocked = dependencyTasks.some((item) => item.status !== "done");
	const dependencies = dependencyTasks.length ? `<div class="dependency-line ${blocked ? "blocked" : "ready"}"><span>${blocked ? "被前置任务阻塞" : "前置任务已完成"}</span>${dependencyTasks.map((item) => `<code>${esc(item.title)}</code>`).join("")}</div>` : "";
	const dependencyChoices = tasks.filter((candidate) => candidate.id !== task.id).map((candidate) => `<label class="check-row"><input type="checkbox" name="dependsOn" value="${esc(candidate.id)}" ${(task.dependsOn || []).includes(candidate.id) ? "checked" : ""}><span>${esc(candidate.title)} <small>${esc(taskStatus[candidate.status] || candidate.status)}</small></span></label>`).join("") || `<span class="meta">没有其他可依赖任务</span>`;
	const editor = `<form class="task-editor hidden" data-task-edit-form="${esc(task.id)}"><label>标题<input name="title" value="${esc(task.title)}" minlength="2" maxlength="300" required></label><label>描述<textarea name="description" rows="3" maxlength="8000">${esc(task.description || "")}</textarea></label><fieldset><legend>前置任务</legend><div class="dependency-picker">${dependencyChoices}</div></fieldset><div class="actions"><button class="btn" type="submit">保存任务</button><button class="btn ghost" data-cancel-task-edit type="button">取消</button></div></form>`;
    return `<article class="item task-item${blocked ? " is-blocked" : ""}"><div class="item-head"><div><div class="title">${esc(task.title)} <span class="badge ${esc(task.status)}">${esc(taskStatus[task.status] || task.status)}</span>${blocked ? `<span class="badge warn">阻塞</span>` : ""}</div>${dependencies}</div><div class="item-actions"><button class="small" data-edit-task="${esc(task.id)}" type="button"><i data-lucide="pencil"></i>编辑</button><button class="icon-btn danger" data-delete-task="${esc(task.id)}" type="button" title="删除任务" aria-label="删除任务"><i data-lucide="trash-2"></i></button></div></div>${task.description ? `<div class="desc">${esc(task.description)}</div>` : ""}${editor}<div class="task-status"><label>状态<select data-task-status="${esc(task.id)}">${Object.entries(taskStatus).map(([value, label]) => `<option value="${value}" ${value === task.status ? "selected" : ""} ${(blocked && value !== "todo") ? "disabled" : ""}>${label}</option>`).join("")}</select></label></div>${workspace}${agent}${handoff}</article>`;
  }).join("") : empty("还没有任务。每个可发布需求至少应有一个明确任务。");
  refreshIcons($("#task-list"));
}

function renderSessions(sessions) {
  return sessions.length ? sessions.map((session) => `<div class="session"><div><b>${esc(session.provider)}</b> <span class="badge ${esc(session.status)}">${esc(session.status)}</span><div class="meta">${fmtTime(session.startedAt)} · ${fmtDuration(session.durationMilliseconds)}${session.pid ? ` · PID ${session.pid}` : ""}</div></div><div class="item-actions"><button class="small" data-session-log="${esc(session.id)}" type="button"><i data-lucide="scroll-text"></i>${state.openLogs.has(session.id) ? "收起" : "日志"}</button>${session.status === "running" ? `<button class="small danger" data-stop-session="${esc(session.id)}" type="button"><i data-lucide="square"></i>停止</button>` : ""}</div><pre class="session-log${state.openLogs.has(session.id) ? "" : " hidden"}" data-log-target="${esc(session.id)}"></pre></div>`).join("") : empty("还没有 Agent 会话，也可以手动完成任务。");
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
    const logView = new Map();
    document.querySelectorAll("[data-log-target]").forEach((pre) => logView.set(pre.dataset.logTarget, {
      scrollTop: pre.scrollTop,
      follow: pre.scrollHeight - pre.scrollTop - pre.clientHeight < 12,
    }));
    document.querySelectorAll("[data-sessions-for]").forEach((box) => {
      const taskId = box.dataset.sessionsFor;
      box.innerHTML = renderSessions(sessions.filter((session) => session.taskId === taskId));
      refreshIcons(box);
    });
    renderRuns(runs || [], state.criteria);
    renderEvidence(evidence || [], state.criteria);
    for (const id of state.openLogs) {
      const pre = document.querySelector(`[data-log-target="${id}"]`);
      if (!pre) continue;
      try {
        const data = await api(`/api/agent-sessions/${id}/log`);
        pre.textContent = data.log || "（暂无输出）";
        const view = logView.get(id);
        pre.scrollTop = !view || view.follow ? pre.scrollHeight : view.scrollTop;
      } catch (_) { /* 保留旧内容，下次再试 */ }
    }
    schedulePoll(sessions || [], runs || []);
  } catch (_) { /* 网络抖动时保持现有界面 */ }
}

// --- 活动中心 ---

async function loadActivity() {
  stopPoll();
  $("#activity-sessions").innerHTML = $("#activity-runs").innerHTML = loadingHTML;
  const [requirements, tasks, sessions] = await Promise.all([api("/api/requirements"), api("/api/tasks"), api("/api/agent-sessions")]);
  const runGroups = await Promise.all((requirements || []).map(async (requirement) => ({ requirement, runs: await api(`/api/requirements/${requirement.id}/runs`) })));
  const taskNames = new Map((tasks || []).map((task) => [task.id, task.title]));
  const taskRecords = new Map((tasks || []).map((task) => [task.id, task]));
  const requirementNames = new Map((requirements || []).map((requirement) => [requirement.id, requirement.title]));
  const runs = runGroups.flatMap((group) => (group.runs || []).map((run) => ({ ...run, requirementTitle: group.requirement.title })));
  const liveSessions = (sessions || []).filter((session) => session.status === "running");
  const liveRuns = runs.filter((run) => ["running", "stopping"].includes(run.status));
  $("#live-count").textContent = String(liveSessions.length + liveRuns.length);
  $("#live-count").classList.toggle("hidden", liveSessions.length + liveRuns.length === 0);
  $("#activity-summary").innerHTML = `<div><b>${liveSessions.length}</b><span>运行中 Agent</span></div><div><b>${liveRuns.length}</b><span>运行中验证</span></div><div><b>${(sessions || []).length}</b><span>会话总数</span></div><div><b>${runs.length}</b><span>验证总数</span></div>`;
  $("#activity-sessions").innerHTML = (sessions || []).length ? sessions.map((session) => `<article class="item"><div class="item-head"><div><div class="title">${esc(taskNames.get(session.taskId) || "已删除任务")} <span class="badge ${esc(session.status)}">${esc(session.status)}</span></div><div class="meta">${esc(session.provider)} · ${fmtTime(session.startedAt)} · ${fmtDuration(session.durationMilliseconds)}</div></div>${taskRecords.get(session.taskId) ? `<button class="small" type="button" data-activity-requirement="${esc(taskRecords.get(session.taskId).requirementId)}" data-activity-mode="execute"><i data-lucide="arrow-up-right"></i>查看任务</button>` : ""}</div></article>`).join("") : empty("还没有 Agent 会话。");
  $("#activity-runs").innerHTML = runs.length ? runs.map((run) => `<article class="item"><div class="item-head"><div><div class="title">${esc(run.name)} <span class="badge ${esc(run.status)}">${esc(run.status)}</span></div><div class="meta">${esc(run.requirementTitle || requirementNames.get(run.requirementId) || "需求")} · ${fmtDuration(run.durationMilliseconds)}</div></div><button class="small" type="button" data-activity-requirement="${esc(run.requirementId)}"><i data-lucide="arrow-up-right"></i>查看验证</button></div></article>`).join("") : empty("还没有验证运行。");
  refreshIcons($("#view-activity"));
}

$("#btn-refresh-activity").addEventListener("click", (event) => busy(event.currentTarget, loadActivity));

// --- 设置 ---

function renderProviderCards() {
  const target = $("#provider-list");
  target.innerHTML = state.aiProviders.length ? state.aiProviders.map((provider) => `<article class="provider-card ai-provider ${provider.available ? "available" : ""}"><header><div><h4>${esc(provider.name)}</h4><span class="badge ${provider.available ? "done" : "warn"}">${provider.available ? "可用" : "不可用"}</span></div><i data-lucide="${provider.transport === "api" ? "cloud-cog" : "terminal"}"></i></header><div class="provider-details"><span>${esc(provider.transport === "api" ? provider.kind : "本机 CLI")}</span>${provider.model ? `<code>${esc(provider.model)}</code>` : ""}${provider.baseUrl ? `<code>${esc(provider.baseUrl)}</code>` : ""}<span>${provider.reason ? esc(provider.reason) : provider.requiresSecret ? `凭据：${provider.secretConfigured ? esc(provider.secretSource || "已配置") : "未配置"}` : "无需凭据"}</span></div><div class="actions">${provider.transport === "api" ? `<button class="small" type="button" data-test-provider="${esc(provider.id)}"><i data-lucide="plug-zap"></i>测试</button><button class="small" type="button" data-edit-provider="${esc(provider.id)}"><i data-lucide="pencil"></i>编辑</button><button class="icon-btn danger" type="button" data-delete-provider="${esc(provider.id)}" title="删除" aria-label="删除"><i data-lucide="trash-2"></i></button>` : ""}</div></article>`).join("") : empty("还没有检测到 AI 提供方。");
  refreshIcons(target);
}

function renderAdapterCards() {
  const target = $("#adapter-list");
  target.innerHTML = state.agentAdapters.length ? state.agentAdapters.map((adapter) => `<article class="provider-card ${adapter.available ? "available" : ""}"><header><div><h4>${esc(adapter.name)}</h4><span class="badge ${adapter.available ? "done" : "warn"}">${adapter.available ? "可用" : "未安装"}</span></div><i data-lucide="bot"></i></header><div class="provider-details"><span>${adapter.builtIn ? "内置适配器" : "自定义适配器"}</span><code>${esc(adapter.command)} ${(adapter.args || []).map(esc).join(" ")}</code>${adapter.reason ? `<span>${esc(adapter.reason)}</span>` : ""}<span>${(adapter.capabilities || []).map(esc).join(" · ")}</span></div>${adapter.builtIn ? "" : `<div class="actions"><button class="small" type="button" data-edit-adapter="${esc(adapter.id)}"><i data-lucide="pencil"></i>编辑</button><button class="icon-btn danger" type="button" data-delete-adapter="${esc(adapter.id)}" title="删除" aria-label="删除"><i data-lucide="trash-2"></i></button></div>`}</article>`).join("") : empty("还没有 Agent 适配器。");
  refreshIcons(target);
}

function renderRepoList() {
  const target = $("#repo-list");
  target.innerHTML = state.repos.length ? state.repos.map((repo) => `<article class="item"><div class="item-head"><div><div class="title"><i data-lucide="folder-git-2"></i> ${esc(repo.name)}</div><div class="meta">${esc(repo.path)} · ${fmtTime(repo.createdAt)}</div></div><button class="icon-btn danger" data-delete-repo="${esc(repo.id)}" type="button" title="移除仓库记录" aria-label="移除仓库记录"><i data-lucide="trash-2"></i></button></div></article>`).join("") : empty("还没有导入仓库。");
  refreshIcons(target);
}

async function loadSettings(tab = "ai") {
  const [providers, adapters, repos] = await Promise.all([api("/api/ai/providers"), api("/api/agent-adapters"), api("/api/repositories")]);
  state.aiProviders = providers || []; state.agentAdapters = adapters || []; state.repos = repos || [];
  document.querySelectorAll("[data-settings-tab]").forEach((link) => link.classList.toggle("active", link.dataset.settingsTab === tab));
  document.querySelectorAll("[data-settings-panel]").forEach((panel) => panel.classList.toggle("hidden", panel.dataset.settingsPanel !== tab));
  renderProviderCards(); renderAdapterCards(); renderRepoList(); renderAIProviders(); refreshIcons($("#view-settings"));
}

// --- 验证运行与证据 ---

function captureDisclosureState(container) {
  const result = new Map();
  container?.querySelectorAll("details[data-disclosure]").forEach((details) => {
    const output = details.querySelector("pre");
    result.set(details.dataset.disclosure, { open: details.open, scrollTop: output?.scrollTop || 0 });
  });
  return result;
}

function restoreDisclosureState(container, disclosureState) {
  container?.querySelectorAll("details[data-disclosure]").forEach((details) => {
    const saved = disclosureState.get(details.dataset.disclosure);
    if (!saved) return;
    details.open = saved.open;
    const output = details.querySelector("pre");
    if (output) output.scrollTop = saved.scrollTop;
  });
}

function renderRuns(runs, criteria) {
  const target = $("#run-list");
  const disclosureState = captureDisclosureState(target);
  const criterionNames = Object.fromEntries(criteria.map((item) => [item.id, item.description]));
  const labels = { running: "运行中", stopping: "正在停止", passed: "通过", failed: "未通过", timed_out: "超时", stopped: "已停止", interrupted: "已中断" };
  target.innerHTML = runs.length ? runs.map((run) => {
    const active = ["running", "stopping"].includes(run.status);
    const timing = active ? `已运行 ${fmtDuration(run.durationMilliseconds)}` : `${fmtDuration(run.durationMilliseconds)} · ${fmtTime(run.completedAt)}`;
    const result = active ? timing : `退出码 ${run.exitCode} · ${timing}`;
    return `<article class="item run-item ${active ? "is-running" : ""}"><div class="item-head"><div><div class="title">${esc(run.name)} <span class="badge ${esc(run.status)}">${esc(labels[run.status] || run.status)}</span></div><div class="meta"><code>${esc(run.command)}</code> · ${result}</div></div>${active ? `<button class="small danger" type="button" data-stop-run="${esc(run.id)}" ${run.status === "stopping" ? "disabled" : ""}>${run.status === "stopping" ? "停止中" : "停止"}</button>` : ""}</div>${run.criterionId ? `<div class="desc">关联：${esc(criterionNames[run.criterionId] || run.criterionId)}</div>` : ""}<details data-disclosure="run:${esc(run.id)}" ${active ? "open" : ""}><summary>${active ? "实时输出" : "查看输出"}</summary><pre class="code run-output">${esc(run.output || (active ? "（等待命令输出）" : "（无输出）"))}</pre></details></article>`;
  }).join("") : empty("尚未运行验证命令。");
  restoreDisclosureState(target, disclosureState);
  refreshIcons(target);
}

function renderEvidence(evidence, criteria) {
  const target = $("#evidence-list");
  const disclosureState = captureDisclosureState(target);
  const criterionNames = Object.fromEntries(criteria.map((item) => [item.id, item.description]));
  target.innerHTML = evidence.length ? evidence.map((item) => `<article class="item"><div class="item-head"><div><div class="title">${esc(item.title)} <span class="badge ${esc(item.status)}">${esc(evidenceStatus[item.status] || item.status)}</span></div><div class="meta">${esc(item.kind)} · ${fmtTime(item.createdAt)}${item.criterionId ? ` · ${esc(criterionNames[item.criterionId] || item.criterionId)}` : ""}</div></div></div>${item.inline ? `<details data-disclosure="evidence:${esc(item.id || `${item.createdAt}:${item.title}`)}"><summary>查看内容</summary><pre class="code evidence-output">${esc(item.inline)}</pre></details>` : ""}${safeEvidenceLink(item.uri)}</article>`).join("") : empty("尚未登记证据。");
  restoreDisclosureState(target, disclosureState);
  refreshIcons(target);
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

$("#btn-new-plan").addEventListener("click", () => {
  setPlanDraft(blankPlan());
  $("#plan-form textarea")?.focus();
});

$("#btn-save-plan").addEventListener("click", async (event) => {
  if (!state.reqId || !state.planDraft) return;
  await busy(event.currentTarget, async () => {
    try {
      const saved = await api(`/api/requirements/${state.reqId}/plan`, { method: "PUT", body: syncPlanFromForm() });
      state.requirementDetail.plan = saved;
      setPlanDraft(saved);
      renderAIPlan(null);
      toast("计划已保存");
    } catch (error) { toast(`计划保存失败：${error.message}`, true); }
  });
});

$("#btn-apply-plan").addEventListener("click", async (event) => {
  if (!state.reqId || !state.planDraft) return;
  if (!await askConfirmation("拆分交付计划", "计划中的验收标准和开发任务会写入当前需求。", "确认拆分")) return;
  await busy(event.currentTarget, async () => {
    try {
      await api(`/api/requirements/${state.reqId}/ai-plan/apply`, { body: { document: syncPlanFromForm() } });
      toast("计划已拆分为验收标准和任务");
      state.workbenchMode = "acceptance";
      history.replaceState(null, "", `#/workbench/${state.reqId}/acceptance`);
      await loadReqDetail();
    } catch (error) { toast(`计划应用失败：${error.message}`, true); }
  });
});

function renderReportStructured(report) {
  const ready = report.candidate.readiness;
  const runLabels = { running: "运行中", stopping: "正在停止", passed: "通过", failed: "未通过", timed_out: "超时", stopped: "已停止", interrupted: "已中断" };
  const runsList = report.verificationRuns.length ? report.verificationRuns.map((run) => `<div class="mini-row"><span>${esc(run.name)}</span><span class="badge ${esc(run.status)}">${esc(runLabels[run.status] || run.status)}</span></div>`).join("") : '<div class="meta">无验证运行</div>';
  const evList = report.evidence.length ? report.evidence.map((item) => `<div class="mini-row"><span>${esc(item.title)}</span><span class="badge ${esc(item.status)}">${esc(evidenceStatus[item.status] || item.status)}</span></div>`).join("") : '<div class="meta">无证据</div>';
  const sessionList = report.agentSessions.length ? report.agentSessions.map((session) => `<div class="mini-row"><span>${esc(session.provider)} · ${fmtDuration(session.durationMilliseconds)}</span><span class="badge ${esc(session.status)}">${esc(session.status)}</span></div>`).join("") : '<div class="meta">无 Agent 会话</div>';
  const sourceList = report.candidate.sources?.length ? report.candidate.sources.map((source) => `<div class="source-row ${source.clean ? "clean" : "dirty"}"><div><b>${esc(source.taskTitle)}</b><code>${esc(source.headCommit)}</code><span>${esc(source.branch)} · ${esc(source.repositoryPath)}</span></div><span class="badge ${source.clean ? "done" : "failed"}">${source.clean ? "已钉住" : "有未提交改动"}</span></div>`).join("") : '<div class="meta">该候选没有关联代码工作区</div>';
  const warnings = report.warnings?.length ? `<div class="release-blocker"><b>报告存在未完成分析</b><span>${report.warnings.map(esc).join("；")}</span></div>` : "";
  return `<div class="report-block"><div class="summary-band compact"><div><b class="${ready.ready ? "text-ok" : "text-warn"}">${ready.ready ? "可进入发布验证" : "尚未就绪"}</b><span>${esc(report.candidate.requirement.title)}</span></div><div><b>${report.evidence.length}</b><span>证据</span></div><div><b>${report.verificationRuns.length}</b><span>验证运行</span></div><div><b>${report.agentSessions.length}</b><span>Agent 会话</span></div><div><b class="${report.evidenceComplete ? "text-ok" : "text-warn"}">${report.evidenceComplete ? "完整" : "缺失"}</b><span>证据覆盖</span></div></div>${warnings}<div class="source-proof"><h4>代码来源</h4>${sourceList}</div><div class="report-cols"><div><h4>验证运行</h4>${runsList}</div><div><h4>证据</h4>${evList}</div><div><h4>Agent 会话</h4>${sessionList}</div></div><div class="suite-handoff"><span>开发证据已汇总</span><a data-suite="releaseguard">进入 ReleaseGuard 验证发布 →</a></div><details class="json-details"><summary>查看原始 JSON</summary><pre class="code" id="report-json" tabindex="0">${esc(JSON.stringify(report, null, 2))}</pre></details></div>`;
}

function renderRcStructured(candidate) {
  const readiness = candidate.readiness;
  const dirty = (candidate.sources || []).filter((source) => !source.clean);
  return `<div class="report-block"><div class="summary-band compact"><div><b class="${readiness.ready ? "text-ok" : "text-warn"}">${readiness.ready ? "可发布" : "未就绪"}</b><span>${esc(candidate.requirement.title)}</span></div>${progressCell(readiness.criteriaSatisfied, readiness.criteriaTotal, "验收通过")}${progressCell(readiness.criteriaWithEvidence, readiness.criteriaTotal, "证据覆盖")}${progressCell(readiness.tasksDone, readiness.tasksTotal, "任务完成")}${progressCell(readiness.sourcesClean || 0, readiness.sourcesTotal || 0, "代码源干净")}</div>${dirty.length ? `<div class="release-blocker"><b>代码来源尚未钉住</b><span>${dirty.map((source) => esc(source.taskTitle)).join("、")} 仍有未提交改动</span></div>` : ""}<div class="suite-handoff"><span>候选清单已生成</span><a data-suite="releaseguard">进入 ReleaseGuard 验证发布 →</a></div><details class="json-details"><summary>查看原始 JSON</summary><pre class="code" id="rc-json" tabindex="0">${esc(JSON.stringify(candidate, null, 2))}</pre></details></div>`;
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
	const [status, diff, structured, commits, branches] = await Promise.all([api(`/api/git/status?repo=${query}`), api(`/api/git/diff?repo=${query}${range}`), api(`/api/git/structured-diff?repo=${query}${range}`), api(`/api/git/log?repo=${query}`), api(`/api/git/branches?repo=${query}&remote=1`)]);
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
      const flags = [[impact.userImpact, "monitor", "用户界面"], [impact.apiImpact, "braces", "API"], [impact.databaseImpact, "database", "数据库"], [impact.configurationImpact, "settings-2", "配置"], [impact.securityImpact, "shield-check", "安全"]].filter(([active]) => active);
      const riskLabels = { low: "低", medium: "中", high: "高" };
      const counts = [[impact.addedFiles, "新增"], [impact.modifiedFiles, "修改"], [impact.deletedFiles, "删除"], [impact.renamedFiles, "重命名"]].filter(([files]) => files?.length);
      const list = (title, icon, items) => items?.length ? `<section class="impact-list"><h4><i data-lucide="${icon}"></i>${title}</h4>${items.map((item) => `<p>${esc(item)}</p>`).join("")}</section>` : "";
      const target = $("#git-impact");
      target.innerHTML = `<div class="impact ${esc(impact.risk)}"><div class="item-head"><strong><i data-lucide="shield-alert"></i>${riskLabels[impact.risk] || esc(impact.risk)}风险</strong><span>${impact.files.length} 个文件</span></div><div class="impact-counts">${counts.map(([files, label]) => `<span><b>${files.length}</b>${label}</span>`).join("") || "没有文件变化"}</div><div class="impact-flags">${flags.map(([, icon, label]) => `<span><i data-lucide="${icon}"></i>${label}</span>`).join("") || "未识别到特殊影响面"}</div>${list("变更摘要", "list-checks", impact.summary)}${list("风险原因", "triangle-alert", impact.riskReasons)}${list("建议验证", "clipboard-check", impact.suggestedVerification)}${impact.rawDiffAvailable ? `<p class="raw-available"><i data-lucide="code-2"></i>原始 Diff 可在下方展开核对</p>` : ""}</div>`;
      refreshIcons(target);
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
			const explanation = await api("/api/git/explain", { body: { repositoryPath: state.gitRepoPath, provider: $("#git-ai-provider").value, from: $("#git-from").value.trim(), to: $("#git-to").value.trim(), staged: $("#git-staged").checked }, timeoutMs: 610000 });
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
  renderRepoList();
}

$("#form-import-repo").addEventListener("submit", async (event) => {
  event.preventDefault(); const form = event.currentTarget;
  await busy(event.submitter, async () => { try { await api("/api/repositories", { body: { path: form.path.value.trim() } }); form.reset(); toast("仓库已导入"); await loadRepos(); } catch (error) { toast(error.message, true); } });
});

$("#form-ai-provider").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const field = (name) => form.elements[name];
  await busy(event.submitter, async () => {
    try {
      await api("/api/ai/providers", { body: { id: field("id").value.trim(), name: field("name").value.trim(), kind: field("kind").value, baseUrl: field("baseUrl").value.trim(), model: field("model").value.trim(), apiPath: field("apiPath").value.trim(), apiKeyHeader: field("apiKeyHeader").value.trim(), apiKeyPrefix: field("apiKeyPrefix").value.trim(), timeoutSeconds: Number(field("timeoutSeconds").value) || 120, secret: field("secret").value, secretEnvironment: field("secretEnvironment").value.trim(), enabled: field("enabled").checked } });
      form.reset(); field("enabled").checked = true; field("timeoutSeconds").value = "120";
      toast("AI 提供方已保存"); await loadSettings("ai");
    } catch (error) { toast(error.message, true); }
  });
});

$("#form-agent-adapter").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const field = (name) => form.elements[name];
  await busy(event.submitter, async () => {
    try {
      await api("/api/agent-adapters", { body: { id: field("id").value.trim(), name: field("name").value.trim(), description: field("description").value.trim(), command: field("command").value.trim(), args: lines(field("args").value), capabilities: lines(field("capabilities").value), enabled: field("enabled").checked } });
      form.reset(); field("enabled").checked = true; field("args").value = "run\n{{prompt}}"; field("capabilities").value = "code_editing\nnon_interactive\nworktree";
      toast("Agent 适配器已保存"); await loadSettings("agents");
    } catch (error) { toast(error.message, true); }
  });
});

// --- 表单提交 ---

$("#form-create-req").addEventListener("submit", async (event) => {
  event.preventDefault(); const form = event.currentTarget;
  await busy(event.submitter, async () => {
    try {
      const intent = form.intent.value.trim();
      const title = (form.title.value.trim() || intent.split(/[。！？.!?\n]/)[0] || intent).slice(0, 300);
      const description = form.description.value.trim() || intent;
      const created = await api("/api/requirements", { body: { title, description } });
	  form.reset(); toast("需求已创建");
      location.hash = `#/workbench/${created.id}/intent`;
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
			if (!await confirmAIRequest(form.provider.value, form.additionalContext.value.trim())) return;
			$("#ai-plan-preview").innerHTML = loadingHTML;
			const plan = await api(`/api/requirements/${state.reqId}/ai-plan`, { body: { provider: form.provider.value, additionalContext: form.additionalContext.value.trim() }, timeoutMs: 610000 });
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

  if (target.matches("[data-open-composer]")) {
    const composer = document.querySelector(`[data-composer="${CSS.escape(target.dataset.openComposer)}"]`);
    if (composer) { composer.open = true; composer.scrollIntoView({ behavior: "smooth", block: "nearest" }); composer.querySelector("input,textarea,select")?.focus(); }
    return;
  }
  if (target.matches("[data-plan-add]")) {
    const plan = syncPlanFromForm();
    const defaults = { question: { question: "", blocking: false, suggestedDefault: "" }, criterion: { description: "", rationale: "" }, test: { title: "", criterion: plan.criteria[0]?.description || "", kind: "integration", setup: [], steps: [], expected: [] }, task: { title: "", description: "", dependsOn: [], rationale: "", order: plan.tasks.length + 1, suggestedAdapter: "human", expectedDeliverables: [] }, risk: { risk: "", severity: "medium", mitigation: "" } };
    const fields = { question: "openQuestions", criterion: "criteria", test: "testCases", task: "tasks", risk: "risks" };
    plan[fields[target.dataset.planAdd]].push(defaults[target.dataset.planAdd]);
    setPlanDraft(plan);
    return;
  }
  if (target.matches("[data-plan-remove]")) {
    const plan = syncPlanFromForm();
    const fields = { question: "openQuestions", criterion: "criteria", test: "testCases", task: "tasks", risk: "risks" };
    plan[fields[target.dataset.planRemove]].splice(Number(target.dataset.planIndex), 1);
    setPlanDraft(plan);
    return;
  }
  if (target.matches("[data-test-provider]")) {
    await busy(target, async () => { try { await api(`/api/ai/providers/${target.dataset.testProvider}/test`, { body: {}, timeoutMs: 610000 }); toast("连接测试通过"); } catch (error) { toast(`连接测试失败：${error.message}`, true); } });
    return;
  }
  if (target.matches("[data-delete-provider]")) {
    if (await askConfirmation("删除 AI 提供方", "配置会被移除，系统凭据库中的关联密钥也会清理。", "删除")) { try { await api(`/api/ai/providers/${target.dataset.deleteProvider}`, { method: "DELETE" }); toast("AI 提供方已删除"); await loadSettings("ai"); } catch (error) { toast(error.message, true); } }
    return;
  }
  if (target.matches("[data-edit-provider]")) {
    const provider = state.aiProviders.find((item) => item.id === target.dataset.editProvider);
    const form = $("#form-ai-provider");
    if (provider && form) {
      for (const name of ["id", "name", "kind", "baseUrl", "model", "apiPath", "apiKeyHeader", "apiKeyPrefix", "timeoutSeconds"]) if (form.elements[name]) form.elements[name].value = provider[name] ?? "";
      form.elements.enabled.checked = provider.enabled;
      form.closest("details").open = true; form.scrollIntoView({ behavior: "smooth", block: "center" });
    }
    return;
  }
  if (target.matches("[data-delete-adapter]")) {
    if (await askConfirmation("删除 Agent 适配器", "历史会话记录会保留，但不能再用该适配器启动新会话。", "删除")) { try { await api(`/api/agent-adapters/${target.dataset.deleteAdapter}`, { method: "DELETE" }); toast("适配器已删除"); await loadSettings("agents"); } catch (error) { toast(error.message, true); } }
    return;
  }
  if (target.matches("[data-edit-adapter]")) {
    const adapter = state.agentAdapters.find((item) => item.id === target.dataset.editAdapter);
    const form = $("#form-agent-adapter");
    if (adapter && form) { for (const name of ["id", "name", "description", "command"]) form.elements[name].value = adapter[name] || ""; form.elements.args.value = lineText(adapter.args); form.elements.capabilities.value = lineText(adapter.capabilities); form.elements.enabled.checked = adapter.enabled; form.closest("details").open = true; form.scrollIntoView({ behavior: "smooth", block: "center" }); }
    return;
  }
  if (target.matches("[data-accept-handoff]")) {
    try { await api(`/api/handoffs/${target.dataset.acceptHandoff}/accept`, { body: {} }); toast("交接已接收"); await loadReqDetail(); } catch (error) { toast(error.message, true); }
    return;
  }
  if (target.matches("[data-activity-requirement]")) { location.hash = `#/workbench/${target.dataset.activityRequirement}/${target.dataset.activityMode || "verify"}`; return; }

  if (target.matches("[data-select-req]")) {
    const id = target.dataset.selectReq;
    if (id && id !== state.reqId) { location.hash = `#/workbench/${id}/${state.workbenchMode}`; }
    return;
  }
	if (target.matches("[data-discard-ai-plan]")) { renderAIPlan(null); setPlanDraft(state.requirementDetail?.plan || null); return; }
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
    target.innerHTML = `<i data-lucide="scroll-text"></i>${opening ? "收起" : "日志"}`;
    refreshIcons(target);
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

$("#start-first-requirement").addEventListener("click", () => {
  if (state.requirements.length) {
    $("#req-list .req-item")?.focus();
    return;
  }
  $("#req-intent").focus();
});

$("#mobile-back").addEventListener("click", () => { state.reqId = ""; location.hash = "#/workbench"; });

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
  if (form.matches("[data-handoff-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api(`/api/tasks/${form.dataset.handoffForm}/handoffs`, { body: { fromSessionId: form.fromSessionId.value, toAdapter: form.toAdapter.value, summary: form.summary.value.trim(), completedWork: lines(form.completedWork.value), remainingWork: lines(form.remainingWork.value), risks: lines(form.risks.value), validation: lines(form.validation.value) } }); form.reset(); toast("任务交接已创建"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
  if (form.matches("[data-session-form]")) {
    event.preventDefault();
    await busy(event.submitter, async () => { try { await api("/api/agent-sessions", { body: { taskId: form.dataset.sessionForm, provider: form.provider.value, prompt: form.prompt.value.trim() } }); form.prompt.value = ""; toast("Agent 会话已启动"); await loadReqDetail(); } catch (error) { toast(error.message, true); } });
    return;
  }
});

route();
