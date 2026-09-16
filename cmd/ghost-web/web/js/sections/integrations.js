/* Ghost Section: Integrations — connect Calendar, Flight, Home Assistant.
   Product setup for skills that need credentials. No SSH, no .env editing. */
'use strict';

async function loadIntegrations(container) {
  if (GhostApp.currentSection() !== 'integrations') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Connected Apps'));
  head.appendChild(GhostUI.h('p', {}, 'The apps and services Ghost can reach — Gmail, Google Calendar, flight tracking, Home Assistant, and camera.'));
  container.appendChild(head);

  const panel = GhostUI.h('div', { className: 'panel' });
  const listEl = GhostUI.h('div', { id: 'int-list' });
  listEl.appendChild(GhostUI.loading('Loading integrations…'));
  panel.appendChild(listEl);
  container.appendChild(panel);

  // Connections strip: vault lifecycle states (never secrets).
  const connEl = GhostUI.h('div', { className: 'chips', id: 'int-connections' });
  container.insertBefore(connEl, panel);
  GhostAPI.proxyGet('/v1/connected-apps').then(res => {
    if (!document.body.contains(container)) return;
    const conns = (res && (res.connected_apps || res.connections)) || [];
    connEl.innerHTML = '';
    const interesting = conns.filter(c => c.status && c.status !== 'connected' && c.status !== 'not_configured');
    interesting.slice(0, 8).forEach(c => {
      connEl.appendChild(GhostUI.h('span', { className: 'chip chip-waiting', title: c.display_name },
        '! ' + c.display_name + ': ' + String(c.status).replace(/_/g, ' ')));
    });
  }).catch(() => {});

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

  // Gmail (OAuth product flow: consent URL in new tab, poll status)
  const gm = ints.gmail || {};
  const gmState = gm.connected ? 'connected' : 'neutral';
  listEl.appendChild(intRow('Gmail', 'Email',
    gmState, gm.connected ? 'Connected' : 'Not connected',
    gm.connected ? () => confirmDisconnectGmail() : () => startGmailConnect()));

  // Outlook (OAuth product flow: consent URL in new tab, poll status)
  const om = ints.outlook || {};
  const omState = om.connected ? 'connected' : 'neutral';
  listEl.appendChild(intRow('Outlook', 'Email + Calendar',
    omState, om.connected ? 'Connected' : 'Not connected',
    om.connected ? () => confirmDisconnectOutlook() : () => startOutlookConnect()));

  // Spotify (OAuth product flow: consent URL in new tab, poll status)
  const sp = ints.spotify || {};
  const spState = sp.connected ? 'connected' : 'neutral';
  listEl.appendChild(intRow('Spotify', 'Music',
    spState, sp.connected ? 'Connected' : 'Not connected',
    sp.connected ? () => confirmDisconnectSpotify() : () => startSpotifyConnect()));

  // GitHub (paste read-only PAT; trust-user: token scopes govern)
  const gh = ints.github || {};
  const ghCfg = gh.configured === true;
  listEl.appendChild(intRow('GitHub', 'Code search',
    ghCfg ? 'ready' : 'neutral', ghCfg ? 'Configured' : 'Not connected',
    () => editGithubToken(ghCfg)));

  // Notion (paste integration token)
  const nt = ints.notion || {};
  const ntCfg = nt.configured === true;
  listEl.appendChild(intRow('Notion', 'Docs',
    ntCfg ? 'ready' : 'neutral', ntCfg ? 'Configured' : 'Not connected',
    () => editNotionToken(ntCfg)));

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

  // Camera mirrors the same readiness the Skills list uses — the two
  // screens can never disagree. Status already fetched above.
  const cam = (status && status.integrations && status.integrations.camera) || {};
  const camAvail = !!cam.available;
  const camRow = GhostUI.h('div', { className: 'model-row' });
  const camMain = GhostUI.h('div', { className: 'model-main' });
  camMain.appendChild(GhostUI.h('div', { className: 'model-name' }, 'Camera'));
  camMain.appendChild(GhostUI.h('div', { className: 'model-sub' },
    'On-device  ·  ' + (cam.detail || (camAvail ? 'camera detected' : 'checking…'))));
  camRow.appendChild(camMain);
  const camTr = GhostUI.h('div', { className: 'ghost-row-trailing' });
  camTr.appendChild(GhostUI.h('span', { className: 'status-pill' },
    GhostUI.statusDot(camAvail ? 'ready' : 'neutral'), camAvail ? 'Ready' : 'No camera'));
  const camBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => GhostApp.navigate('skills') }, 'View skills');
  camTr.appendChild(camBtn);
  camRow.appendChild(camTr);
  listEl.appendChild(camRow);
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
    body.appendChild(GhostUI.h('p', { className: 'type-mono', style: 'word-break:break-all' }, (res && res.message) || 'Install the calendar helper where the Ghost service can see it, then try again.'));
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
    body.appendChild(GhostUI.h('p', { className: 'text-tertiary' }, 'No setup link was returned. Make sure the calendar helper is installed, then try again.'));
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

