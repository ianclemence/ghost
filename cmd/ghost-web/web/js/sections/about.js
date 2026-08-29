/* Ghost Section: About \u2014 the product, quietly. */
'use strict';

async function loadAbout(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'About'));
  head.appendChild(GhostUI.h('p', {}, 'The product behind your AI.'));
  container.appendChild(head);

  const [statusRes, identityRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/status'),
    GhostAPI.get('/api/admin/identity'),
  ]);

  const version = statusRes.status === 'fulfilled' ? (statusRes.value.version || '?') : '?';
  const identity = identityRes.status === 'fulfilled' ? identityRes.value : {};

  const brand = GhostUI.h('div', { className: 'row-flex', style: 'margin-bottom:var(--s-5);gap:var(--s-3)' });
  brand.appendChild(GhostUI.ghostMark('lg'));
  const brandText = GhostUI.h('div', {});
  brandText.appendChild(GhostUI.h('div', { className: 'type-title' }, identity.ghost_name || 'Ghost'));
  brandText.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, identity.owner_name ? ('Owned by ' + identity.owner_name) : 'Your AI, Your Memory, Your Machine'));
  brand.appendChild(brandText);
  container.appendChild(brand);

  // Identity panel
  if (identity.configured) {
    const idPanel = GhostUI.h('div', { className: 'panel' });
    idPanel.appendChild(GhostUI.h('div', { className: 'panel-head' }, GhostUI.h('div', {}, GhostUI.h('h2', {}, 'Your Ghost'))));
    const details = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
    if (identity.ghost_id) details.appendChild(kvRow('Ghost ID', identity.ghost_id.slice(0, 12) + '\u2026'));
    if (identity.created_at) details.appendChild(kvRow('Created', GhostUI.timeAgo(Math.floor(new Date(identity.created_at).getTime() / 1000))));
    idPanel.appendChild(details);
    container.appendChild(idPanel);
  }

  const prose = GhostUI.h('div', { className: 'panel prose' });
  prose.innerHTML = GhostUI.md(`
Ghost is a personal AI that lives on your own hardware. It remembers what matters, works for you without being watched, and stays with you across your devices.

- **Ghost Web** is where you own, configure, understand, and take care of Ghost.
- **Ghost Mobile** is where you talk to Ghost and take it with you.
- **The Ghost Pod** is the hardware Ghost lives on.

This console is version **${version}**.

Ghost is open-source. Source, documentation, and license are in the project repository. Configuration and secrets live only on your device \u2014 nothing here is sent to a central service unless you explicitly connect a cloud provider.
  `);
  container.appendChild(prose);
}

function kvRow(label, value) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-label' }, label));
  r.appendChild(GhostUI.h('div', { className: 'kv-value' }, value));
  return r;
}
