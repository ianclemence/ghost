/* Ghost Section: Devices — paired phones and the secure connect flow. */
'use strict';

async function loadDevices(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Devices'));
  head.appendChild(GhostUI.h('p', {}, 'Phones paired with your Ghost — add a new one or remove one.'));
  container.appendChild(head);

  GhostApp.setActions(null);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'dev-list' });
  listEl.appendChild(GhostUI.loading('Loading devices…'));
  container.appendChild(listEl);

  const awayEl = GhostUI.h('div', { id: 'away-card' });
  container.appendChild(awayEl);

  await renderDevices(listEl);
  renderAway(awayEl);
}

/* ── Away from home: Ghost Connect ───────────────────────────────────────
   Link this Pod to the hosted relay, check it, and connect a phone that is
   not on the home network. Everything here is what `ghost relay link|status|
   pair` does, so the console and the terminal never disagree. */
let awayTimer = null;

function drawQR(text, host) {
  const canvas = GhostUI.h('canvas', {});
  const box = GhostUI.h('div', { className: 'ghost-qr' });
  box.appendChild(canvas);
  if (!GhostQR.draw(text, canvas, 5)) {
    box.innerHTML = '';
    host.appendChild(GhostUI.h('div', { className: 'qr-fallback-string' }, text));
  } else host.appendChild(box);
}

async function renderAway(el) {
  if (awayTimer) { clearInterval(awayTimer); awayTimer = null; }
  el.innerHTML = '';
  let st;
  try { st = await GhostAPI.proxyGet('/v1/connect/status'); }
  catch (e) { return; }

  const card = GhostUI.h('div', { className: 'ghost-card away-card' });
  card.appendChild(GhostUI.h('div', { className: 'ghost-card-title' }, 'Away from home'));
  el.appendChild(card);

  if (!st.available && !st.linked) {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' },
      'At home your phone connects straight to your Ghost. To reach it from anywhere, use Tailscale or your own relay, or Ghost Connect once it opens.'));
    return;
  }

  if (st.pending && st.pending.state === 'waiting') { awayWaiting(card, st.pending, el); return; }

  if (!st.linked) {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' },
      'Reach your Ghost from anywhere with nothing to set up. Everything is sealed on your phone, so the relay carries it and can’t read it.'));
    if (st.pending && (st.pending.state === 'needs_plan' || st.pending.state === 'expired' || st.pending.state === 'error')) {
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub away-note' }, st.pending.message || 'That didn’t finish.'));
    }
    card.appendChild(GhostUI.h('div', { className: 'away-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (ev) => {
        ev.target.disabled = true;
        try { await GhostAPI.proxyPost('/v1/connect/link', {}); renderAway(el); }
        catch (e) { ev.target.disabled = false; GhostUI.toast('Couldn’t start. Check the Pod’s internet connection.', 'err'); }
      } }, 'Link to Ghost Connect')));
    return;
  }

  // Linked
  const tunnel = st.tunnel || 'offline';
  const label = tunnel === 'connected' ? 'Connected' : tunnel === 'needs-payment' ? 'Plan needs attention' : 'Connecting…';
  const dot = tunnel === 'connected' ? 'ready' : tunnel === 'needs-payment' ? 'error' : 'neutral';
  const line = GhostUI.h('div', { className: 'ghost-card-meta' });
  line.appendChild(GhostUI.statusDot ? GhostUI.statusDot(dot) : document.createTextNode(''));
  line.appendChild(document.createTextNode(' ' + label));
  card.appendChild(line);
  if (tunnel === 'needs-payment') {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub away-note' },
      'Your Ghost Connect plan has lapsed, so the relay has stopped. Your Ghost still works at home. Renew on the Ghost site to turn it back on.'));
  } else {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' },
      'Your phone can reach this Ghost from anywhere. The pass renews itself while your plan is active.'));
  }
  card.appendChild(GhostUI.h('div', { className: 'away-actions' },
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showRemotePairing() }, 'Connect a phone for away from home'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Unlink Ghost Connect?', 'Phones will stop reaching your Ghost away from home. At home, and over any other route you set up, nothing changes.', 'Unlink'))) return;
      try { await GhostAPI.proxyPost('/v1/connect/unlink', {}); GhostUI.toast('Unlinked'); renderAway(el); }
      catch (e) { GhostUI.toast('Couldn’t unlink it.', 'err'); }
    } }, 'Unlink')));
}

