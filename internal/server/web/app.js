'use strict';

const api = {
  async get(path) { const r = await fetch(path); if (!r.ok) throw await err(r); return r.json(); },
  async post(path, body) {
    const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
    if (!r.ok) throw await err(r); return r.json();
  },
  async del(path) { const r = await fetch(path, { method: 'DELETE' }); if (!r.ok) throw await err(r); return r.json(); },
};
async function err(r) { try { const j = await r.json(); return new Error(j.error || r.statusText); } catch { return new Error(r.statusText); } }

const state = { overviewRange: 'week', reportsRange: 'week', projects: [] };
const fmtH = (h) => (h || 0).toFixed(1);

function toast(msg) {
  const t = document.getElementById('toast');
  t.textContent = msg; t.hidden = false;
  clearTimeout(t._h); t._h = setTimeout(() => (t.hidden = true), 2600);
}

/* ---- navigation ---- */
document.querySelectorAll('.nav-item').forEach((b) =>
  b.addEventListener('click', () => showView(b.dataset.view)));

function showView(name) {
  document.querySelectorAll('.nav-item').forEach((b) => b.classList.toggle('active', b.dataset.view === name));
  document.querySelectorAll('.view').forEach((v) => (v.hidden = v.id !== 'view-' + name));
  if (name === 'overview') loadOverview();
  if (name === 'triage') loadTriage();
  if (name === 'reports') loadReports();
  if (name === 'rules') loadRules();
}

document.querySelectorAll('.range-tabs').forEach((tabs) => {
  tabs.querySelectorAll('button').forEach((btn) =>
    btn.addEventListener('click', () => {
      tabs.querySelectorAll('button').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      if (tabs.dataset.target === 'overview') { state.overviewRange = btn.dataset.range; loadOverview(); }
      else { state.reportsRange = btn.dataset.range; loadReports(); }
    }));
});

/* ---- status / actions ---- */
async function loadStatus() {
  try {
    const s = await api.get('/api/status');
    const el = document.getElementById('provider-status');
    el.textContent = s.provider_connected ? s.provider + ' connected' : s.provider + ' offline';
    el.classList.toggle('on', s.provider_connected);
    el.classList.toggle('off', !s.provider_connected);
  } catch (e) { toast(e.message); }
}
document.getElementById('btn-sync').addEventListener('click', async () => {
  try { const r = await api.post('/api/sync', {}); toast(`Synced: +${r.created} new, ${r.updated} updated`); refresh(); }
  catch (e) { toast('Sync failed: ' + e.message); }
});
document.getElementById('btn-classify').addEventListener('click', async () => {
  try { const r = await api.post('/api/classify', { llm: true }); toast(`Classified ${r.matched_by_rule} by rule, ${r.matched_by_llm} by LLM`); refresh(); }
  catch (e) { toast('Classify failed: ' + e.message); }
});

/* ---- overview ---- */
async function loadOverview() {
  const rep = await api.get('/api/report?range=' + state.overviewRange);
  const row = document.getElementById('stat-row');
  row.innerHTML = statCard('Billable', rep.billable_hours, 'h') +
    statCard('Internal', rep.internal_hours, 'h') +
    statCard('Unclassified', rep.unclassified_hours, 'h') +
    statCard('Total tracked', rep.total_hours, 'h');

  const cards = document.getElementById('project-cards');
  if (!rep.projects || !rep.projects.length) {
    cards.innerHTML = `<div class="empty">No classified time yet in this range. Sync, then classify.</div>`;
    return;
  }
  const max = Math.max(...rep.projects.map((p) => p.hours));
  cards.innerHTML = rep.projects.map((ph) => projectCard(ph, max)).join('');
  cards.querySelectorAll('[data-done]').forEach((b) =>
    b.addEventListener('click', async () => {
      try { await api.post(`/api/projects/${b.dataset.done}/done`); toast('Project archived'); refresh(); }
      catch (e) { toast(e.message); }
    }));
}
const statCard = (label, val, unit) =>
  `<div class="stat"><div class="label">${label}</div><div class="value">${fmtH(val)}<small> ${unit}</small></div></div>`;

