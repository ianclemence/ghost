/* Ghost Section: Integrations — connect Calendar, Flight, Home Assistant.
   Product setup for skills that need credentials. No SSH, no .env editing. */
'use strict';

async function loadIntegrations(container) {
  if (GhostApp.currentSection() !== 'integrations') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Integrations'));
  head.appendChild(GhostUI.h('p', {}, 'Connect the services Ghost can use on your behalf.'));
  container.appendChild(head);

  const panel = GhostUI.h('div', { className: 'panel' });
  const listEl = GhostUI.h('div', { id: 'int-list' });
  listEl.appendChild(GhostUI.loading('Loading integrations…'));
  panel.appendChild(listEl);
  container.appendChild(panel);

  let status;
  try { status = await GhostAPI.get('/api/admin/integrations/status'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn’t load integrations', e.message || 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const ints = (status && status.integrations) || {};
  listEl.innerHTML = '';

  // Calendar
  const cal = ints.calendar || {};
  const calState = cal.connected ? 'connected' : 'neutral';
  listEl.appendChild(intRow('Google Calendar', 'Calendar',
    calState, cal.connected ? 'Connected' : 'Not connected',
    cal.connected ? () => confirmDisconnectCalendar() : () => startCalendarConnect()));

  // Flight
  const fl = ints.flight || {};
  const flCfg = fl.configured === true;
  listEl.appendChild(intRow('Flight tracking', 'Aviation data',
    flCfg ? 'ready' : 'neutral', flCfg ? 'Configured' : 'Not configured',
    () => editFlightKey(flCfg)));

  // Home Assistant
  const ha = ints.homeassistant || {};
  const haCfg = ha.configured === true;
  listEl.appendChild(intRow('Home Assistant', 'Smart home',
    haCfg ? 'ready' : 'neutral', haCfg ? 'Configured' : 'Not configured',
    () => editHass(haCfg)));

  // Hardware note (no credentials — points to Skills readiness)
  const hw = GhostUI.h('div', { className: 'model-row' });
  const hwMain = GhostUI.h('div', { className: 'model-main' });
  hwMain.appendChild(GhostUI.h('div', { className: 'model-name' }, 'Camera & hardware'));
  hwMain.appendChild(GhostUI.h('div', { className: 'model-sub' }, 'Sensors  ·  Uses on-device tools, no account needed'));
  hw.appendChild(hwMain);
  const hwTr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  hwTr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot('ready'), 'On-device'));
  const hwBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => GhostApp.navigate('skills') }, 'View skills');
  hwTr.appendChild(hwBtn);
  hw.appendChild(hwTr);
  listEl.appendChild(hw);
}

function intRow(name, kind, state, sub, onClick) {
  const row = GhostUI.h('div', { className: 'model-row' });
  const main = GhostUI.h('div', { className: 'model-main' });
  main.appendChild(GhostUI.h('div', { className: 'model-name' }, name));
  main.appendChild(GhostUI.h('div', { className: 'model-sub' }, kind + '  ·  ' + sub));
  row.appendChild(main);
  const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  const label = state === 'connected' ? 'Connected' : state === 'ready' ? 'Configured' : 'Not connected';
  tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(state), label));
  if (onClick) {
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick },
      state === 'connected' || state === 'ready' ? 'Edit' : 'Configure'));
  }
  row.appendChild(tr);
  return row;
}

