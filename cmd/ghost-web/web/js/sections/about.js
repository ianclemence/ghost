/* Ghost Section: About — the product, quietly. */
'use strict';

async function loadAbout(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'About'));
  container.appendChild(head);

  let version = '?';
  try { version = (await GhostAPI.get('/api/admin/status')).version || '?'; } catch (e) {}

  const brand = GhostUI.h('div', { className: 'row-flex', style: 'margin-bottom:var(--s-5);gap:var(--s-3)' });
  brand.appendChild(GhostUI.ghostMark('lg'));
  brand.appendChild(GhostUI.h('div', {}, GhostUI.h('div', { className: 'type-title' }, 'Ghost'), GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Your AI, Your Memory, Your Machine')));
  container.appendChild(brand);

  const prose = GhostUI.h('div', { className: 'panel prose' });
  prose.innerHTML = GhostUI.md(`
Ghost is a personal AI that lives on your own hardware. It remembers what matters, works for you without being watched, and stays with you across your devices.

- **Ghost Web** is where you own, configure, understand, and take care of Ghost.
- **Ghost Mobile** is where you talk to Ghost and take it with you.
- **The Ghost Pod** is the hardware Ghost lives on.

This console is version **${version}**.

Ghost is open-source. Source, documentation, and license are in the project repository. Configuration and secrets live only on your device — nothing here is sent to a central service unless you explicitly connect a cloud provider.
  `);
  container.appendChild(prose);
}

GhostApp.registerSection('about', loadAbout);
