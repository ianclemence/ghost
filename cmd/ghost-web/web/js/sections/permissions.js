/* Ghost Section: Permissions — what Ghost is allowed to do. */
'use strict';

async function loadPermissions(container, opts) {
  const embedded = !!(opts && opts.embedded);
  container.innerHTML = '';
  if (!embedded) {
    const head = GhostUI.h('div', { className: 'page-head' });
    head.appendChild(GhostUI.h('h1', {}, 'Permissions'));
    head.appendChild(GhostUI.h('p', {}, 'Approvals waiting for your decision, and permissions you\u2019ve made standing — revoke any time.'));
    container.appendChild(head);
  }

  const pendingEl = GhostUI.h('div', { id: 'perm-pending' });
  pendingEl.appendChild(GhostUI.loading('Checking for approval requests\u2026'));
  container.appendChild(pendingEl);

  const grantsEl = GhostUI.h('div', { id: 'perm-grants' });
  container.appendChild(grantsEl);

  async function refresh() {
    if (!document.body.contains(container)) return;
    let pending = [], grants = [];
    try {
      const pr = await GhostAPI.proxyGet('/v1/permissions/requests?status=pending');
      pending = pr.requests || [];
    } catch (e) { pending = 'error'; }
    try {
      const gr = await GhostAPI.proxyGet('/v1/permissions/grants');
      grants = gr.grants || [];
    } catch (e) { grants = 'error'; }
    if (!document.body.contains(container)) return;
    paintPending(pendingEl, pending, refresh);
    paintGrants(grantsEl, grants, refresh);
  }
  await refresh();
}

function groupPanel() {
  const panel = GhostUI.h('div', { className: 'panel' });
  return panel;
}

function groupHead(title, sub) {
  const head = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, title));
  if (sub) text.appendChild(GhostUI.h('p', {}, sub));
  head.appendChild(text);
  return head;
}

// riskWords translates the broker's risk taxonomy into owner language.
// Capability and action ids never reach the screen.
function riskWords(risk) {
  switch (String(risk || '')) {
    case 'read_only': return 'Only reads \u2014 changes nothing';
    case 'low_risk': return 'Low risk';
    case 'high_impact': return 'Can affect the outside world';
    case 'consequential': return 'Needs your approval';
    default: return 'Needs your approval';
  }
}

// permTitle mirrors the backend ApprovalCard vocabulary (cardTitle in
// pkg/permissions) for places that have no card — standing grants. New
// capability families fall back to plain action words, never raw ids.
function permTitle(capability, action) {
  const cap = String(capability || '');
  let act = String(action || '');
  const ci = act.indexOf(':');
  if (ci >= 0) act = act.slice(ci + 1);
  const has = (...words) => words.some(w => cap.includes(w));
  if (has('calendar')) return (/create|add/.test(act) ? 'Add calendar events' : 'Use your calendar');
  if (has('telegram', 'message')) return 'Send messages';
  if (has('mail', 'email')) return 'Send email';
  if (has('hass', 'home')) return 'Control home devices';
  if (has('file', 'delete')) return 'Change files';
  if (has('reminder')) return 'Manage reminders';
  if (has('routine', 'schedul')) return 'Manage routines';
  if (has('artifact')) return 'Create files and documents';
  if (has('browser')) return 'Browse the web for you';
  if (has('computer')) return 'Control this computer';
  if (has('weather', 'aqi', 'currency', 'crypto', 'places', 'flight')) return 'Look up information';
  if (act && act !== cap) return 'Allow ' + act.replace(/[_.-]+/g, ' ').trim();
  return 'Allow this action';
}

