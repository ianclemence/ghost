/* Ghost Section: Devices — paired devices */
'use strict';

async function loadDevices(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-devices' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Devices'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Things connected to your Ghost.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/pairing/devices');
    const devices = data.devices || [];

    if (devices.length === 0) {
      section.appendChild(GhostUI.emptyState('No devices connected.', 'Connect your phone to Ghost.'));
    }

    for (const dev of devices) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, dev.display_name || 'Device'));

      let sub = '';
      if (dev.last_seen_at) {
        const diff = Date.now() - new Date(dev.last_seen_at).getTime();
        if (diff < 300000) sub = 'Connected now';
        else if (diff < 3600000) sub = `Last seen ${Math.round(diff/60000)}m ago`;
        else if (diff < 86400000) sub = `Last seen ${Math.round(diff/3600000)}h ago`;
        else sub = `Last seen ${Math.round(diff/86400000)}d ago`;
      }
      if (sub) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub));
      r.appendChild(c);

      if (dev.paired_at) {
        const d = new Date(dev.paired_at);
        r.appendChild(GhostUI.h('span', { className: 'type-footnote text-tertiary' }, d.toLocaleDateString()));
      }
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load devices.', e.message));
  }

  section.appendChild(listEl);

  // Connect button
  const actions = GhostUI.h('div', { style: 'margin-top:var(--space-xxl)' });
  actions.appendChild(GhostUI.btn('Connect another device', 'secondary', () => showPairingFlow()));
  section.appendChild(actions);

  container.appendChild(section);
}

async function showPairingFlow() {
  try {
    const res = await GhostAPI.proxyPost('/v1/pairing/invitations', {
      display_name: 'Phone',
      transport: 'lan',
      host: '0.0.0.0',
      port: '8766'
    });
    const token = res.token || res.pairing_token;
    const podId = res.pod_id || '';
    const qrText = `ghost://pair?v=1&pod=${podId}&transport=lan&token=${token}`;

    const body = GhostUI.h('div', { textAlign: 'center' });
    body.appendChild(GhostUI.h('div', { className: 'type-callout', style: 'margin-bottom:var(--space-lg)' }, 'Open the Ghost app on your phone and scan this code.'));
    body.appendChild(GhostUI.h('div', { className: 'type-mono', style: 'padding:var(--space-lg);background:var(--ghost-bg-sunken);border-radius:var(--radius-md);word-break:break-all;margin-bottom:var(--space-md)' }, qrText));
    body.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary' }, 'This code expires in 5 minutes.'));

    GhostUI.modal('Connect another device', body, [
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel')
    ]);
  } catch (e) {
    GhostUI.toast('Couldn\'t create pairing invitation.');
  }
}

GhostApp.registerSection('devices', loadDevices);
