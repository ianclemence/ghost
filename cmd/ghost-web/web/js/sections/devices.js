/* Ghost Section: Devices — paired phones and the secure connect flow. */
'use strict';

async function loadDevices(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Devices'));
  head.appendChild(GhostUI.h('p', {}, 'Phones and other devices that can reach Ghost.'));
  container.appendChild(head);

  GhostApp.setActions(null);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'dev-list' });
  listEl.appendChild(GhostUI.loading('Loading devices…'));
  container.appendChild(listEl);

  await renderDevices(listEl);
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
    listEl.appendChild(GhostUI.h('div', { style: 'text-align:center' }, GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showPairingModal() }, 'Connect a device')));
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
    if (d.capabilities && d.capabilities.length > 0) {
      sub.appendChild(document.createTextNode('  \u00b7  ' + d.capabilities.join(', ')));
    }
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
  function close() { closed = true; if (pollTimer) clearInterval(pollTimer); if (countdownTimer) clearInterval(countdownTimer); backdrop.remove(); }

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
