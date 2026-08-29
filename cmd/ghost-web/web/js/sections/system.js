/* Ghost Section: System — the machine Ghost lives on. */
'use strict';

async function loadSystem(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'System'));
  head.appendChild(GhostUI.h('p', {}, 'The hardware and services your Ghost runs on.'));
  container.appendChild(head);

  const statusEl = GhostUI.h('div', { className: 'loading' }, GhostUI.h('span', {}, 'Reading system…'));
  container.appendChild(statusEl);

  let st;
  try { st = await GhostAPI.get('/api/admin/status'); }
  catch (e) { statusEl.outerHTML = ''; container.appendChild(GhostUI.errorState('Couldn’t read system', 'Ghost may be starting.')); return; }
  statusEl.remove();

  // Ghost + Hardware — one panel
  const g = GhostUI.h('div', { className: 'panel' });
  g.appendChild(panelHead('Ghost'));
  const gk = GhostUI.h('div', { className: 'kv' });
  gk.appendChild(kv('Version', st.version || '—'));
  gk.appendChild(kv('Uptime', st.uptime || '—'));
  gk.appendChild(kv('Model', (st.provider || '—') + (st.model ? ' · ' + st.model : '')));
  gk.appendChild(kv('Address', (st.ip || '—') + (st.hostname ? '  (' + st.hostname + ')' : '')));
  gk.appendChild(kv('CPU', (st.cpu_percent != null ? st.cpu_percent.toFixed(0) + '%' : '—')));
  if (st.memory) gk.appendChild(kv('Memory', GhostUI.fmtNum(Math.round(st.memory.used / 1048576)) + ' MB / ' + GhostUI.fmtNum(Math.round(st.memory.total / 1048576)) + ' MB'));
  if (st.disk) gk.appendChild(kv('Storage', GhostUI.fmtNum(Math.round(st.disk.used / 1073741824)) + ' GB / ' + GhostUI.fmtNum(Math.round(st.disk.total / 1073741824)) + ' GB'));
  if (st.load) gk.appendChild(kv('Load', st.load.one.toFixed(2) + ' / ' + st.load.five.toFixed(2) + ' / ' + st.load.fifteen.toFixed(2)));
  g.appendChild(gk);
  container.appendChild(g);

  // Services
  const sv = GhostUI.h('div', { className: 'panel' });
  sv.appendChild(panelHead('Services'));
  const svk = GhostUI.h('div', { className: 'kv' });
  (st.services || []).forEach(s => {
    svk.appendChild(kvRow2(s.name, s.active ? 'Running' : 'Stopped', s.active ? 'ready' : 'bad'));
  });
  sv.appendChild(svk);
  container.appendChild(sv);

  // Diagnostics
  const diag = GhostUI.h('div', { className: 'panel' });
  const dh = GhostUI.h('div', { className: 'panel-head' });
  const dhText = GhostUI.h('div');
  dhText.appendChild(GhostUI.h('h2', {}, 'Diagnostics'));
  dh.appendChild(dhText);
  const runBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Run diagnostics');
  runBtn.addEventListener('click', () => runDiag(diagBody));
  dh.appendChild(runBtn);
  diag.appendChild(dh);
  const diagBody = GhostUI.h('div', { id: 'diag-body', style: 'margin-top:var(--s-3)' });
  diagBody.appendChild(GhostUI.emptyState('Not run yet', 'Run a check to see how Ghost is doing.'));
  diag.appendChild(diagBody);
  container.appendChild(diag);
  runDiag(diagBody);

  // Danger zone
  const danger = GhostUI.h('div', { className: 'panel', style: 'border-color:var(--bad-soft)' });
  const dk = GhostUI.h('div', { className: 'kv' });
  dk.appendChild(kvRowBtn('Restart this device', 'Reboots the hardware Ghost runs on. Use only if something is wrong.', 'Restart', async () => {
    if (!(await GhostUI.confirmModal('Restart this device?', 'Your Ghost will be offline for a minute or two while the device reboots.', 'Restart device'))) return;
    try { await GhostAPI.post('/api/admin/reboot'); GhostUI.toast('Restarting…'); }
    catch (e) { GhostUI.toast('Couldn’t restart.', 'err'); }
  }));
  danger.appendChild(dk);
  container.appendChild(danger);

  // Updates panel
  const upPanel = GhostUI.h('div', { className: 'panel' });
  upPanel.appendChild(panelHead('Updates'));
  const upKv = GhostUI.h('div', { className: 'kv' });
  upKv.appendChild(kv('Version', st.version || '—'));
  upPanel.appendChild(upKv);
  const upActions = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const updateBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => runUpdate(upLogBox, updateBtn) }, 'Update Ghost');
  upActions.appendChild(updateBtn);
  upPanel.appendChild(upActions);
  const upLogBox = GhostUI.h('div', { className: 'panel hidden', style: 'margin-top:var(--s-4)' });
  upPanel.appendChild(upLogBox);
  container.appendChild(upPanel);

  try {
    const upRes = await GhostAPI.get('/api/admin/update/status');
    if (upRes.running) runUpdate(upLogBox, updateBtn);
    else if (upRes.log) { upLogBox.classList.remove('hidden'); upLogBox.appendChild(GhostUI.h('pre', { className: 'type-mono', style: 'white-space:pre-wrap;margin:0' }, upRes.log)); }
  } catch (e) { /* not running */ }
}