function awayWaiting(card, p, el) {
  card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, 'On your computer or phone, open this address and sign in, then check the code matches:'));
  const link = GhostUI.h('a', { href: p.verify_url, target: '_blank', rel: 'noopener', className: 'away-link' }, p.verify_url.replace(/^https?:\/\//, ''));
  card.appendChild(link);
  card.appendChild(GhostUI.h('div', { className: 'away-code', 'aria-label': 'Your code' }, p.user_code));
  const qr = GhostUI.h('div', { className: 'qr-wrap' });
  drawQR(p.verify_url, qr);
  card.appendChild(qr);
  const left = GhostUI.h('div', { className: 'qr-expiry' }, '');
  card.appendChild(left);
  card.appendChild(GhostUI.h('div', { className: 'away-actions' },
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => renderAway(el) }, 'Refresh')));
  const exp = new Date(p.expires_at).getTime();
  const tick = () => { const s = Math.max(0, Math.floor((exp - Date.now()) / 1000)); left.textContent = s > 0 ? 'Waiting… the code works for ' + Math.ceil(s / 60) + ' more minutes' : 'This code expired.'; };
  tick();
  awayTimer = setInterval(async () => {
    tick();
    if (!document.body.contains(card)) { clearInterval(awayTimer); awayTimer = null; return; }
    try {
      const st = await GhostAPI.proxyGet('/v1/connect/status');
      if (!st.pending || st.pending.state !== 'waiting' || st.linked) { renderAway(el); if (st.linked) GhostUI.toast('Linked. Your Ghost is on Ghost Connect.'); }
    } catch (e) { /* keep waiting */ }
  }, 3000);
}

function showRemotePairing() {
  const backdrop = GhostUI.h('div', { className: 'ghost-modal-backdrop' });
  const m = GhostUI.h('div', { className: 'ghost-modal', style: 'max-width:440px' });
  m.appendChild(GhostUI.h('div', { className: 'modal__title' }, 'Connect a phone for away from home'));
  m.appendChild(GhostUI.h('div', { className: 'modal__body type-callout text-tertiary' },
    'Open Ghost on the phone and scan this. It works once and expires in a few minutes. It carries a key for that phone, so don’t share it.'));
  const wrap = GhostUI.h('div', { className: 'qr-wrap' });
  m.appendChild(wrap);
  const actions = GhostUI.h('div', { className: 'modal__actions' });
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => backdrop.remove() }, 'Close'));
  m.appendChild(actions);
  backdrop.appendChild(m);
  backdrop.addEventListener('click', e => { if (e.target === backdrop) backdrop.remove(); });
  document.body.appendChild(backdrop);
  wrap.appendChild(GhostUI.loading('Preparing…'));
  GhostAPI.proxyPost('/v1/connect/pair', {}).then(r => {
    wrap.innerHTML = '';
    drawQR(r.uri, wrap);
    wrap.appendChild(GhostUI.h('div', { className: 'qr-expiry' }, 'Expires in ' + Math.round((r.expires_in || 300) / 60) + ' minutes'));
  }).catch(() => { backdrop.remove(); GhostUI.toast('Couldn’t prepare a phone. Is Ghost Connect connected?', 'err'); });
}

