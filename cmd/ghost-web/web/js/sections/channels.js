/* Ghost Section: Channels — how Ghost can reach you and the world. */
'use strict';

async function loadChannels(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Channels'));
  head.appendChild(GhostUI.h('p', {}, 'How Ghost reaches you.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'chan-list' });
  listEl.appendChild(GhostUI.loading('Loading channels…'));
  container.appendChild(listEl);

  const [cfgRes, devRes, statusRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/channels'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
    GhostAPI.proxyGet('/v1/channels/status'),
  ]);

  const cfg = cfgRes.status === 'fulfilled' ? (cfgRes.value.channels || {}) : {};
  const devCount = devRes.status === 'fulfilled' ? (devRes.value.devices || []).length : 0;
  const op = statusRes.status === 'fulfilled' ? (statusRes.value.channels || {}) : {};

  listEl.innerHTML = '';

  // Ghost Mobile (always present, connected if a device is paired)
  listEl.appendChild(channelRow('Ghost Mobile', devCount > 0 ? 'connected' : 'neutral',
    devCount > 0 ? (devCount + ' device connected') : 'No phone connected',
    devCount > 0 ? null : () => GhostApp.navigate('devices')));

  // Telegram
  const tg = cfg.telegram || {};
  const tgOk = isSet(tg.token);
  listEl.appendChild(channelRow('Telegram', op.telegram ? 'connected' : (tgOk ? 'ready' : 'neutral'),
    tgOk ? (op.telegram ? 'Connected' : 'Configured') : 'Not configured',
    () => editTelegram(tg)));

  // Discord
  const dc = cfg.discord || {};
  const dcOk = isSet(dc.token);
  listEl.appendChild(channelRow('Discord', op.discord ? 'connected' : (dcOk ? 'ready' : 'neutral'),
    dcOk ? (op.discord ? 'Connected' : 'Configured') : 'Not configured',
    () => editDiscord(dc)));

  // Email
  const em = cfg.email || {};
  const emOk = em.enabled && em.smtp_host;
  listEl.appendChild(channelRow('Email', emOk ? 'ready' : 'neutral',
    emOk ? (em.to ? 'Delivers to ' + em.to : 'Configured') : 'Not configured',
    () => editEmail(em)));


}

function isSet(v) { return v && !String(v).startsWith('•') && String(v).length > 0; }

function channelRow(name, state, sub, onClick) {
  const row = onClick ? GhostUI.h('div', { className: 'ghost-link-row' }) : GhostUI.h('div', { className: 'ghost-row' });
  const c = GhostUI.h('div', { className: 'ghost-row-content' });
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, name));
  c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub));
  row.appendChild(c);
  const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  const label = state === 'connected' ? 'Connected' : state === 'ready' ? 'Configured' : 'Not connected';
  tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(state), label));
  if (onClick) tr.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
  row.appendChild(tr);
  if (onClick) row.addEventListener('click', onClick);
  return row;
}

function saveChannels(payload) {
  return GhostAPI.post('/api/admin/channels/save', payload);
}

function secretField(label, val, ph) {
  const f = GhostUI.h('div', { className: 'field' });
  f.appendChild(GhostUI.h('label', {}, label));
  const i = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: ph || (val ? 'Enter a new value to replace the current one' : 'Paste token') });
  f.appendChild(i);
  return f;
}

function editTelegram(cur) {
  const body = GhostUI.h('div');
  body.appendChild(secretField('Bot token', cur.token, 'Telegram bot token'));
  const field = body.querySelector('input');
  GhostUI.modal('Configure Telegram', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      const token = field.value.trim();
      if (!token) { GhostUI.toast('Enter a token.'); return; }
      try { await saveChannels({ telegram: { enabled: true, token } }); e.target.closest('.ghost-modal-backdrop').remove(); GhostUI.toast('Telegram saved'); loadChannels(document.getElementById('view')); }
      catch (err) { GhostUI.toast('Couldn’t save.', 'err'); }
    } }, 'Save'),
  ]);
}

function editDiscord(cur) {
  const body = GhostUI.h('div');
  body.appendChild(secretField('Bot token', cur.token, 'Discord bot token'));
  const field = body.querySelector('input');
  GhostUI.modal('Configure Discord', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      const token = field.value.trim();
      if (!token) { GhostUI.toast('Enter a token.'); return; }
      try { await saveChannels({ discord: { enabled: true, token } }); e.target.closest('.ghost-modal-backdrop').remove(); GhostUI.toast('Discord saved'); loadChannels(document.getElementById('view')); }
      catch (err) { GhostUI.toast('Couldn’t save.', 'err'); }
    } }, 'Save'),
  ]);
}

function editEmail(cur) {
  const body = GhostUI.h('div');
  const f1 = GhostUI.h('div', { className: 'field' }); f1.appendChild(GhostUI.h('label', {}, 'SMTP host')); const host = GhostUI.input(cur.smtp_host || 'smtp.example.com'); f1.appendChild(host); body.appendChild(f1);
  const f2 = GhostUI.h('div', { className: 'field' }); f2.appendChild(GhostUI.h('label', {}, 'SMTP port')); const port = GhostUI.h('input', { className: 'ghost-input', type: 'number', value: cur.smtp_port || 587 }); f2.appendChild(port); body.appendChild(f2);
  const f3 = GhostUI.h('div', { className: 'field' }); f3.appendChild(GhostUI.h('label', {}, 'From address')); const from = GhostUI.input(cur.from || ''); f3.appendChild(from); body.appendChild(f3);
  const f4 = GhostUI.h('div', { className: 'field' }); f4.appendChild(GhostUI.h('label', {}, 'Deliver to')); const to = GhostUI.input(cur.to || ''); f4.appendChild(to); body.appendChild(f4);
  const f5 = GhostUI.h('div', { className: 'field' }); f5.appendChild(GhostUI.h('label', {}, 'Password')); const pw = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: cur.username ? 'Enter to replace' : 'Email password / app password' }); f5.appendChild(pw); body.appendChild(f5);
  GhostUI.modal('Configure Email', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      try {
        await saveChannels({ email: { enabled: true, smtp_host: host.value.trim(), smtp_port: parseInt(port.value, 10) || 587, from: from.value.trim(), to: to.value.trim(), password: pw.value } });
        e.target.closest('.ghost-modal-backdrop').remove(); GhostUI.toast('Email saved'); loadChannels(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Couldn’t save.', 'err'); }
    } }, 'Save'),
  ]);
}

GhostApp.registerSection('channels', loadChannels);
