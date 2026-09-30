/* Ghost Section: About \u2014 the product, quietly. */
'use strict';

async function loadAbout(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'About'));
  head.appendChild(GhostUI.h('p', {}, 'Your Ghost: who it belongs to and what it is.'));
  container.appendChild(head);

  const [identityRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/identity'),
  ]);

  const identity = identityRes.status === 'fulfilled' ? identityRes.value : {};

  const brand = GhostUI.h('div', { className: 'row-flex', style: 'margin-bottom:var(--s-5);gap:var(--s-3)' });
  brand.appendChild(GhostUI.ghostMark('lg'));
  const brandText = GhostUI.h('div', {});
  brandText.appendChild(GhostUI.h('div', { className: 'type-title' }, identity.ghost_name || 'Ghost'));
  brandText.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, identity.owner_name ? ('Owned by ' + identity.owner_name) : 'Your AI. Your Memory. Your Machine.'));
  brand.appendChild(brandText);
  container.appendChild(brand);

  // Identity panel
  if (identity.configured) {
    const idPanel = GhostUI.h('div', { className: 'panel' });
    idPanel.appendChild(GhostUI.h('div', { className: 'panel-head' }, GhostUI.h('div', {}, GhostUI.h('h2', {}, 'Your Ghost'))));
    const details = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
    if (identity.ghost_id) details.appendChild(aboutKvRow('Ghost ID', identity.ghost_id.slice(0, 12) + '\u2026'));
    if (identity.created_at) details.appendChild(aboutKvRow('Created', GhostUI.timeAgo(Math.floor(new Date(identity.created_at).getTime() / 1000))));
    idPanel.appendChild(details);
    container.appendChild(idPanel);
  }

  const prose = GhostUI.h('div', { className: 'panel prose', style: 'margin-top:var(--s-3)' });
  prose.innerHTML = GhostUI.md(`
Ghost is a personal AI that lives on a small computer in your home, called a Pod. It remembers the people and plans in your life, does things for you, and speaks up when something needs you.

- **This console** is where you set Ghost up, connect it to things, and look after it.
- **The Ghost app** is where you talk to it, approve what it wants to do, and step in when it needs you.
- **The Pod** is the machine it lives on.

Your memory, files and logins stay on this Pod. Nothing is sent to a central service. Only what you ask Ghost to think about goes to the AI model you chose, and only if that model is in the cloud.

Ghost is open source. The code, the documentation and the license are at [github.com/ianclemence/ghost](https://github.com/ianclemence/ghost).
  `);
  container.appendChild(prose);
}

function aboutKvRow(label, value) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-key' }, label));
  r.appendChild(GhostUI.h('div', { className: 'kv-val' }, value));
  return r;
}

GhostApp.registerSection('about', loadAbout);
