/* Ghost Section: Home — "What needs me? What's coming? What did you do? Are you okay?"
   Not a server dashboard: a calm briefing, with Ghost's decisions
   (approvals) answerable right here. */

'use strict';

async function loadHome(container) {
  container.innerHTML = '';
  const seq = GhostApp.getRenderSeq();
  const view = GhostUI.h('div', { className: 'home' });

  // Header: greeting + one honest status line.
  const header = GhostUI.h('header', { className: 'page-head home-head' });
  const headText = GhostUI.h('div', { className: 'home-head-text' });
  const greet = GhostUI.h('h1', { id: 'home-greet' }, greetingFor(new Date().getHours()) + '.');
  headText.appendChild(greet);
  const statusLine = GhostUI.h('p', { className: 'home-statusline', role: 'status', 'aria-live': 'polite' });
  const dot = GhostUI.h('span', { className: 'home-status-dot', 'aria-hidden': 'true' });
  const statusText = GhostUI.h('span', {}, 'Reading Ghost…');
  statusLine.appendChild(dot);
  statusLine.appendChild(statusText);
  headText.appendChild(statusLine);
  header.appendChild(headText);
  // Ghost's presence, made visible: it breathes slowly when all is well and
  // quickens when Ghost is waiting on you.
  const orb = GhostUI.h('div', { className: 'presence-orb', 'data-state': 'idle', 'aria-hidden': 'true' });
  header.appendChild(orb);
  view.appendChild(header);

  const needs = homeCard('Needs you', null);
  const upcoming = homeCard('Coming up', { label: 'All routines', to: 'routines' });
  const glance = homeCard('At a glance', null);
  const recent = homeCard('What Ghost did', { label: 'All activity', to: 'activity' });
  needs.card.classList.add('home-card-wide');
  recent.card.classList.add('home-card-wide');
  [needs, upcoming, glance, recent].forEach(c => c.body.appendChild(GhostUI.loading('…')));

  const grid = GhostUI.h('div', { className: 'home-grid' });
  [needs, upcoming, glance, recent].forEach(c => grid.appendChild(c.card));
  view.appendChild(grid);
  container.appendChild(view);

  // Independent fetches: one failure never blanks the page.
  const [meta, doctor, health, channels, activity, jobs, memory, selfMem, devices, ollama, activeModel, identity, consoleStatus, proactive, pending] = await Promise.allSettled([
    GhostAPI.get('/api/admin/auth/meta'),
    GhostAPI.proxyGet('/v1/doctor'),
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/channels/status'),
    GhostAPI.proxyGet('/v1/activity?limit=20'),
    GhostAPI.proxyGet('/v1/routinefeed'),
    GhostAPI.proxyGet('/v1/memory/files'),
    GhostAPI.proxyGet('/v1/memory/self'),
    GhostAPI.proxyGet('/v1/pairing/devices'),
    GhostAPI.get('/api/ollama/models'),
    GhostAPI.proxyGet('/v1/model'),
    GhostAPI.proxyGet('/v1/identity'),
    GhostAPI.get('/api/status'),
    GhostAPI.proxyGet('/v1/proactive'),
    GhostAPI.proxyGet('/v1/permissions/requests?status=pending'),
  ]);
  if (!document.body.contains(container)) return;
  if (GhostApp.getRenderSeq() !== seq) return;

  const ownerName = (meta.status === 'fulfilled' && meta.value && meta.value.owner_name || '').trim();
  if (ownerName) greet.textContent = greetingFor(new Date().getHours()) + ', ' + ownerName + '.';

  const overall = computeOverall(doctor, health);
  dot.className = 'home-status-dot home-status-dot-' + overall.state;
  orb.dataset.state = overall.state === 'ok' ? 'idle' : (overall.state === 'offline' ? 'offline' : 'attention');
  const active = activeModel.status === 'fulfilled' && activeModel.value && activeModel.value.active;
  const ghostInfo = (identity.status === 'fulfilled' && identity.value && identity.value.ghost) || {};
  const who = ghostInfo.name && ghostInfo.name !== 'Ghost' ? ghostInfo.name : 'Ghost';
  statusText.textContent = overall.state === 'ok'
    ? who + ' is healthy' + (active ? ', thinking with ' + (GhostUI.modelFriendly(active).model || active) + '.' : '.')
    : overall.label + '. ' + overall.detail;

  const approvals = pending.status === 'fulfilled' ? ((pending.value && pending.value.requests) || []) : [];
  // Notices can be set aside without a refetch, so Needs you and the status
  // line repaint from what is already loaded.
  const paintNeeds = () => {
    const shown = renderNeeds(needs, approvals, doctor, channels, devices, consoleStatus, () => loadHome(container), paintNeeds);
    if (overall.state === 'warn' && shown.warnVisible === 0 && shown.setAside > 0) {
      const n = shown.setAside;
      dot.className = 'home-status-dot home-status-dot-ok';
      orb.dataset.state = 'idle';
      statusText.textContent = who + ' is up. ' + n + (n === 1 ? ' notice' : ' notices') + ' dismissed.';
    } else {
      dot.className = 'home-status-dot home-status-dot-' + overall.state;
      orb.dataset.state = overall.state === 'ok' ? 'idle' : (overall.state === 'offline' ? 'offline' : 'attention');
      statusText.textContent = overall.state === 'ok'
        ? who + ' is healthy' + (active ? ', thinking with ' + (GhostUI.modelFriendly(active).model || active) + '.' : '.')
        : overall.label + '. ' + overall.detail;
    }
  };
  paintNeeds();
  renderUpcoming(upcoming.body, jobs);
  renderGlance(glance.body, memory, jobs, devices, ollama, activeModel, proactive);
  renderActivity(recent.body, activity);
  // Live approvals: repaint Needs-you from each presence poll (it already
  // fetched the pending list) so cards answered elsewhere or expired
  // disappear without a manual reload.
  GhostApp.setSectionRefresh((live) => {
    if (GhostApp.getRenderSeq() !== seq) return;
    if (!document.body.contains(container)) return;
    if (!Array.isArray(live)) return;
    approvals.length = 0;
    live.forEach(a => approvals.push(a));
    paintNeeds();
  });
}

