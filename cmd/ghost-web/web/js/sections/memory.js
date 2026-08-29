/* Ghost Section: Memory — what Ghost remembers. */
'use strict';

async function loadMemory(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Memory'));
  const sub = GhostUI.h('p', {}, 'What Ghost remembers about you.');
  head.appendChild(sub);
  container.appendChild(head);

  const searchWrap = GhostUI.h('div', { style: 'margin-bottom:var(--s-4)' });
  const search = GhostUI.input('Search memories…');
  searchWrap.appendChild(search);
  container.appendChild(searchWrap);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'mem-list' });
  listEl.appendChild(GhostUI.loading('Loading memories…'));
  container.appendChild(listEl);

  let res = [];
  try {
    res = await GhostAPI.proxyGet('/v1/memory/files');
  } catch (e) {
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn’t reach memory', 'Ghost may still be starting. Try again in a moment.'));
    return;
  }
  const files = Array.isArray(res) ? res : (res.files || res.items || []);

  function render(items) {
    listEl.innerHTML = '';
    if (!Array.isArray(items) || items.length === 0) {
      listEl.appendChild(GhostUI.emptyState('Nothing remembered yet', 'Ghost hasn’t stored any memory. As you talk, it will remember what matters.'));
      return;
    }
    items.forEach(f => {
      const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => openMemory(container, f.name) });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, f.name.replace(/\.md$/, '')));
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Updated ' + GhostUI.timeAgo(f.modified)));
      row.appendChild(c);
      row.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
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

async function openMemory(container, name) {
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadMemory(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '‹'));
  back.appendChild(GhostUI.h('span', {}, 'All memories'));
  container.appendChild(back);

  const view = GhostUI.h('div', { className: 'panel' });
  view.appendChild(GhostUI.loading('Opening memory…'));
  container.appendChild(view);

  let data;
  try { data = await GhostAPI.proxyGet('/v1/memory/file?name=' + encodeURIComponent(name)); }
  catch (e) {
    view.innerHTML = '';
    view.appendChild(GhostUI.errorState('Couldn’t open this memory', 'It may have been removed.'));
    return;
  }

  view.innerHTML = '';
  const head = GhostUI.h('div', { className: 'panel-head' });
  head.appendChild(GhostUI.h('div', {}, GhostUI.h('h2', {}, name.replace(/\.md$/, '')), GhostUI.h('p', {}, 'Stored on your Ghost')));
  const actions = GhostUI.h('div', { className: 'ghost-row-trailing' });
  actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
    if (!(await GhostUI.confirmModal('Forget this memory?', 'Ghost will delete “' + name + '” from its memory. This can’t be undone.', 'Forget'))) return;
    try { await GhostAPI.proxyDel('/v1/memory/file?name=' + encodeURIComponent(name)); GhostUI.toast('Memory forgotten'); loadMemory(container); }
    catch (e) { GhostUI.toast('Couldn’t forget that.', 'err'); }
  } }, 'Forget'));
  head.appendChild(actions);
  view.appendChild(head);

  const body = GhostUI.h('div', { className: 'markdown-body' });
  body.innerHTML = GhostUI.md(data.content || '');
  view.appendChild(body);
}

GhostApp.registerSection('memory', loadMemory);
