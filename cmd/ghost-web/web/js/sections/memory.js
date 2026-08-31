/* Ghost Section: Memory \u2014 what Ghost remembers about you. */
'use strict';

async function loadMemory(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Memory'));
  const sub = GhostUI.h('p', {}, 'What Ghost remembers about you.');
  head.appendChild(sub);
  container.appendChild(head);

  const [selfRes, filesRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/memory/self'),
    GhostAPI.proxyGet('/v1/memory/files'),
  ]);
  if (!document.body.contains(container)) return;

  const self = selfRes.status === 'fulfilled' ? (selfRes.value || {}) : {};
  const facts = Array.isArray(self.entries) ? self.entries : [];
  const curated = (Array.isArray(self.notes) ? self.notes : []).concat(Array.isArray(self.you) ? self.you : []);
  const files = filesRes.status === 'fulfilled'
    ? (Array.isArray(filesRes.value) ? filesRes.value : (filesRes.value.files || filesRes.value.items || []))
    : [];

  // One calm empty state when there's nothing at all — never two.
  if (facts.length === 0 && curated.length === 0 && files.length === 0) {
    container.appendChild(GhostUI.emptyState('Ghost is still getting to know you', 'Talk to Ghost and it will remember the things that matter about you here.'));
    return;
  }

  if (facts.length || curated.length) {
    renderFacts(container, facts, curated);
  }

  if (files.length) {
    renderNotes(container, files);
  }
}

// renderFacts shows the structured state Ghost learns from conversation — the
// facts it knows about you plus any notes it has saved. Each item can be
// forgotten so memory stays yours.
function renderFacts(container, facts, curated) {
  const panel = GhostUI.h('div', { className: 'panel' });
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const t = GhostUI.h('div');
  t.appendChild(GhostUI.h('h2', {}, 'About you'));
  t.appendChild(GhostUI.h('p', {}, 'Learned from your conversations. Forget anything below and it won\u2019t be used anymore.'));
  ph.appendChild(t);
  panel.appendChild(ph);
  const body = GhostUI.h('div', { className: 'self-body' });
  panel.appendChild(body);
  container.appendChild(panel);

  function group(label, rows, build) {
    if (!rows.length) return;
    body.appendChild(GhostUI.h('div', { className: 'self-group' }, label));
    rows.forEach(row => body.appendChild(build(row)));
  }

  const forgetBtn = async (payload) => {
    try {
      await GhostAPI.proxyPost('/v1/memory/self/forget', payload);
      GhostUI.toast('Forgotten');
      loadMemory(container);
    } catch (e) { GhostUI.toast('Couldn\u2019t forget that.', 'err'); }
  };

  group('About you', facts, (e) => {
    const row = GhostUI.h('div', { className: 'ghost-row self-entry' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:400' }, e.value || e.label));
    if (e.label && e.value) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, e.label));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Forget');
    btn.addEventListener('click', () => forgetBtn({ id: e.id }));
    tr.appendChild(btn);
    row.appendChild(tr);
    return row;
  });

  group('What Ghost has learned', curated, (entry) => {
    const row = GhostUI.h('div', { className: 'ghost-row self-entry' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:400' }, entry));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Forget');
    btn.addEventListener('click', () => forgetBtn({ target: 'memory', entry: entry }));
    tr.appendChild(btn);
    row.appendChild(tr);
    return row;
  });
}

// renderNotes is the searchable list of memory files Ghost has written.
function renderNotes(container, files) {
  const searchWrap = GhostUI.h('div', { style: 'margin-bottom:var(--s-4);margin-top:var(--s-4)' });
  const search = GhostUI.input('Search memories\u2026');
  searchWrap.appendChild(search);
  container.appendChild(searchWrap);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'mem-list' });
  container.appendChild(listEl);

  function render(items) {
    listEl.innerHTML = '';
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

  render(files);
  search.addEventListener('input', () => {
    const q = search.value.trim().toLowerCase();
    render(files.filter(f => f.name.toLowerCase().includes(q)));
  });
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
