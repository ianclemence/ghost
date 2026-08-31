/* Ghost Section: Memory \u2014 what Ghost remembers. */
'use strict';

async function loadMemory(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Memory'));
  const sub = GhostUI.h('p', {}, 'What Ghost remembers about you.');
  head.appendChild(sub);
  container.appendChild(head);

  const selfPanel = GhostUI.h('div', { className: 'panel' });
  container.appendChild(selfPanel);
  renderSelf(selfPanel);

  const searchWrap = GhostUI.h('div', { style: 'margin-bottom:var(--s-4)' });
  const search = GhostUI.input('Search memories\u2026');
  searchWrap.appendChild(search);
  container.appendChild(searchWrap);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'mem-list' });
  listEl.appendChild(GhostUI.loading('Loading memories\u2026'));
  container.appendChild(listEl);

  let res = [];
  try {
    res = await GhostAPI.proxyGet('/v1/memory/files');
  } catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t reach memory', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const files = Array.isArray(res) ? res : (res.files || res.items || []);

  function render(items) {
    listEl.innerHTML = '';
    if (!Array.isArray(items) || items.length === 0) {
      listEl.appendChild(GhostUI.emptyState('Nothing remembered yet', 'Start talking to Ghost \u2014 it keeps notes on what matters. You can read or forget them here anytime.'));
      return;
    }
    items.forEach(f => {
      const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => openMemory(container, f) });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      const title = f.title || f.name.replace(/\.md$/, '');
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, title));
      const sub = GhostUI.h('div', { className: 'ghost-row-subtitle' });
      if (f.kind) sub.appendChild(GhostUI.h('span', { style: 'text-transform:capitalize' }, f.kind));
      if (f.summary) {
        if (f.kind) sub.appendChild(document.createTextNode(' \u00b7 '));
        sub.appendChild(document.createTextNode(f.summary.length > 80 ? f.summary.slice(0, 80) + '\u2026' : f.summary));
      } else {
        sub.appendChild(document.createTextNode('Updated ' + GhostUI.timeAgo(f.modified)));
      }
      c.appendChild(sub);
      row.appendChild(c);
      row.appendChild(GhostUI.h('span', { className: 'chevron' }, '\u203a'));
      listEl.appendChild(row);
    });
  }

  sub.textContent = files.length ? (GhostUI.fmtNum(files.length) + ' memories stored on this device.') : 'What Ghost remembers about you.';
  render(files);

  search.addEventListener('input', () => {
    const q = search.value.trim().toLowerCase();
    render(files.filter(f => f.name.toLowerCase().includes(q)));
  });
}

// renderSelf shows the structured "what Ghost knows about you" state — the same
// profile and notes that are always injected into every conversation. Making this
// visible and editable is what makes memory feel personal and owned.
async function renderSelf(panel) {
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const t = GhostUI.h('div');
  t.appendChild(GhostUI.h('h2', {}, 'What Ghost knows about you'));
  t.appendChild(GhostUI.h('p', {}, 'These are the things Ghost has learned and keeps in mind. You can forget anything below.'));
  ph.appendChild(t);
  panel.appendChild(ph);

  const body = GhostUI.h('div', { className: 'self-body' });
  body.appendChild(GhostUI.loading('Reading\u2026'));
  panel.appendChild(body);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/memory/self'); }
  catch (e) { body.innerHTML = ''; body.appendChild(GhostUI.emptyState('Unavailable', 'Couldn\u2019t read your saved profile right now.')); return; }
  if (!document.body.contains(panel)) return;
  body.innerHTML = '';

  const you = Array.isArray(res.you) ? res.you : [];
  const notes = Array.isArray(res.notes) ? res.notes : [];

  if (you.length === 0 && notes.length === 0) {
    body.appendChild(GhostUI.emptyState('Still getting to know you', 'Talk to Ghost and it will remember what matters about you here.'));
    return;
  }

  function group(title, items, target) {
    if (!items.length) return;
    const head = GhostUI.h('div', { className: 'self-group', style: 'font-size:var(--t-micro);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-faint);font-weight:600' }, title);
    body.appendChild(head);
    items.forEach(entry => {
      const row = GhostUI.h('div', { className: 'ghost-row self-entry' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:400' }, entry));
      row.appendChild(c);
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
      const forget = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Forget');
      forget.addEventListener('click', async () => {
        forget.disabled = true;
        try {
          await GhostAPI.proxyPost('/v1/memory/self/forget', { target: target, entry: entry });
          GhostUI.toast('Forgotten');
          renderSelf(panel);
        } catch (e) { forget.disabled = false; GhostUI.toast('Couldn\u2019t forget that.', 'err'); }
      });
      tr.appendChild(forget);
      row.appendChild(tr);
      body.appendChild(row);
    });
  }
  group('About you', you, 'user');
  group('What Ghost has learned', notes, 'memory');
}

async function openMemory(container, file) {
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadMemory(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '\u2039'));
  back.appendChild(GhostUI.h('span', {}, 'All memories'));
  container.appendChild(back);

  const view = GhostUI.h('div', { className: 'panel' });
  view.appendChild(GhostUI.loading('Opening memory\u2026'));
  container.appendChild(view);

  let data;
  try { data = await GhostAPI.proxyGet('/v1/memory/file?name=' + encodeURIComponent(file.name)); }
  catch (e) {
    view.innerHTML = '';
    view.appendChild(GhostUI.errorState('Couldn\u2019t open this memory', 'It may have been removed.'));
    return;
  }

  view.innerHTML = '';
  const head = GhostUI.h('div', { className: 'panel-head' });
  const titleText = file.title || file.name.replace(/\.md$/, '');
  const meta = GhostUI.h('p', {});
  if (file.kind) meta.appendChild(GhostUI.h('span', { style: 'text-transform:capitalize;font-weight:500' }, file.kind));
  if (file.source) {
    if (file.kind) meta.appendChild(document.createTextNode(' \u00b7 '));
    meta.appendChild(document.createTextNode('Source: ' + file.source));
  }
  if (meta.childNodes.length > 0) {
    head.appendChild(GhostUI.h('div', {}, GhostUI.h('h2', {}, titleText), meta));
  } else {
    head.appendChild(GhostUI.h('div', {}, GhostUI.h('h2', {}, titleText), GhostUI.h('p', {}, 'Stored on your Ghost')));
  }
  const actions = GhostUI.h('div', { className: 'ghost-row-trailing' });
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
    if (!(await GhostUI.confirmModal('Forget this memory?', 'Ghost will delete \u201c' + file.name + '\u201d from its memory. This can\u2019t be undone.', 'Forget'))) return;
    try { await GhostAPI.proxyDel('/v1/memory/file?name=' + encodeURIComponent(file.name)); GhostUI.toast('Memory forgotten'); loadMemory(container); }
    catch (e) { GhostUI.toast('Couldn\u2019t forget that.', 'err'); }
  } }, 'Forget'));
  head.appendChild(actions);
  view.appendChild(head);

  if (file.summary) {
    view.appendChild(GhostUI.h('div', { className: 'type-callout', style: 'margin-bottom:var(--s-4);padding:var(--s-3) var(--s-4);background:var(--ink-ghost);border-radius:var(--r-sm)' }, file.summary));
  }
  const body = GhostUI.h('div', { className: 'markdown-body' });
  body.innerHTML = GhostUI.md(data.content || '');
  view.appendChild(body);
}

GhostApp.registerSection('memory', loadMemory);
