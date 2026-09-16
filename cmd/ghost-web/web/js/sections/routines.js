/* Ghost Section: Routines — things Ghost does for you, on its own. */
'use strict';

async function loadRoutines(container) {
  if (GhostApp.currentSection() !== 'routines') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Routines'));
  head.appendChild(GhostUI.h('p', {}, 'Standing instructions Ghost runs like a conversation, on a schedule — pause, resume, or cancel them here.'));
  const cross = GhostUI.h('p', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-2)' }, 'For one-off reminders and deliveries to your apps, see ');
  const crossLink = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', style: 'padding:0', onClick: () => GhostApp.navigate('automations') }, 'Automations');
  cross.appendChild(crossLink);
  cross.appendChild(document.createTextNode('.'));
  head.appendChild(cross);
  container.appendChild(head);

  const newBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showCreateRoutine(container, refresh) }, 'New routine');
  const btnRow = GhostUI.h('div', { style: 'margin-bottom:var(--s-4)' });
  btnRow.appendChild(newBtn);
  container.appendChild(btnRow);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'routine-list' });
  listEl.appendChild(GhostUI.loading('Loading routines…'));
  container.appendChild(listEl);

  async function refresh() {
    if (!document.body.contains(container)) return;
    let items = [];
    try {
      const res = await GhostAPI.proxyGet('/v1/routines');
      items = res.routines || [];
    } catch (e) {
      if (!document.body.contains(container)) return;
      listEl.innerHTML = '';
      listEl.appendChild(GhostUI.errorState('Couldn\'t load routines', 'Ghost may still be starting.'));
      return;
    }
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    if (items.length === 0) {
      listEl.appendChild(GhostUI.emptyState('No routines yet', 'Say “Every Monday at 9 AM, prepare my weekly brief” and it appears here.'));
      return;
    }
    items.forEach(r => {
      const card = GhostUI.h('div', { className: 'ghost-card' });
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-title' }, r.name));
      let nextStr = '';
      if (r.next_run) {
        const d = new Date(r.next_run);
        if (!isNaN(d.getTime())) nextStr = ' · next: ' + d.toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' });
      }
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, r.status + nextStr));
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, r.instruction));
      const row = GhostUI.h('div', { className: 'btn-row' });
      const act = r.status === 'paused'
        ? [['Resume', 'resume']]
        : [['Pause', 'pause']];
      act.push(['Cancel', 'cancel']);
      act.forEach(([label, op]) => {
        row.appendChild(GhostUI.btn(label, 'secondary', async () => {
          try {
            await GhostAPI.proxyPost('/v1/routines/' + encodeURIComponent(r.id) + '/' + op, {});
          } catch (e) { GhostUI.toast('Couldn\'t do that — try again.'); return; }
          refresh();
        }));
      });
      card.appendChild(row);
      listEl.appendChild(card);
    });
  }
  await refresh();
}

// parseRoutineSchedule accepts plain words first ("Every Monday at 9am",
// "every 2 hours") and cron expressions as the advanced fallback — same
// shape as the Automations form, so owners never need cron syntax.
function parseRoutineSchedule(text) {
  const t = String(text || '').trim();
  if (/^[\d\*\/\-\,\s]+$/.test(t) && t.split(/\s+/).length === 5) {
    return { kind: 'cron', expr: t };
  }
  let m = t.match(/^every\s+(\d+)\s*m(?:in)?/i);
  if (m) return { kind: 'every', every_seconds: parseInt(m[1], 10) * 60 };
  m = t.match(/^every\s+(\d+\.?\d*)\s*h/i);
  if (m) return { kind: 'every', every_seconds: Math.round(parseFloat(m[1]) * 3600) };
  m = t.match(/^every\s+(\d+\.?\d*)\s*d(?:ay)?/i);
  if (m) return { kind: 'every', every_seconds: Math.round(parseFloat(m[1]) * 86400) };
  const at = new Date(t);
  if (!isNaN(at.getTime()) && !/^every/i.test(t)) return { kind: 'at', at: at.toISOString() };
  return { kind: 'cron', expr: t };
}

function showCreateRoutine(container, refresh) {
  const body = GhostUI.h('div');
  const nameInput = GhostUI.input('Name — e.g. Weekly brief');
  const instrInput = GhostUI.input('What should Ghost do? — e.g. prepare my weekly brief');
  const schedInput = GhostUI.input('When — e.g. Every Monday at 9am');
  [nameInput, instrInput, schedInput].forEach(el => { el.style.marginBottom = 'var(--s-2)'; el.style.width = '100%'; body.appendChild(el); });
  body.appendChild(GhostUI.h('p', { style: 'opacity:.7' },
    'Plain words work ("every 2 hours", "weekdays at 9am"); a cron expression works too. Tip: just say “Every Monday at 9…” in chat and Ghost proposes the routine for you.'));
  GhostUI.modal('New routine', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const payload = { name: nameInput.value.trim(), instruction: instrInput.value.trim() };
      if (!payload.name || !payload.instruction) { GhostUI.toast('Name and instruction are required.', 'err'); return; }
      const schedText = schedInput.value.trim();
      if (!schedText) { GhostUI.toast('Tell Ghost when it should run.', 'err'); return; }
      Object.assign(payload, parseRoutineSchedule(schedText));
      try {
        await GhostAPI.proxyPost('/v1/routines', payload);
      } catch (err) { GhostUI.toast('Couldn\'t create that routine.', 'err'); return; }
      e.target.closest('.ghost-modal-backdrop').remove();
      refresh();
    } }, 'Create'),
  ]);
}

GhostApp.registerSection('routines', loadRoutines);