// homeCard builds a titled card; `link` adds a quiet "see all" affordance.
function homeCard(title, link) {
  const card = GhostUI.h('section', { className: 'panel home-card' });
  const head = GhostUI.h('div', { className: 'panel-head' });
  head.appendChild(GhostUI.h('h2', {}, title));
  if (link) {
    head.appendChild(GhostUI.h('button', { className: 'home-link', onClick: () => GhostApp.navigate(link.to) }, link.label + ' →'));
  }
  card.appendChild(head);
  const body = GhostUI.h('div', { className: 'home-card-body' });
  card.appendChild(body);
  return { card, head, body };
}

// What can be set aside, and how it is remembered. A decision Ghost is waiting
// on (an approval) is never set aside: you answer it. Notices (setup gaps,
// warnings) can be, one by one or together; a failure cannot, because you can
// silence a nag but not an outage. What is set aside is remembered in this
// browser by what it said, so if it changes (a warning gets worse, a new one
// appears) it comes back.
const SET_ASIDE_KEY = 'ghost:home:set-aside';

function noticeKey(it) { return (it.state || 'warn') + '|' + it.title + '|' + it.detail; }
function canSetAside(it) { return it.state !== 'bad'; }

function readSetAside() {
  try {
    const raw = JSON.parse(localStorage.getItem(SET_ASIDE_KEY) || '[]');
    return new Set(Array.isArray(raw) ? raw : []);
  } catch (e) { return new Set(); }
}

function writeSetAside(set) {
  try { localStorage.setItem(SET_ASIDE_KEY, JSON.stringify(Array.from(set).slice(-50))); } catch (e) {}
}