async function startGmailConnect() {
  let res;
  try { res = await GhostAPI.post('/api/admin/integrations/gmail/oauth/start', {}); }
  catch (e) {
    GhostUI.toast((e && e.message) || 'Couldn’t start Gmail setup.', 'err');
    return;
  }
  if (res && res.status === 'ready') { GhostUI.toast('Gmail already connected'); loadIntegrations(document.getElementById('view')); return; }
  if (!res || !res.auth_url) {
    GhostUI.toast((res && res.message) || 'Gmail sign-in isn’t set up on this Ghost yet.', 'err');
    return;
  }
  window.open(res.auth_url, '_blank', 'noopener');
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'Google’s sign-in screen opened in a new tab. Approve access, then come back here and press Check connection.'));
  GhostUI.modal('Connect Gmail', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      try {
        const st = await GhostAPI.get('/api/admin/integrations/status');
        const gm = st && st.integrations && st.integrations.gmail;
        if (gm && gm.connected) {
          e.target.closest('.ghost-modal-backdrop').remove();
          GhostUI.toast('Gmail connected');
          loadIntegrations(document.getElementById('view'));
        } else {
          GhostUI.toast('Not connected yet — approve access first.', 'err');
        }
      } catch (err) { GhostUI.toast('Couldn’t check status.', 'err'); }
    } }, 'Check connection'),
  ]);
}

async function confirmDisconnectGmail() {
  if (!(await GhostUI.confirmModal('Disconnect Gmail?', 'Ghost will no longer read or send your email. You can reconnect anytime.', 'Disconnect'))) return;
  try { await GhostAPI.post('/api/admin/integrations/gmail/disconnect', {}); GhostUI.toast('Gmail disconnected'); }
  catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
  loadIntegrations(document.getElementById('view'));
}

async function startOutlookConnect() {
  let res;
  try { res = await GhostAPI.post('/api/admin/integrations/outlook/oauth/start', {}); }
  catch (e) {
    GhostUI.toast((e && e.message) || 'Couldn’t start Outlook setup.', 'err');
    return;
  }
  if (res && res.status === 'ready') { GhostUI.toast('Outlook already connected'); loadIntegrations(document.getElementById('view')); return; }
  if (!res || !res.auth_url) {
    GhostUI.toast((res && res.message) || 'Outlook sign-in isn’t set up on this Ghost yet.', 'err');
    return;
  }
  window.open(res.auth_url, '_blank', 'noopener');
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'Microsoft’s sign-in screen opened in a new tab. Approve access, then come back here and press Check connection.'));
  GhostUI.modal('Connect Outlook', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      try {
        const st = await GhostAPI.get('/api/admin/integrations/status');
        const om = st && st.integrations && st.integrations.outlook;
        if (om && om.connected) {
          e.target.closest('.ghost-modal-backdrop').remove();
          GhostUI.toast('Outlook connected');
          loadIntegrations(document.getElementById('view'));
        } else {
          GhostUI.toast('Not connected yet — approve access first.', 'err');
        }
      } catch (err) { GhostUI.toast('Couldn’t check status.', 'err'); }
    } }, 'Check connection'),
  ]);
}

async function confirmDisconnectOutlook() {
  if (!(await GhostUI.confirmModal('Disconnect Outlook?', 'Ghost will no longer read or send your email or calendar. You can reconnect anytime.', 'Disconnect'))) return;
  try { await GhostAPI.post('/api/admin/integrations/outlook/disconnect', {}); GhostUI.toast('Outlook disconnected'); }
  catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
  loadIntegrations(document.getElementById('view'));
}

