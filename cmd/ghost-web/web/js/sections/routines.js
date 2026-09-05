/* Ghost Section: Routines — things Ghost does for you, on its own. */
'use strict';

async function loadRoutines(container) {
  if (GhostApp.currentSection() !== 'routines') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Routines'));
  head.appendChild(GhostUI.h('p', {}, 'Persistent instructions Ghost carries out on schedule — through the same runtime as chat.'));
  container.appendChild(head);

  const newBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showCreateRoutine(container, refresh) }, 'New routine');
  GhostApp.setActions(newBtn);

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

function showCreateRoutine(container, refresh) {
  const body = GhostUI.h('div');
  const nameInput = GhostUI.input('Name — e.g. Weekly brief');
  const instrInput = GhostUI.input('What should Ghost do? — e.g. prepare my weekly brief');
  const kindSel = GhostUI.select([{ value: 'cron', label: 'Weekly / cron' }, { value: 'every', label: 'Every N seconds' }, { value: 'at', label: 'Once at time' }]);
  const exprInput = GhostUI.input('Schedule — cron “0 9 * * MON”, seconds, or RFC3339 time');
  [nameInput, instrInput, kindSel, exprInput].forEach(el => { el.style.marginBottom = 'var(--s-2)'; el.style.width = '100%'; body.appendChild(el); });
  body.appendChild(GhostUI.h('p', { style: 'opacity:.7' },
    'Tip: just say “Every Monday at 9…” in chat and Ghost proposes the routine for you.'));
  GhostUI.modal('New routine', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const kind = kindSel.value;
      const payload = { name: nameInput.value.trim(), instruction: instrInput.value.trim(), kind };
      if (!payload.name || !payload.instruction) { GhostUI.toast('Name and instruction are required.', 'err'); return; }
      if (kind === 'cron') payload.expr = exprInput.value.trim();
      else if (kind === 'every') payload.every_seconds = parseInt(exprInput.value.trim(), 10) || 0;
      else payload.at = exprInput.value.trim();
      try {
        await GhostAPI.proxyPost('/v1/routines', payload);
      } catch (err) { GhostUI.toast('Couldn\'t create that routine.', 'err'); return; }
      e.target.closest('.ghost-modal-backdrop').remove();
      refresh();
    } }, 'Create'),
  ]);
}

GhostApp.registerSection('routines', loadRoutines);
