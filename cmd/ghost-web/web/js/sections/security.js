/* Ghost Section: Security \u2014 who can reach your Ghost. */
'use strict';

async function loadSecurity(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Security'));
  head.appendChild(GhostUI.h('p', {}, 'Who can reach your Ghost, plus backups and recovery when something goes wrong.'));
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
    fails.slice(0, 5).forEach(f => list.appendChild(securityKv(f.ip, new Date(f.time).toLocaleString())));
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
  bkKv.appendChild(securityKv('Memory', 'Included'));
  bkKv.appendChild(securityKv('Skills', 'Included'));
  bkKv.appendChild(securityKv('Configuration', 'Included'));
  bkKv.appendChild(securityKv('Automations', 'Included'));
  bkKv.appendChild(securityKv('Secrets', 'Not included'));
  bk.appendChild(bkKv);
  const bkActions = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const bkBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => GhostUI.downloadBackup(bkBtn) }, 'Download backup');
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

  const rec = GhostUI.h('div', { className: 'panel' });
  const recHead = GhostUI.h('div', { className: 'panel-head' });
  const recTitle = GhostUI.h('div');
  recTitle.appendChild(GhostUI.h('h2', {}, 'Recovery'));
  recTitle.appendChild(GhostUI.h('p', {}, 'If Ghost seems stuck, restart it first \u2014 reboot the device only if it doesn\u2019t come back. Neither touches your memory or devices.'));
  recHead.appendChild(recTitle);
  rec.appendChild(recHead);
  const recList = GhostUI.h('div', {});
  recList.appendChild(recoveryRow('Restart Ghost', 'Restarts the Ghost service. Use this first if Ghost seems stuck \u2014 it doesn\u2019t touch your memory or devices.', 'Restart', async () => {
    if (!(await GhostUI.confirmModal('Restart Ghost?', 'Your Ghost will be offline for a few moments while it restarts. Your memory and settings are safe.', 'Restart Ghost'))) return;
    try { await GhostAPI.post('/api/admin/ghost/restart'); GhostUI.toast('Ghost is restarting\u2026'); }
    catch (e) { GhostUI.toast('Couldn\u2019t restart Ghost.', 'err'); }
  }));
  recList.appendChild(recoveryRow('Reboot this device', 'Reboots the hardware Ghost runs on. Use only if something is wrong.', 'Reboot', async () => {
    if (!(await GhostUI.confirmModal('Reboot this device?', 'Your Ghost will be offline for a minute or two while the device reboots.', 'Reboot device'))) return;
    try { await GhostAPI.post('/api/admin/reboot'); GhostUI.toast('Rebooting\u2026'); }
    catch (e) { GhostUI.toast('Couldn\u2019t reboot.', 'err'); }
  }));
  rec.appendChild(recList);
  container.appendChild(rec);
}

function recoveryRow(k, sub, btnLabel, onClick) {
  const r = GhostUI.h('div', { className: 'ghost-row' });
  const c = GhostUI.h('div', { className: 'ghost-row-content' });
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, k));
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub));
  r.appendChild(c);
  r.appendChild(GhostUI.h('div', { className: 'ghost-row-trailing' },
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-danger', onClick }, btnLabel)));
  return r;
}

function securityKv(k, v) { const r = GhostUI.h('div', { className: 'kv-row' }); r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k)); r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v)); return r; }

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
