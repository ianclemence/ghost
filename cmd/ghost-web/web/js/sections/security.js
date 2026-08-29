/* Ghost Section: Security — who can reach your Ghost. */
'use strict';

async function loadSecurity(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Security'));
  head.appendChild(GhostUI.h('p', {}, 'Who can reach your Ghost.'));
  container.appendChild(head);

  const panel = GhostUI.h('div', { className: 'panel' });

  // Owner access
  const ownerRow = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => showChangePassword() });
  const oc = GhostUI.h('div', { className: 'ghost-row-content' });
  oc.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Change password'));
  oc.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'The password used to open this console'));
  ownerRow.appendChild(oc); ownerRow.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
  panel.appendChild(ownerRow);

  // Devices
  const devRow = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => GhostApp.navigate('devices') });
  const dc = GhostUI.h('div', { className: 'ghost-row-content' });
  dc.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Manage devices'));
  dc.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Connect, view, or disconnect trusted phones and tablets'));
  devRow.appendChild(dc); devRow.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
  panel.appendChild(devRow);

  container.appendChild(panel);

  // Active sessions
  const sessPanel = GhostUI.h('div', { className: 'panel' });
  const sessHead = GhostUI.h('div', { className: 'panel-head' });
  const sessTitle = GhostUI.h('div');
  sessTitle.appendChild(GhostUI.h('h2', {}, 'Active sessions'));
  sessTitle.appendChild(GhostUI.h('p', {}, 'Signed-in browsers and devices.'));
  sessHead.appendChild(sessTitle);
  sessPanel.appendChild(sessHead);
  const sessBody = GhostUI.h('div', { id: 'sess-body' });
  sessBody.appendChild(GhostUI.loading('Loading sessions\u2026'));
  sessPanel.appendChild(sessBody);
  container.appendChild(sessPanel);
  loadSessions(sessBody);

  // Recent failed sign-ins (only if any)
  const [failRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/auth/failed-logins'),
  ]);
  const fails = failRes.status === 'fulfilled' ? (failRes.value.attempts || []) : [];
  if (fails.length > 0) {
    const sec = GhostUI.h('div', { className: 'panel' });
    const sh = GhostUI.h('div', { className: 'panel-head' });
    const shText = GhostUI.h('div');
    shText.appendChild(GhostUI.h('h2', {}, 'Recent failed sign-ins'));
    sh.appendChild(shText);
    sec.appendChild(sh);
    const list = GhostUI.h('div', { className: 'kv' });
    fails.slice(0, 5).forEach(f => list.appendChild(kv(f.ip, new Date(f.time).toLocaleString())));
    sec.appendChild(list);
    container.appendChild(sec);
  }

  // Backups panel
  const bk = GhostUI.h('div', { className: 'panel' });
  const bkH = GhostUI.h('div', { className: 'panel-head' });
  const bkText = GhostUI.h('div');
  bkText.appendChild(GhostUI.h('h2', {}, 'Backups'));
  bkText.appendChild(GhostUI.h('p', {}, 'Download a copy of your Ghost’s memory, skills, and configuration.'));
  bkH.appendChild(bkText);
  bk.appendChild(bkH);
  const bkKv = GhostUI.h('div', { className: 'kv' });
  bkKv.appendChild(kv('Memory', 'Included'));
  bkKv.appendChild(kv('Skills', 'Included'));
  bkKv.appendChild(kv('Configuration', 'Included'));
  bkKv.appendChild(kv('Automations', 'Included'));
  bkKv.appendChild(kv('Secrets', 'Not included'));
  bk.appendChild(bkKv);
  const bkActions = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const bkBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => doBackup(bkBtn) }, 'Download backup');
  bkActions.appendChild(bkBtn);
  bk.appendChild(bkActions);
  bk.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-3)' }, 'Store the file somewhere safe. Secrets like API keys and your password are not included.'));
  container.appendChild(bk);
}

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

async function loadSessions(body) {
  let res;
  try { res = await GhostAPI.get('/api/admin/sessions'); }
  catch (e) {
    body.innerHTML = '';
    body.appendChild(GhostUI.errorState('Couldn\u2019t load sessions', 'Try again in a moment.'));
    return;
  }
  body.innerHTML = '';
  const sessions = res.sessions || [];
  if (sessions.length === 0) {
    body.appendChild(GhostUI.emptyState('No active sessions', 'No one is signed in right now.'));
    return;
  }
  sessions.forEach(s => {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, s.current ? 'This browser' : (s.user_agent || 'Unknown device').slice(0, 50)));
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Signed in ' + GhostUI.timeAgo(Math.floor(new Date(s.issued_at).getTime() / 1000)) + (s.ip ? '  \u00b7  ' + s.ip : '')));
    row.appendChild(c);
    if (!s.current) {
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
      tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
        try { await GhostAPI.post('/api/admin/sessions/revoke', { token: s.token }); GhostUI.toast('Session signed out'); loadSessions(body); }
        catch (e) { GhostUI.toast('Couldn\u2019t sign out.', 'err'); }
      } }, 'Sign out'));
      row.appendChild(tr);
    } else {
      row.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot('ready'), 'You'));
    }
    body.appendChild(row);
  });
  if (sessions.length > 1) {
    const allBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-danger', style: 'margin-top:var(--s-3)', onClick: async () => {
      if (!(await GhostUI.confirmModal('Sign out all other sessions?', 'You will remain signed in on this browser.', 'Sign out all'))) return;
      try { await GhostAPI.post('/api/admin/sessions/revoke', { action: 'revoke_all' }); GhostUI.toast('Other sessions signed out'); loadSessions(body); }
      catch (e) { GhostUI.toast('Couldn\u2019t sign out.', 'err'); }
    } }, 'Sign out all other sessions');
    body.appendChild(allBtn);
  }
}

function kv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }

function showChangePassword() {
  const body = GhostUI.h('div');
  const mk = (label, ph) => {
    const f = GhostUI.h('div', { className: 'field' });
    f.appendChild(GhostUI.h('label', {}, label));
    const i = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: ph || '' });
    f.appendChild(i); body.appendChild(f); return i;
  };
  const cur = mk('Current password');
  const np = mk('New password', 'At least 8 characters');
  const cf = mk('Confirm new password');

  GhostUI.modal('Change password', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      if (np.value !== cf.value) { GhostUI.toast('Passwords don’t match.'); return; }
      if (np.value.length < 8) { GhostUI.toast('At least 8 characters.'); return; }
      try {
        await GhostAPI.post('/api/admin/password', { current: cur.value, new: np.value, confirm: cf.value });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Password changed');
      } catch (err) { GhostUI.toast('Couldn’t change it.', 'err'); }
    } }, 'Save'),
  ]);
}

GhostApp.registerSection('security', loadSecurity);
