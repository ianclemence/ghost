/* Ghost Section: Ideas — what Ghost noticed, and what it can do about it.
 *
 * Two kinds of card live here, both evidence-first:
 *
 *   • Proposals — a grounded observation ("your 09:00 reminder never reached
 *     you"), why it matters now, and exactly one action Ghost can take.
 *     Approve runs it through the permission broker and the real capability;
 *     the verified result appears on the card. Dismiss and Snooze never act.
 *   • Advice — model-drafted suggestions whose citations are verified. Accept
 *     and dismiss write receipts; nothing acts without the owner.
 *
 * Nothing on this screen is a notification feed: every card names what Ghost
 * saw, why it matters, and what (if anything) it can do next.
 */
'use strict';

function ideasStatusLabel(idea) {
  const s = (idea.status || '').toLowerCase();
  const map = {
    pending: 'Waiting',
    presented: 'Needs you',
    snoozed: 'Snoozed',
    accepted: 'Approved',
    dismissed: 'Dismissed',
    expired: 'Expired',
    executing: 'Running',
    completed: 'Handled',
    failed: 'Couldn\u2019t handle it',
    superseded: 'Situation changed',
  };
  return map[s] || (s ? s : '');
}

function ideasStatusTone(idea) {
  const s = (idea.status || '').toLowerCase();
  if (s === 'completed' || s === 'accepted') return 'ok';
  if (s === 'failed') return 'err';
  if (s === 'presented') return 'warn';
  return 'muted';
}

async function loadIdeas(container) {
  if (GhostApp.currentSection() !== 'ideas') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Ideas'));
  head.appendChild(GhostUI.h('p', {},
    'What Ghost noticed \u2014 with the evidence it used, and one action you can approve. Nothing here acts without you.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'ideas-list' });
  listEl.appendChild(GhostUI.loading('Looking at what needs you\u2026'));
  container.appendChild(listEl);

  let open;
  let resolved;
  let promises;
  try {
    const results = await Promise.all([
      GhostAPI.proxyGet('/v1/ideas?status=open'),
      GhostAPI.proxyGet('/v1/ideas?status=resolved'),
      GhostAPI.proxyGet('/v1/commitments').catch(() => ({ commitments: [] })),
    ]);
    open = results[0];
    resolved = results[1];
    promises = results[2];
  } catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load ideas', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  if (!document.body.contains(container)) return;
  renderIdeas(listEl, container,
    Array.isArray(open && open.ideas) ? open.ideas : [],
    Array.isArray(resolved && resolved.ideas) ? resolved.ideas : [],
    Array.isArray(promises && promises.commitments) ? promises.commitments : []);
}

function renderPromises(listEl, container, promises) {
  const live = promises.filter(c => c.status === 'open' || c.status === 'blocked');
  if (live.length === 0) return;
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h2', { style: 'font-size:var(--t-h3)' }, 'Promises Ghost is holding'));
  listEl.appendChild(head);

  live.forEach(c => {
    const card = GhostUI.h('div', { className: 'ghost-card' });
    const titleRow = GhostUI.h('div', { className: 'ghost-card-title' });
    titleRow.appendChild(document.createTextNode(c.text));
    titleRow.appendChild(GhostUI.h('span', {
      className: 'type-foot', style: 'margin-left:var(--s-2);font-weight:600',
    }, c.status === 'blocked' ? 'Blocked' : 'Open'));
    card.appendChild(titleRow);

    const when = c.due_at
      ? 'Due ' + new Date(c.due_at).toLocaleString()
      : 'No date on it';
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, when));
    if (c.provenance && c.provenance.quote) {
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' },
        'You said: “' + c.provenance.quote + '”'));
    }
    if (c.outcome_note) {
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, 'Last try: ' + c.outcome_note));
    }
    const row = GhostUI.h('div', { className: 'btn-row' });
    row.appendChild(GhostUI.btn('Close it', 'secondary', async () => {
      try {
        await GhostAPI.proxyPost('/v1/commitments/' + encodeURIComponent(c.id) + '/cancel', {});
        GhostUI.toast('Closed. Ghost will stop holding it.', 'ok');
      } catch (e) {
        GhostUI.toast('Couldn’t close that — try again.', 'err');
      }
      loadIdeas(container);
    }));
    card.appendChild(row);
    listEl.appendChild(card);
  });
}

