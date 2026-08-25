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

  return { el, h, ghostMark, statusDot, badge, btn, input, textarea, select, toggle, row, linkRow, sectionGroup, emptyState, loading, errorState, modal, toast, confirmModal };
})();