// Ghost's pending decisions, answerable in place; then the notices that need
// the owner (setup gaps, health warnings). Returns what it showed, so the page
// can word the status line honestly.
function renderNeeds(needs, approvals, doctorRes, channelsRes, devicesRes, consoleRes, reload, repaint) {
  const body = needs.body;
  body.innerHTML = '';
  const old = needs.head.querySelector('.home-link-dismiss');
  if (old) old.remove();

  const all = collectAttentionItems(doctorRes, channelsRes, devicesRes, consoleRes);
  const set = readSetAside();
  const asideItems = all.filter(it => canSetAside(it) && set.has(noticeKey(it)));
  const items = all.filter(it => !(canSetAside(it) && set.has(noticeKey(it)))).slice(0, 5);
  const shown = { warnVisible: items.filter(it => it.state === 'warn').length, setAside: asideItems.length };

  const setAside = (list, label) => {
    const next = readSetAside();
    list.forEach(it => next.add(noticeKey(it)));
    writeSetAside(next);
    repaint();
    GhostUI.toast(label, null, 6000, {
      label: 'Undo',
      onClick: () => {
        const back = readSetAside();
        list.forEach(it => back.delete(noticeKey(it)));
        writeSetAside(back);
        repaint();
      },
    });
  };
  const showAgain = () => {
    const next = readSetAside();
    asideItems.forEach(it => next.delete(noticeKey(it)));
    writeSetAside(next);
    repaint();
  };

  approvals.forEach(p => body.appendChild(approvalRow(p, reload)));

  if (items.length > 0) {
    const list = GhostUI.h('ul', { className: 'home-attention-list', role: 'list' });
    items.forEach(it => {
      const li = renderAttentionItem(it);
      if (canSetAside(it)) {
        const x = GhostUI.h('button', {
          className: 'home-dismiss',
          type: 'button',
          'aria-label': 'Dismiss: ' + it.title,
          title: 'Dismiss',
          onClick: () => {
            // Let it leave before the list repaints, so the row below does not
            // simply jump into its place.
            li.classList.add('is-leaving');
            setTimeout(() => setAside([it], 'Dismissed \u201c' + it.title + '\u201d'), 170);
          },
        });
        x.innerHTML = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>';
        li.querySelector('.home-attention-head').appendChild(x);
      }
      list.appendChild(li);
    });
    body.appendChild(list);
  }

  const dismissible = items.filter(canSetAside);
  if (dismissible.length >= 2) {
    const all = GhostUI.h('button', {
      className: 'home-link home-link-dismiss',
      type: 'button',
      onClick: () => setAside(dismissible, 'Dismissed ' + dismissible.length + ' notices'),
    }, 'Dismiss notices');
    needs.head.appendChild(all);
  }

  if (approvals.length === 0 && items.length === 0) {
    const ok = GhostUI.h('div', { className: 'home-clear' });
    ok.appendChild(GhostUI.h('span', { className: 'home-attention-dot home-attention-dot-ok', 'aria-hidden': 'true' }));
    ok.appendChild(GhostUI.h('span', {}, asideItems.length ? 'Nothing is waiting on you.' : 'All clear. Nothing is waiting on you.'));
    body.appendChild(ok);
  }
  if (asideItems.length > 0) {
    const n = asideItems.length;
    const quiet = GhostUI.h('div', { className: 'home-set-aside' });
    quiet.appendChild(GhostUI.h('span', {}, n + (n === 1 ? ' notice' : ' notices') + ' dismissed'));
    quiet.appendChild(GhostUI.h('button', { className: 'home-set-aside-show', type: 'button', onClick: showAgain }, 'Show'));
    body.appendChild(quiet);
  }
  return shown;
}

function approvalRow(p, reload) {
  const card = p.card || {};
  const row = GhostUI.h('div', { className: 'home-approval' });
  row.appendChild(GhostUI.h('span', { className: 'home-ember', 'aria-hidden': 'true' }));
  const text = GhostUI.h('div', { className: 'home-approval-text' });
  text.appendChild(GhostUI.h('div', { className: 'home-approval-title' }, card.title || 'Ghost is asking'));
  if (card.description) text.appendChild(GhostUI.h('div', { className: 'home-approval-desc' }, card.description));
  row.appendChild(text);
  const acts = GhostUI.h('div', { className: 'home-approval-actions' });
  const decide = async (grant) => {
    acts.querySelectorAll('button').forEach(b => { b.disabled = true; });
    try {
      // No scope is sent: the server stores the canonical scope the request
      // was asked under, so the grant matches exactly what it authorizes.
      const res = await GhostAPI.proxyPost('/v1/permissions/resolve', { id: p.id, grant });
      if (res && res.resumed === false) {
        GhostUI.toast('Recorded, but it couldn\u2019t run: ' + (res.resume_reason || 'nothing left to resume.'));
      }
    } catch (e) {
      // A failed resolve usually means the card is already dead (expired,
      // answered on another surface): say why, then reload anyway so the
      // dead card leaves instead of sitting there looking broken.
      GhostUI.toast((e && e.message) || 'Couldn’t record that choice. Try again.');
    }
    GhostApp.refreshPresence();
    reload();
  };
  // All four grants, like the Approvals page: browser work is multi-step,
  // and "Allow once" authorizing a single click is why approvals felt like
  // they did nothing.
  [['Allow', 'allow_once', 'primary'], ['This task', 'allow_task', 'secondary'], ['Always', 'allow_always', 'secondary'], ['Deny', 'deny', 'secondary']].forEach(([label, grant, kind]) => {
    const b = GhostUI.btn(label, kind, () => decide(grant));
    b.classList.add('ghost-btn-sm');
    acts.appendChild(b);
  });
  row.appendChild(acts);
  return row;
}

