/* Ghost Section: Files — what you have sent Ghost, to look at and to remove.
   A gallery: photos show themselves, everything else gets a tile that says
   what it is. Open one to read it; arrow keys move between them. */
'use strict';

const FILE_KIND_WORD = {
  image: 'Photo', document: 'Document', spreadsheet: 'Spreadsheet', text: 'Text',
  audio: 'Audio', video: 'Video', archive: 'Archive', other: 'File',
};

// A small glyph per kind, drawn once, tinted by the tile it sits in.
const FILE_GLYPH = {
  image: '<path d="M4 6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5v11a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17.5v-11Z"/><circle cx="9" cy="9.5" r="1.6"/><path d="m4.5 17 4.6-4.4a1.5 1.5 0 0 1 2.1 0L14 15.2l1.6-1.5a1.5 1.5 0 0 1 2 0L20 16"/>',
  document: '<path d="M7 3.5h6.6L18.5 8.4V19a1.5 1.5 0 0 1-1.5 1.5H7A1.5 1.5 0 0 1 5.5 19V5A1.5 1.5 0 0 1 7 3.5Z"/><path d="M13.5 3.8V8.5h4.7M8.5 12.5h7M8.5 15.5h5"/>',
  spreadsheet: '<rect x="4" y="4.5" width="16" height="15" rx="2"/><path d="M4 9.5h16M4 14.5h16M10 4.5v15"/>',
  text: '<path d="M5 6.5h14M5 10.5h14M5 14.5h9M5 18.5h6"/>',
  audio: '<path d="M4 12h1.5M8 8v8M12 5v14M16 9v6M20 11v2"/>',
  video: '<rect x="4" y="5.5" width="16" height="13" rx="2.5"/><path d="m10.5 9.5 4 2.5-4 2.5v-5Z"/>',
  archive: '<path d="M4.5 7.5 6 4.5h12l1.5 3M4.5 7.5h15V19a1 1 0 0 1-1 1h-13a1 1 0 0 1-1-1V7.5Z"/><path d="M10 11.5h4"/>',
  other: '<path d="M7 3.5h6.6L18.5 8.4V19a1.5 1.5 0 0 1-1.5 1.5H7A1.5 1.5 0 0 1 5.5 19V5A1.5 1.5 0 0 1 7 3.5Z"/>',
};

function fileGlyph(kind) {
  const d = FILE_GLYPH[kind] || FILE_GLYPH.other;
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + d + '</svg>';
}

function fileSizeText(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB';
  if (n >= 1024) return Math.round(n / 1024) + ' KB';
  return n + ' B';
}

function fileExt(name) {
  const m = /\.([A-Za-z0-9]{1,5})$/.exec(name || '');
  return m ? m[1].toUpperCase() : '';
}

function fileWhen(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return '';
  const now = new Date();
  const opts = d.getFullYear() === now.getFullYear() ? { month: 'short', day: 'numeric' } : { month: 'short', day: 'numeric', year: 'numeric' };
  return d.toLocaleDateString([], opts);
}

// A PDF reads as its printed pages (fonts, layout, pictures), never as the
// text pulled out of it. The preview payload names the mime; the file name
// is the fallback for uploads recorded before the mime was kept.
function isPdfPreview(pv, f) {
  if (pv && pv.mime === 'application/pdf') return true;
  const name = (pv && pv.name) || (f && f.name) || '';
  return /\.pdf$/i.test(name);
}
var GhostFiles = { isPdfPreview: isPdfPreview };

