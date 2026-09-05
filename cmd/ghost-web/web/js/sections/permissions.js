/* Ghost Section: Permissions — what Ghost is allowed to do. */
'use strict';

async function loadPermissions(container) {
  if (GhostApp.currentSection() !== 'permissions') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Permissions'));
  head.appendChild(GhostUI.h('p', {}, 'Consequential actions ask first. Standing permissions live here — revoke any time.'));
  container.appendChild(head);

  const pendingEl = GhostUI.h('div', { className: 'ghost-list', id: 'perm-pending' });
  pendingEl.appendChild(GhostUI.loading('Checking for approval requests…'));
  container.appendChild(pendingEl);

  const grantsEl = GhostUI.h('div', { className: 'ghost-list', id: 'perm-grants' });
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

function paintPending(el, pending, refresh) {
  el.innerHTML = '';
  el.appendChild(GhostUI.h('h2', {}, 'Waiting for approval'));
  if (pending === 'error') {
    el.appendChild(GhostUI.errorState('Couldn\'t load approval requests', 'Ghost may still be starting.'));
    return;
  }
  if (pending.length === 0) {
    el.appendChild(GhostUI.emptyState('Nothing waiting', 'When Ghost needs approval, it appears here.'));
    return;
  }
  pending.forEach(p => {
    const card = GhostUI.h('div', { className: 'ghost-card' });
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-title' }, (p.reason || p.capability) + ' — ' + p.action));
    card.appendChild(GhostUI.h('div', { className: 'ghost-card-meta' }, 'Risk: ' + p.risk + (p.target ? ' · ' + p.target : '')));
    const row = GhostUI.h('div', { className: 'btn-row' });
    [['Allow once', 'allow_once'], ['Always allow', 'allow_always'], ['Deny', 'deny']].forEach(([label, grant]) => {
      row.appendChild(GhostUI.btn(label, grant === 'deny' ? 'danger' : 'primary', async () => {
        try {
          await GhostAPI.proxyPost('/v1/permissions/resolve', { id: p.id, grant, scope: grant === 'deny' ? (p.target || 'owner') : (p.target || 'owner') });
        } catch (e) { GhostUI.toast('Couldn\'t record that choice — try again.'); return; }
        refresh();
      }));
    });
    card.appendChild(row);
    el.appendChild(card);
  });
}

function paintGrants(el, grants, refresh) {
  el.innerHTML = '';
  el.appendChild(GhostUI.h('h2', {}, 'Always allowed'));
  if (grants === 'error') {
    el.appendChild(GhostUI.errorState('Couldn\'t load permissions', 'Ghost may still be starting.'));
    return;
  }
  if (grants.length === 0) {
    el.appendChild(GhostUI.emptyState('No standing permissions', 'Choose “Always allow” on any approval to add one.'));
    return;
  }
  grants.forEach(g => {
    if (String(g.action).startsWith('deny:')) return; // denials are policy, not grants
    const row = GhostUI.h('div', { className: 'ghost-row' });
    row.appendChild(GhostUI.h('div', { className: 'ghost-row-main' },
      GhostUI.h('div', { className: 'ghost-row-title' }, g.capability + ' · ' + g.action),
      GhostUI.h('div', { className: 'ghost-row-meta' }, 'Scope: ' + g.scope)));
    row.appendChild(GhostUI.btn('Revoke', 'secondary', async () => {
      try {
        await GhostAPI.proxyPost('/v1/permissions/revoke', { capability: g.capability, action: g.action, scope: g.scope });
      } catch (e) { GhostUI.toast('Couldn\'t revoke — try again.'); return; }
      refresh();
    }));
    el.appendChild(row);
  });
}

GhostApp.registerSection('permissions', loadPermissions);