async function renderDevices(listEl) {
  let res;
  try { res = await GhostAPI.proxyGet('/v1/pairing/devices'); }
  catch (e) {
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn’t reach your Ghost', 'The device service may be starting. Try again shortly.'));
    return;
  }
  const devices = res.devices || [];
  listEl.innerHTML = '';

  if (devices.length > 0) {
    const connectBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showPairingModal() }, 'Connect another device');
    GhostApp.setActions(connectBtn);
  } else {
    GhostApp.setActions(null);
  }

  if (devices.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No devices connected', 'Connect your phone by scanning a code \u2014 then you can talk to Ghost from anywhere, while your Ghost stays on this hardware.'));
    listEl.appendChild(GhostUI.h('div', { style: 'text-align:center;border-top:0' }, GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showPairingModal() }, 'Connect a device')));
    return;
  }
  const now = Date.now();
  devices.forEach(d => {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const title = GhostUI.h('div', { className: 'ghost-row-title' });
    title.appendChild(document.createTextNode(d.display_name || 'Device'));
    const plat = (d.platform || 'device');
    let statusText, dot;
    if (d.last_seen_at) {
      const seen = new Date(d.last_seen_at).getTime();
      if (now - seen < 3 * 60000) { statusText = 'Connected now'; dot = 'ready'; }
      else { statusText = 'Last seen ' + GhostUI.timeAgo(Math.floor(seen / 1000)); dot = 'neutral'; }
    } else { statusText = 'Paired'; dot = 'neutral'; }
    title.appendChild(GhostUI.h('span', { className: 'ghost-row-subtitle', style: 'margin-left:var(--s-2);font-weight:400' }, '\u00b7  ' + statusText));
    c.appendChild(title);
    const sub = GhostUI.h('div', { className: 'ghost-row-subtitle' });
    sub.appendChild(document.createTextNode(plat.charAt(0).toUpperCase() + plat.slice(1)));
    sub.appendChild(document.createTextNode('  \u00b7  added ' + GhostUI.timeAgo(Math.floor(new Date(d.paired_at).getTime() / 1000))));
    c.appendChild(sub);
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Disconnect this device?', d.display_name + ' will no longer be able to reach your Ghost. Your Ghost itself is not affected.', 'Disconnect'))) return;
      try { await GhostAPI.proxyPost('/v1/pairing/revoke', { device_id: d.device_id }); GhostUI.toast('Device disconnected'); renderDevices(document.getElementById('dev-list')); }
      catch (e) { GhostUI.toast('Couldn’t disconnect it.', 'err'); }
    } }, 'Disconnect'));
    row.appendChild(tr);
    listEl.appendChild(row);
  });
}