async function runUpdate(logBox, btn) {
  btn.disabled = true; btn.textContent = 'Updating…';
  logBox.classList.remove('hidden');
  logBox.innerHTML = '';
  logBox.appendChild(GhostUI.loading('Starting update…'));
  try { await GhostAPI.post('/api/admin/update'); } catch (e) { GhostUI.toast('Couldn’t start update.', 'err'); btn.disabled = false; btn.textContent = 'Update Ghost'; return; }
  const poll = setInterval(async () => {
    try {
      const s = await GhostAPI.get('/api/admin/update/status');
      logBox.innerHTML = '';
      logBox.appendChild(GhostUI.h('pre', { className: 'type-mono', style: 'white-space:pre-wrap;margin:0;max-height:300px;overflow:auto' }, s.log || ''));
      if (!s.running) {
        clearInterval(poll);
        btn.disabled = false; btn.textContent = 'Update Ghost';
        if (s.success) GhostUI.toast('Ghost is up to date');
      }
    } catch (e) { clearInterval(poll); btn.disabled = false; btn.textContent = 'Update Ghost'; }
  }, 1500);
}

function panelHead(title) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, title));
  h.appendChild(text);
  return h;
}
function kv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }
function kvRow2(k, v, state) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k));
  const tr = GhostUI.h('div', { className: 'kv-val' });
  tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(state), v));
  r.appendChild(tr);
  return r;
}
function kvRowBtn(k, sub, btnLabel, onClick) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  const c = GhostUI.h('div', { className: 'ghost-row-content' });
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, k));
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub));
  r.appendChild(c);
  r.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-danger', onClick }, btnLabel));
  return r;
}

async function runDiag(body) {
  body.innerHTML = ''; body.appendChild(GhostUI.loading('Running diagnostics…'));
  let res;
  try { res = await GhostAPI.proxyGet('/v1/doctor'); }
  catch (e) { body.innerHTML = ''; body.appendChild(GhostUI.errorState('Diagnostics unavailable', 'The gateway may be starting.')); return; }
  body.innerHTML = '';
  const grid = GhostUI.h('div', { className: 'diag-grid' });
  const checks = res.checks || [];
  if (checks.length === 0) grid.appendChild(GhostUI.emptyState('Nothing to report', 'No checks returned.'));
  checks.forEach(ch => {
    const row = GhostUI.h('div', { className: 'diag-row' });
    const st = ch.status === 'ok' ? 'ready' : ch.status === 'warn' ? 'warn' : 'bad';
    row.appendChild(GhostUI.h('span', { className: 'status-dot ' + st }));
    row.appendChild(GhostUI.h('div', { className: 'diag-name' }, ch.name));
    const msg = GhostUI.h('div', { className: 'diag-msg' });
    msg.textContent = ch.message || '';
    row.appendChild(msg);
    grid.appendChild(row);
  });
  body.appendChild(grid);
}

GhostApp.registerSection('system', loadSystem);
