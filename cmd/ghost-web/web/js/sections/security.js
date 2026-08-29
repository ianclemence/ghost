/* Ghost Section: Security \u2014 who can reach your Ghost. */
'use strict';

async function loadSecurity(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Security'));
  head.appendChild(GhostUI.h('p', {}, 'Who can reach your Ghost.'));
  container.appendChild(head);

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
  bkText.appendChild(GhostUI.h('p', {}, 'Download a copy of your Ghost\u2019s memory, skills, and configuration.'));
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

  // Change password panel
  const panel = GhostUI.h('div', { className: 'panel' });
  const panelHead = GhostUI.h('div', { className: 'panel-head' });
  panelHead.appendChild(GhostUI.h('div', {}, GhostUI.h('h2', {}, 'Change password'), GhostUI.h('p', {}, 'The password used to open this console')));
  panel.appendChild(panelHead);

  const pwFields = GhostUI.h('div', { className: 'field' });
  pwFields.appendChild(GhostUI.h('label', {}, 'Current password'));
  const curPw = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password' });
  pwFields.appendChild(curPw);
  panel.appendChild(pwFields);

  const npFields = GhostUI.h('div', { className: 'field' });
  npFields.appendChild(GhostUI.h('label', {}, 'New password'));
  const newPw = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: 'At least 8 characters' });
  npFields.appendChild(newPw);
  panel.appendChild(npFields);

  const cfFields = GhostUI.h('div', { className: 'field' });
  cfFields.appendChild(GhostUI.h('label', {}, 'Confirm new password'));
  const cfPw = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password' });
  cfFields.appendChild(cfPw);
  panel.appendChild(cfFields);

  const pwErr = GhostUI.h('div', { className: 'type-foot', style: 'color:var(--bad);min-height:18px;margin-bottom:var(--s-2)' });
  panel.appendChild(pwErr);

  const pwBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary' }, 'Save');
  pwBtn.addEventListener('click', async () => {
    pwErr.textContent = '';
    if (newPw.value !== cfPw.value) { pwErr.textContent = 'Passwords don\u2019t match.'; return; }
    if (newPw.value.length < 8) { pwErr.textContent = 'At least 8 characters.'; return; }
    if (!curPw.value) { pwErr.textContent = 'Current password is required.'; return; }
    pwBtn.disabled = true;
    try {
      await GhostAPI.post('/api/admin/password', { current: curPw.value, new: newPw.value, confirm: cfPw.value });
      GhostUI.toast('Password changed');
      curPw.value = ''; newPw.value = ''; cfPw.value = '';
    } catch (err) {
      pwErr.textContent = err.message || 'Couldn\u2019t change it.';
    } finally { pwBtn.disabled = false; }
  });
  panel.appendChild(pwBtn);
  container.appendChild(panel);
}

async function doBackup(btn) {
  btn.disabled = true; const orig = btn.textContent; btn.textContent = 'Preparing\u2026';
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
    GhostUI.toast('Couldn\u2019t create the backup.', 'err');
  } finally { btn.disabled = false; btn.textContent = orig; }
}

function kv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }

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

GhostApp.registerSection('security', loadSecurity);