// The groups the filter offers. "Other" gathers whatever has no group of its own.
const FILE_FILTERS = [
  { key: 'all', label: 'All', match: () => true },
  { key: 'photos', label: 'Photos', match: (f) => f.kind === 'image' },
  { key: 'docs', label: 'Documents', match: (f) => f.kind === 'document' || f.kind === 'spreadsheet' || f.kind === 'text' },
  { key: 'other', label: 'Other', match: (f) => !['image', 'document', 'spreadsheet', 'text'].includes(f.kind) },
];

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
  const files = (Array.isArray(res && res.files) ? res.files : []).slice()
    .sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')));
  const days = (res && res.retention_days) || 30;
  head.appendChild(GhostUI.h('p', {}, 'Photos and files you have sent Ghost, kept on this device for ' + days +
    ' days or until you delete them. Deleting removes the file itself.'));

  if (files.length === 0) {
    const empty = GhostUI.h('div', { className: 'files-empty' });
    empty.appendChild(GhostUI.h('div', { className: 'files-empty-art', html: fileGlyph('image') }));
    empty.appendChild(GhostUI.h('h2', {}, 'Nothing here yet'));
    empty.appendChild(GhostUI.h('p', {}, 'Send Ghost a photo or a document from the app, or with /attach in the terminal, and it will appear here for you to look at.'));
    container.appendChild(empty);
    return;
  }

  // Toolbar: filter on the left, the total on the right.
  const total = files.reduce((n, f) => n + (f.size || 0), 0);
  const bar = GhostUI.h('div', { className: 'files-bar' });
  const seg = GhostUI.h('div', { className: 'files-seg', role: 'tablist', 'aria-label': 'Filter files' });
  const summary = GhostUI.h('div', { className: 'files-summary' });
  bar.appendChild(seg);
  bar.appendChild(summary);
  container.appendChild(bar);
  const grid = GhostUI.h('div', { className: 'file-grid' });
  container.appendChild(grid);

  let filter = 'all';
  function visible() { return files.filter(FILE_FILTERS.find((x) => x.key === filter).match); }

  function drawSeg() {
    seg.innerHTML = '';
    FILE_FILTERS.forEach((fl) => {
      const n = files.filter(fl.match).length;
      if (fl.key !== 'all' && n === 0) return;
      const b = GhostUI.h('button', {
        className: 'files-seg-btn' + (fl.key === filter ? ' on' : ''), type: 'button', role: 'tab',
        'aria-selected': String(fl.key === filter),
        onClick: () => { filter = fl.key; drawSeg(); drawGrid(); },
      }, fl.label, GhostUI.h('span', { className: 'files-seg-n' }, String(n)));
      seg.appendChild(b);
    });
  }

  function drawGrid() {
    grid.innerHTML = '';
    const list = visible();
    summary.textContent = list.length + (list.length === 1 ? ' file' : ' files') + ' · ' + fileSizeText(list.reduce((n, f) => n + (f.size || 0), 0));
    list.forEach((f, i) => grid.appendChild(fileCard(f, i, list)));
  }

  function fileCard(f, i, list) {
    const kind = FILE_KIND_WORD[f.kind] ? f.kind : 'other';
    const card = GhostUI.h('button', {
      className: 'file-card file-' + kind, type: 'button',
      'aria-label': 'Open ' + f.name,
      onClick: () => openViewer(list, i),
    });
    card.style.setProperty('--i', String(Math.min(i, 14)));
    const thumb = GhostUI.h('div', { className: 'file-thumb' });
    const tile = GhostUI.h('div', { className: 'file-tile' });
    tile.appendChild(GhostUI.h('span', { className: 'file-tile-glyph', html: fileGlyph(kind) }));
    tile.appendChild(GhostUI.h('span', { className: 'file-tile-ext' }, fileExt(f.name) || FILE_KIND_WORD[kind]));
    thumb.appendChild(tile);
    if (kind === 'image') {
      // The photo itself, small. It fades in over the tile; if the Pod cannot
      // make a picture of it, the tile simply stays.
      const img = GhostUI.h('img', { className: 'file-img', alt: '', loading: 'lazy', decoding: 'async', src: '/api/proxy/v1/files/' + encodeURIComponent(f.id) + '/thumb' });
      img.addEventListener('load', () => { img.classList.add('in'); thumb.classList.add('has-img'); });
      img.addEventListener('error', () => img.remove());
      thumb.appendChild(img);
    }
    card.appendChild(thumb);
    const meta = GhostUI.h('div', { className: 'file-meta' });
    meta.appendChild(GhostUI.h('div', { className: 'file-name', title: f.name }, f.name));
    meta.appendChild(GhostUI.h('div', { className: 'file-sub' },
      [fileExt(f.name) || FILE_KIND_WORD[kind], fileSizeText(f.size || 0), fileWhen(f.created_at)].filter(Boolean).join(' · ')));
    card.appendChild(meta);
    return card;
  }

  // ── Viewer ──
  let viewer = null;
  function closeViewer() {
    if (!viewer) return;
    const v = viewer; viewer = null;
    document.removeEventListener('keydown', v.onKey);
    v.root.classList.add('leaving');
    setTimeout(() => v.root.remove(), 160);
  }

  function openViewer(list, startAt) {
    closeViewer();
    let idx = startAt;
    const root = GhostUI.h('div', { className: 'file-viewer', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'File preview', tabindex: '-1' });
    const scrim = GhostUI.h('div', { className: 'file-viewer-scrim', onClick: closeViewer });
    const panel = GhostUI.h('div', { className: 'file-viewer-panel' });
    root.appendChild(scrim);
    root.appendChild(panel);
    document.body.appendChild(root);

    const top = GhostUI.h('div', { className: 'fv-top' });
    const titles = GhostUI.h('div', { className: 'fv-titles' });
    const nameEl = GhostUI.h('div', { className: 'fv-name' });
    const subEl = GhostUI.h('div', { className: 'fv-sub' });
    titles.appendChild(nameEl); titles.appendChild(subEl);
    const closeBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-icon fv-close', type: 'button', 'aria-label': 'Close', onClick: closeViewer, html: '<svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="m3.5 3.5 9 9m0-9-9 9"/></svg>' });
    top.appendChild(titles); top.appendChild(closeBtn);

    const stage = GhostUI.h('div', { className: 'fv-stage' });
    const prev = GhostUI.h('button', { className: 'fv-nav fv-prev', type: 'button', 'aria-label': 'Previous file', onClick: () => go(-1), html: '<svg viewBox="0 0 16 16" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m10 3-5 5 5 5"/></svg>' });
    const next = GhostUI.h('button', { className: 'fv-nav fv-next', type: 'button', 'aria-label': 'Next file', onClick: () => go(1), html: '<svg viewBox="0 0 16 16" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m6 3 5 5-5 5"/></svg>' });
    const body = GhostUI.h('div', { className: 'fv-body' });
    stage.appendChild(prev); stage.appendChild(body); stage.appendChild(next);

    const foot = GhostUI.h('div', { className: 'fv-foot' });
    const count = GhostUI.h('div', { className: 'fv-count' });
    const dl = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Download');
    const del = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary fv-delete', type: 'button' }, 'Delete');
    const acts = GhostUI.h('div', { className: 'fv-actions' }, dl, del);
    foot.appendChild(count); foot.appendChild(acts);

    panel.appendChild(top); panel.appendChild(stage); panel.appendChild(foot);

    let token = 0;
    async function show() {
      const f = list[idx];
      const my = ++token;
      const kind = FILE_KIND_WORD[f.kind] ? f.kind : 'other';
      nameEl.textContent = f.name;
      subEl.textContent = [FILE_KIND_WORD[kind], fileSizeText(f.size || 0), fileWhen(f.created_at), f.source ? 'from ' + f.source : ''].filter(Boolean).join(' · ');
      count.textContent = (idx + 1) + ' of ' + list.length;
      prev.disabled = idx === 0; next.disabled = idx === list.length - 1;
      body.className = 'fv-body fv-' + kind;
      body.innerHTML = '';
      body.appendChild(GhostUI.h('div', { className: 'fv-loading' }, GhostUI.h('span', { className: 'spinner' })));
      let pv = null;
      try { pv = await GhostAPI.proxyGet('/v1/files/' + encodeURIComponent(f.id) + '/preview'); }
      catch (e) { pv = { previewable: false, reason: 'Ghost couldn’t open a preview just now.' }; }
      if (my !== token || !viewer) return;
      body.innerHTML = '';
      if (isPdfPreview(pv, f)) {
        showPdf(body, f, pv, my);
        return;
      }
      if (pv && pv.previewable && pv.image_base64) {
        const img = GhostUI.h('img', { className: 'fv-photo', alt: f.name, src: 'data:' + (pv.mime || 'image/jpeg') + ';base64,' + pv.image_base64 });
        body.appendChild(img);
      } else if (pv && pv.previewable && typeof pv.content === 'string') {
        if (pv.extracted) body.appendChild(GhostUI.h('div', { className: 'fv-note' }, 'The text Ghost can read in this file'));
        body.appendChild(GhostUI.h('pre', { className: 'fv-text' + (f.kind === 'text' ? ' mono' : '') }, pv.content));
        if (pv.truncated) body.appendChild(GhostUI.h('div', { className: 'fv-note' }, 'Showing the start of a long file. Download it to read the rest.'));
      } else {
        const big = GhostUI.h('div', { className: 'fv-none' });
        big.appendChild(GhostUI.h('div', { className: 'fv-none-art file-tile', html: fileGlyph(kind) }));
        big.appendChild(GhostUI.h('div', { className: 'fv-none-ext' }, fileExt(f.name) || FILE_KIND_WORD[kind]));
        big.appendChild(GhostUI.h('p', {}, (pv && pv.reason) || 'There is no preview for this kind of file.'));
        body.appendChild(big);
      }
    }

    function go(d) {
      const n = idx + d;
      if (n < 0 || n >= list.length) return;
      idx = n;
      show();
    }

    // A PDF as its pages: each drawn on the Pod as printed, paged here. If
    // the Pod cannot draw them (a scan-only PDF, a missing renderer), the
    // extracted text below is the honest fallback, same as the app.
    async function showPdf(host, f, pv, my) {
      let page = 1, pages = 0;
      const wrap = GhostUI.h('div', { className: 'fv-pdf' });
      const pager = GhostUI.h('div', { className: 'fv-actions fv-pager' });
      const prevB = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, '‹ Prev page');
      const count = GhostUI.h('div', { className: 'fv-count' });
      const nextB = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Next page ›');
      pager.appendChild(prevB); pager.appendChild(count); pager.appendChild(nextB);
      const sheet = GhostUI.h('div', { className: 'fv-sheet' });
      wrap.appendChild(pager); wrap.appendChild(sheet);
      host.appendChild(wrap);

      function drawPager() {
        count.textContent = pages > 0 ? ('Page ' + page + ' of ' + pages) : ('Page ' + page);
        prevB.disabled = page <= 1;
        nextB.disabled = pages > 0 && page >= pages;
        pager.style.display = (pages <= 1 && page === 1 && sheet.firstChild) ? 'none' : '';
      }
      async function draw() {
        const mine = ++token;
        sheet.innerHTML = '';
        sheet.appendChild(GhostUI.h('div', { className: 'fv-loading' }, GhostUI.h('span', { className: 'spinner' })));
        drawPager();
        let r = null;
        try { r = await GhostAPI.proxyGet('/v1/files/' + encodeURIComponent(f.id) + '/page?n=' + page + '&w=1400'); }
        catch (e) { r = null; }
        if (mine !== token || my !== token || !viewer) return;
        sheet.innerHTML = '';
        if (r && r.ok && r.image_base64) {
          pages = r.pages || pages;
          sheet.appendChild(GhostUI.h('img', { className: 'fv-photo', alt: f.name + ' — page ' + page, src: 'data:image/png;base64,' + r.image_base64 }));
        } else {
          // The pages would not draw: fall back to the extracted text.
          if (pv && typeof pv.content === 'string' && pv.content) {
            if (pv.extracted) sheet.appendChild(GhostUI.h('div', { className: 'fv-note' }, 'The pages wouldn’t draw, so here is the text Ghost can read in this file'));
            sheet.appendChild(GhostUI.h('pre', { className: 'fv-text' }, pv.content));
            if (pv.truncated) sheet.appendChild(GhostUI.h('div', { className: 'fv-note' }, 'Showing the start of a long file. Download it to read the rest.'));
          } else {
            sheet.appendChild(GhostUI.h('div', { className: 'fv-note' }, 'The pages wouldn’t draw. Download it to read it.'));
          }
        }
        drawPager();
      }
      prevB.addEventListener('click', () => { if (page > 1) { page--; draw(); } });
      nextB.addEventListener('click', () => { page++; draw(); });
      draw();
    }

    dl.addEventListener('click', async () => {
      const f = list[idx];
      dl.disabled = true; const label = dl.textContent; dl.textContent = 'Preparing…';
      try {
        const r = await GhostAPI.proxyGet('/v1/files/' + encodeURIComponent(f.id) + '/content');
        const bin = atob(r.base64 || '');
        const bytes = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
        const url = URL.createObjectURL(new Blob([bytes], { type: r.mime || 'application/octet-stream' }));
        const a = document.createElement('a');
        a.href = url; a.download = r.name || f.name; document.body.appendChild(a); a.click(); a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 4000);
      } catch (e) { GhostUI.toast('Couldn’t download that.', 'err'); }
      dl.disabled = false; dl.textContent = label;
    });

    del.addEventListener('click', async () => {
      const f = list[idx];
      const ok = await GhostUI.confirmModal('Delete “' + f.name + '”?', 'It is removed from this device. This can’t be undone.', 'Delete');
      if (!ok) return;
      try {
        await GhostAPI.proxyDel('/v1/files/' + encodeURIComponent(f.id));
        GhostUI.toast('Deleted');
        closeViewer();
        loadFiles(container);
      } catch (e) { GhostUI.toast('Couldn’t delete that.', 'err'); }
    });

    const onKey = (e) => {
      if (document.querySelector('.ghost-modal-backdrop')) return; // a confirm is open
      if (e.key === 'Escape') closeViewer();
      else if (e.key === 'ArrowLeft') go(-1);
      else if (e.key === 'ArrowRight') go(1);
    };
    document.addEventListener('keydown', onKey);
    window.addEventListener('hashchange', closeViewer, { once: true });
    viewer = { root, onKey };
    show();
    root.focus({ preventScroll: true });
  }

  drawSeg();
  drawGrid();
}

GhostApp.registerSection('files', loadFiles);
