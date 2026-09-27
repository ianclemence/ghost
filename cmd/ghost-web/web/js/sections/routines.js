/* Ghost Section: Routines — everything Ghost runs for you, in one list.
 *
 * One destination for routines, reminders, and scheduled actions. The owner
 * never files their intent as a "routine" or an "automation"; the gateway
 * merges both backing models at /v1/routinefeed and this surface renders one feed.
 * Creation happens in conversation; this screen reviews and steers.
 *
 * (This file used to be called things.js. Nothing on this surface is a
 * "thing" — not to the owner, and not in the API: the feed answers with
 * "routines". The old file read res.things, so it always rendered the empty
 * state no matter how many reminders existed.)
 */
'use strict';

// The feed's kind set is closed (pkg/routinefeed): reminder, routine,
// automation, task. The default exists only so a newly added kind still
// reads as a routine rather than as the word "Thing".
function routineKindLabel(kind) {
  switch (kind) {
    case 'reminder': return 'Reminder';
    case 'routine': return 'Recurring';
    case 'automation': return 'Scheduled';
    case 'task': return 'Task';
    default: return 'Routine';
  }
}

function routineState(item) {
  switch (item.state) {
    case 'waiting': return { label: 'Waiting for you', cls: 'text-warning' };
    case 'failed': return { label: 'Needs attention', cls: 'text-danger' };
    case 'paused': return { label: 'Paused', cls: 'text-tertiary' };
    case 'done': return { label: 'Done', cls: 'text-ok' };
    case 'cancelled': return { label: 'Cancelled', cls: 'text-tertiary' };
    default: return { label: 'Active', cls: 'text-ok' };
  }
}

async function loadRoutines(container) {
  if (GhostApp.currentSection() !== 'routines') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Routines'));
  head.appendChild(GhostUI.h('p', {},
    'Everything Ghost runs for you \u2014 recurring briefs, reminders, and scheduled actions. Pause, resume, or stop any of them here.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'routines-list' });
  listEl.appendChild(GhostUI.loading('Loading what Ghost is doing\u2026'));
  container.appendChild(listEl);

  let res;
  try {
    res = await GhostAPI.proxyGet('/v1/routinefeed');
  } catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load what Ghost is doing', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  if (!document.body.contains(container)) return;
  renderRoutines(listEl, container, Array.isArray(res && res.routines) ? res.routines : []);
}

function renderRoutines(listEl, container, items) {
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState(
      'Nothing yet',
      'Say \u201cevery Monday at 9, prepare my weekly brief\u201d in a chat and it appears here.'));
    return;
  }

  items.forEach(item => {
    const card = GhostUI.h('div', { className: 'ghost-card' });

    const titleRow = GhostUI.h('div', { className: 'ghost-card-title' });
    titleRow.appendChild(document.createTextNode(item.title || 'Untitled'));
    titleRow.appendChild(GhostUI.h('span', {
      className: routineState(item).cls,
      style: 'margin-left:var(--s-2);font-size:var(--t-foot);font-weight:600',
    }, routineState(item).label));
    card.appendChild(titleRow);

    const schedule = item.schedule && item.schedule !== 'Manual' ? item.schedule : 'No schedule';
    const metaParts = [routineKindLabel(item.kind), schedule];
    if (item.run_count > 0) metaParts.push('ran ' + item.run_count + '\u00d7');
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, metaParts.join('  \u00b7  ')));

    if (item.what) {
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, item.what));
    }
    if (item.last_error) {
      card.appendChild(GhostUI.h('div', { className: 'type-foot text-danger', style: 'margin-top:var(--s-1)' }, item.last_error));
    }

    const row = GhostUI.h('div', { className: 'btn-row' });
    if (item.state === 'active') {
      row.appendChild(GhostUI.btn('Pause', 'secondary', () => routineAction(container, item, 'pause')));
    } else if (item.state === 'paused' || item.state === 'failed') {
      row.appendChild(GhostUI.btn('Resume', 'secondary', () => routineAction(container, item, 'resume')));
    }
    if (item.state === 'active' || item.state === 'paused' || item.state === 'waiting') {
      row.appendChild(GhostUI.btn('Stop', 'danger', async () => {
        const ok = await GhostUI.confirmModal(
          'Stop this?',
          'Ghost will stop \u201c' + (item.title || 'this') + '\u201d.',
          'Stop');
        if (ok) routineAction(container, item, 'cancel');
      }));
    }
    if (row.childNodes.length > 0) card.appendChild(row);

    listEl.appendChild(card);
  });
}

// routineAction routes to the correct backend action based on provenance so the
// routine metadata sidecar stays consistent for routine-sourced entries.
async function routineAction(container, item, op) {
  const isRoutine = item.source === 'routine';
  const base = isRoutine ? '/v1/routines/' : '/v1/scheduled/';
  try {
    await GhostAPI.proxyPost(base + encodeURIComponent(item.id) + '/' + op, {});
  } catch (e) {
    GhostUI.toast('Couldn\u2019t do that \u2014 try again.', 'err');
    return;
  }
  loadRoutines(container);
}

GhostApp.registerSection('routines', loadRoutines);
