/* Ghost Section: Routines — everything Ghost runs for you, in one list.
 *
 * One destination for routines, reminders, and scheduled actions. The owner
 * never files their intent as a "routine" or an "automation"; the gateway
 * merges both backing models at /v1/routinefeed and this surface renders one feed.
 * Creation happens in conversation; this screen reviews and steers.
 */
'use strict';

function thingKindLabel(kind) {
  switch (kind) {
    case 'reminder': return 'Reminder';
    case 'routine': return 'Recurring';
    case 'automation': return 'Scheduled';
    case 'task': return 'Task';
    default: return 'Thing';
  }
}

function thingState(thing) {
  switch (thing.state) {
    case 'waiting': return { label: 'Waiting for you', cls: 'text-warning' };
    case 'failed': return { label: 'Needs attention', cls: 'text-danger' };
    case 'paused': return { label: 'Paused', cls: 'text-tertiary' };
    case 'done': return { label: 'Done', cls: 'text-ok' };
    case 'cancelled': return { label: 'Cancelled', cls: 'text-tertiary' };
    default: return { label: 'Active', cls: 'text-ok' };
  }
}

async function loadThings(container) {
  if (GhostApp.currentSection() !== 'routines') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Routines'));
  head.appendChild(GhostUI.h('p', {},
    'Everything Ghost runs for you \u2014 recurring briefs, reminders, and scheduled actions. Pause, resume, or stop any of them here.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'things-list' });
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
  renderThings(listEl, container, Array.isArray(res && res.things) ? res.things : []);
}

function renderThings(listEl, container, items) {
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState(
      'Nothing yet',
      'Say \u201cevery Monday at 9, prepare my weekly brief\u201d in a chat and it appears here.'));
    return;
  }

  items.forEach(thing => {
    const card = GhostUI.h('div', { className: 'ghost-card' });

    const titleRow = GhostUI.h('div', { className: 'ghost-card-title' });
    titleRow.appendChild(document.createTextNode(thing.title || 'Untitled'));
    titleRow.appendChild(GhostUI.h('span', {
      className: thingState(thing).cls,
      style: 'margin-left:var(--s-2);font-size:var(--t-foot);font-weight:600',
    }, thingState(thing).label));
    card.appendChild(titleRow);

    const schedule = thing.schedule && thing.schedule !== 'Manual' ? thing.schedule : 'No schedule';
    const metaParts = [thingKindLabel(thing.kind), schedule];
    if (thing.run_count > 0) metaParts.push('ran ' + thing.run_count + '\u00d7');
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, metaParts.join('  \u00b7  ')));

    if (thing.what) {
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, thing.what));
    }
    if (thing.last_error) {
      card.appendChild(GhostUI.h('div', { className: 'type-foot text-danger', style: 'margin-top:var(--s-1)' }, thing.last_error));
    }

    const row = GhostUI.h('div', { className: 'btn-row' });
    if (thing.state === 'active') {
      row.appendChild(GhostUI.btn('Pause', 'secondary', () => thingAction(container, thing, 'pause')));
    } else if (thing.state === 'paused' || thing.state === 'failed') {
      row.appendChild(GhostUI.btn('Resume', 'secondary', () => thingAction(container, thing, 'resume')));
    }
    if (thing.state === 'active' || thing.state === 'paused' || thing.state === 'waiting') {
      row.appendChild(GhostUI.btn('Stop', 'danger', async () => {
        const ok = await GhostUI.confirmModal(
          'Stop this?',
          'Ghost will stop \u201c' + (thing.title || 'this') + '\u201d.',
          'Stop');
        if (ok) thingAction(container, thing, 'cancel');
      }));
    }
    if (row.childNodes.length > 0) card.appendChild(row);

    listEl.appendChild(card);
  });
}

// thingAction routes to the correct backend action based on provenance so the
// routine metadata sidecar stays consistent for routine-sourced Things.
async function thingAction(container, thing, op) {
  const isRoutine = thing.source === 'routine';
  const base = isRoutine ? '/v1/routines/' : '/v1/scheduled/';
  try {
    await GhostAPI.proxyPost(base + encodeURIComponent(thing.id) + '/' + op, {});
  } catch (e) {
    GhostUI.toast('Couldn\u2019t do that \u2014 try again.', 'err');
    return;
  }
  loadThings(container);
}

GhostApp.registerSection('routines', loadThings);