function showPairingModal() {
  const backdrop = GhostUI.h('div', { className: 'ghost-modal-backdrop' });
  const m = GhostUI.h('div', { className: 'ghost-modal', style: 'max-width:420px' });
  m.appendChild(GhostUI.h('div', { className: 'modal__title' }, 'Connect another device'));
  m.appendChild(GhostUI.h('div', { className: 'modal__body type-callout text-tertiary' }, 'Open Ghost on your phone and scan this code. It expires shortly and can be used once.'));

  const wrap = GhostUI.h('div', { className: 'qr-wrap' });
  const canvas = GhostUI.h('canvas', {});
  const qrBox = GhostUI.h('div', { className: 'ghost-qr' });
  qrBox.appendChild(canvas);
  wrap.appendChild(qrBox);
  const expiry = GhostUI.h('div', { className: 'qr-expiry' }, '');
  wrap.appendChild(expiry);
  const fallback = GhostUI.h('div', { className: 'qr-fallback-string hidden' });
  wrap.appendChild(fallback);
  const manual = GhostUI.h('div', { className: 'qr-manual hidden' });
  wrap.appendChild(manual);
  m.appendChild(wrap);

  const actions = GhostUI.h('div', { className: 'modal__actions' });
  const cancelBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost' }, 'Cancel');
  actions.appendChild(cancelBtn);
  m.appendChild(actions);
  backdrop.appendChild(m);
  backdrop.addEventListener('click', e => { if (e.target === backdrop) close(); });
  document.body.appendChild(backdrop);

  let pollTimer = null, countdownTimer = null, currentInvite = null, closed = false;
  // Reset the pairing baseline for every dialog: a stale count from a
  // previous session would otherwise trigger an instant false success.
  window.__ghostDeviceCount = null;
  function close() { closed = true; if (pollTimer) clearInterval(pollTimer); if (countdownTimer) clearInterval(countdownTimer); window.__ghostDeviceCount = null; backdrop.remove(); }

  function copyText(text, btn) {
    const restore = () => { btn.textContent = 'Copy token'; };
    const done = () => { btn.textContent = 'Copied ✓'; setTimeout(restore, 1500); GhostUI.toast('Token copied'); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(() => fallbackCopy(text, done));
    } else {
      fallbackCopy(text, done);
    }
  }

  function fallbackCopy(text, done) {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy'); done(); }
    catch (e) { GhostUI.toast('Copy failed — select and copy manually', 'err'); }
    document.body.removeChild(ta);
  }

  cancelBtn.addEventListener('click', async () => {
    if (currentInvite) { try { await GhostAPI.proxyPost('/v1/pairing/cancel', { pairing_id: currentInvite.pairing_id }); } catch (e) {} }
    close();
  });

  async function generate() {
    let inv;
    try {
      // Pass the address the admin used to reach this console so the QR
      // carries a usable host. If the console was opened via localhost,
      // let the gateway detect its LAN address instead.
      const body = { display_name: 'Phone', transport: 'lan' };
      const host = window.location.hostname;
      if (host && host !== 'localhost' && host !== '127.0.0.1') body.host = host;
      inv = await GhostAPI.proxyPost('/v1/pairing/invitations', body);
    }
    catch (e) { close(); GhostUI.toast('Couldn’t create a code.', 'err'); return; }
    currentInvite = inv;
    const url = 'ghost://pair?v=1&pod=' + encodeURIComponent(inv.pod_id) + '&transport=' + encodeURIComponent(inv.transport) + '&host=' + encodeURIComponent(inv.host) + '&port=' + encodeURIComponent(inv.port) + '&token=' + encodeURIComponent(inv.token);
    const ok = GhostQR.draw(url, canvas, 5);
    if (!ok) { qrBox.innerHTML = ''; fallback.classList.remove('hidden'); fallback.textContent = url; }
    else fallback.classList.add('hidden');

    // Manual entry: expose the bare token so it can be typed/copied on a phone
    // without scanning the QR.
    manual.classList.remove('hidden');
    manual.innerHTML = '';
    manual.appendChild(GhostUI.h('div', { className: 'qr-manual-label' }, "Can’t scan? Enter this token in the app’s manual screen:"));
    manual.appendChild(GhostUI.h('div', { className: 'qr-manual-token' }, inv.token));
    const copyBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm' }, 'Copy token');
    copyBtn.addEventListener('click', () => copyText(inv.token, copyBtn));
    manual.appendChild(copyBtn);
    const expiresAt = new Date(inv.expires_at).getTime();
    const tick = () => {
      const left = Math.max(0, Math.floor((expiresAt - Date.now()) / 1000));
      expiry.textContent = left > 0 ? 'Expires in ' + left + 's' : 'This code has expired.';
      if (left <= 0 && !closed) { clearInterval(countdownTimer); expired(); }
    };
    tick();
    countdownTimer = setInterval(tick, 1000);
  }

  async function poll() {
    try {
      const res = await GhostAPI.proxyGet('/v1/pairing/devices');
      const count = (res.devices || []).length;
      if (window.__ghostDeviceCount != null && count > window.__ghostDeviceCount) {
        window.__ghostDeviceCount = count;
        success();
        return;
      }
      if (window.__ghostDeviceCount == null) {
        window.__ghostDeviceCount = count;
      }
    } catch (e) { /* ignore */ }
  }

  function success() {
    clearInterval(pollTimer); clearInterval(countdownTimer);
    m.innerHTML = '';
    m.appendChild(GhostUI.h('div', { style: 'text-align:center;padding:var(--s-4) 0' },
      GhostUI.h('div', { style: 'font-size:40px;margin-bottom:var(--s-3)' }, '✓'),
      GhostUI.h('div', { className: 'type-title' }, 'Device connected'),
      GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-top:var(--s-2)' }, 'Your phone can now reach your Ghost.')
    ));
    m.appendChild(GhostUI.h('div', { className: 'modal__actions', style: 'justify-content:center' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => { close(); renderDevices(document.getElementById('dev-list')); } }, 'Done')));
  }

  function expired() {
    clearInterval(pollTimer);
    m.innerHTML = '';
    m.appendChild(GhostUI.h('div', { className: 'modal__title' }, 'Code expired'));
    m.appendChild(GhostUI.h('div', { className: 'modal__body type-callout text-tertiary' }, 'For security, pairing codes expire after a few minutes.'));
    m.appendChild(GhostUI.h('div', { className: 'modal__actions', style: 'justify-content:center' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => { document.body.removeChild(backdrop); showPairingModal(); } }, 'Generate a new code')));
  }

  generate();
  pollTimer = setInterval(poll, 2000);
}

GhostApp.registerSection('devices', loadDevices);
