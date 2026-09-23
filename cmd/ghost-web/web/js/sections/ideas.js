/* Ghost Section: Ideas — suggestions with evidence.
 *
 * Every idea cites the rows behind it, so the owner judges the source, not
 * the prose. Accept and dismiss write receipts. Unverified drafts say so on
 * the card. Nothing here acts without the owner.
 */
'use strict';

async function loadIdeas(container) {
  if (GhostApp.currentSection() !== 'ideas') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Ideas'));
  head.appendChild(GhostUI.h('p', {},
    'Suggestions with evidence. Nothing here acts without you.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'ideas-list' });
  listEl.appendChild(GhostUI.loading('Looking for ideas\u2026'));
  container.appendChild(listEl);

  let res;
  try {
    res = await GhostAPI.proxyGet('/v1/ideas');
  } catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load ideas', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  if (!document.body.contains(container)) return;
  renderIdeas(listEl, container, Array.isArray(res && res.ideas) ? res.ideas : []);
}

function renderIdeas(listEl, container, items) {
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState(
      'No ideas right now',
      'Ghost suggests things when it notices something \u2014 each one says why.'));
    return;
  }

  items.forEach(idea => {
    const card = GhostUI.h('div', { className: 'ghost-card' });

    const titleRow = GhostUI.h('div', { className: 'ghost-card-title' });
    titleRow.appendChild(document.createTextNode(idea.title || 'Untitled'));
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
    (idea.sources || []).forEach(s => {
      const line = 'Why: ' + (s.excerpt || (s.kind + ':' + s.ref));
      card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, line));
    });
    if (idea.unverified) {
      card.appendChild(GhostUI.h('div', { className: 'type-foot text-warning', style: 'margin-top:var(--s-1)' },
        'Ghost could not verify this one \u2014 treat with care.'));
    }

    const row = GhostUI.h('div', { className: 'btn-row' });
    row.appendChild(GhostUI.btn('Accept', 'secondary', async () => {
      await ideaAction(container, idea, 'accept');
    }));
    row.appendChild(GhostUI.btn('Dismiss', 'secondary', async () => {
      await ideaAction(container, idea, 'dismiss');
    }));
    card.appendChild(row);

    listEl.appendChild(card);
  });
}

async function ideaAction(container, idea, op) {
  if (op === 'accept') {
    const ok = await GhostUI.confirmModal(
      'Accept this idea?',
      '\u201c' + (idea.title || 'this') + '\u201d.',
      'Accept');
    if (!ok) return;
  }
  try {
    await GhostAPI.proxyPost('/v1/ideas/' + encodeURIComponent(idea.id) + '/' + op, {});
  } catch (e) {
    GhostUI.toast('Couldn\u2019t do that \u2014 try again.', 'err');
    return;
  }
  loadIdeas(container);
}

GhostApp.registerSection('ideas', loadIdeas);