function renderIdeas(listEl, container, openItems, resolvedItems, promises) {
  listEl.innerHTML = '';

  renderPromises(listEl, container, promises || []);

  if (openItems.length === 0) {
    listEl.appendChild(GhostUI.emptyState(
      'Nothing needs you',
      'When Ghost notices something worth your attention \u2014 a reminder that never landed, a routine that keeps failing \u2014 it shows up here with the evidence.'));
  }

  openItems.forEach(idea => listEl.appendChild(renderIdeaCard(container, idea, true)));

  if (resolvedItems.length > 0) {
    const head = GhostUI.h('div', { className: 'page-head', style: 'margin-top:var(--s-4)' });
    head.appendChild(GhostUI.h('h2', { style: 'font-size:var(--t-h3)' }, 'Recently handled'));
    listEl.appendChild(head);
    resolvedItems.slice(0, 10).forEach(idea => listEl.appendChild(renderIdeaCard(container, idea, false)));
  }
}

function renderIdeaCard(container, idea, actionable) {
  const card = GhostUI.h('div', { className: 'ghost-card' });

  const titleRow = GhostUI.h('div', { className: 'ghost-card-title' });
  titleRow.appendChild(document.createTextNode(idea.title || 'Untitled'));
  const label = ideasStatusLabel(idea);
  if (label) {
    const tone = ideasStatusTone(idea);
    titleRow.appendChild(GhostUI.h('span', {
      className: 'type-foot ' + (tone === 'err' ? 'text-warning' : ''),
      style: 'margin-left:var(--s-2);font-weight:600',
    }, label));
  }
  if (idea.unverified) {
    titleRow.appendChild(GhostUI.h('span', {
      className: 'text-warning',
      style: 'margin-left:var(--s-2);font-size:var(--t-foot);font-weight:600',
    }, 'Needs checking'));
  }
  card.appendChild(titleRow);

  if (idea.body) {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-sub' }, idea.body));
  }
  if (idea.reason && idea.reason !== idea.body) {
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, 'Why now: ' + idea.reason));
  }
  (idea.sources || []).forEach(s => {
    const line = 'Evidence: ' + (s.excerpt || (s.kind + ':' + s.ref));
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, line));
  });

  // The verified outcome, stated in the runtime's words — never a claim.
  if (idea.result) {
    card.appendChild(GhostUI.h('div', {
      className: 'ghost-card-meta',
      style: 'margin-top:var(--s-1)',
    }, (idea.outcome === 'failed' ? 'Result: ' : 'Done: ') + idea.result));
  }
  if (idea.unverified) {
    card.appendChild(GhostUI.h('div', { className: 'type-foot text-warning', style: 'margin-top:var(--s-1)' },
      'Ghost could not verify this one \u2014 treat with care.'));
  }

  if (actionable && (idea.status === 'pending' || idea.status === 'presented' || idea.status === 'snoozed')) {
    const row = GhostUI.h('div', { className: 'btn-row' });
    const primary = (idea.plan && idea.plan.describe) ? capitalize(idea.plan.describe) : 'Do it';
    if (idea.plan) {
      row.appendChild(GhostUI.btn(primary, 'primary', async () => {
        await ideaAction(container, idea, 'approve', true);
      }));
    }
    row.appendChild(GhostUI.btn('Later', 'secondary', async () => {
      await ideaAction(container, idea, 'snooze', false);
    }));
    row.appendChild(GhostUI.btn('No thanks', 'secondary', async () => {
      await ideaAction(container, idea, 'dismiss', false);
    }));
    card.appendChild(row);

    if (idea.plan && idea.plan.risk && idea.plan.risk !== 'read_only') {
      card.appendChild(GhostUI.h('div', { className: 'type-foot', style: 'margin-top:var(--s-1)' },
        'Ghost will ask the permission broker before this runs.'));
    }
  }
  return card;
}

function capitalize(s) {
  s = String(s || '');
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}

async function ideaAction(container, idea, op, confirm) {
  if (confirm) {
    const ok = await GhostUI.confirmModal(
      'Let Ghost do this?',
      capitalize(idea.plan && idea.plan.describe ? idea.plan.describe : 'Take this action') + ' \u2014 \u201c' + (idea.title || 'this') + '\u201d.',
      'Yes, do it');
    if (!ok) return;
  }
  let res;
  try {
    res = await GhostAPI.proxyPost('/v1/ideas/' + encodeURIComponent(idea.id) + '/' + op, {});
  } catch (e) {
    GhostUI.toast('Couldn\u2019t do that \u2014 try again.', 'err');
    return;
  }
  if (res && res.result) {
    GhostUI.toast(res.result, (res.ok === false) ? 'err' : 'ok');
  } else if (res && res.ok === false && res.error) {
    GhostUI.toast(res.error, 'err');
  } else if (op === 'snooze') {
    GhostUI.toast('Snoozed \u2014 Ghost will bring it back later.', 'ok');
  } else if (op === 'dismiss') {
    GhostUI.toast('Dismissed.', 'ok');
  }
  loadIdeas(container);
}

GhostApp.registerSection('ideas', loadIdeas);
