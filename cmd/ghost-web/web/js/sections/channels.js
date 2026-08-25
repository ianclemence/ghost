/* Ghost Section: Channels — how Ghost reaches you */
'use strict';

async function loadChannels(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-channels' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Channels'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'How Ghost can reach you.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/channels/status');
    const channelNames = ['telegram', 'discord', 'slack', 'email', 'whatsapp', 'line', 'sms', 'wechat'];

    for (const name of channelNames) {
      const ch = data[name] || {};
      const r = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, name.charAt(0).toUpperCase() + name.slice(1)));

      const running = ch.running || false;
      const enabled = ch.enabled || false;
      let statusText = 'Not connected';
      let dotState = 'offline';
      if (running) { statusText = 'Connected'; dotState = 'online'; }
      else if (enabled) { statusText = 'Configured'; dotState = 'warning'; }
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, statusText));
      r.appendChild(c);

      const dot = GhostUI.h('div', { style: 'display:flex;align-items:center;gap:var(--space-sm)' });
      dot.appendChild(GhostUI.statusDot(dotState));
      r.appendChild(dot);
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load channels.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

GhostApp.registerSection('channels', loadChannels);