// The next things Ghost will do, soonest first.
function renderUpcoming(body, jobsRes) {
  body.innerHTML = '';
  const arr = jobsRes.status === 'fulfilled' && jobsRes.value && Array.isArray(jobsRes.value.routines) ? jobsRes.value.routines : [];
  const live = arr.filter(r => r.state === 'active' || r.state === 'waiting')
    .sort((a, b) => (Date.parse(a.next_run_at) || Infinity) - (Date.parse(b.next_run_at) || Infinity))
    .slice(0, 4);
  if (live.length === 0) {
    body.appendChild(GhostUI.h('p', { className: 'home-empty' }, 'Nothing scheduled. Tell Ghost “every Monday at 8, brief me on my week” and it appears here.'));
    return;
  }
  const list = GhostUI.h('ul', { className: 'home-list', role: 'list' });
  live.forEach(r => {
    const li = GhostUI.h('li', { className: 'home-list-row' });
    const main = GhostUI.h('div', { className: 'home-list-main' });
    main.appendChild(GhostUI.h('div', { className: 'home-list-title' }, r.title));
    if (r.schedule) main.appendChild(GhostUI.h('div', { className: 'home-list-sub' }, r.schedule));
    li.appendChild(main);
    const when = whenAhead(r.next_run_at);
    if (when) li.appendChild(GhostUI.h('div', { className: 'home-list-when' }, when));
    list.appendChild(li);
  });
  body.appendChild(list);
}

// "in 20 min", "today 3:00 PM", "tomorrow 8:00 AM", "Monday 8:00 AM".
function whenAhead(iso) {
  const t = Date.parse(iso);
  if (!isFinite(t)) return '';
  const now = Date.now(), d = t - now;
  if (d <= 60000) return 'now';
  if (d < 3600000) return 'in ' + Math.round(d / 60000) + ' min';
  const at = new Date(t);
  const clock = at.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const day0 = x => { const c = new Date(x); c.setHours(0, 0, 0, 0); return c.getTime(); };
  const days = Math.round((day0(t) - day0(now)) / 86400000);
  if (days === 0) return d < 6 * 3600000 ? 'in ' + Math.round(d / 3600000) + ' h' : 'today ' + clock;
  if (days === 1) return 'tomorrow ' + clock;
  if (days < 7) return at.toLocaleDateString([], { weekday: 'long' }) + ' ' + clock;
  return at.toLocaleDateString([], { day: 'numeric', month: 'short' });
}

