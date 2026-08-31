/* Ghost Section: Skills — what Ghost knows how to do. */
'use strict';

async function loadSkills(container) {
  if (GhostApp.currentSection() !== 'skills') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Skills'));
  head.appendChild(GhostUI.h('p', {}, 'What Ghost knows how to do.'));
  container.appendChild(head);

  const installBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => showInstall() }, 'Install skill');
  GhostApp.setActions(installBtn);

  const panel = GhostUI.h('div', { className: 'panel' });
  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'skill-list' });
  listEl.appendChild(GhostUI.loading('Loading skills…'));
  panel.appendChild(listEl);
  container.appendChild(panel);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/skills'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\u2019t load skills', e.message || 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const skills = Array.isArray(res) ? res : (res.skills || res.items || []);
  renderList(listEl, skills);
}

function renderList(listEl, skills) {
  listEl.innerHTML = '';
  if (!skills || skills.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No skills installed yet', 'Add your first skill from a GitHub repository, or Ghost comes with built-in skills ready to enable. Skills are the things Ghost can do for you.'));
    return;
  }
  skills.forEach(s => {
    const row = GhostUI.h('div', { className: 'ghost-link-row', onClick: () => openSkill(s.name) });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, s.name));
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

async function openSkill(name) {
  const backdrop = GhostUI.h('div', { className: 'ghost-modal-backdrop skill-modal-backdrop' });
  const modal = GhostUI.h('div', { className: 'ghost-modal skill-modal' });
  backdrop.appendChild(modal);

  const header = GhostUI.h('div', { className: 'skill-modal-header' });
  const titleArea = GhostUI.h('div');
  titleArea.appendChild(GhostUI.h('h2', { className: 'skill-modal-title' }, name));
  titleArea.appendChild(GhostUI.h('p', { className: 'skill-modal-sub', id: 'skill-modal-sub' }, 'Loading skill…'));
  header.appendChild(titleArea);
  const closeBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-icon skill-modal-close', onClick: () => backdrop.remove() }, '✕');
  header.appendChild(closeBtn);
  modal.appendChild(header);

  const body = GhostUI.h('div', { className: 'skill-modal-body', id: 'skill-modal-body' });
  body.appendChild(GhostUI.loading('Loading skill…'));
  modal.appendChild(body);

  const actions = GhostUI.h('div', { className: 'ghost-modal-actions skill-modal-actions', id: 'skill-modal-actions' });
  modal.appendChild(actions);

  backdrop.addEventListener('click', (e) => { if (e.target === backdrop) backdrop.remove(); });
  document.body.appendChild(backdrop);

  let data;
  try { data = await GhostAPI.proxyGet('/v1/skills/read?name=' + encodeURIComponent(name)); }
  catch (e) {
    body.innerHTML = '';
    body.appendChild(GhostUI.errorState('Couldn’t open this skill', 'It may have been removed.'));
    return;
  }

  const sub = document.getElementById('skill-modal-sub');
  const enabled = data.enabled !== false;
  const parts = [];
  if (data.bundled) parts.push('Built-in');
  if (data.files && data.files.length) parts.push(data.files.length + ' file' + (data.files.length === 1 ? '' : 's'));
  if (data.user_modified) parts.push('Modified');
  if (parts.length > 0 && sub) sub.textContent = parts.join('  \u00b7  ') + (enabled ? '' : '  \u00b7  Disabled');
  else if (sub) sub.textContent = data.description || (enabled ? 'Installed skill' : 'Disabled');

  body.innerHTML = '';
  const content = GhostUI.h('div', { className: 'markdown-body' });
  const sk = (data.files || []).find(f => f.path === 'SKILL.md' || f.path === 'SKILL.md.disabled');
  content.innerHTML = GhostUI.md(sk ? sk.content : 'No documentation.');
  body.appendChild(content);

  // Show other files in this skill (excluding SKILL.md)
  const otherFiles = (data.files || []).filter(f => f.path !== 'SKILL.md' && f.path !== 'SKILL.md.disabled');
  if (otherFiles.length > 0) {
    const filesWrap = GhostUI.h('div', { style: 'margin-top:var(--s-4);border-top:1px solid var(--ink-ghost);padding-top:var(--s-3)' });
    filesWrap.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-2)' }, 'Files in this skill'));
    for (const f of otherFiles) {
      const fRow = GhostUI.h('div', { className: 'ghost-row-subtitle', style: 'padding:var(--s-1) 0;font-family:var(--font-mono);font-size:var(--t-foot)' }, f.path);
      filesWrap.appendChild(fRow);
    }
    body.appendChild(filesWrap);
  }

  actions.innerHTML = '';
  const toggleBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, enabled ? 'Disable' : 'Enable');
  toggleBtn.addEventListener('click', async () => {
    try { await GhostAPI.proxyPost('/v1/skills/toggle', { name, enabled: !enabled }); GhostUI.toast(enabled ? 'Skill disabled' : 'Skill enabled'); backdrop.remove(); loadSkills(document.getElementById('view')); }
    catch (e) { GhostUI.toast('Couldn’t change it.', 'err'); }
  });
  actions.appendChild(toggleBtn);
  if (!(data.bundled)) {
    actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
      if (!(await GhostUI.confirmModal('Remove this skill?', name + ' will be deleted from your Ghost. This can’t be undone.', 'Remove'))) return;
      try { await GhostAPI.post('/api/admin/skills/remove', { name }); GhostUI.toast('Removed'); backdrop.remove(); loadSkills(document.getElementById('view')); }
      catch (e) { GhostUI.toast('Couldn’t remove it.', 'err'); }
    } }, 'Remove'));
  }
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
