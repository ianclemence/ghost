/* Ghost Section: Memory \u2014 what Ghost remembers about you. */
'use strict';

async function loadMemory(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Memory'));
  const sub = GhostUI.h('p', {}, 'Facts Ghost has learned about you, plus a search over past conversations.');
  head.appendChild(sub);
  container.appendChild(head);

  const [selfRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/memory/self'),
  ]);
  if (!document.body.contains(container)) return;

  renderRecall(container);

  const self = selfRes.status === 'fulfilled' ? (selfRes.value || {}) : {};
  const rawFacts = Array.isArray(self.entries) ? self.entries : [];
  // Present the canonical memory collection — duplicate extractions that
  // represent the same fact collapse into one visible memory.
  const facts = GhostSemantic.canonicalizeEntries(rawFacts);
  // Show only the model's own curated notes here. The user-profile lines
  // (self.you) are derived from the same structured facts already shown in the
  // profile hero, so including them would just duplicate the facts above.
  const curated = (Array.isArray(self.notes) ? self.notes : []);

  // One calm empty state when there's nothing at all.
  if (facts.length === 0 && curated.length === 0) {
    container.appendChild(GhostUI.emptyState('Ghost is still getting to know you', 'Talk to Ghost and it will remember the things that matter about you here.'));
    return;
  }

  if (facts.length || curated.length) {
    try { renderFacts(container, facts, curated); }
    catch (e) {
      console.error('renderFacts failed', e);
      container.appendChild(GhostUI.errorState('Couldn\u2019t show these memories', String(e && e.message || e)));
    }
  }
}

