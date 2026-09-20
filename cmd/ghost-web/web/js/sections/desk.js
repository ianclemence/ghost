/* Ghost Section: The Desk — the work Ghost has done on your machine.
 *
 * A read-only projection of documents, artifacts, tools, and live surfaces.
 * /v1/desk returns already-normalized items; this surface renders one feed
 * with kind filters. Opening an item previews it; acting on it is a new
 * conversation turn through the normal approval flow. The Desk grants nothing.
 */
'use strict';

function deskKindLabel(kind) {
  switch (kind) {
    case 'document': return 'File';
    case 'artifact': return 'Made for you';
    case 'tool': return 'Tool';
    case 'surface': return 'Live session';
    default: return 'Item';
  }
}

function deskKindClass(kind) {
  switch (kind) {
    case 'artifact': return 'text-ok';
    case 'tool': return 'text-accent';
    case 'surface': return 'text-info';
    default: return 'text-tertiary';
  }
}

function deskSize(bytes) {
  if (!bytes || bytes <= 0) return '';
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return Math.round(bytes / 1024) + ' KB';
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
}

async function loadDesk(container) {
  if (GhostApp.currentSection() !== 'desk') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'The Desk'));
  head.appendChild(GhostUI.h('p', {},
    'The work Ghost has done on your machine — files it writes, things it makes for you, tools it builds, and live sessions it acts on.'));
  head.appendChild(GhostUI.h('p', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-2)' },
    'The Desk shows your Ghost\u2019s work. It never acts on its own — ask Ghost in a chat to open, send, or change anything.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'desk-list' });
  listEl.appendChild(GhostUI.loading('Looking at your Ghost\u2019s desk\u2026'));
  container.appendChild(listEl);

  let res;
  try {
    res = await GhostAPI.proxyGet('/v1/desk');
  } catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t reach your Ghost\u2019s desk', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  if (!document.body.contains(container)) return;
  renderDesk(listEl, Array.isArray(res && res.items) ? res.items : []);
}

function renderDesk(listEl, items) {
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState(
      'Your desk is clear',
      'When Ghost writes a file, makes something for you, or builds a tool, it appears here.'));
    return;
  }
  items.forEach(it => {
    const row = GhostUI.h('div', { className: 'ghost-link-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const titleRow = GhostUI.h('div', { className: 'ghost-row-title' });
    titleRow.appendChild(document.createTextNode(it.title || 'Untitled'));
    titleRow.appendChild(GhostUI.h('span', {
      className: deskKindClass(it.kind),
      style: 'margin-left:var(--s-2);font-size:var(--t-foot);font-weight:600',
    }, deskKindLabel(it.kind)));
    c.appendChild(titleRow);
    const parts = [];
    if (it.summary) parts.push(it.summary);
    const sz = deskSize(it.size);
    if (sz) parts.push(sz);
    if (parts.length) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, parts.join('  ·  ')));
    row.appendChild(c);
    listEl.appendChild(row);
  });
}

GhostApp.registerSection('desk', loadDesk);