// Small, honest facts: what Ghost knows, runs, thinks with, watches.
function renderGlance(body, memoryRes, jobsRes, devicesRes, ollamaRes, activeModelRes, proactiveRes) {
  body.innerHTML = '';
  const mem = memoryRes.status === 'fulfilled' ? extractMemoryCount(memoryRes.value) : null;
  const jobs = jobsRes.status === 'fulfilled' ? extractJobCount(jobsRes.value) : null;
  const running = jobsRes.status === 'fulfilled' ? extractActiveJobCount(jobsRes.value) : null;
  const devs = devicesRes.status === 'fulfilled' ? extractDeviceCount(devicesRes.value) : null;
  const local = inferLocalAI(ollamaRes, activeModelRes);
  const tiles = [
    ['Memory', mem == null ? '—' : GhostUI.fmtNum(mem), mem === 1 ? 'thing remembered' : 'things remembered', 'memory'],
    ['Routines', jobs == null ? '—' : String(running), jobs ? 'running of ' + jobs : 'none yet', 'routines'],
    ['Devices', devs == null ? '—' : String(devs), devs === 0 ? 'not connected' : 'connected', 'devices'],
    ['Local AI', local.state === 'ok' ? 'Ready' : local.label, local.state === 'ok' ? local.label.replace('Ready · ', '') : '', 'intelligence'],
  ];
  const grid = GhostUI.h('div', { className: 'home-tiles' });
  tiles.forEach(([k, v, sub, to]) => {
    const t = GhostUI.h('button', { className: 'home-tile', onClick: () => GhostApp.navigate(to) });
    t.appendChild(GhostUI.h('div', { className: 'home-tile-key' }, k));
    t.appendChild(GhostUI.h('div', { className: 'home-tile-val' }, v));
    if (sub) t.appendChild(GhostUI.h('div', { className: 'home-tile-sub' }, sub));
    grid.appendChild(t);
  });
  body.appendChild(grid);
  const watching = proactiveSummary(proactiveRes);
  if (watching && watching !== '—') body.appendChild(GhostUI.h('p', { className: 'home-watching' }, 'Watching: ' + watching));
}

function greetingFor(hour) {
  if (hour < 12) return 'Good morning';
  if (hour < 18) return 'Good afternoon';
  return 'Good evening';
}

// recoveryModal explains, in plain language, how to bring Ghost back if it ever
// stops working — using the console, not a terminal. Recovery never touches your
// memories, skills, or settings.
function recoveryModal() {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('p', {}, 'If your Ghost ever seems stuck, try these in order. Recovery never touches your memories, skills, or settings \u2014 those stay on the device.'));
  const list = GhostUI.h('ol', { style: 'margin:var(--s-3) 0;padding-left:var(--s-5)' });
  const steps = [
    'Restart Ghost  \u2014  open the Security section and choose “Restart Ghost”. This fixes most hiccups and takes a few moments.',
    'Still stuck?  Restart this device  \u2014  the hardware Ghost runs on. A minute or two of downtime is normal.',
    'If everything else fails, your backup is the safety net  \u2014  download it from the Security section, and you can bring your Ghost back from it.',
  ];
  steps.forEach(s => list.appendChild(GhostUI.h('li', { style: 'margin-bottom:var(--s-2)' }, s)));
  body.appendChild(list);
  GhostUI.modal('If Ghost seems stuck', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Close'),
  ]);
}

function activitySkeleton() {
  const wrap = GhostUI.h('ul', { className: 'home-activity-list', role: 'list' });
  for (let i = 0; i < 4; i++) {
    const row = GhostUI.h('li', { className: 'home-activity-row home-activity-row-skel' });
    row.appendChild(GhostUI.h('div', { className: 'home-activity-when', 'aria-hidden': 'true' }, '\u00a0'));
    const body = GhostUI.h('div', { className: 'home-activity-body-col' });
    body.appendChild(GhostUI.h('div', { className: 'home-activity-title-skel', 'aria-hidden': 'true' }, '\u00a0'));
    body.appendChild(GhostUI.h('div', { className: 'home-activity-meta-skel', 'aria-hidden': 'true' }, '\u00a0'));
    row.appendChild(body);
    wrap.appendChild(row);
  }
  return wrap;
}

function computeOverall(doctorRes, healthRes) {
  if (healthRes.status !== 'fulfilled') {
    return { state: 'offline', label: 'Ghost is unavailable', detail: 'The Ghost service isn\u2019t responding.' };
  }
  const d = doctorRes.status === 'fulfilled' ? doctorRes.value : null;
  if (d && d.status === 'error') {
    return { state: 'bad', label: 'Ghost needs your attention', detail: 'Some Ghost capabilities aren\u2019t currently available.' };
  }
  if (d && d.status === 'warning') {
    return { state: 'warn', label: 'Ghost is up', detail: 'A few things could use a look.' };
  }
  return { state: 'ok', label: 'Ghost is healthy', detail: 'Ghost is running normally.' };
}

