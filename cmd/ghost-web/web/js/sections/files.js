/* Ghost Section: Files — what you have sent Ghost, and deleting it. */
'use strict';

const FILE_KIND_WORD = {
  image: 'Photo', document: 'Document', spreadsheet: 'Spreadsheet', text: 'Text',
  audio: 'Audio', video: 'Video', archive: 'Archive',
};

function fileSizeText(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB';
  if (n >= 1024) return Math.round(n / 1024) + ' KB';
  return n + ' B';
}

async function loadFiles(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Files'));
  container.appendChild(head);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/files'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    container.appendChild(GhostUI.errorState('Couldn’t load your files', e.message || 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const files = Array.isArray(res && res.files) ? res.files : [];
  const days = (res && res.retention_days) || 30;
  head.appendChild(GhostUI.h('p', {}, 'Photos and files you have sent Ghost, kept on this device for ' + days +
    ' days or until you delete them. Deleting removes the file itself.'));

  if (files.length === 0) {
    container.appendChild(GhostUI.emptyState('No files yet',
      'Attach a photo or document in the app or with /attach in the terminal, and it appears here.'));
    return;
  }

  const panel = GhostUI.h('div', { className: 'panel' });
  files.forEach((f) => {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, f.name));
    const when = f.created_at ? new Date(f.created_at).toLocaleDateString([], { month: 'short', day: 'numeric' }) : '';
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' },
      [FILE_KIND_WORD[f.kind] || 'File', fileSizeText(f.size), when].filter(Boolean).join(' · ')));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    tr.appendChild(GhostUI.h('button', {
      className: 'ghost-btn ghost-btn-secondary ghost-btn-sm',
      onClick: async () => {
        const ok = await GhostUI.confirmModal('Delete “' + f.name + '”?',
          'It is removed from this device. This can’t be undone.', 'Delete');
        if (!ok) return;
        try {
          await GhostAPI.proxyDel('/v1/files/' + encodeURIComponent(f.id));
          GhostUI.toast('Deleted');
          loadFiles(container);
        } catch (e) { GhostUI.toast('Couldn’t delete that.', 'err'); }
      },
    }, 'Delete'));
    row.appendChild(tr);
    panel.appendChild(row);
  });
  container.appendChild(panel);
}

GhostApp.registerSection('files', loadFiles);
