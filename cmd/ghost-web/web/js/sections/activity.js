/* Ghost Section: Activity — what Ghost has been doing. */
'use strict';

async function loadActivity(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Activity'));
  head.appendChild(GhostUI.h('p', {}, 'A record of what Ghost has done on your behalf.'));
  container.appendChild(head);

  const tabs = [
    { key: 'timeline', label: 'Timeline' },
    { key: 'routines', label: 'Routines' },
    { key: 'automations', label: 'Automations' },
  ];
  let activeTab = 'timeline';

  const tabRow = GhostUI.h('div', { className: 'chips' });
  const bodyWrap = GhostUI.h('div', { id: 'act-body' });
  container.appendChild(tabRow);
  container.appendChild(bodyWrap);

  function showTab(key) {
    activeTab = key;
    tabRow.querySelectorAll('.chip').forEach(x => x.classList.toggle('active', x.dataset.tab === key));
    // Fresh inner div per tab so in-flight fetches from the previous tab
    // find their elements detached and bail instead of painting stale UI.
    bodyWrap.innerHTML = '';
    const body = GhostUI.h('div', {});
    bodyWrap.appendChild(body);
    if (key === 'routines') { loadRoutines(body); return; }
    if (key === 'automations') { loadAutomations(body); return; }
    renderTimeline(body);
  }

  tabs.forEach(t => {
    const c = GhostUI.h('button', { className: 'chip' + (t.key === activeTab ? ' active' : ''), onClick: () => showTab(t.key) }, t.label);
    c.dataset.tab = t.key;
    tabRow.appendChild(c);
  });
  showTab('timeline');
}

async function renderTimeline(container) {

  const filters = [
    { key: 'all', label: 'All' },
    { key: 'messages', label: 'Messages' },
    { key: 'automations', label: 'Automations' },
    { key: 'memory', label: 'Memory' },
    { key: 'errors', label: 'Errors' },
  ];
  let activeFilter = 'all';

  const filterRow = GhostUI.h('div', { className: 'chips' });
  filters.forEach(f => {
    const c = GhostUI.h('button', { className: 'chip' + (f.key === activeFilter ? ' active' : ''), onClick: () => {
      activeFilter = f.key;
      filterRow.querySelectorAll('.chip').forEach(x => x.classList.remove('active'));
      c.classList.add('active');
      paint();
    } }, f.label);
    c.dataset.filter = f.key;
    filterRow.appendChild(c);
  });
  container.appendChild(filterRow);

  const timeline = GhostUI.h('div', { className: 'timeline', id: 'act-timeline' });
  timeline.style.minHeight = '300px';
  timeline.appendChild(GhostUI.loading('Loading activity…'));
  container.appendChild(timeline);

  // Canonical chips: the event stream's human narrative ("What is Ghost
  // doing for me?"). Rendered first; the legacy timeline follows.
  const chipsEl = GhostUI.h('div', { className: 'chips chips-live', id: 'act-chips' });
  container.insertBefore(chipsEl, timeline);
  const STATE_GLYPH = { running: '◌', waiting: '!', success: '✓', failed: '×', cancelled: '−', paused: '❚❚' };
  GhostAPI.proxyGet('/v1/activity?limit=20').then(res => {
    if (!document.body.contains(container)) return;
    const chips = (res && res.activity) || [];
    chipsEl.innerHTML = '';
    if (chips.length === 0) return;
    chips.slice(0, 12).forEach(c => {
      const glyph = STATE_GLYPH[c.state] || '•';
      const el = GhostUI.h('button', {
        className: 'chip chip-' + c.state,
        title: c.detail || c.title,
        onClick: () => GhostUI.toast(c.detail ? c.title + ' — ' + c.detail : c.title),
      }, glyph + ' ' + c.title);
      chipsEl.appendChild(el);
    });
  }).catch(() => { /* legacy timeline still renders below */ });

  const [sessions, jobs, memories, traces] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/sessions'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/memory/files'),
    GhostAPI.proxyGet('/v1/traces'),
  ]);

  const items = [];

  if (sessions.status === 'fulfilled') {
    const sessVal = sessions.value;
    const sessArr = Array.isArray(sessVal) ? sessVal : (sessVal.sessions || sessVal.items || []);
    for (const s of sessArr) {
      items.push({ kind: 'conversation', ts: s.last_activity, title: s.title, meta: GhostUI.fmtNum(s.message_count) + ' messages' });
    }
  }

  if (jobs.status === 'fulfilled') {
    const jobVal = jobs.value;
    const jobArr = Array.isArray(jobVal) ? jobVal : (jobVal.jobs || jobVal.items || []);
    for (const j of jobArr) {
      const lr = j.state && j.state.last_run_at;
      if (lr) items.push({ kind: 'automation', ts: Math.floor(new Date(lr).getTime() / 1000), job: j });
    }
  }

  if (memories.status === 'fulfilled') {
    const memVal = memories.value;
    const memArr = Array.isArray(memVal) ? memVal : (memVal.files || memVal.items || []);
    for (const m of memArr) {
      items.push({ kind: 'memory', ts: m.modified, file: m });
    }
  }

  if (traces.status === 'fulfilled') {
    const traceVal = traces.value;
    const incObj = traceVal.incidents || {};
    const incArr = Array.isArray(incObj) ? incObj : Object.values(incObj);
    for (const inc of incArr) {
      items.push({ kind: 'error', ts: inc.last_at || 0, inc });
    }
  }

  // Central semantic interpretation + grouping so repeated activity collapses
  // and nothing internal (paths, ids, raw event names) reaches the timeline.
  const semantic = GhostSemantic.groupItems(items.map(i => GhostSemantic.activityItem(i)));

  const FILTER_KIND = { all: null, messages: 'conversation', automations: 'automation', memory: 'memory', errors: 'error' };

  function paint() {
    const t = document.getElementById('act-timeline');
    t.innerHTML = '';
    const want = FILTER_KIND[activeFilter];
    const filtered = want ? semantic.filter(i => i.kind === want) : semantic;
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