async function startCalendarConnect() {
  let res;
  try { res = await GhostAPI.post('/api/admin/integrations/calendar/start', {}); }
  catch (e) {
    const msg = (e && e.message) || '';
    if (msg) GhostUI.toast(msg, 'err');
    else GhostUI.toast('Couldn’t start calendar setup.', 'err');
    return;
  }
  if (res && res.status === 'ready') { GhostUI.toast('Calendar already connected'); loadIntegrations(document.getElementById('view')); return; }
  if (res && res.status === 'needs_setup' && !res.setup_url) {
    const body = GhostUI.h('div');
    body.appendChild(GhostUI.h('p', {}, 'Calendar setup needs one admin step first:'));
    body.appendChild(GhostUI.h('p', { className: 'type-mono', style: 'word-break:break-all' }, (res && res.message) || 'Install gcalcli where the Ghost service can see it, then try again.'));
    GhostUI.modal('Calendar setup', body, [
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Got it'),
    ]);
    return;
  }
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'To connect Google Calendar:'));
  const ol = GhostUI.h('ol', { style: 'margin:0 0 var(--s-3) 1.2em' });
  ol.appendChild(GhostUI.h('li', {}, 'Open the setup link on any device and approve access.'));
  ol.appendChild(GhostUI.h('li', {}, 'Come back here and press Check connection.'));
  body.appendChild(ol);
  if (res && res.setup_url) {
    const link = GhostUI.h('a', { href: res.setup_url, target: '_blank', rel: 'noopener', style: 'word-break:break-all' }, res.setup_url);
    body.appendChild(GhostUI.h('p', {}, link));
  } else {
    body.appendChild(GhostUI.h('p', { className: 'text-tertiary' }, 'No setup link was returned. Make sure gcalcli is installed, then try again.'));
  }
  GhostUI.modal('Connect Calendar', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      try {
        const st = await GhostAPI.get('/api/admin/integrations/status');
        const cal = st && st.integrations && st.integrations.calendar;
        if (cal && cal.connected) {
          e.target.closest('.ghost-modal-backdrop').remove();
          GhostUI.toast('Calendar connected');
          loadIntegrations(document.getElementById('view'));
        } else {
          GhostUI.toast('Not connected yet — approve access first.', 'err');
        }
      } catch (err) { GhostUI.toast('Couldn’t check status.', 'err'); }
    } }, 'Check connection'),
  ]);
}

async function confirmDisconnectCalendar() {
  if (!(await GhostUI.confirmModal('Disconnect Calendar?', 'Ghost will no longer read your calendar. You can reconnect anytime.', 'Disconnect'))) return;
  try { await GhostAPI.post('/api/admin/integrations/calendar/disconnect', {}); GhostUI.toast('Calendar disconnected'); }
  catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
  loadIntegrations(document.getElementById('view'));
}

function editFlightKey(configured) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' },
    'Flight status uses AviationStack (free tier: 100 lookups/month). Get a key at aviationstack.com, then paste it here. It is stored securely on this device only.'));
  const f = GhostUI.h('div', { className: 'field' });
  f.appendChild(GhostUI.h('label', {}, configured ? 'New API key (leave blank to keep current)' : 'API key'));
  const inp = GhostUI.h('input', { className: 'ghost-input', type: 'password', placeholder: configured ? '••• current key saved •••' : 'aviationstack API key', autocomplete: 'off' });
  f.appendChild(inp); body.appendChild(f);
  GhostUI.modal(configured ? 'Edit flight key' : 'Connect flight tracking', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const key = inp.value.trim();
      if (!key) { e.target.closest('.ghost-modal-backdrop').remove(); return; }
      try {
        await GhostAPI.post('/api/admin/integrations/flight/save', { api_key: key });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Flight tracking connected');
        loadIntegrations(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Save failed: ' + (err.message || ''), 'err'); }
    } }, 'Save'),
  ]);
}

function editHass(configured) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' },
    'Point Ghost at your Home Assistant instance. Stored securely on this device only.'));
  const mk = (label, ph, pw) => {
    const f = GhostUI.h('div', { className: 'field' });
    f.appendChild(GhostUI.h('label', {}, label));
    const i = GhostUI.h('input', { className: 'ghost-input', placeholder: ph, autocomplete: 'off' });
    if (pw) i.type = 'password';
    f.appendChild(i); body.appendChild(f); return i;
  };
  const urlInp = mk('Home Assistant URL', 'http://homeassistant.local:8123', false);
  const tokInp = mk('Long-lived access token', configured ? '••• current token saved •••' : 'paste token', true);
  GhostUI.modal(configured ? 'Edit Home Assistant' : 'Connect Home Assistant', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const url = urlInp.value.trim(), token = tokInp.value.trim();
      if (!url || !token) { GhostUI.toast('URL and token are required.'); return; }
      if (!/^https?:\/\//i.test(url)) { GhostUI.toast('URL must start with http:// or https://', 'err'); return; }
      try {
        await GhostAPI.post('/api/admin/integrations/homeassistant/save', { url, token });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Home Assistant connected');
        loadIntegrations(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Save failed: ' + (err.message || ''), 'err'); }
    } }, 'Save'),
  ]);
}

GhostApp.registerSection('integrations', loadIntegrations);
