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

  // A tree: each day is a branch, and what Ghost did that day hangs from it.
  // Every node carries a dot in the colour of its outcome, the time, what
  // happened, and (quietly) why Ghost acted. Days fold away with their caret.
  // The same thing failing or succeeding five times in a row is one line
  // with a count, not five lines (newest first, so the first of a run wins).
  const folded = [];
  items.forEach(item => {
    const prev = folded[folded.length - 1];
    const same = prev && prev.item.title === item.title && prev.item.state === item.state
      && (prev.item.summary || '') === (item.summary || '');
    if (same) prev.count++; else folded.push({ item, count: 1 });
  });
  const groups = [];
  folded.forEach(({ item, count }) => {
    const t = item.timestamp ? new Date(item.timestamp) : null;
    const valid = t && !isNaN(t.getTime());
    const label = valid ? activityDayLabel(t) : 'Earlier';
    let g = groups[groups.length - 1];
    if (!g || g.label !== label) { g = { label, items: [] }; groups.push(g); }
    g.items.push({ item, t: valid ? t : null, count });
  });

  const tree = GhostUI.h('div', { className: 'act-tree' });
  groups.forEach((g, gi) => {
    const day = GhostUI.h('section', { className: 'act-day', 'data-open': 'true' });
    const head = GhostUI.h('button', { className: 'act-day-head', type: 'button', 'aria-expanded': 'true' });
    head.appendChild(GhostUI.h('span', { className: 'act-caret', 'aria-hidden': 'true', html: '<svg viewBox="0 0 12 12" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m3.5 4.5 2.5 3 2.5-3"/></svg>' }));
    head.appendChild(GhostUI.h('span', { className: 'act-day-label' }, g.label));
    head.appendChild(GhostUI.h('span', { className: 'act-day-count' }, String(g.items.length)));
    const branch = GhostUI.h('ol', { className: 'act-branch' });
    g.items.forEach(({ item, t, count }, ni) => {
      const node = GhostUI.h('li', { className: 'act-node tone-' + GhostUI.activityTone(item.state) });
      node.style.setProperty('--n', String(Math.min(gi * 3 + ni, 12)));
      node.appendChild(GhostUI.h('span', { className: 'act-dot', 'aria-hidden': 'true' }));
      const body = GhostUI.h('div', { className: 'act-body' });
      const titleLine = GhostUI.h('div', { className: 'ghost-row-title act-title' }, item.title || 'Activity');
      const word = GhostUI.activityWord(item.state);
      const summary = (item.summary || '').trim();
      if (word && word.toLowerCase() !== summary.toLowerCase()) {
        titleLine.appendChild(GhostUI.h('span', { className: 'status-pill activity-status' }, GhostUI.statusDot(GhostUI.activityTone(item.state)), word));
      }
      if (count > 1) titleLine.appendChild(GhostUI.h('span', { className: 'act-repeat', title: 'Happened ' + count + ' times in a row' }, '\u00d7' + count));
      body.appendChild(titleLine);
      const bits = [];
      if (t) bits.push(t.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }));
      if (summary) bits.push(summary);
      if (bits.length) body.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, bits.join('  \u00b7  ')));
      // Revelation: why Ghost asked or acted.
      if (item.why) body.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle type-foot text-tertiary', style: 'margin-top:2px' }, item.why));
      // The raw technical text (tool error, query) is one click away, in a
      // quiet monospace box, for anyone who wants it.
      if (item.diagnostic) {
        const more = GhostUI.h('details', { className: 'act-details' });
        more.appendChild(GhostUI.h('summary', {}, 'Details'));
        more.appendChild(GhostUI.h('pre', { className: 'act-raw' }, item.diagnostic));
        body.appendChild(more);
      }
      node.appendChild(body);
      branch.appendChild(node);
    });
    head.addEventListener('click', () => {
      const open = day.getAttribute('data-open') !== 'true';
      day.setAttribute('data-open', String(open));
      head.setAttribute('aria-expanded', String(open));
    });
    day.appendChild(head);
    day.appendChild(branch);
    tree.appendChild(day);
  });
  listEl.appendChild(tree);
}

function activityDayLabel(d) {
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return 'Today';
  const y = new Date(now); y.setDate(y.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return 'Yesterday';
  return d.toLocaleDateString([], { weekday: 'long', day: 'numeric', month: 'short' });
}

GhostApp.registerSection('activity', loadActivity);
