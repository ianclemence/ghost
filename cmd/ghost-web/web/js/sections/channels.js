/* Ghost Section: Channels — how Ghost can reach you and the world. */
'use strict';

async function loadChannels(container) {
  if (GhostApp.currentSection() !== 'channels') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Channels'));
  head.appendChild(GhostUI.h('p', {}, 'How Ghost reaches you.'));
  container.appendChild(head);

  const panel = GhostUI.h('div', { className: 'panel' });
  const listEl = GhostUI.h('div', { id: 'chan-list' });
  listEl.appendChild(GhostUI.loading('Loading channels…'));
  panel.appendChild(listEl);
  container.appendChild(panel);

  const [cfgRes, devRes, statusRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/channels'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
    GhostAPI.proxyGet('/v1/channels/status'),
  ]);

  const cfg = cfgRes.status === 'fulfilled' ? (cfgRes.value.channels || {}) : {};
  const devCount = devRes.status === 'fulfilled' ? (devRes.value.devices || []).length : 0;
  const op = statusRes.status === 'fulfilled' ? (statusRes.value.channels || {}) : {};

  listEl.innerHTML = '';

  // Ghost Mobile
  listEl.appendChild(channelRow('Ghost Mobile', 'Mobile', devCount > 0 ? 'connected' : 'neutral',
    devCount > 0 ? (devCount + ' device connected') : 'No phone connected',
    devCount > 0 ? null : () => GhostApp.navigate('devices')));

  // Telegram
  const tg = cfg.telegram || {};
  const tgOk = isSet(tg.token);
  const tgConnected = tgOk && op.telegram;
  listEl.appendChild(channelRow('Telegram', 'Messaging', tgConnected ? 'connected' : (tgOk ? 'ready' : 'neutral'),
    tgConnected ? 'Connected' : (tgOk ? 'Configured' : 'Not configured'),
    () => editTelegram(tg)));

  // Discord
  const dc = cfg.discord || {};
  const dcOk = isSet(dc.token);
  const dcConnected = dcOk && op.discord;
  listEl.appendChild(channelRow('Discord', 'Messaging', dcConnected ? 'connected' : (dcOk ? 'ready' : 'neutral'),
    dcConnected ? 'Connected' : (dcOk ? 'Configured' : 'Not configured'),
    () => editDiscord(dc)));

  // Slack
  const sl = cfg.slack || {};
  const slOk = isSet(sl.bot_token) && isSet(sl.app_token);
  const slConnected = slOk && op.slack;
  listEl.appendChild(channelRow('Slack', 'Messaging', slConnected ? 'connected' : (slOk ? 'ready' : 'neutral'),
    slConnected ? 'Connected' : (slOk ? 'Configured' : 'Not configured'),
    () => editSlack(sl)));

  // WhatsApp
  const wa = cfg.whatsapp || {};
  const waOk = wa.enabled;
  const waConnected = waOk && op.whatsapp;
  listEl.appendChild(channelRow('WhatsApp', 'Messaging', waConnected ? 'connected' : (waOk ? 'ready' : 'neutral'),
    waConnected ? 'Connected' : (waOk ? 'Configured' : 'Not configured'),
    () => editWhatsApp(wa)));

  // Email
  const em = cfg.email || {};
  const emOk = em.enabled && em.smtp_host;
  listEl.appendChild(channelRow('Email', 'Email', emOk ? 'ready' : 'neutral',
    emOk ? (em.to ? 'Delivers to ' + em.to : 'Configured') : 'Not configured',
    () => editEmail(em)));
}

function isSet(v) { return v && !String(v).startsWith('•') && String(v).length > 0; }

function channelRow(name, kind, state, sub, onClick) {
  const row = GhostUI.h('div', { className: 'model-row' });
  const main = GhostUI.h('div', { className: 'model-main' });
  main.appendChild(GhostUI.h('div', { className: 'model-name' }, name));
  main.appendChild(GhostUI.h('div', { className: 'model-sub' }, kind + '  ·  ' + sub));
  row.appendChild(main);
  const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  const label = state === 'connected' ? 'Connected' : state === 'ready' ? 'Configured' : 'Not connected';
  tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(state), label));
  if (onClick) {
    const btnLabel = state === 'connected' || state === 'ready' ? 'Edit' : 'Configure';
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick }, btnLabel));
  }
  row.appendChild(tr);
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

function editSlack(cur) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' }, 'Slack requires both a Bot token (xoxb-) and an App-level token (xapp-) with the connections:write scope.'));
  const botWrap = GhostUI.h('div');
  botWrap.appendChild(GhostUI.h('label', { style: 'display:block;font-size:var(--t-foot);color:var(--ink-soft);margin-bottom:4px' }, 'Bot token'));
  const botField = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: cur.bot_token ? 'Enter a new bot token to replace the current one' : 'xoxb-…' });
  botWrap.appendChild(botField);
  body.appendChild(botWrap);
  const appWrap = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
  appWrap.appendChild(GhostUI.h('label', { style: 'display:block;font-size:var(--t-foot);color:var(--ink-soft);margin-bottom:4px' }, 'App token'));
  const appField = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: cur.app_token ? 'Enter a new app token to replace the current one' : 'xapp-…' });
  appWrap.appendChild(appField);
  body.appendChild(appWrap);
  GhostUI.modal('Configure Slack', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      const bot = botField.value.trim();
      const app = appField.value.trim();
      if (!bot || !app) { GhostUI.toast('Both tokens are required.'); return; }
      try { await saveChannels({ slack: { enabled: true, bot_token: bot, app_token: app } }); e.target.closest('.ghost-modal-backdrop').remove(); GhostUI.toast('Slack saved'); loadChannels(document.getElementById('view')); }
      catch (err) { GhostUI.toast('Couldn’t save.', 'err'); }
    } }, 'Save'),
  ]);
}

function editWhatsApp(cur) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' }, 'WhatsApp uses a bridge URL — point it at your running whatsapp-web.js bridge or compatible gateway.'));
  const wrap = GhostUI.h('div');
  wrap.appendChild(GhostUI.h('label', { style: 'display:block;font-size:var(--t-foot);color:var(--ink-soft);margin-bottom:4px' }, 'Bridge URL'));
  const urlField = GhostUI.h('input', { className: 'ghost-input', placeholder: 'http://localhost:3000', value: cur.bridge_url || '' });
  wrap.appendChild(urlField);
  body.appendChild(wrap);
  GhostUI.modal('Configure WhatsApp', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: e => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async e => {
      const url = urlField.value.trim();
      if (!url) { GhostUI.toast('Enter a bridge URL.'); return; }
      try { await saveChannels({ whatsapp: { enabled: true, bridge_url: url } }); e.target.closest('.ghost-modal-backdrop').remove(); GhostUI.toast('WhatsApp saved'); loadChannels(document.getElementById('view')); }
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
