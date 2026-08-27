/* Ghost Section: Backups */
'use strict';

async function loadBackups(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Backups'));
  head.appendChild(GhostUI.h('p', {}, 'Download a copy of your Ghost’s memory, skills, and configuration.'));
  container.appendChild(head);

  const panel = GhostUI.h('div', { className: 'panel' });
  const pk = GhostUI.h('div', { className: 'kv' });
  pk.appendChild(kv('Memory', 'Included'));
  pk.appendChild(kv('Skills', 'Included'));
  pk.appendChild(kv('Configuration', 'Included'));
  pk.appendChild(kv('Automations', 'Included'));
  pk.appendChild(kv('Secrets', 'Not included'));
  panel.appendChild(pk);
  container.appendChild(panel);

  const actions = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => doBackup(btn) }, 'Download backup');
  actions.appendChild(btn);
  container.appendChild(actions);

  container.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-3)' },
    'Store the file somewhere safe. Secrets like API keys and your password are not included.'));
}

function kv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }

async function doBackup(btn) {
  btn.disabled = true; const orig = btn.textContent; btn.textContent = 'Preparing…';
  try {
    const res = await fetch('/api/admin/backup');
    if (!res.ok) throw new Error('failed');
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'ghost-backup-' + new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-') + '.tar.gz';
    document.body.appendChild(a); a.click(); a.remove();
    URL.revokeObjectURL(url);
    GhostUI.toast('Backup downloaded');
  } catch (e) {
    GhostUI.toast('Couldn’t create the backup.', 'err');
  } finally { btn.disabled = false; btn.textContent = orig; }
}

GhostApp.registerSection('backups', loadBackups);