// proactiveSummary renders the quiet-work state as one honest phrase: whether
// Ghost is in quiet hours, how much of today's check-in budget is used, and
// whether anything is waiting. Never a number for its own sake.
function proactiveSummary(res) {
  if (!res || res.status !== 'fulfilled' || !res.value || !res.value.proactive) return '\u2014';
  const p = res.value.proactive;
  if (p.enabled === false) return 'Not watching for things';
  const open = Number(p.open_proposals || 0);
  if (open > 0) {
    return open + (open === 1 ? ' thing to review' : ' things to review');
  }
  if (p.waiting > 0) {
    return p.quiet
      ? p.waiting + ' waiting until ' + (p.quiet_end || 'morning')
      : p.waiting + ' ready';
  }
  if (p.quiet) return 'Quiet until ' + (p.quiet_end || 'morning');
  if (p.budget_max > 0) return p.budget_used + ' of ' + p.budget_max + ' check-ins used today';
  return 'Idle';
}

function inferLocalAI(ollamaRes, activeModelRes) {
  // /api/ollama/models is the authoritative source for installed local models.
  // The status is product language ("local AI is ready"), never a model name:
  // Ghost's runtime chooses the intelligence; the owner doesn't pick models.
  if (ollamaRes.status !== 'fulfilled') return { label: 'Unavailable', state: 'bad' };
  const v = ollamaRes.value;
  if (!v || v.ok === false) return { label: 'Unavailable', state: 'bad' };
  const models = Array.isArray(v.models) ? v.models : [];
  if (models.length === 0) return { label: 'Not configured', state: 'warn' };
  const _active = (activeModelRes.status === 'fulfilled' && activeModelRes.value && activeModelRes.value.active) || '';
  return { label: 'Ready \u00b7 ' + models.length + ' installed', state: 'ok' };
}

function extractMemoryCount(v) {
  // Count the canonical, current memory collection Ghost relies on, not raw
  // historical rows. Duplicate extractions of the same fact count once.
  if (v && Array.isArray(v.entries)) {
    return GhostSemantic.canonicalizeEntries(v.entries).length;
  }
  if (Array.isArray(v)) return v.length;
  if (v && typeof v === 'object') {
    const files = v.files || v.items || [];
    return Array.isArray(files) ? files.length : 0;
  }
  return 0;
}

function extractJobCount(v) {
  // /v1/routinefeed returns one normalized feed covering routines and scheduled
  // items. Fall back to legacy job/item shapes so a stale gateway still
  // renders something honest rather than an em dash.
  if (v && Array.isArray(v.routines)) return v.routines.length;
  const arr = Array.isArray(v) ? v : (v && (v.jobs || v.items)) || [];
  return Array.isArray(arr) ? arr.length : 0;
}

function extractActiveJobCount(v) {
  const arr = v && Array.isArray(v.routines)
    ? v.routines
    : (Array.isArray(v) ? v : (v && (v.jobs || v.items)) || []);
  if (!Array.isArray(arr)) return 0;
  // The unified feed uses normalized states. Legacy scheduled shapes used
  // scheduled/running. Treat both active and waiting as "running" from the
  // owner's point of view — a thing waiting on them is still live.
  return arr.filter(j =>
    j.state === 'active' || j.state === 'waiting' ||
    j.state === 'scheduled' || j.state === 'running' || j.enabled === true,
  ).length;
}

function extractDeviceCount(v) {
  const arr = Array.isArray(v) ? v : (v && (v.devices || v.items)) || [];
  return Array.isArray(arr) ? arr.length : 0;
}

function renderActivity(container, activityRes) {
  if (!document.body.contains(container)) return;
  container.innerHTML = '';
  container.setAttribute('aria-busy', 'false');

  const items = collectActivityItems(activityRes);

  if (items.length === 0) {
    container.appendChild(GhostUI.emptyState(
      'No recent activity',
      'Ghost hasn\u2019t done anything noteworthy yet. As you use Ghost, this is where it will appear.'
    ));
    return;
  }

  const list = GhostUI.h('ul', { className: 'home-activity-list', role: 'list' });
  const top = items.slice(0, 7);
  let day = '';
  for (const it of top) {
    const label = homeDayLabel(it.ts);
    if (label !== day) {
      day = label;
      list.appendChild(GhostUI.h('li', { className: 'home-activity-day', role: 'presentation' }, label));
    }
    list.appendChild(renderActivityRow(it));
  }
  container.appendChild(list);
}

