/* Ghost UI Components — reusable DOM builders */
'use strict';

const GhostUI = (() => {
  function el(tag, attrs, ...children) {
    const e = document.createElement(tag);
    if (attrs) {
      for (const [k, v] of Object.entries(attrs)) {
        if (k === 'className') e.className = v;
        else if (k === 'html') e.innerHTML = v;
        else if (k.startsWith('on')) e.addEventListener(k.slice(2).toLowerCase(), v);
        else if (k === 'dataset') Object.assign(e.dataset, v);
        else e.setAttribute(k, v);
      }
    }
    for (const c of children) {
      if (c == null) continue;
      if (typeof c === 'string' || typeof c === 'number') e.appendChild(document.createTextNode(c));
      else if (c instanceof Node) e.appendChild(c);
      else if (Array.isArray(c)) c.forEach(ch => { if (ch instanceof Node) e.appendChild(ch); });
    }
    return e;
  }

  const h = el;

  function ghostMark(size) {
    const cls = size ? `ghost-mark ghost-mark-${size}` : 'ghost-mark';
    return h('span', { className: cls, html: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C7.58 2 4 5.58 4 10v10c0 1.1.9 2 2 2h1c.55 0 1-.45 1-1v-6h6v6c0 .55.45 1 1 1h1c1.1 0 2-.9 2-2V10c0-4.42-3.58-8-8-8zm-2 9a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3zm4 0a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3z"/></svg>' });
  }

  function statusDot(state) {
    return h('span', { className: `status-dot ${state}` });
  }

  function badge(text, variant) {
    return h('span', { className: `ghost-badge ghost-badge-${variant || 'neutral'}` }, text);
  }

  function btn(label, variant, onClick) {
    const cls = `ghost-btn ghost-btn-${variant || 'primary'}`;
    const b = h('button', { className: cls, onClick }, label);
    return b;
  }

  function input(placeholder, type) {
    return h('input', { className: 'ghost-input', placeholder, type: type || 'text' });
  }

  function textarea(placeholder) {
    return h('textarea', { className: 'ghost-input', placeholder });
  }

  function select(options, selected) {
    const s = h('select', { className: 'ghost-select' });
    for (const opt of options) {
      const o = h('option', { value: opt.value }, opt.label);
      if (opt.value === selected) o.selected = true;
      s.appendChild(o);
    }
    return s;
  }

  function toggle(on, onChange) {
    const t = h('div', { className: `ghost-toggle ${on ? 'on' : ''}` });
    t.addEventListener('click', () => {
      t.classList.toggle('on');
      onChange(t.classList.contains('on'));
    });
    return t;
  }

  function row(title, subtitle, trailing) {
    const r = h('div', { className: 'ghost-row' });
    const content = h('div', { className: 'ghost-row-content' });
    content.appendChild(h('div', { className: 'ghost-row-title' }, title));
    if (subtitle) content.appendChild(h('div', { className: 'ghost-row-subtitle' }, subtitle));
    r.appendChild(content);
    if (trailing) r.appendChild(h('div', { className: 'ghost-row-trailing' }, trailing));
    return r;
  }

  function linkRow(title, subtitle, onClick) {
    const r = h('div', { className: 'ghost-link-row', onClick });
    const content = h('div', { className: 'ghost-row-content' });
    content.appendChild(h('div', { className: 'ghost-row-title' }, title));
    if (subtitle) content.appendChild(h('div', { className: 'ghost-row-subtitle' }, subtitle));
    r.appendChild(content);
    r.appendChild(h('span', { className: 'chevron' }, '\u203A'));
    return r;
  }

  function sectionGroup(label, ...items) {
    const g = h('div', { className: 'section-group' });
    if (label) g.appendChild(h('div', { className: 'section-label' }, label));
    const list = h('div', { className: 'ghost-list' });
    items.forEach(i => list.appendChild(i));
    g.appendChild(list);
    return g;
  }

  function emptyState(title, text) {
    const e = h('div', { className: 'empty-state' });
    e.appendChild(h('div', { className: 'empty-state-title' }, title));
    if (text) e.appendChild(h('div', { className: 'empty-state-text' }, text));
    return e;
  }

  function loading(text) {
    return h('div', { className: 'loading' },
      h('div', { className: 'spinner' }),
      text || 'Loading\u2026'
    );
  }

  function errorState(title, text) {
    const e = h('div', { className: 'error-state' });
    e.appendChild(h('div', { className: 'error-state-title' }, title));
    e.appendChild(h('div', { className: 'error-state-text' }, text));
    return e;
  }

  function modal(title, body, actions) {
    const backdrop = h('div', { className: 'ghost-modal-backdrop' });
    const m = h('div', { className: 'ghost-modal' });
    m.appendChild(h('div', { className: 'ghost-modal-title' }, title));
    if (body) {
      const b = h('div', { className: 'ghost-modal-body' });
      if (typeof body === 'string') b.textContent = body;
      else b.appendChild(body);
      m.appendChild(b);
    }
    if (actions) {
      const a = h('div', { className: 'ghost-modal-actions' });
      actions.forEach(act => a.appendChild(act));
      m.appendChild(a);
    }
    backdrop.appendChild(m);
    backdrop.addEventListener('click', (e) => { if (e.target === backdrop) backdrop.remove(); });
    document.body.appendChild(backdrop);
    return backdrop;
  }

  function toast(msg, duration) {
    let container = document.querySelector('.ghost-toast-container');
    if (!container) {
      container = h('div', { className: 'ghost-toast-container' });
      document.body.appendChild(container);
    }
    const t = h('div', { className: 'ghost-toast' }, msg);
    container.appendChild(t);
    setTimeout(() => t.remove(), duration || 3000);
  }

  function confirmModal(title, message, confirmLabel, onConfirm) {
    return modal(title, message, [
      h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => { e.target.closest('.ghost-modal-backdrop').remove(); } }, 'Cancel'),
      h('button', { className: 'ghost-btn ghost-btn-danger', onClick: (e) => { e.target.closest('.ghost-modal-backdrop').remove(); onConfirm(); } }, confirmLabel || 'Confirm')
    ]);
  }

  // ── Formatting helpers ──
  function fmtNum(n) { return (n == null ? '0' : n).toLocaleString('en-US'); }

  function timeAgo(unixSec) {
    if (!unixSec) return 'never';
    const diff = Math.floor(Date.now() / 1000) - unixSec;
    if (diff < 0) return 'just now';
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    const d = Math.floor(diff / 86400);
    if (d < 30) return d + 'd ago';
    return new Date(unixSec * 1000).toLocaleDateString();
  }

  function clockTime(unixSec) {
    if (!unixSec) return '';
    return new Date(unixSec * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  function dayLabel(unixSec) {
    const d = new Date(unixSec * 1000);
    const today = new Date();
    if (d.toDateString() === today.toDateString()) return 'Today';
    const y = new Date(); y.setDate(y.getDate() - 1);
    if (d.toDateString() === y.toDateString()) return 'Yesterday';
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  }

  // Markdown → HTML (escapes input first). Supports headings, paragraphs,
  // bold, italic, inline code, fenced code blocks, ordered & unordered lists,
  // blockquotes, horizontal rules, tables, and links.
  function md(src) {
    const esc = (s) => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
    const lines = (src || '').replace(/\r\n/g, '\n').split('\n');
    let html = '', i = 0, inCode = false, listKind = null, blockquoteOpen = false;
    const inline = (t) => esc(t)
      .replace(/`([^`]+)`/g, (_, c) => '<code>' + c + '</code>')
      .replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
      .replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>')
      .replace(/\[([^\]]+)\]\((https?:[^)]+)\)/g, (_, c, url) => '<a href="' + url + '" target="_blank" rel="noopener">' + c + '</a>');
    const closeLists = () => { if (listKind) { html += listKind === 'ol' ? '</ol>' : '</ul>'; listKind = null; } };
    const closeBlockquote = () => { if (blockquoteOpen) { html += '</blockquote>'; blockquoteOpen = false; } };

    while (i < lines.length) {
      const line = lines[i];

      // Fenced code block
      if (/^```/.test(line)) {
        closeLists(); closeBlockquote();
        if (inCode) { html += '</code></pre>'; inCode = false; }
        else { html += '<pre><code>'; inCode = true; }
        i++; continue;
      }
      if (inCode) { html += esc(line) + '\n'; i++; continue; }

      // Horizontal rule
      if (/^\s*(---+|\*\*\*+|___+)\s*$/.test(line)) {
        closeLists(); closeBlockquote();
        html += '<hr />';
        i++; continue;
      }

      // Headings
      if (/^#### /.test(line)) { closeLists(); closeBlockquote(); html += '<h4>' + inline(line.slice(5)) + '</h4>'; i++; continue; }
      if (/^### /.test(line)) { closeLists(); closeBlockquote(); html += '<h3>' + inline(line.slice(4)) + '</h3>'; i++; continue; }
      if (/^## /.test(line)) { closeLists(); closeBlockquote(); html += '<h2>' + inline(line.slice(3)) + '</h2>'; i++; continue; }
      if (/^# /.test(line)) { closeLists(); closeBlockquote(); html += '<h1>' + inline(line.slice(2)) + '</h1>'; i++; continue; }

      // Blockquote
      if (/^>\s?/.test(line)) {
        closeLists();
        if (!blockquoteOpen) { html += '<blockquote>'; blockquoteOpen = true; }
        html += '<p>' + inline(line.replace(/^>\s?/, '')) + '</p>';
        i++; continue;
      } else { closeBlockquote(); }

      // Unordered list
      if (/^\s*[-*] /.test(line)) {
        if (listKind !== 'ul') { closeLists(); html += '<ul>'; listKind = 'ul'; }
        html += '<li>' + inline(line.replace(/^\s*[-*] /, '')) + '</li>';
        i++; continue;
      }

      // Ordered list
      if (/^\s*\d+\.\s+/.test(line)) {
        if (listKind !== 'ol') { closeLists(); html += '<ol>'; listKind = 'ol'; }
        html += '<li>' + inline(line.replace(/^\s*\d+\.\s+/, '')) + '</li>';
        i++; continue;
      }

      // Table (simple | col | col | with --- separator)
      if (/\|/.test(line) && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(lines[i + 1])) {
        closeLists();
        const cells = line.split('|').map(c => c.trim());
        if (cells[0] === '') cells.shift();
        if (cells[cells.length - 1] === '') cells.pop();
        html += '<table><thead><tr>';
        for (const c of cells) html += '<th>' + inline(c) + '</th>';
        html += '</tr></thead><tbody>';
        i += 2;
        while (i < lines.length && /\|/.test(lines[i]) && lines[i].trim() !== '') {
          const row = lines[i].split('|').map(c => c.trim());
          if (row[0] === '') row.shift();
          if (row[row.length - 1] === '') row.pop();
          html += '<tr>';
          for (const c of row) html += '<td>' + inline(c) + '</td>';
          html += '</tr>';
          i++;
        }
        html += '</tbody></table>';
        continue;
      }

      // Blank line
      if (line.trim() === '') { closeLists(); i++; continue; }

      // Paragraph
      closeLists();
      html += '<p>' + inline(line) + '</p>';
      i++;
    }
    closeLists(); closeBlockquote();
    if (inCode) html += '</code></pre>';
    return html;
  }

  return { el, h, ghostMark, statusDot, badge, btn, input, textarea, select, toggle, row, linkRow, sectionGroup, emptyState, loading, errorState, modal, toast, confirmModal, fmtNum, timeAgo, clockTime, dayLabel, md };
})();