// renderFacts presents the structured state Ghost learns from conversation as a
// calm profile hero — grouped by kind, value-first, with provenance and quiet
// reinforcement, plus any curated notes.
function renderFacts(container, facts, curated) {
  const panel = GhostUI.h('div', { className: 'panel' });
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const t = GhostUI.h('div');
  t.appendChild(GhostUI.h('h2', {}, 'What Ghost knows about you'));
  t.appendChild(GhostUI.h('p', {}, 'Learned from your conversations. Forget anything below and it won\u2019t be used anymore.'));
  ph.appendChild(t);
  panel.appendChild(ph);
  const body = GhostUI.h('div', { className: 'self-body' });
  panel.appendChild(body);
  container.appendChild(panel);

  const forgetBtn = async (payload) => {
    try {
      if (payload && payload.id) payload = Object.assign({ reason: 'forgotten from the Memory screen' }, payload);
      await GhostAPI.proxyPost('/v1/memory/self/forget', payload);
      GhostUI.toast('Forgotten');
      loadMemory(container);
    } catch (e) { GhostUI.toast('Couldn\u2019t forget that.', 'err'); }
  };

  // Grouped by what a memory *is*, in the order a person thinks of their own
  // life: who they are, the people around them, what is coming up, what they
  // have and can do, what they care about, what they are working toward.
  const KIND_ORDER = ['identity', 'person', 'relationship', 'event', 'health', 'possession', 'skill', 'preference', 'interest', 'opinion', 'routine', 'goal', 'project', 'decision', 'constraint', 'fact'];
  const KIND_LABEL = {
    identity: 'Identity', person: 'People in your life', relationship: 'People', event: 'Dates and events',
    health: 'Health (private)', possession: 'What you own', skill: 'What you can do',
    preference: 'Preferences', interest: 'Interests', opinion: 'Views', routine: 'Routines',
    goal: 'Goals', project: 'Projects', decision: 'Decisions', constraint: 'Rules you live by', fact: 'About you',
  };
  const kindOf = (k) => (KIND_LABEL[k] ? k : 'fact');
  const byKind = {};
  facts.forEach(e => {
    const k = kindOf(e.kind);
    (byKind[k] = byKind[k] || []).push(e);
  });

  function metaFor(e) {
    const parts = [];
    if (e.created_at) parts.push('Learned ' + GhostUI.timeAgo(Math.floor(new Date(e.created_at).getTime() / 1000)));
    if (e.reinforce_count > 1) parts.push('confirmed ' + e.reinforce_count + '\u00d7');
    return parts.join('  \u00b7  ');
  }

  // explainInto renders the receipt for one memory: the owner's own words,
  // how confident Ghost is, where it came from, and whether it was replaced
  // or forgotten. Plain language — this is the trust feature, not a debug view.
  async function explainInto(host, id) {
    host.innerHTML = '';
    host.appendChild(GhostUI.loading('Checking\u2026'));
    let res;
    try { res = await GhostAPI.proxyGet('/v1/memory/explain?id=' + encodeURIComponent(id)); }
    catch (err) {
      host.innerHTML = '';
      host.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Couldn\u2019t fetch the receipt right now.'));
      return;
    }
    const ex = (res && res.explanation) || {};
    host.innerHTML = '';
    if (ex.quote) {
      host.appendChild(GhostUI.h('div', { className: 'type-body', style: 'margin-bottom:var(--s-1)' }, 'You said: \u201c' + ex.quote + '\u201d'));
    } else {
      host.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-1)' }, 'Saved before Ghost kept quotes, so there are no exact words on file.'));
    }
    const bits = [];
    if (typeof ex.confidence === 'number' && ex.confidence > 0) bits.push(Math.round(ex.confidence * 100) + '% confident');
    if (ex.entry && ex.entry.created_at) bits.push('learned ' + GhostUI.timeAgo(Math.floor(new Date(ex.entry.created_at).getTime() / 1000)));
    if (ex.message_ids && ex.message_ids.length) bits.push('from message ' + ex.message_ids.join(', '));
    if (bits.length) host.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, bits.join('  \u00b7  ')));
    if (ex.superseded_by) host.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Replaced by a newer memory: ' + (ex.superseded_by.value || '')));
    if (ex.forgotten_at) host.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Forgotten' + (ex.forgotten_reason ? ' \u2014 ' + ex.forgotten_reason : '') + '.'));
  }

  function factRow(e) {
    const wrap = GhostUI.h('div', { className: 'self-entry-wrap' });
    const row = GhostUI.h('div', { className: 'ghost-row self-entry' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const title = GhostSemantic.memoryTitleFor(e);
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:500' }, title));
    const sub = GhostUI.h('div', { className: 'ghost-row-subtitle' });
    const dom = GhostSemantic.inferDomainFromValue(e.kind, e.domain, e.value) || e.domain;
    const tag = GhostSemantic.domainTag(dom, e.kind);
    if (tag) sub.appendChild(GhostUI.h('span', { className: 'tag', style: 'text-transform:capitalize;font-size:0.75rem;padding:0.1em 0.4em;border-radius:var(--r-sm);background:var(--paper-sunken);color:var(--ink-muted);margin-right:var(--s-2)' }, tag));
    const meta = metaFor(e);
    if (e.about) sub.appendChild(document.createTextNode('About ' + e.about + '  \u00b7  '));
    if (meta) sub.appendChild(document.createTextNode(meta));
    if (e.valid_until) {
      const until = new Date(e.valid_until);
      if (!isNaN(until)) sub.appendChild(document.createTextNode('  \u00b7  until ' + until.toLocaleDateString([], { month: 'short', day: 'numeric' })));
    }
    if (e.sensitive) sub.appendChild(GhostUI.h('span', { className: 'tag', style: 'margin-left:var(--s-2);font-size:0.75rem;padding:0.1em 0.4em;border-radius:var(--r-sm);background:var(--warn-soft);color:var(--warn)' }, 'Private'));
    c.appendChild(sub);
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const why = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Why?');
    const detail = GhostUI.h('div', { className: 'receipt', style: 'display:none;padding:var(--s-2) var(--s-4) var(--s-3);background:var(--paper-sunken);border-radius:var(--r-sm);margin:0 0 var(--s-2)' });
    why.addEventListener('click', async () => {
      if (detail.style.display === 'none') {
        detail.style.display = 'block';
        why.textContent = 'Hide';
        await explainInto(detail, e.id);
      } else {
        detail.style.display = 'none';
        why.textContent = 'Why?';
      }
    });
    const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Forget');
    btn.addEventListener('click', () => forgetBtn({ id: e.id }));

    // Correct: change what Ghost believes without forgetting it. The old value
    // stays in its history; what is typed here becomes the receipt.
    const fix = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Correct');
    const editor = GhostUI.h('div', { className: 'receipt memory-editor', style: 'display:none' });
    fix.addEventListener('click', () => {
      if (editor.style.display !== 'none') { editor.style.display = 'none'; return; }
      editor.innerHTML = '';
      editor.style.display = 'block';
      editor.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' },
        'What is true now? Ghost keeps the old version in its history.'));
      const input = GhostUI.h('input', { className: 'ghost-input', type: 'text', maxlength: '300', 'aria-label': 'Corrected memory' });
      input.value = e.value || '';
      const save = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', type: 'button' }, 'Save');
      const cancel = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Cancel');
      cancel.addEventListener('click', () => { editor.style.display = 'none'; });
      const submit = async () => {
        const value = input.value.trim();
        if (!value || value === (e.value || '').trim()) { GhostUI.toast('Type what is true now.'); return; }
        save.disabled = true;
        try {
          await GhostAPI.proxyPost('/v1/memory/self/correct', { id: e.id, value: value });
          GhostUI.toast('Updated');
          loadMemory(container);
        } catch (err) { save.disabled = false; GhostUI.toast('Couldn\u2019t save that. It may have changed already.', 'err'); }
      };
      save.addEventListener('click', submit);
      input.addEventListener('keydown', (ev) => { if (ev.key === 'Enter') submit(); if (ev.key === 'Escape') editor.style.display = 'none'; });
      const actions = GhostUI.h('div', { className: 'memory-editor-actions' }, save, cancel);
      editor.appendChild(input);
      editor.appendChild(actions);
      input.focus();
    });
    tr.appendChild(why);
    tr.appendChild(fix);
    tr.appendChild(btn);
    row.appendChild(tr);
    wrap.appendChild(row);
    wrap.appendChild(detail);
    wrap.appendChild(editor);
    return wrap;
  }

  KIND_ORDER.forEach(k => {
    const list = byKind[k];
    if (!list || !list.length) return;
    body.appendChild(GhostUI.h('div', { className: 'self-group' }, KIND_LABEL[k]));
    list.sort((a, b) => (b.reinforce_count || 0) - (a.reinforce_count || 0));
    list.forEach(e => body.appendChild(factRow(e)));
  });

  if (curated.length) {
    body.appendChild(GhostUI.h('div', { className: 'self-group' }, 'What Ghost has learned'));
    curated.forEach(entry => {
      const row = GhostUI.h('div', { className: 'ghost-row self-entry' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:400' }, entry));
      row.appendChild(c);
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
      const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', type: 'button' }, 'Forget');
      btn.addEventListener('click', () => forgetBtn({ target: 'memory', entry: entry }));
      tr.appendChild(btn);
      row.appendChild(tr);
      body.appendChild(row);
    });
  }
}

// recallMsg renders a single recall message. Recall strings are "role: content";
// we split off the role label and render the content as markdown so headings,
// bold, lists, and links read properly instead of as raw "#" / "**" characters.
function recallMsg(m) {
  const idx = m.indexOf(': ');
  const role = idx > 0 ? m.slice(0, idx) : '';
  const content = idx > 0 ? m.slice(idx + 2) : m;
  const el = GhostUI.h('div', { className: 'recall-msg' });
  if (role) el.appendChild(GhostUI.h('div', { className: 'recall-msg-role' }, role));
  const md = GhostUI.h('div', { className: 'markdown-body' });
  md.innerHTML = GhostUI.md(content);
  el.appendChild(md);
  return el;
}

// renderRecall lets the user search Ghost's past conversations ("what did we
// talk about earlier?"). It summarizes across sessions with a cloud model when
// available and falls back to the raw matches offline.
async function renderRecall(container) {
  const panel = GhostUI.h('div', { className: 'panel' });
  const ph = GhostUI.h('div', { className: 'panel-head' });
  const t = GhostUI.h('div');
  t.appendChild(GhostUI.h('h2', {}, 'Recall'));
  t.appendChild(GhostUI.h('p', {}, 'Search what you and Ghost have talked about.'));
  ph.appendChild(t);
  panel.appendChild(ph);
  const wrap = GhostUI.h('div', { className: 'row-flex', style: 'gap:var(--s-2)' });
  const input = GhostUI.input('What did we talk about\u2026');
  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', type: 'button' }, 'Recall');
  wrap.appendChild(input);
  wrap.appendChild(btn);
  panel.appendChild(wrap);
  const body = GhostUI.h('div', { style: 'margin-top:var(--s-4)' });
  panel.appendChild(body);
  container.appendChild(panel);

  btn.addEventListener('click', async () => {
    const q = input.value.trim();
    if (!q) return;
    body.innerHTML = '';
    body.appendChild(GhostUI.loading('Recalling\u2026'));
    let res;
    try { res = await GhostAPI.proxyGet('/v1/recall?query=' + encodeURIComponent(q)); }
    catch (e) { body.innerHTML = ''; body.appendChild(GhostUI.errorState('Couldn\u2019t recall', 'Ghost may still be starting.')); return; }
    body.innerHTML = '';
    if (!res.sessions || res.sessions.length === 0) {
      body.appendChild(GhostUI.emptyState('Nothing found', 'No past conversation matched that.'));
      return;
    }
    const list = GhostUI.h('div', { className: 'ghost-list', id: 'recall-list' });
    if (res.summarized && res.summary) {
      const sum = GhostUI.h('div', { className: 'markdown-body', style: 'padding:var(--s-3) var(--s-4);background:var(--paper-sunken);border-radius:var(--r-sm);margin-bottom:var(--s-3)' });
      sum.innerHTML = GhostUI.md(res.summary);
      list.appendChild(sum);
    } else {
      list.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-3)' }, 'Offline recall (no cloud model) \u2014 raw matches:'));
    }
    res.sessions.forEach(sess => {
      const row = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      const firstUser = (sess.messages || []).find(m => (m.indexOf(': ') > 0 ? m.slice(0, m.indexOf(':')) : '') === 'user');
      const preview = firstUser ? firstUser.slice(firstUser.indexOf(': ') + 2) : '';
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-weight:500' }, GhostSemantic.conversationTitle(preview)));
      (sess.messages || []).forEach(m => c.appendChild(recallMsg(m)));
      row.appendChild(c);
      list.appendChild(row);
    });
    body.appendChild(list);
  });
}

GhostApp.registerSection('memory', loadMemory);