function projectCard(ph, max) {
  const p = ph.project;
  const color = p.type === 'billable' ? 'var(--billable)' : 'var(--internal)';
  const w = max > 0 ? Math.round((ph.hours / max) * 100) : 0;
  const doneBtn = p.status === 'active'
    ? `<button class="ghost" data-done="${p.id}">Mark done</button>` : '';
  return `<div class="card">
    <div class="card-top">
      <div><h3>${escapeHtml(p.name)}</h3>${p.client ? `<div class="client">${escapeHtml(p.client)}</div>` : ''}</div>
      <span class="pill ${p.status === 'done' ? 'done' : p.type}">${p.status === 'done' ? 'done' : p.type}</span>
    </div>
    <div class="hours">${fmtH(ph.hours)}<small> h</small></div>
    <div class="bar"><span style="width:${w}%;background:${color}"></span></div>
    <div class="card-actions">${doneBtn}</div>
  </div>`;
}

/* ---- triage ---- */
async function loadTriage() {
  const [events, projects] = await Promise.all([
    api.get('/api/unclassified?limit=100'),
    api.get('/api/projects?status=active'),
  ]);
  state.projects = projects;
  const list = document.getElementById('triage-list');
  updateBadge(events.length);
  if (!events.length) { list.innerHTML = `<div class="empty">Nothing to triage. 🎉</div>`; return; }
  const opts = projects.map((p) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
  list.innerHTML = events.map((e) => triageItem(e, opts)).join('');
  list.querySelectorAll('.triage-item').forEach((item) => {
    item.querySelector('.assign').addEventListener('click', async () => {
      const id = item.dataset.id;
      const project = item.querySelector('select').value;
      const makeRule = item.querySelector('input[type=checkbox]').checked;
      const field = item.querySelector('.rule-field').value;
      try {
        const r = await api.post(`/api/events/${id}/assign`, { project, make_rule: makeRule, rule_field: field });
        toast(r.rule_created ? 'Assigned + rule created' : 'Assigned');
        item.remove();
        const n = document.querySelectorAll('.triage-item').length;
        updateBadge(n);
        if (!n) list.innerHTML = `<div class="empty">Nothing to triage. 🎉</div>`;
      } catch (e) { toast(e.message); }
    });
  });
}
function triageItem(e, opts) {
  const secs = Math.round(e.duration_seconds / 60);
  return `<div class="triage-item" data-id="${e.id}">
    <div class="meta">
      <div class="title">${escapeHtml(e.title || e.app || '(untitled)')}</div>
      <div class="info">
        <span>app: ${escapeHtml(e.app || '—')}</span>
        ${e.repo ? `<span>repo: ${escapeHtml(e.repo)}</span>` : ''}
        ${e.url ? `<span>${escapeHtml(hostOf(e.url))}</span>` : ''}
        <span>${secs} min</span>
      </div>
    </div>
    <div class="triage-controls">
      <select>${opts}</select>
      <label class="mk-rule"><input type="checkbox" /> rule on
        <select class="rule-field">
          <option value="title">title</option><option value="app">app</option>
          <option value="repo">repo</option><option value="url">url</option>
        </select>
      </label>
      <button class="assign">Assign</button>
    </div>
  </div>`;
}
function updateBadge(n) {
  const b = document.getElementById('triage-badge');
  b.textContent = n; b.hidden = n === 0;
}

/* ---- reports ---- */
let lastReport = null;
async function loadReports() {
  const rep = await api.get('/api/report?range=' + state.reportsRange);
  lastReport = rep;
  const rows = (rep.projects || []).map((ph) =>
    `<tr><td>${escapeHtml(ph.project.name)}</td><td>${ph.project.type}</td>
     <td>${escapeHtml(ph.project.client || '—')}</td><td class="num">${fmtH(ph.hours)}</td></tr>`).join('');
  document.getElementById('report-table').innerHTML = `<table>
    <thead><tr><th>Project</th><th>Type</th><th>Client</th><th class="num">Hours</th></tr></thead>
    <tbody>${rows || `<tr><td colspan="4" class="empty">No data in range</td></tr>`}</tbody>
    <tfoot>
      <tr><td colspan="3">Billable</td><td class="num">${fmtH(rep.billable_hours)}</td></tr>
      <tr><td colspan="3">Internal</td><td class="num">${fmtH(rep.internal_hours)}</td></tr>
      <tr><td colspan="3">Unclassified</td><td class="num">${fmtH(rep.unclassified_hours)}</td></tr>
      <tr><td colspan="3">Total</td><td class="num">${fmtH(rep.total_hours)}</td></tr>
    </tfoot></table>`;
}
document.getElementById('copy-md').addEventListener('click', () => {
  if (!lastReport) return;
  const lines = ['| Project | Type | Client | Hours |', '|---|---|---|---:|'];
  (lastReport.projects || []).forEach((ph) =>
    lines.push(`| ${ph.project.name} | ${ph.project.type} | ${ph.project.client || '—'} | ${fmtH(ph.hours)} |`));
  lines.push('', `Billable: ${fmtH(lastReport.billable_hours)} h`, `Internal: ${fmtH(lastReport.internal_hours)} h`, `Total: ${fmtH(lastReport.total_hours)} h`);
  copy(lines.join('\n'));
});
document.getElementById('copy-csv').addEventListener('click', () => {
  if (!lastReport) return;
  const rows = ['project,type,client,hours'];
  (lastReport.projects || []).forEach((ph) =>
    rows.push(`${csv(ph.project.name)},${ph.project.type},${csv(ph.project.client || '')},${fmtH(ph.hours)}`));
  copy(rows.join('\n'));
});
const csv = (s) => /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
function copy(text) { navigator.clipboard.writeText(text).then(() => toast('Copied')); }

/* ---- rules ---- */
async function loadRules() {
  const [rules, projects] = await Promise.all([api.get('/api/rules'), api.get('/api/projects')]);
  const byId = Object.fromEntries(projects.map((p) => [p.id, p.name]));
  document.getElementById('rule-project').innerHTML =
    projects.filter((p) => p.status === 'active').map((p) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
  const list = document.getElementById('rules-list');
  if (!rules.length) { list.innerHTML = `<div class="empty">No rules yet.</div>`; return; }
  list.innerHTML = rules.map((r) => `<div class="rule-row">
    <div><strong>${escapeHtml(byId[r.project_id] || '#' + r.project_id)}</strong>
      — <code>${r.field} ${r.match}</code> <code>${escapeHtml(r.pattern)}</code>
      ${r.active ? '' : '<span class="pill done">inactive</span>'}</div>
    <button class="del" data-id="${r.id}" title="delete">×</button>
  </div>`).join('');
  list.querySelectorAll('.del').forEach((b) =>
    b.addEventListener('click', async () => { await api.del('/api/rules/' + b.dataset.id); toast('Rule deleted'); loadRules(); }));
}
document.getElementById('rule-form').addEventListener('submit', async (ev) => {
  ev.preventDefault();
  try {
    await api.post('/api/rules', {
      project: document.getElementById('rule-project').value,
      field: document.getElementById('rule-field').value,
      match: document.getElementById('rule-match').value,
      pattern: document.getElementById('rule-pattern').value,
    });
    document.getElementById('rule-pattern').value = '';
    toast('Rule added'); loadRules();
  } catch (e) { toast(e.message); }
});

/* ---- helpers ---- */
function escapeHtml(s) { return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function hostOf(u) { try { return new URL(u).hostname.replace(/^www\./, ''); } catch { return u; } }

function refresh() {
  loadStatus();
  const active = document.querySelector('.nav-item.active').dataset.view;
  showView(active);
}

/* boot */
loadStatus();
loadOverview();
