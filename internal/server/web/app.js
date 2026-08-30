'use strict';

const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => Array.from(root.querySelectorAll(s));
const state = { csrf: '', items: [], report: null, active: 'overview' };

const api = {
  async request(method, path, body) {
    const init = { method, credentials: 'same-origin', headers: {} };
    if (body !== undefined) {
      init.headers['Content-Type'] = 'application/json';
      init.headers['X-Tally-CSRF'] = state.csrf;
      init.body = JSON.stringify(body || {});
    }
    const res = await fetch(path, init);
    if (!res.ok) throw await apiError(res);
    return res.status === 204 ? null : res.json();
  },
  get(path) { return this.request('GET', path); },
  post(path, body) { return this.request('POST', path, body); },
  put(path, body) { return this.request('PUT', path, body); },
  del(path) { return this.request('DELETE', path, {}); },
  async session(token = '') {
    const headers = token ? { Authorization: `Bearer ${token}` } : {};
    const res = await fetch('/api/session', { credentials: 'same-origin', headers });
    if (!res.ok) throw await apiError(res);
    return res.json();
  },
};
async function apiError(res) {
  let message = res.statusText;
  try { const j = await res.json(); message = j.error || message; } catch {}
  const err = new Error(message); err.status = res.status; return err;
}

function text(v) { return String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function attr(v) { return text(v); }
function hours(seconds) { return (Number(seconds || 0) / 3600).toFixed(2); }
function hnum(v) { return Number(v || 0).toFixed(2); }
function money(cur, minor) { return `${text(cur || 'USD')} ${(Number(minor || 0) / 100).toFixed(2)}`; }
function dateOnly(v) { return v ? String(v).slice(0, 10) : '—'; }
function dateTime(v) { if (!v) return '—'; const d = new Date(v); return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString(); }
function toast(msg, bad = false) { const t = $('#toast'); t.textContent = msg; t.classList.toggle('bad', bad); t.hidden = false; clearTimeout(t._h); t._h = setTimeout(() => { t.hidden = true; }, 3600); }
function loading(el, msg = 'Loading…') { el.innerHTML = `<div class="empty">${text(msg)}</div>`; }
function empty(msg) { return `<div class="empty">${text(msg)}</div>`; }

async function bootstrap() {
  try {
	let s;
	try {
	  s = await api.session();
	} catch (e) {
	  if (e.status !== 401) throw e;
	  const token = window.prompt('Tally API token (used once and not stored)');
	  if (!token) throw new Error('API token required');
	  s = await api.session(token);
	}
    state.csrf = s.csrf_token || '';
    $('#session-status').textContent = state.csrf ? 'secure session ready' : 'session missing csrf';
    $('#session-status').classList.toggle('on', !!state.csrf);
    bindEvents();
    await Promise.all([loadStatus(), loadItems(), loadOverview()]);
  } catch (e) {
    $('#session-status').textContent = 'session failed';
    toast('Session bootstrap failed: ' + e.message, true);
  }
}

function bindEvents() {
  $$('.nav-item').forEach(b => b.addEventListener('click', () => showView(b.dataset.view)));
  $('#overview-range').addEventListener('change', loadOverview);
  $('#overview-refresh').addEventListener('click', loadOverview);
  $('#btn-sync').addEventListener('click', manualSync);
  $('#btn-classify').addEventListener('click', manualClassify);
  $('#item-form').addEventListener('submit', saveItem);
  $('#item-cancel').addEventListener('click', resetItemForm);
  $('#item-status').addEventListener('change', loadItems);
  $('#items-refresh').addEventListener('click', loadItems);
  $('#triage-refresh').addEventListener('click', loadTriage);
  $('#rule-form').addEventListener('submit', createRule);
  $('#report-form').addEventListener('submit', ev => { ev.preventDefault(); loadReport(); });
  $('#copy-md').addEventListener('click', copyMarkdown);
  $('#copy-csv').addEventListener('click', copyCSV);
  $('#finalize-report').addEventListener('click', finalizeSnapshot);
  $('#billing-form').addEventListener('submit', saveBilling);
  $('#load-billing').addEventListener('click', loadBillingProfile);
  $('#snap-refresh').addEventListener('click', loadSnapshots);
}

async function showView(name) {
  state.active = name;
  $$('.nav-item').forEach(b => b.classList.toggle('active', b.dataset.view === name));
  $$('.view').forEach(v => { v.hidden = v.id !== `view-${name}`; });
  const loaders = { overview: loadOverview, items: loadItems, triage: loadTriage, reports: loadReport, billing: prepareBilling, rules: loadRules, snapshots: loadSnapshots };
  if (loaders[name]) await loaders[name]();
}

async function loadStatus() {
  try {
    const s = await api.get('/api/status');
    const el = $('#provider-status');
    el.textContent = s.provider_connected ? `${s.provider} connected` : `${s.provider || 'provider'} offline`;
    el.classList.toggle('on', !!s.provider_connected); el.classList.toggle('off', !s.provider_connected);
  } catch (e) { toast('Status failed: ' + e.message, true); }
}
async function manualSync() { try { const r = await api.post('/api/sync', {}); const conflicts = r.conflicts || 0; toast(`Synced: ${r.created || 0} new, ${r.updated || 0} updated, ${r.deleted || 0} deleted, ${conflicts} conflicts`, conflicts > 0); await refreshActive(); } catch (e) { toast('Sync failed: ' + e.message, true); } }
async function manualClassify() { try { const r = await api.post('/api/classify', { llm: true }); toast(`Classified: ${r.matched_by_rule || 0} rule, ${r.matched_by_llm || 0} LLM`); await refreshActive(); } catch (e) { toast('Classify failed: ' + e.message, true); } }
async function refreshActive() { await loadStatus(); await showView(state.active); }

async function loadItems() {
  const status = $('#item-status') ? $('#item-status').value : '';
  const list = $('#items-list'); if (list) loading(list);
  try {
    state.items = (await api.get('/api/work-items' + (status ? `?status=${encodeURIComponent(status)}` : ''))) || [];
    fillItemSelects();
    if (!list) return;
    if (!state.items.length) { list.innerHTML = empty('No work items yet. Create one above.'); return; }
    list.innerHTML = state.items.map(itemRow).join('');
    $$('[data-edit]', list).forEach(b => b.addEventListener('click', () => editItem(b.dataset.edit)));
    $$('[data-done]', list).forEach(b => b.addEventListener('click', () => changeItemStatus(b.dataset.done, 'done')));
    $$('[data-reactivate]', list).forEach(b => b.addEventListener('click', () => changeItemStatus(b.dataset.reactivate, 'reactivate')));
  } catch (e) { if (list) list.innerHTML = empty('Could not load work items: ' + e.message); }
}
function itemRow(w) {
  const action = w.status === 'active' ? `<button class="ghost" data-done="${w.id}" type="button">Done</button>` : `<button class="ghost" data-reactivate="${w.id}" type="button">Reactivate</button>`;
  return `<article class="row-card"><div><div class="row-title">${text(w.name)}</div><div class="meta"><span>${text(w.kind)}</span><span>${text(w.status)}</span>${w.context ? `<span>${text(w.context)}</span>` : ''}${w.description ? `<span>${text(w.description)}</span>` : ''}</div></div><div class="actions"><button class="ghost" data-edit="${w.id}" type="button">Edit</button>${action}</div></article>`;
}
function resetItemForm() { $('#item-id').value = ''; $('#item-form').reset(); $('#item-submit').textContent = 'Create item'; }
function editItem(id) { const w = state.items.find(x => String(x.id) === String(id)); if (!w) return; $('#item-id').value = w.id; $('#item-name').value = w.name || ''; $('#item-kind').value = w.kind || 'project'; $('#item-context').value = w.context || ''; $('#item-description').value = w.description || ''; $('#item-submit').textContent = 'Save item'; $('#item-name').focus(); }
async function saveItem(ev) { ev.preventDefault(); const id = $('#item-id').value; const body = { name: $('#item-name').value.trim(), kind: $('#item-kind').value, context: $('#item-context').value, description: $('#item-description').value }; try { id ? await api.put(`/api/work-items/${id}`, body) : await api.post('/api/work-items', body); toast(id ? 'Work item updated' : 'Work item created'); resetItemForm(); await loadItems(); await loadOverview(); } catch (e) { toast('Save failed: ' + e.message, true); } }
async function changeItemStatus(id, action) { try { await api.post(`/api/work-items/${id}/${action}`, {}); toast(action === 'done' ? 'Work item marked done' : 'Work item reactivated'); await loadItems(); await loadRules(); } catch (e) { toast(e.message, true); } }
function fillItemSelects() {
  const active = state.items.filter(i => i.status === 'active');
  const opts = active.map(i => `<option value="${i.id}">${text(i.name)} (${text(i.kind)})</option>`).join('');
  ['#report-item', '#bill-item', '#rule-project'].forEach(sel => { const el = $(sel); if (el) el.innerHTML = opts || '<option value="">No active items</option>'; });
}

async function loadOverview() {
  const row = $('#stat-row'), list = $('#overview-list'); loading(row); loading(list);
  try {
    const range = $('#overview-range').value;
    const rep = await api.get('/api/report?range=' + encodeURIComponent(range) + '&billing=true');
    row.innerHTML = stat('Tracked', hnum(rep.total_hours), 'h') + stat('Unclassified', hnum(rep.unclassified_hours), 'h') + stat('Exact seconds', rep.total_exact_seconds || 0, 's') + stat('Items', (rep.items || []).length, '');
    const lines = rep.items || [];
    if (!lines.length) { list.innerHTML = empty('No classified time in this range.'); return; }
    const max = Math.max(...lines.map(l => l.exact_seconds || 0), 1);
    list.innerHTML = lines.map(l => overviewCard(l, max)).join('');
  } catch (e) { row.innerHTML = ''; list.innerHTML = empty('Overview failed: ' + e.message); }
}
function stat(label, val, unit) { return `<div class="stat"><div class="label">${text(label)}</div><div class="value">${text(val)}<small> ${text(unit)}</small></div></div>`; }
function overviewCard(l, max) { const w = Math.round(((l.exact_seconds || 0) / max) * 100); const item = l.work_item || {}; return `<article class="card"><div class="card-top"><div><h3>${text(item.name)}</h3><div class="client">${text(item.context || item.kind || '')}</div></div><span class="pill ${text(item.kind)}">${text(item.kind)}</span></div><div class="hours">${hnum(l.exact_hours)}<small>hours exact</small></div><div class="bar"><span style="width:${w}%"></span></div>${l.billing ? `<div class="meta"><span>Rounded ${hours(l.billing.adjusted_seconds)} h</span><span>${money(l.billing.currency, l.billing.amount_minor)}</span></div>` : '<div class="meta"><span>No billing projection</span></div>'}</article>`; }

async function loadTriage() {
  const list = $('#triage-list'), auditList = $('#audit-list'); loading(list); loading(auditList);
  try {
    if (!state.items.length) await loadItems();
    const [eventsRaw, auditRaw] = await Promise.all([
      api.get('/api/unclassified?limit=' + encodeURIComponent($('#triage-limit').value || '100')),
      api.get('/api/audit')
    ]);
    const events = eventsRaw || [], audit = auditRaw || [];
    renderAudit(auditList, audit);
    updateBadge(events.length);
    const active = state.items.filter(i => i.status === 'active');
    const opts = active.map(i => `<option value="${i.id}">${text(i.name)}</option>`).join('');
    if (!events.length) { list.innerHTML = empty('Nothing to triage.'); return; }
    if (!opts) { list.innerHTML = empty('Create an active work item before triage assignment.'); return; }
    list.innerHTML = events.map(e => triageRow(e, opts)).join('');
    $$('.assign', list).forEach(b => b.addEventListener('click', () => assignEvent(b.closest('.row-card'))));
  } catch (e) { list.innerHTML = empty('Triage failed: ' + e.message); }
}
function triageRow(e, opts) { return `<article class="row-card" data-id="${e.id}"><div><div class="row-title">${text(e.title || e.app || '(untitled)')}</div><div class="meta"><span>${text(e.app || '—')}</span>${e.repo ? `<span>${text(e.repo)}</span>` : ''}${e.url ? `<span>${text(hostOf(e.url))}</span>` : ''}<span>${Math.round((e.duration_seconds || 0) / 60)} min</span></div></div><div class="triage-controls"><select class="assign-item">${opts}</select><label class="check"><input class="make-rule" type="checkbox" /> rule</label><select class="rule-field"><option>title</option><option>app</option><option>repo</option><option>url</option></select><button class="assign" type="button">Assign</button></div></article>`; }
function renderAudit(list, audit) { const names = Object.fromEntries(state.items.map(i => [i.id, i.name])); const rows = audit.slice(-50).reverse(); if (!rows.length) { list.innerHTML = empty('No correction history yet.'); return; } list.innerHTML = rows.map(a => { const oldItem = a.old_work_item_id ? (names[a.old_work_item_id] || '#' + a.old_work_item_id) : 'unclassified'; const newItem = a.new_work_item_id ? (names[a.new_work_item_id] || '#' + a.new_work_item_id) : 'unclassified'; return `<article class="row-card"><div><div class="row-title">${text(oldItem)} → ${text(newItem)}</div><div class="meta"><span>${text(a.old_source || 'none')} → ${text(a.new_source || 'none')}</span><span>${text(a.source_key || 'unknown source')}</span><span>${text(dateTime(a.created_at))}</span></div></div></article>`; }).join(''); }
async function assignEvent(row) { try { const r = await api.post(`/api/events/${row.dataset.id}/assign`, { project: $('.assign-item', row).value, make_rule: $('.make-rule', row).checked, rule_field: $('.rule-field', row).value }); toast(r.rule_created ? 'Assigned and created rule' : 'Assigned'); await loadTriage(); } catch (e) { toast('Assignment failed: ' + e.message, true); } }
function updateBadge(n) { const b = $('#triage-badge'); b.textContent = n; b.hidden = !n; }
function hostOf(u) { try { return new URL(u).hostname.replace(/^www\./, ''); } catch { return u || ''; } }

async function loadReport() {
  if (!state.items.length) await loadItems();
  const table = $('#report-table'), summary = $('#report-summary'); loading(table); summary.innerHTML = '';
  try {
    const params = reportParams();
    state.report = await api.get('/api/report?' + params.toString());
    renderReport(state.report);
  } catch (e) { table.innerHTML = empty('Report failed: ' + e.message); }
}
function reportParams() { const p = new URLSearchParams(); const period = $('#report-period').value; p.set('period', period); p.set('timezone', $('#report-timezone').value || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'); if ($('#report-billing').checked) p.set('billing', 'true'); if (period === 'final') p.set('item', $('#report-item').value); if ($('#report-from').value) p.set('from', $('#report-from').value); if ($('#report-to').value) p.set('to', $('#report-to').value); return p; }
function renderReport(rep) {
  $('#report-summary').innerHTML = stat('Tracked exact', hours(rep.total_exact_seconds), 'h') + stat('Unclassified', hours(rep.unclassified_seconds), 'h') + stat('From', dateOnly(rep.from), '') + stat('To', dateOnly(rep.to), '');
  const rows = (rep.items || []).map(l => { const w = l.work_item || {}, b = l.billing; return `<tr><td>${text(w.name)}</td><td>${text(w.kind)}</td><td>${text(w.context || '—')}</td><td class="num">${l.exact_seconds || 0}</td><td class="num">${hnum(l.exact_hours)}</td><td class="num">${b ? b.adjusted_seconds : '—'}</td><td class="num">${b ? hours(b.adjusted_seconds) : '—'}</td><td class="num">${b ? money(b.currency, b.rate_minor) : '—'}</td><td class="num">${b ? money(b.currency, b.amount_minor) : '—'}</td></tr>`; }).join('');
  const totals = Object.entries(rep.totals_by_currency || {}).map(([c, m]) => `<span class="total-chip">${money(c, m)}</span>`).join('') || '<span class="muted">No billed totals</span>';
  $('#report-table').innerHTML = `<div class="table-wrap"><table><thead><tr><th>Item</th><th>Kind</th><th>Context</th><th class="num">Exact sec</th><th class="num">Exact h</th><th class="num">Rounded sec</th><th class="num">Rounded h</th><th class="num">Rate</th><th class="num">Amount</th></tr></thead><tbody>${rows || `<tr><td colspan="9" class="empty">No data in period</td></tr>`}</tbody></table></div><div class="totals"><strong>Totals by currency</strong>${totals}</div>`;
}
function copyMarkdown() { if (!state.report) return toast('Run a report first', true); const lines = ['| Item | Kind | Context | Exact hours | Rounded hours | Amount |','|---|---|---|---:|---:|---:|']; (state.report.items || []).forEach(l => { const w = l.work_item || {}, b = l.billing; lines.push(`| ${w.name || ''} | ${w.kind || ''} | ${w.context || '—'} | ${hnum(l.exact_hours)} | ${b ? hours(b.adjusted_seconds) : '—'} | ${b ? `${b.currency} ${(b.amount_minor / 100).toFixed(2)}` : '—'} |`); }); copy(lines.join('\n')); }
function copyCSV() { if (!state.report) return toast('Run a report first', true); const rows = ['item,kind,context,exact_seconds,exact_hours,rounded_seconds,rounded_hours,currency,rate_minor,amount_minor']; (state.report.items || []).forEach(l => { const w = l.work_item || {}, b = l.billing || {}; rows.push([w.name,w.kind,w.context,l.exact_seconds,hnum(l.exact_hours),b.adjusted_seconds || '', b.adjusted_seconds ? hours(b.adjusted_seconds) : '', b.currency || '', b.rate_minor || '', b.amount_minor || ''].map(csv).join(',')); }); copy(rows.join('\n')); }
function csv(v) { const s = String(v ?? ''); return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s; }
function copy(txt) { navigator.clipboard.writeText(txt).then(() => toast('Copied')); }
async function finalizeSnapshot() { try { const label = prompt('Snapshot label', `Tally ${new Date().toISOString().slice(0,10)}`); if (label === null) return; const body = Object.fromEntries(reportParams()); body.label = label; body.billing = $('#report-billing').checked; const s = await api.post('/api/billing/snapshots', body); toast(`Snapshot #${s.id} finalized`); } catch (e) { toast('Finalize failed: ' + e.message, true); } }

async function prepareBilling() { if (!state.items.length) await loadItems(); await loadBillingProfile(); }
function billingScopeKey() { const scope = $('#bill-scope').value; if (scope === 'global') return ''; if (scope === 'client') return $('#bill-client').value.trim(); return $('#bill-item').value; }
async function loadBillingProfile() { const out = $('#billing-result'); loading(out); try { const scope = $('#bill-scope').value, key = billingScopeKey(); const qs = scope === 'work_item' ? `work_item=${encodeURIComponent(key)}` : `scope_type=${encodeURIComponent(scope)}&scope_key=${encodeURIComponent(key)}`; const r = await api.get('/api/billing/profile?' + qs); const p = r.profile || r; $('#bill-enabled').checked = !!p.enabled; $('#bill-rate').value = p.hourly_rate_minor || 0; $('#bill-currency').value = p.currency || 'USD'; $('#bill-increment').value = p.rounding_increment_minutes || 15; $('#bill-period').value = p.period_mode || 'custom'; out.innerHTML = `<strong>Loaded:</strong> ${text(r.inherited_from || p.scope_type || scope)} ${text(p.scope_key || '')}`; } catch (e) { out.innerHTML = text('No saved profile loaded: ' + e.message); } }
async function saveBilling(ev) { ev.preventDefault(); const p = { scope_type: $('#bill-scope').value, scope_key: billingScopeKey(), enabled: $('#bill-enabled').checked, hourly_rate_minor: Number($('#bill-rate').value || 0), currency: $('#bill-currency').value, rounding_mode: 'up', rounding_increment_minutes: Number($('#bill-increment').value || 15), rounding_scope: 'period_work_item', period_mode: $('#bill-period').value }; try { const saved = await api.put('/api/billing/profile', p); $('#billing-result').innerHTML = `<strong>Saved:</strong> ${text(saved.scope_type)} ${text(saved.scope_key || 'global')} · ${saved.enabled ? 'enabled' : 'disabled'} · ${money(saved.currency, saved.hourly_rate_minor)}/h`; toast('Billing profile saved'); } catch (e) { toast('Billing save failed: ' + e.message, true); } }

async function loadRules() { const list = $('#rules-list'); loading(list); try { if (!state.items.length) await loadItems(); fillItemSelects(); const rules = (await api.get('/api/rules')) || []; const names = Object.fromEntries(state.items.map(i => [i.id, i.name])); if (!rules.length) { list.innerHTML = empty('No rules yet.'); return; } list.innerHTML = rules.map(r => `<article class="row-card"><div><div class="row-title">${text(names[r.work_item_id] || names[r.project_id] || '#' + r.work_item_id)}</div><div class="meta"><span>${text(r.field)} ${text(r.match)}</span><code>${text(r.pattern)}</code><span>priority ${text(r.priority)}</span><span>${r.active ? 'active' : 'inactive'}</span></div></div><button class="ghost danger" data-del-rule="${r.id}" type="button">Delete</button></article>`).join(''); $$('[data-del-rule]', list).forEach(b => b.addEventListener('click', async () => { try { await api.del('/api/rules/' + b.dataset.delRule); toast('Rule deleted'); await loadRules(); } catch (e) { toast(e.message, true); } })); } catch (e) { list.innerHTML = empty('Rules failed: ' + e.message); } }
async function createRule(ev) { ev.preventDefault(); try { await api.post('/api/rules', { project: $('#rule-project').value, field: $('#rule-field').value, match: $('#rule-match').value, pattern: $('#rule-pattern').value, priority: Number($('#rule-priority').value || 100) }); $('#rule-pattern').value = ''; toast('Rule created'); await loadRules(); } catch (e) { toast('Rule create failed: ' + e.message, true); } }

async function loadSnapshots() { const list = $('#snapshots-list'); loading(list); $('#snapshot-detail').textContent = ''; try { const snaps = (await api.get('/api/billing/snapshots')) || []; if (!snaps.length) { list.innerHTML = empty('No finalized snapshots yet.'); return; } list.innerHTML = snaps.map(s => `<article class="row-card"><div><div class="row-title">#${s.id} ${text(s.label || '(untitled)')}</div><div class="meta"><span>${text(s.period_mode)}</span><span>${dateOnly(s.from)} to ${dateOnly(s.to)}</span><span>${text(s.timezone)}</span></div></div><button class="ghost" data-snap="${s.id}" type="button">View JSON</button></article>`).join(''); $$('[data-snap]', list).forEach(b => b.addEventListener('click', () => viewSnapshot(b.dataset.snap))); } catch (e) { list.innerHTML = empty('Snapshots failed: ' + e.message); } }
async function viewSnapshot(id) { try { const s = await api.get('/api/billing/snapshots/' + id); $('#snapshot-detail').textContent = JSON.stringify(s, null, 2); } catch (e) { toast('Snapshot load failed: ' + e.message, true); } }

bootstrap();