async function startSpotifyConnect() {
  let res;
  try { res = await GhostAPI.post('/api/admin/integrations/spotify/oauth/start', {}); }
  catch (e) {
    GhostUI.toast((e && e.message) || 'Couldn’t start Spotify setup.', 'err');
    return;
  }
  if (res && res.status === 'ready') { GhostUI.toast('Spotify already connected'); loadIntegrations(document.getElementById('view')); return; }
  if (!res || !res.auth_url) {
    GhostUI.toast((res && res.message) || 'Spotify sign-in isn’t set up on this Ghost yet.', 'err');
    return;
  }
  window.open(res.auth_url, '_blank', 'noopener');
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'Spotify’s sign-in screen opened in a new tab. Approve access, then come back here and press Check connection.'));
  GhostUI.modal('Connect Spotify', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      try {
        const st = await GhostAPI.get('/api/admin/integrations/status');
        const sp = st && st.integrations && st.integrations.spotify;
        if (sp && sp.connected) {
          e.target.closest('.ghost-modal-backdrop').remove();
          GhostUI.toast('Spotify connected');
          loadIntegrations(document.getElementById('view'));
        } else {
          GhostUI.toast('Not connected yet — approve access first.', 'err');
        }
      } catch (err) { GhostUI.toast('Couldn’t check status.', 'err'); }
    } }, 'Check connection'),
  ]);
}

async function confirmDisconnectSpotify() {
  if (!(await GhostUI.confirmModal('Disconnect Spotify?', 'Ghost will no longer control your music. You can reconnect anytime.', 'Disconnect'))) return;
  try { await GhostAPI.post('/api/admin/integrations/spotify/disconnect', {}); GhostUI.toast('Spotify disconnected'); }
  catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
  loadIntegrations(document.getElementById('view'));
}

function editGithubToken(configured) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' },
    'Paste a read-only personal access token (Settings → Developer settings → Personal access tokens). Your token’s own scopes govern what Ghost can see. Stored securely on this device only.'));
  const f = GhostUI.h('div', { className: 'field' });
  f.appendChild(GhostUI.h('label', {}, configured ? 'New token (leave blank to keep current)' : 'Personal access token'));
  const inp = GhostUI.h('input', { className: 'ghost-input', type: 'password', placeholder: configured ? '••• current token saved •••' : 'ghp_…', autocomplete: 'off' });
  f.appendChild(inp); body.appendChild(f);
  GhostUI.modal(configured ? 'Edit GitHub token' : 'Connect GitHub', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const token = inp.value.trim();
      if (!token) { e.target.closest('.ghost-modal-backdrop').remove(); return; }
      try {
        await GhostAPI.post('/api/admin/integrations/github/save', { token });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('GitHub connected');
        loadIntegrations(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Save failed: ' + (err.message || ''), 'err'); }
    } }, 'Save'),
  ]);
  if (configured) {
    body.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Disconnect GitHub?', 'Ghost will no longer search your code.', 'Disconnect'))) return;
      try { await GhostAPI.post('/api/admin/integrations/github/disconnect', {}); GhostUI.toast('GitHub disconnected'); }
      catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
      loadIntegrations(document.getElementById('view'));
    } }, 'Disconnect'));
  }
}

function editNotionToken(configured) {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' },
    'Paste a Notion internal integration token and share the pages you want Ghost to search. Stored securely on this device only.'));
  const f = GhostUI.h('div', { className: 'field' });
  f.appendChild(GhostUI.h('label', {}, configured ? 'New token (leave blank to keep current)' : 'Integration token'));
  const inp = GhostUI.h('input', { className: 'ghost-input', type: 'password', placeholder: configured ? '••• current token saved •••' : 'ntn_… / secret_…', autocomplete: 'off' });
  f.appendChild(inp); body.appendChild(f);
  GhostUI.modal(configured ? 'Edit Notion token' : 'Connect Notion', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const token = inp.value.trim();
      if (!token) { e.target.closest('.ghost-modal-backdrop').remove(); return; }
      try {
        await GhostAPI.post('/api/admin/integrations/notion/save', { token });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Notion connected');
        loadIntegrations(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Save failed: ' + (err.message || ''), 'err'); }
    } }, 'Save'),
  ]);
  if (configured) {
    body.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Disconnect Notion?', 'Ghost will no longer search your docs.', 'Disconnect'))) return;
      try { await GhostAPI.post('/api/admin/integrations/notion/disconnect', {}); GhostUI.toast('Notion disconnected'); }
      catch (e) { GhostUI.toast('Couldn’t disconnect.', 'err'); return; }
      loadIntegrations(document.getElementById('view'));
    } }, 'Disconnect'));
  }
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
