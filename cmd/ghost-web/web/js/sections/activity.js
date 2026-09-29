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

  // Grouped by day; each row: what Ghost did, the outcome the runtime
  // recorded, and why it acted. The same words as the phone and Home.
  let day = '';
  items.forEach(item => {
    const t = item.timestamp ? new Date(item.timestamp) : null;
    const label = t && !isNaN(t.getTime()) ? activityDayLabel(t) : 'Earlier';
    if (label !== day) {
      day = label;
      listEl.appendChild(GhostUI.h('div', { className: 'activity-day' }, label));
    }
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, item.title || 'Activity'));
    const word = GhostUI.activityWord(item.state);
    const summary = (item.summary || '').trim();
    const bits = [];
    if (summary) bits.push(summary);
    if (t && !isNaN(t.getTime())) bits.push(t.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }));
    if (bits.length) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, bits.join('  ·  ')));
    // Revelation: why Ghost asked or acted.
    if (item.why) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle type-foot text-tertiary', style: 'margin-top:2px' }, item.why));
    row.appendChild(c);
    if (word && word.toLowerCase() !== summary.toLowerCase()) {
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
      tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot(GhostUI.activityTone(item.state)), word));
      row.appendChild(tr);
    }
    listEl.appendChild(row);
  });
}

function activityDayLabel(d) {
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return 'Today';
  const y = new Date(now); y.setDate(y.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return 'Yesterday';
  return d.toLocaleDateString([], { weekday: 'long', day: 'numeric', month: 'short' });
}

GhostApp.registerSection('activity', loadActivity);
