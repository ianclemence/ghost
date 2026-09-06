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
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, (p.reason || p.capability) + ' \u2014 ' + p.action));
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Risk: ' + p.risk + (p.target ? ' \u00b7 ' + p.target : '')));
      card.appendChild(c);
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing perm-actions' });
      [['Allow once', 'allow_once'], ['Always allow', 'allow_always'], ['Deny', 'deny']].forEach(([label, grant]) => {
        const b = GhostUI.btn(label, grant === 'deny' ? 'danger' : grant === 'allow_always' ? 'secondary' : 'primary', async () => {
          try {
            await GhostAPI.proxyPost('/v1/permissions/resolve', { id: p.id, grant, scope: (p.target || 'owner') });
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
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, g.capability + ' \u00b7 ' + g.action));
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Scope: ' + g.scope));
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
