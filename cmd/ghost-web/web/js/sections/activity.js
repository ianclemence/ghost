/* Ghost Section: Activity — what Ghost has been doing. */
'use strict';

async function loadActivity(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Activity'));
  head.appendChild(GhostUI.h('p', {}, 'A record of what Ghost has done on your behalf.'));
  container.appendChild(head);

  const filters = [
    { key: 'all', label: 'All' },
    { key: 'messages', label: 'Messages' },
    { key: 'automations', label: 'Automations' },
    { key: 'memory', label: 'Memory' },
    { key: 'errors', label: 'Errors' },
  ];
  let activeFilter = 'all';
  const chips = GhostUI.h('div', { className: 'chips' });
  filters.forEach(f => {
    const c = GhostUI.h('button', { className: 'chip' + (f.key === activeFilter ? ' active' : ''), onClick: () => { activeFilter = f.key; chips.querySelectorAll('.chip').forEach(x => x.classList.remove('active')); c.classList.add('active'); paint(); } }, f.label);
    c.dataset.filter = f.key;
    chips.appendChild(c);
  });
  container.appendChild(chips);

  const timeline = GhostUI.h('div', { className: 'timeline', id: 'act-timeline' });
  timeline.appendChild(GhostUI.loading('Loading activity…'));
  container.appendChild(timeline);

  const [sessions, jobs, memories, traces] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/sessions'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/memory/files'),
    GhostAPI.proxyGet('/v1/traces'),
  ]);

  const items = [];
  if (sessions.status === 'fulfilled') {
    for (const s of (sessions.value.sessions || [])) {
      items.push({ kind: 'messages', ts: s.last_activity, title: (s.title && s.title.trim()) ? s.title : 'Conversation', meta: GhostUI.fmtNum(s.message_count) + ' messages' });
    }
  }
  if (jobs.status === 'fulfilled') {
    for (const j of (jobs.value.jobs || [])) {
      const lr = j.state && j.state.last_run_at;
      if (lr) items.push({ kind: 'automations', ts: Math.floor(new Date(lr).getTime() / 1000), title: j.name, meta: 'Last run' });
    }
  }
  if (memories.status === 'fulfilled') {
    for (const m of (memories.value || []).slice(0, 20)) {
      items.push({ kind: 'memory', ts: m.modified, title: m.name.replace(/\.md$/, ''), meta: 'Remembered' });
    }
  }
  if (traces.status === 'fulfilled') {
    const incidents = (traces.value.incidents || []).slice(0, 20);
    for (const inc of incidents) {
      items.push({ kind: 'errors', ts: Math.floor((inc.timestamp || 0) / 1000) || 0, title: inc.message || 'Incident', meta: inc.level || 'error' });
    }
  }

  items.sort((a, b) => b.ts - a.ts);

  function paint() {
    const t = document.getElementById('act-timeline');
    t.innerHTML = '';
    const filtered = activeFilter === 'all' ? items : items.filter(i => i.kind === activeFilter);
    if (filtered.length === 0) {
      const labels = { all: 'Nothing here yet', messages: 'No conversations yet', automations: 'No automations have run', memory: 'Nothing remembered yet', errors: 'No errors — Ghost is healthy' };
      t.appendChild(GhostUI.emptyState(labels[activeFilter] || 'Nothing here', 'This view will fill in as Ghost works for you.'));
      return;
    }
    let lastDay = null;
    for (const it of filtered) {
      const day = GhostUI.dayLabel(it.ts);
      if (day !== lastDay) { t.appendChild(GhostUI.h('div', { className: 'tl-day' }, day)); lastDay = day; }
      const item = GhostUI.h('div', { className: 'tl-item' });
      item.appendChild(GhostUI.h('div', { className: 'tl-time' }, GhostUI.clockTime(it.ts)));
      const body = GhostUI.h('div', { className: 'tl-body' });
      body.appendChild(GhostUI.h('div', { className: 'tl-title' }, it.title));
      body.appendChild(GhostUI.h('div', { className: 'tl-meta' }, it.meta));
      item.appendChild(body);
      t.appendChild(item);
    }
  }
  paint();
}

GhostApp.registerSection('activity', loadActivity);
