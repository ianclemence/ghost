/* Ghost Section: Updates — keep Ghost current. */
'use strict';

async function loadUpdates(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Updates'));
  head.appendChild(GhostUI.h('p', {}, 'Keep Ghost current.'));
  container.appendChild(head);

  const [stRes, upRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/status'),
    GhostAPI.get('/api/admin/update/status'),
  ]);
  const version = stRes.status === 'fulfilled' ? (stRes.value.version || '?') : '?';
  const up = upRes.status === 'fulfilled' ? upRes.value : { running: false, log: '' };

  const panel = GhostUI.h('div', { className: 'panel' });
  const pk = GhostUI.h('div', { className: 'kv' });
  pk.appendChild(kv('Current version', version));
  panel.appendChild(pk);
  container.appendChild(panel);

  const actions = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const updateBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => runUpdate(logBox, updateBtn) }, 'Update Ghost');
  actions.appendChild(updateBtn);
  container.appendChild(actions);

  const logBox = GhostUI.h('div', { className: 'panel hidden', style: 'margin-top:var(--s-4)' });
  container.appendChild(logBox);

  if (up.running) runUpdate(logBox, updateBtn);
  else if (up.log) { logBox.classList.remove('hidden'); logBox.appendChild(GhostUI.h('pre', { className: 'type-mono', style: 'white-space:pre-wrap;margin:0' }, up.log)); }
}

function kv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }

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

GhostApp.registerSection('updates', loadUpdates);
