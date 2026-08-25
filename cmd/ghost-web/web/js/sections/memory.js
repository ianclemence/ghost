/* Ghost Section: Memory — What Ghost remembers */
'use strict';

async function loadMemory(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-memory' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Memory'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost remembers about you.'));

  // Search
  const searchWrap = GhostUI.h('div', { style: 'margin-bottom:var(--space-xxl)' });
  const searchInput = GhostUI.input('Search memories\u2026');
  searchInput.id = 'memory-search';
  searchWrap.appendChild(searchInput);
  section.appendChild(searchWrap);

  // Load memory files
  const listEl = GhostUI.h('div', { className: 'ghost-list' });
  const countEl = GhostUI.h('div', { className: 'type-subhead text-tertiary', style: 'margin-bottom:var(--space-md)' });

  try {
    const files = await GhostAPI.proxyGet('/v1/memory/files');
    const fileArr = Array.isArray(files) ? files : [];
    countEl.textContent = `${fileArr.length} memory file${fileArr.length !== 1 ? 's' : ''}`;
    section.appendChild(countEl);

    for (const f of fileArr) {
      const r = GhostUI.h('div', { className: 'ghost-row', style: 'cursor:pointer', onClick: () => openMemoryFile(f.name) });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      const title = f.name.replace(/\.md$/, '').replace(/[-_]/g, ' ');
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, title));
      if (f.size) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, `${Math.round(f.size / 1024)}KB`));
      r.appendChild(c);
      r.appendChild(GhostUI.h('span', { className: 'chevron' }, '\u203a'));
      listEl.appendChild(r);
    }

    if (fileArr.length === 0) {
      section.appendChild(GhostUI.emptyState('Ghost is still getting to know you.', 'Talk to Ghost, and it will remember.'));
    }
  } catch (e) {
    countEl.textContent = '';
    section.appendChild(GhostUI.errorState('Ghost couldn\'t load your memories.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

async function openMemoryFile(name) {
  const content = GhostUI.h('div', { id: 'section-memory', style: 'max-width:var(--content-max-width)' });
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--space-lg)', onClick: () => GhostApp.navigate('memory') });
  back.appendChild(GhostUI.h('span', { className: 'type-callout text-accent' }, '\u2190 Memory'));
  content.appendChild(back);

  try {
    const data = await GhostAPI.proxyGet('/v1/memory/file?name=' + encodeURIComponent(name));
    const title = name.replace(/\.md$/, '').replace(/[-_]/g, ' ');
    content.appendChild(GhostUI.h('div', { className: 'type-title', style: 'margin-bottom:var(--space-xl)' }, title));
    const body = GhostUI.h('div', { className: 'type-body', style: 'white-space:pre-wrap;line-height:1.7' });
    body.textContent = data.content || 'Empty.';
    content.appendChild(body);
  } catch (e) {
    content.appendChild(GhostUI.errorState('Ghost couldn\'t load this memory.', e.message));
  }

  const appContent = document.getElementById('ghost-content');
  appContent.innerHTML = '';
  appContent.appendChild(content);
}

GhostApp.registerSection('memory', loadMemory);
