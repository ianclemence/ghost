/* Ghost Section: Activity — "what Ghost has been doing."
   Uses the canonical, user-safe activity projection (/v1/activity), not
   conversation history and not raw runtime events. Read-only timeline. */
'use strict';

async function loadActivity(container) {
  if (GhostApp.currentSection() !== 'activity') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Activity'));
  head.appendChild(GhostUI.h('p', {}, 'What Ghost has been doing. Each item is something real Ghost did or is waiting on \u2014 nothing here is guessed from conversation text.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'activity-list' });
  listEl.appendChild(GhostUI.loading('Loading activity…'));
  container.appendChild(listEl);

  const refresh = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => loadActivity(container) }, 'Refresh');
  GhostApp.setActions(refresh);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/activity?limit=100'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load activity', 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;

  const items = (res && res.activity) || [];
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState('Nothing here yet', 'As Ghost works for you, what it did will appear here \u2014 checked, sent, remembered, scheduled.'));
    return;
  }

  const stateDot = { running: 'neutral', waiting: 'warn', success: 'ready', failed: 'bad', cancelled: 'neutral', paused: 'neutral' };
  items.forEach(item => {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const title = GhostUI.h('div', { className: 'ghost-row-title' });
    title.appendChild(document.createTextNode(item.title || 'Activity'));
    c.appendChild(title);
    const sub = GhostUI.h('div', { className: 'ghost-row-subtitle' });
    if (item.summary) sub.appendChild(document.createTextNode(item.summary));
    if (item.summary && item.timestamp) sub.appendChild(document.createTextNode('  \u00b7  '));
    if (item.timestamp) sub.appendChild(document.createTextNode(activityTime(item.timestamp)));
    c.appendChild(sub);
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const st = item.state || '';
    tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(stateDot[st] || 'neutral'), humanState(st)));
    row.appendChild(tr);
    listEl.appendChild(row);
  });
}

function humanState(st) {
  const map = { running: 'Running', waiting: 'Waiting', success: 'Done', failed: 'Failed', cancelled: 'Cancelled', paused: 'Paused' };
  return map[st] || (st || '');
}

function activityTime(ts) {
  if (!ts) return '';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '';
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' \u00b7 ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

GhostApp.registerSection('activity', loadActivity);
