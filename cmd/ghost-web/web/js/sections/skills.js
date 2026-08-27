/* Ghost Section: Skills — what Ghost knows how to do. */
'use strict';

async function loadSkills(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Skills'));
  head.appendChild(GhostUI.h('p', {}, 'What Ghost knows how to do.'));
  container.appendChild(head);

  const installBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showInstall() }, 'Install skill');
  GhostApp.setActions(installBtn);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'skill-list' });
  listEl.appendChild(GhostUI.loading('Loading skills…'));
  container.appendChild(listEl);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/skills'); }
  catch (e) {
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn’t load skills', 'Ghost may still be starting.'));
    return;
  }
  renderList(listEl, res.skills || []);
}

function renderList(listEl, skills) {
  listEl.innerHTML = '';
  if (skills.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No skills installed', 'Ghost comes with built-in skills. If none are present, something may be wrong.'));
    return;
  }
  skills.forEach(s => {
    const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => openSkill(container, s.name) });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const titleRow = GhostUI.h('div', { className: 'ghost-row-title' }, s.name);
    c.appendChild(titleRow);
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, s.description || 'No description'));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const enabled = s.enabled !== 'false' && s.enabled !== false;
    if (!enabled) tr.appendChild(GhostUI.h('span', { className: 'type-foot text-tertiary' }, 'Off'));
    tr.appendChild(GhostUI.h('span', { className: 'chevron' }, '›'));
    row.appendChild(tr);
    listEl.appendChild(row);
  });
}

async function openSkill(container, name) {
  container.innerHTML = '';
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--s-4);width:fit-content', onClick: () => loadSkills(container) });
  back.appendChild(GhostUI.h('span', { className: 'chevron', style: 'transform:rotate(180deg)' }, '‹'));
  back.appendChild(GhostUI.h('span', {}, 'All skills'));
  container.appendChild(back);

  const view = GhostUI.h('div', { className: 'panel' });
  view.appendChild(GhostUI.loading('Opening skill…'));
  container.appendChild(view);

  let data;
  try { data = await GhostAPI.proxyGet('/v1/skills/read?name=' + encodeURIComponent(name)); }
  catch (e) {
    view.innerHTML = '';
    view.appendChild(GhostUI.errorState('Couldn’t open this skill', 'It may have been removed.'));
    return;
  }

  view.innerHTML = '';
  const enabled = data.enabled !== false;
  const head = GhostUI.h('div', { className: 'panel-head' });
  head.appendChild(GhostUI.h('div', {}, GhostUI.h('h2', {}, name), GhostUI.h('p', {}, data.description || 'Installed skill')));
  const actions = GhostUI.h('div', { className: 'ghost-row-trailing' });
  const toggleBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, enabled ? 'Disable' : 'Enable');
  toggleBtn.addEventListener('click', async () => {
    try { await GhostAPI.proxyPost('/v1/skills/toggle', { name, enabled: !enabled }); GhostUI.toast(enabled ? 'Skill disabled' : 'Skill enabled'); openSkill(container, name); }
    catch (e) { GhostUI.toast('Couldn’t change it.', 'err'); }
  });
  actions.appendChild(toggleBtn);
  if (!(data.bundled)) {
    actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Remove this skill?', name + ' will be deleted from your Ghost. This can’t be undone.', 'Remove'))) return;
      try { await GhostAPI.post('/api/admin/skills/remove', { name }); GhostUI.toast('Removed'); loadSkills(container); }
      catch (e) { GhostUI.toast('Couldn’t remove it.', 'err'); }
    } }, 'Remove'));
  }
  head.appendChild(actions);
  view.appendChild(head);

  const meta = GhostUI.h('div', { className: 'kv', style: 'margin-bottom:var(--s-4)' });
  meta.appendChild(kvRow('Source', data.bundled ? 'Built into Ghost' : 'Installed by you'));
  if (data.user_modified) meta.appendChild(kvRow('Your edits', 'Preserved across Ghost updates'));
  view.appendChild(meta);

  const body = GhostUI.h('div', { className: 'markdown-body' });
  const sk = (data.files || []).find(f => f.path === 'SKILL.md' || f.path === 'SKILL.md.disabled');
  body.innerHTML = GhostUI.md(sk ? sk.content : 'No documentation.');
  view.appendChild(body);
}

function kvRow(k, v) {
  const r = GhostUI.h('div', { className: 'kv-row' });
  r.appendChild(GhostUI.h('div', { className: 'kv-key' }, k));
  r.appendChild(GhostUI.h('div', { className: 'kv-val' }, v));
  return r;
}

function showInstall() {
  const body = GhostUI.h('div');
  body.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-4)' }, 'Install a skill from a GitHub repository. Provide the owner, repo, and the folder path.'));
  const mk = (label, ph, key) => {
    const f = GhostUI.h('div', { className: 'field' });
    f.appendChild(GhostUI.h('label', {}, label));
    const i = GhostUI.h('input', { className: 'ghost-input', placeholder: ph });
    f.appendChild(i); body.appendChild(f); body[key] = i;
  };
  mk('Repository owner', 'ianclemence', '_owner');
  mk('Repository', 'ghost-skills', '_repo');
  mk('Path to skill folder', 'skills/research', '_path');

  GhostUI.modal('Install skill', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const owner = body._owner.value.trim(), repo = body._repo.value.trim(), p = body._path.value.trim();
      if (!owner || !repo || !p) { GhostUI.toast('All three fields are required.'); return; }
      try {
        await GhostAPI.post('/api/admin/skills/install', { owner, repo, path: p });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Skill installed');
        loadSkills(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Install failed: ' + (err.message || ''), 'err'); }
    } }, 'Install'),
  ]);
}

GhostApp.registerSection('skills', loadSkills);