// collectActivityItems reads the canonical, user-safe activity projection
// (/v1/activity). It is deliberately NOT built from /v1/sessions: Ghost is
// one relationship, and this feed is "what Ghost has done", never a chat
// list.
function collectActivityItems(activityRes) {
  if (activityRes.status !== 'fulfilled') return [];
  const arr = (activityRes.value && activityRes.value.activity) || [];
  const items = [];
  for (const a of arr) {
    const ts = unixOf(a.timestamp);
    if (!ts) continue;
    const word = activityState(a.state);
    const summary = (a.summary || '').trim();
    // Never say the same thing twice ("No change · No change").
    const parts = [summary];
    if (word && word.toLowerCase() !== summary.toLowerCase()) parts.unshift(word);
    const title = a.title || 'Activity';
    const meta = parts.filter(Boolean).join('  \u00b7  ');
    // The same thing repeating is one row with a count.
    const prev = items[items.length - 1];
    if (prev && prev.title === title && prev.meta === meta) { prev.count = (prev.count || 1) + 1; continue; }
    items.push({ kind: 'activity', ts, title, meta, why: (a.why || '').trim() });
  }
  // /v1/activity returns newest first and is already user-safe; no further
  // semantic grouping is needed here.
  return items;
}

function unixOf(ts) {
  if (!ts) return 0;
  const d = new Date(ts);
  return isNaN(d.getTime()) ? 0 : Math.floor(d.getTime() / 1000);
}

function activityState(st) {
  return GhostUI.activityWord(st);
}

function renderActivityRow(it) {
  const row = GhostUI.h('li', { className: 'home-activity-row' });
  row.appendChild(GhostUI.h('div', { className: 'home-activity-when' }, formatTime(new Date(it.ts * 1000))));
  const body = GhostUI.h('div', { className: 'home-activity-body-col' });
  body.appendChild(GhostUI.h('div', { className: 'home-activity-title' }, it.count > 1 ? it.title + '  \u00d7' + it.count : it.title));
  if (it.meta) body.appendChild(GhostUI.h('div', { className: 'home-activity-meta' }, it.meta));
  if (it.why) body.appendChild(GhostUI.h('div', { className: 'home-activity-why' }, it.why));
  row.appendChild(body);
  return row;
}

// Day label for grouping ("Today", "Yesterday", "Mon 28 Sep").
function homeDayLabel(unixSec) {
  const d = new Date(unixSec * 1000);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return 'Today';
  const y = new Date(now); y.setDate(y.getDate() - 1);
  if (d.toDateString() === y.toDateString()) return 'Yesterday';
  return d.toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' });
}

