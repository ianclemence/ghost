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

  // Ghost visual mark — the same artwork as the mobile app (GhostMark),
  // so the console and the phone present one identity. Colorized via
  // currentColor; sized by the .ghost-mark-* classes.
  function ghostMark(size) {
    const cls = size ? `ghost-mark ghost-mark-${size}` : 'ghost-mark';
    return h('span', { className: cls, html: '<svg viewBox="0 0 32 32" fill="none"><path fill="currentColor" d="M4.859 7.401v2.115h13.256v-4.231h-13.256v2.115zM22.91 7.401v2.115h4.231v-4.231h-4.231v2.115zM4.859 16.427v2.115h22.282v-4.231h-22.282v2.115zM4.859 25.311v1.974h8.744v-3.949h-8.744v1.975zM18.398 25.311v1.974h8.744v-3.949h-8.744v1.975z"/></svg>' });
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

  // modelFriendly turns a raw model id like "deepseek:deepseek-v4-flash" or
  // "ollama:qwen3:0.6b" into something a normal person understands. The friendly
  // name is the headline; the raw id is shown as a subtle secondary line.
  function modelFriendly(full) {
    const raw = (full || '').trim();
    let provider = '', model = raw;
    const sep = raw.indexOf(':');
    if (sep >= 0) { provider = raw.slice(0, sep); model = raw.slice(sep + 1); }
    const m = (model || '').toLowerCase();

    const provName = {
      openai: 'OpenAI', anthropic: 'Claude', moonshot: 'Kimi', groq: 'Groq',
      deepseek: 'DeepSeek', qwen: 'Qwen', gemini: 'Gemini', zhipu: 'Zhipu',
      openrouter: 'OpenRouter', nvidia: 'Nvidia', shengsuanyun: 'ShengSuanYun',
      ollama: 'Local', vllm: 'Local',
    }[provider] || (provider ? provider.charAt(0).toUpperCase() + provider.slice(1) : '');

    let name = 'Standard';
    if (provider === 'ollama' || provider === 'vllm') {
      const size = m.match(/(\d+(?:\.\d+)?)b/);
      if (size) {
        const n = parseFloat(size[1]);
        name = n <= 1 ? 'Local \u2014 small' : n <= 8 ? 'Local \u2014 medium' : 'Local \u2014 large';
      } else {
        name = 'Local';
      }
    } else if (/(vision|multimodal)/.test(m)) {
      name = 'Vision';
    } else if (/(flash|mini|haiku|nano|fast|lite|quick|small)/.test(m)) {
      name = 'Fast';
    } else if (/(pro|opus|sonnet|reason|thinking|o3|o4|large|max|ultra|extended)/.test(m)) {
      name = 'Thinking';
    }

    return { name, provider, model, raw, provName };
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

  const CLOSE_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M3.5 3.5l9 9M12.5 3.5l-9 9"/></svg>';
  let modalSeq = 0;

  // dismiss takes a modal away with a short fade, and hands focus back to
  // whatever opened it.
  function dismiss(backdrop) {
    if (!backdrop || !backdrop.isConnected || backdrop.classList.contains('is-leaving')) return;
    backdrop.classList.add('is-leaving');
    const opener = backdrop._opener;
    const done = () => { Element.prototype.remove.call(backdrop); if (opener && opener.isConnected) { try { opener.focus({ preventScroll: true }); } catch (e) {} } };
    if (window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches) done();
    else setTimeout(done, 130);
    document.removeEventListener('keydown', backdrop._onKey, true);
  }

  // mountModal is the one place a dialog is built, so modal and confirmModal
  // behave the same: labelled, closable with Escape or the X, focus kept inside,
  // focus returned on close.
  function mountModal(box, title, onClose, wide) {
    const backdrop = h('div', { className: 'ghost-modal-backdrop' });
    const id = 'ghost-modal-title-' + (++modalSeq);
    box.setAttribute('role', 'dialog');
    box.setAttribute('aria-modal', 'true');
    box.setAttribute('aria-labelledby', id);
    if (wide) box.classList.add('ghost-modal-wide');
    const heading = box.querySelector('.ghost-modal-title');
    if (heading) heading.id = id;
    const x = h('button', { className: 'ghost-modal-close', type: 'button', 'aria-label': 'Close' });
    x.innerHTML = CLOSE_ICON;
    box.insertBefore(x, box.firstChild);
    backdrop.appendChild(box);
    backdrop._opener = document.activeElement;
    // Every existing way of closing a modal (.remove()) gets the fade and the cleanup.
    backdrop.remove = () => dismiss(backdrop);
    const close = () => { dismiss(backdrop); if (onClose) onClose(); };
    x.addEventListener('click', close);
    backdrop.addEventListener('mousedown', (e) => { if (e.target === backdrop) close(); });
    backdrop._onKey = (e) => {
      if (e.key === 'Escape') { e.stopPropagation(); close(); return; }
      if (e.key !== 'Tab') return;
      const f = Array.from(box.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')).filter(el => !el.disabled && el.offsetParent !== null);
      if (!f.length) return;
      const first = f[0], last = f[f.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
    };
    document.addEventListener('keydown', backdrop._onKey, true);
    document.body.appendChild(backdrop);
    // Focus the first thing to type into, else the main action, else the dialog.
    const target = box.querySelector('input:not([type=hidden]), textarea, select') || box.querySelector('.ghost-btn-primary, .ghost-btn-danger') || x;
    setTimeout(() => { try { target.focus({ preventScroll: true }); } catch (e) {} }, 30);
    return backdrop;
  }

  function modal(title, body, actions, opts) {
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
    return mountModal(m, title, null, opts && opts.wide);
  }

  function toast(msg, variant, duration) {
    let container = document.querySelector('.ghost-toast-container');
    if (!container) {
      container = h('div', { className: 'ghost-toast-container' });
      document.body.appendChild(container);
    }
    const cls = variant === 'err' ? 'ghost-toast ghost-toast--err' : variant === 'ok' ? 'ghost-toast ghost-toast--ok' : 'ghost-toast';
    const t = h('div', { className: cls }, msg);
    container.appendChild(t);
    setTimeout(() => t.remove(), duration || 3000);
  }

  // confirmModal asks before something that matters. Pass tone 'primary' for a
  // question that isn't destructive; the default is the red button.
  function confirmModal(title, message, confirmLabel, tone) {
    return new Promise((resolve) => {
      const box = h('div', { className: 'ghost-modal' });
      box.appendChild(h('div', { className: 'ghost-modal-title' }, title));
      if (typeof message === 'string') {
        box.appendChild(h('div', { className: 'ghost-modal-body' }, message));
      } else if (message) {
        const b = h('div', { className: 'ghost-modal-body' });
        b.appendChild(message);
        box.appendChild(b);
      }
      let backdrop;
      const settle = (v) => { dismiss(backdrop); resolve(v); };
      const footer = h('div', { className: 'ghost-modal-footer' });
      footer.appendChild(h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button', onClick: () => settle(false) }, 'Cancel'));
      footer.appendChild(h('button', { className: 'ghost-btn ' + (tone === 'primary' ? 'ghost-btn-primary' : 'ghost-btn-danger'), type: 'button', onClick: () => settle(true) }, confirmLabel || 'Confirm'));
      box.appendChild(footer);
      backdrop = mountModal(box, title, () => resolve(false));
    });
  }

  // downloadBackup downloads an encrypted Ghost State archive. Shared by
  // the Security section and the Home "Back up your Ghost" surface. The
  // passphrase encrypts the archive; without it the file cannot be
  // restored, so it is confirmed twice and never sent anywhere else.
  async function downloadBackup(btn) {
    const orig = btn.textContent;
    const body = h('div');
    body.appendChild(h('p', { className: 'type-callout text-secondary', style: 'margin-bottom:var(--s-4)' }, 'Choose a passphrase to encrypt this backup. You will need it to restore. There is no way to recover a forgotten passphrase.'));
    const pw1 = input('Backup passphrase (at least 8 characters)', 'password');
    pw1.style.marginBottom = 'var(--s-2)';
    const pw2 = input('Repeat the passphrase', 'password');
    pw2.style.marginBottom = 'var(--s-2)';
    const err = h('div', { className: 'type-foot', style: 'color:var(--bad);min-height:18px' });
    body.appendChild(pw1);
    body.appendChild(pw2);
    body.appendChild(err);
    const backdrop = modal('Download backup', body, [
      h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
      h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
        err.textContent = '';
        if (pw1.value.length < 8) { err.textContent = 'At least 8 characters.'; return; }
        if (pw1.value !== pw2.value) { err.textContent = 'Passphrases don\u2019t match.'; return; }
        const dl = e.target;
        dl.disabled = true;
        try {
          const res = await fetch('/api/admin/backup', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ passphrase: pw1.value }) });
          if (!res.ok) {
            const j = await res.json().catch(() => null);
            throw new Error((j && j.error) || 'failed');
          }
          const blob = await res.blob();
          const url = URL.createObjectURL(blob);
          const a = h('a');
          a.href = url;
          a.download = 'ghost-state-' + new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-') + '.ghost';
          document.body.appendChild(a); a.click(); a.remove();
          URL.revokeObjectURL(url);
          e.target.closest('.ghost-modal-backdrop').remove();
          toast('Backup downloaded \u2014 store it somewhere safe');
        } catch (ex) {
          err.textContent = ex.message || 'Couldn\u2019t create the backup.';
          dl.disabled = false;
        }
      } }, 'Download'),
    ]);
    setTimeout(() => pw1.focus(), 50);
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
    const when = new Date(unixSec * 1000);
    const sameYear = when.getFullYear() === new Date().getFullYear();
    return when.toLocaleDateString([], sameYear ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' });
  }

  // Outcome words for an activity state, from the runtime's own record.
  // One vocabulary for every surface (the phone app uses the same words).
  const ACTIVITY_WORDS = {
    running: ['Working', 'neutral'], waiting: ['Waiting on you', 'warn'], pending: ['Waiting on you', 'warn'],
    success: ['Done', 'ready'], done: ['Done', 'ready'], completed: ['Done', 'ready'], succeeded: ['Done', 'ready'],
    verified: ['Verified', 'ready'], changed: ['Changed', 'warn'], unchanged: ['No change', 'neutral'],
    denied: ['You said no', 'neutral'], failed: ['Didn\u2019t work', 'bad'], error: ['Didn\u2019t work', 'bad'],
    cancelled: ['Stopped', 'neutral'], paused: ['Paused', 'neutral'],
  };
  function activityWord(state) {
    const w = ACTIVITY_WORDS[(state || '').toLowerCase()];
    return w ? w[0] : '';
  }
  function activityTone(state) {
    const w = ACTIVITY_WORDS[(state || '').toLowerCase()];
    return w ? w[1] : 'neutral';
  }

  function clockTime(unixSec) {
    if (!unixSec) return '';
    return new Date(unixSec * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }

  // stripFrontmatter splits a SKILL.md / doc string into its YAML frontmatter
  // block (a leading "---\n ... \n---") and the body. Docs carry frontmatter
  // that must not render as markdown, so callers render `body` and use `meta`
  // (as key: value text) for a styled header.
  function stripFrontmatter(src) {
    const s = (src || '').replace(/^\uFEFF/, '').trimStart();
    if (s.startsWith('---')) {
      const end = s.indexOf('\n---', 3);
      if (end >= 0) {
        return { meta: s.slice(3, end).trim(), body: s.slice(end + 4).trim() };
      }
    }
    return { meta: '', body: s };
  }

  // frontmatterValue pulls a simple "key: value" field out of a frontmatter blob.
  function frontmatterValue(meta, key) {
    if (!meta) return '';
    const re = new RegExp('(?:^|\\n)' + key + '\\s*:\\s*([^\\n]+)');
    const m = meta.match(re);
    return m ? m[1].trim().replace(/["']/g, '') : '';
  }

  function dayLabel(unixSec) {
    const d = new Date(unixSec * 1000);
    const today = new Date();
    if (d.toDateString() === today.toDateString()) return 'Today';
    const y = new Date(); y.setDate(y.getDate() - 1);
    if (d.toDateString() === y.toDateString()) return 'Yesterday';
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  }

  // Markdown → HTML (escapes input first; model/content text is untrusted).
  // Supports headings, paragraphs, bold, italic, strikethrough, inline code,
  // fenced code blocks, ordered & unordered lists (incl. GFM task items),
  // nested blockquotes, horizontal rules, tables, links, bare-URL autolinks,
  // and image alt-text badges (images are never fetched).
  //
  // Two invariants keep this safe:
  //  1. escape FIRST — every byte of content is HTML-escaped before any
  //     markup is recognized, so raw HTML can never execute.
  //  2. rendered fragments are STASHED behind placeholders — a link or code
  //     span cannot be re-matched by a later rule, so `[x](url)` inside
  //     `backticks` stays literal code and a URL inside an href is never
  //     auto-linked twice.
  function md(src) {
    // Escape quotes as well as angle brackets: content quotes must never be
    // able to terminate a generated attribute (href="...").
    const esc = (s) => s.replace(/[&<>"']/g, c => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[c]));
    const lines = (src || '').replace(/\r\n/g, '\n').split('\n');
    let html = '', i = 0, inCode = false, listKind = null, blockquoteOpen = false;
    const inline = (t) => {
      const stash = [];
      const keep = (frag) => { stash.push(frag); return '\u0000' + (stash.length - 1) + '\u0000'; };
      // A URL is only ever http(s) and stops at the first whitespace or
      // quote, so nothing can be smuggled into the href attribute.
      const safeUrl = (u) => {
        const cut = String(u).split(/[\s"'<>]/)[0];
        return /^https?:\/\/[^/]/i.test(cut) ? cut : '';
      };
      const anchor = (url, label) => {
        const href = safeUrl(url);
        if (!href) return label;
        return '<a href="' + href + '" target="_blank" rel="noopener">' + label + '</a>';
      };
      let s = esc(t);
      // Code spans first, stashed: literals win.
      s = s.replace(/`([^`]+)`/g, (_, c) => keep('<code>' + c + '</code>'));
      // Images before links: alt-text badge only, never a remote fetch.
      s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g,
        (_, alt) => keep('<span class="md-image">◈ ' + (alt || 'image') + '</span>'));
      // Explicit links, stashed so their URL is not auto-linked again.
      s = s.replace(/\[([^\]]+)\]\((https?:[^)]+)\)/g, (_, c, url) => keep(anchor(url, c)));
      // Bare-URL autolinks.
      s = s.replace(/(^|[\s(])(https?:\/\/[^\s<)]+)/g, (m, pre, url) => pre + keep(anchor(url, url)));
      // Emphasis.
      s = s.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
      s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
      s = s.replace(/~~([^~\n]+)~~/g, '<del>$1</del>');
      return s.replace(/\u0000(\d+)\u0000/g, (_, n) => stash[Number(n)] ?? '');
    };
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

      // Blockquote (any nesting depth collapses to one level, like the CLI)
      if (/^\s*>\s?/.test(line)) {
        closeLists();
        if (!blockquoteOpen) { html += '<blockquote>'; blockquoteOpen = true; }
        html += '<p>' + inline(line.replace(/^(\s*>\s?)+/, '')) + '</p>';
        i++; continue;
      } else { closeBlockquote(); }

      // Unordered list (with optional GFM task marker)
      const ul = line.match(/^\s*[-*] (\[([ xX])\] )?(.*)$/);
      if (ul) {
        if (listKind !== 'ul') { closeLists(); html += '<ul>'; listKind = 'ul'; }
        const done = ul[2] && ul[2].toLowerCase() === 'x';
        const box = ul[2] === undefined ? '' : (done ? '☑ ' : '☐ ');
        const cls = ul[2] === undefined ? '' : (done ? ' class="task done"' : ' class="task"');
        html += '<li' + cls + '>' + box + inline(ul[3]) + '</li>';
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

  return { el, h, ghostMark, statusDot, badge, btn, input, textarea, select, toggle, row, linkRow, sectionGroup, emptyState, loading, errorState, modal, dismiss, toast, confirmModal, downloadBackup, fmtNum, activityWord,
    activityTone,
    timeAgo, clockTime, dayLabel, md, modelFriendly, stripFrontmatter, frontmatterValue };
})();