function paintPending(el, pending, refresh) {
  el.innerHTML = '';
  const panel = groupPanel();
  panel.appendChild(groupHead('Waiting for approval', 'Requests for actions Ghost wants to take. Approve one-off, always allow, or deny.'));
  const list = GhostUI.h('div', { className: 'ghost-list' });
  if (pending === 'error') {
    list.appendChild(GhostUI.errorState('Couldn\u2019t load approval requests', 'Ghost may still be starting.'));
  } else if (pending.length === 0) {
    list.appendChild(GhostUI.emptyState('Nothing waiting', 'When Ghost needs approval, it appears here.'));
  } else {
    pending.forEach(p => {
      const card = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      // Prefer the backend's native card (plain title + description), then
      // the request reason, then the local title map. Raw capability and
      // action ids are never shown.
      const title = (p.card && p.card.title) || p.reason || permTitle(p.capability, p.action);
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, title));
      const desc = (p.card && p.card.description) || '';
      const sub = riskWords(p.risk) + (p.target ? ' \u00b7 ' + p.target : '') + (desc && desc !== title ? ' \u00b7 ' + desc : '');
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub));
      card.appendChild(c);
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing perm-actions' });
      [['Allow once', 'allow_once'], ['Always allow', 'allow_always'], ['Deny', 'deny']].forEach(([label, grant]) => {
        const b = GhostUI.btn(label, grant === 'deny' ? 'danger' : grant === 'allow_always' ? 'secondary' : 'primary', async () => {
          try {
            // No scope is sent: the server stores the canonical scope the
            // request was asked under, so the grant matches exactly the
            // runtime invocation it authorizes.
            await GhostAPI.proxyPost('/v1/permissions/resolve', { id: p.id, grant });
          } catch (e) { GhostUI.toast('Couldn\u2019t record that choice \u2014 try again.'); return; }
          refresh();
        });
        b.classList.add('ghost-btn-sm');
        tr.appendChild(b);
      });
      card.appendChild(tr);
      list.appendChild(card);
    });
  }
  panel.appendChild(list);
  el.appendChild(panel);
}

function scopeLabel(scope) {
  if (!scope) return 'unknown scope';
  if (scope === 'owner') return 'everywhere on this Ghost';
  if (scope.startsWith('contact:')) return 'messages to ' + scope.slice(8);
  if (scope.startsWith('session:')) return 'this chat only';
  return scope;
}

function paintGrants(el, grants, refresh) {
  el.innerHTML = '';
  const panel = groupPanel();
  panel.appendChild(groupHead('Always allowed', 'Standing permissions Ghost may use without asking. Revoke any time.'));
  const list = GhostUI.h('div', { className: 'ghost-list' });
  if (grants === 'error') {
    list.appendChild(GhostUI.errorState('Couldn\u2019t load permissions', 'Ghost may still be starting.'));
  } else if (grants.length === 0) {
    list.appendChild(GhostUI.emptyState('No standing permissions', 'Choose \u201cAlways allow\u201d on any approval to add one.'));
  } else {
    grants.forEach(g => {
      if (String(g.action).startsWith('deny:')) return; // denials are policy, not grants
      const row = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, permTitle(g.capability, g.action)));
      // Standing grants carry no risk field; scope plus expiry is the
      // honest subtitle (grants expire — authority must not accumulate).
      let grantSub = 'Applies to: ' + scopeLabel(g.scope);
      if (g.expires_at) {
        const d = new Date(g.expires_at);
        if (!isNaN(d.getTime())) grantSub += ' \u00b7 Expires ' + d.toLocaleDateString([], { month: 'short', day: 'numeric' });
      }
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, grantSub));
      row.appendChild(c);
      row.appendChild(GhostUI.h('div', { className: 'ghost-row-trailing' },
        GhostUI.btn('Revoke', 'secondary', async () => {
          try {
            await GhostAPI.proxyPost('/v1/permissions/revoke', { capability: g.capability, action: g.action, scope: g.scope });
          } catch (e) { GhostUI.toast('Couldn\u2019t revoke \u2014 try again.'); return; }
          refresh();
        })));
      list.appendChild(row);
    });
  }
  panel.appendChild(list);
  el.appendChild(panel);
}