function formatTime(d) {
  return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

function renderAttention(container, doctorRes, channelsRes, devicesRes, ollamaRes, consoleRes) {
  if (!document.body.contains(container)) return;
  container.innerHTML = '';
  container.setAttribute('aria-busy', 'false');

  const items = collectAttentionItems(doctorRes, channelsRes, devicesRes, consoleRes);

  if (items.length === 0) {
    const empty = GhostUI.h('div', { className: 'home-attention-empty' });
    empty.appendChild(GhostUI.h('span', { className: 'home-attention-dot home-attention-dot-ok', 'aria-hidden': 'true' }));
    empty.appendChild(GhostUI.h('span', {}, 'Nothing right now.'));
    container.appendChild(empty);
    return;
  }

  const list = GhostUI.h('ul', { className: 'home-attention-list', role: 'list' });
  for (const it of items) {
    list.appendChild(renderAttentionItem(it));
  }
  container.appendChild(list);
}

function collectAttentionItems(doctorRes, channelsRes, devicesRes, consoleRes) {
  const items = [];

  // Console moved ports: same per-port dismissal as the System banner, so a
  // new move re-arms it on both surfaces.
  if (consoleRes && consoleRes.status === 'fulfilled') {
    const cs = consoleRes.value || {};
    if (cs.console_port && cs.console_port_requested && cs.console_port !== cs.console_port_requested) {
      let dismissed = false;
      try { dismissed = !!sessionStorage.getItem('ghost:port-banner:' + cs.console_port); } catch (e) {}
      if (!dismissed) {
        const who = cs.console_port_occupant ? ' (' + cs.console_port_occupant + ' is using port ' + cs.console_port_requested + ')' : '';
        items.push({
          title: 'Console moved to port ' + cs.console_port,
          detail: 'Its usual port (' + cs.console_port_requested + ') was taken' + who + '. Bookmarks to this address keep working.',
          cta: { label: 'View system', section: 'system' },
          state: 'warn',
        });
      }
    }
  }

  // Doctor checks with errors or warnings become attention items.
  if (doctorRes.status === 'fulfilled') {
    const checks = doctorRes.value.checks || [];
    for (const c of checks) {
      if (c.status === 'error') {
        items.push({
          title: c.label || prettyName(c.name),
          detail: c.message || 'This check failed.',
          cta: ctaForCheck(c.name),
          state: 'bad',
        });
      } else if (c.status === 'warning') {
        items.push({
          title: c.label || prettyName(c.name),
          detail: c.message || 'Worth a look.',
          cta: ctaForCheck(c.name),
          state: 'warn',
        });
      }
    }
  }

  // Channels with repeated delivery failures.
  if (channelsRes.status === 'fulfilled') {
    const chs = channelsRes.value.channels || {};
    for (const name of Object.keys(chs)) {
      const raw = chs[name];
      const map = raw && typeof raw === 'object' ? raw : {};
      const failures = map.failure_count || 0;
      const lastErr = map.last_send_error || '';
      if (failures >= 3 && lastErr) {
        items.push({
          title: channelTitle(name) + ' connection lost',
          detail: failures >= 5
            ? 'Ghost hasn\u2019t been able to deliver messages through ' + channelTitle(name) + '.'
            : 'Repeated send failures on ' + channelTitle(name) + '.',
          cta: { label: 'Check channel', section: 'channels' },
          state: failures >= 5 ? 'bad' : 'warn',
        });
      }
    }
  }

  // Setup gap: no mobile device paired. Surfaced as info, not a failure.
  if (devicesRes.status === 'fulfilled') {
    const devs = Array.isArray(devicesRes.value) ? devicesRes.value : (devicesRes.value.devices || []);
    if (devs.length === 0) {
      items.push({
        title: 'Connect your phone',
        detail: 'Ghost Mobile isn\u2019t connected yet. Ghost stays on this hardware \u2014 your phone is how you take it with you.',
        cta: { label: 'Connect device', section: 'devices' },
        state: 'info',
      });
    }
  }

  return items;
}

function prettyName(s) {
  if (!s) return 'Issue';
  return s.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
}

function channelTitle(name) {
  const map = { telegram: 'Telegram', discord: 'Discord', slack: 'Slack', whatsapp: 'WhatsApp', email: 'Email', ghost_mobile: 'Ghost Mobile', mobile: 'Ghost Mobile' };
  return map[name] || name.charAt(0).toUpperCase() + name.slice(1);
}

function ctaForCheck(name) {
  const n = (name || '').toLowerCase();
  if (n.includes('ollama') || n.includes('model')) return { label: 'Check AI', section: 'intelligence' };
  if (n.includes('connect')) return { label: 'Open Apps', section: 'apps' };
  if (n.includes('memory')) return { label: 'View memory', section: 'memory' };
  if (n.includes('channel')) return { label: 'Check channels', section: 'channels' };
  if (n.includes('disk') || n.includes('storage') || n.includes('service')) return { label: 'View system', section: 'system' };
  if (n.includes('auth') || n.includes('session')) return { label: 'View security', section: 'security' };
  return { label: 'View system', section: 'system' };
}

function renderAttentionItem(it) {
  const li = GhostUI.h('li', { className: 'home-attention-item home-attention-' + (it.state || 'warn') });
  const head = GhostUI.h('div', { className: 'home-attention-head' });
  head.appendChild(GhostUI.h('span', { className: 'home-attention-dot', 'aria-hidden': 'true' }));
  head.appendChild(GhostUI.h('div', { className: 'home-attention-title' }, it.title));
  li.appendChild(head);
  li.appendChild(GhostUI.h('div', { className: 'home-attention-detail' }, it.detail));
  if (it.cta && it.cta.section) {
    const link = GhostUI.h('button', {
      className: 'home-attention-cta',
      type: 'button',
      onClick: () => GhostApp.navigate(it.cta.section),
    }, it.cta.label + '  \u2192');
    li.appendChild(link);
  }
  return li;
}

GhostApp.registerSection('home', loadHome);
