/* Ghost Section: Skills — what Ghost knows how to do */
'use strict';

async function loadSkills(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-skills' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Skills'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost knows how to do.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.get('/api/admin/skills');
    const skills = data.skills || [];

    if (skills.length === 0) {
      section.appendChild(GhostUI.emptyState('No skills installed.', 'Ghost can learn new skills from the community.'));
    }

    for (const skill of skills) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, skill.name));
      if (skill.description) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, skill.description));
      r.appendChild(c);

      const tags = GhostUI.h('div', { style: 'display:flex;gap:var(--space-xs);align-items:center' });
      if (skill.bundled === 'true') tags.appendChild(GhostUI.badge('Built-in', 'neutral'));
      const isEnabled = skill.enabled !== 'false';
      if (!isEnabled) tags.appendChild(GhostUI.badge('Disabled', 'warning'));
      if (skill.user_modified === 'true') tags.appendChild(GhostUI.badge('Modified', 'warning'));

      // Toggle button (only for non-bundled skills)
      if (skill.bundled !== 'true') {
        const toggleBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: async () => {
          try {
            await GhostAPI.post('/api/admin/skills/toggle', { name: skill.name, enabled: !isEnabled });
            GhostUI.toast(`Skill ${isEnabled ? 'disabled' : 'enabled'}.`);
            loadSkills(container);
          } catch (e) {
            GhostUI.toast('Failed to toggle skill.');
          }
        }}, isEnabled ? 'Disable' : 'Enable');
        tags.appendChild(toggleBtn);
      }

      // Remove button (only for non-bundled skills)
      if (skill.bundled !== 'true') {
        const removeBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: async () => {
          if (!confirm(`Remove skill ${skill.name}?`)) return;
          try {
            await GhostAPI.post('/api/admin/skills/remove', { name: skill.name });
            GhostUI.toast('Skill removed.');
            loadSkills(container);
          } catch (e) {
            GhostUI.toast('Failed to remove skill.');
          }
        }}, 'Remove');
        tags.appendChild(removeBtn);
      }

      r.appendChild(tags);
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load skills.', e.message));
  }

  section.appendChild(listEl);

  // Actions
  const actions = GhostUI.h('div', { style: 'margin-top:var(--space-xxl);display:flex;gap:var(--space-sm)' });
  actions.appendChild(GhostUI.btn('Install from ClawHub', 'secondary', () => showClawHubSearchModal()));
  actions.appendChild(GhostUI.btn('Install from GitHub', 'ghost', () => showGitHubInstallModal()));
  actions.appendChild(GhostUI.btn('Sync skills', 'ghost', async () => {
    try {
      const res = await GhostAPI.post('/api/admin/skills/sync');
      GhostUI.toast(res.report || 'Skills synced.');
      loadSkills(container);
    } catch (e) {
      GhostUI.toast('Failed to sync skills.');
    }
  }));
  section.appendChild(actions);

  container.appendChild(section);
}

function showClawHubSearchModal() {
  const body = GhostUI.h('div');
  const group = GhostUI.h('div', { className: 'form-group' });
  group.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Search skills'));
  const input = GhostUI.input('Search ClawHub...');
  group.appendChild(input);
  body.appendChild(group);

  const results = GhostUI.h('div', { className: 'ghost-list', style: 'margin-top:var(--space-md)' });
  body.appendChild(results);

  const doSearch = async () => {
    const q = input.value.trim();
    if (!q) return;
    try {
      const data = await GhostAPI.get(`/api/admin/skills/clawhub/search?q=${encodeURIComponent(q)}`);
      results.innerHTML = '';
      const items = data.results || [];
      if (items.length === 0) {
        results.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary' }, 'No skills found.'));
        return;
      }
      for (const item of items) {
        const r = GhostUI.h('div', { className: 'ghost-row', style: 'cursor:pointer', onClick: async () => {
          try {
            const res = await GhostAPI.post('/api/admin/skills/clawhub/install', { slug: item.slug });
            GhostUI.toast(res.message || 'Skill installed.');
          } catch (e) {
            GhostUI.toast('Failed to install skill.');
          }
        }});
        const c = GhostUI.h('div', { className: 'ghost-row-content' });
        c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, item.display_name || item.slug));
        if (item.summary) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, item.summary));
        r.appendChild(c);
        r.appendChild(GhostUI.h('span', { className: 'chevron' }, '\u203a'));
        results.appendChild(r);
      }
    } catch (e) {
      results.innerHTML = '';
      results.appendChild(GhostUI.h('div', { className: 'type-callout text-error' }, 'Search failed.'));
    }
  };

  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') doSearch(); });

  GhostUI.modal('Install from ClawHub', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: doSearch }, 'Search')
  ]);
}

function showGitHubInstallModal() {
  const body = GhostUI.h('div');

  const ownerGroup = GhostUI.h('div', { className: 'form-group' });
  ownerGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'GitHub owner'));
  const ownerInput = GhostUI.input('e.g. ianclemence');
  ownerGroup.appendChild(ownerInput);
  body.appendChild(ownerGroup);

  const repoGroup = GhostUI.h('div', { className: 'form-group' });
  repoGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Repository'));
  const repoInput = GhostUI.input('e.g. ghost-skills');
  repoGroup.appendChild(repoInput);
  body.appendChild(repoGroup);

  const pathGroup = GhostUI.h('div', { className: 'form-group' });
  pathGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Path in repo'));
  const pathInput = GhostUI.input('e.g. skills/my-skill');
  pathGroup.appendChild(pathInput);
  body.appendChild(pathGroup);

  const nameGroup = GhostUI.h('div', { className: 'form-group' });
  nameGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Skill name (optional)'));
  const nameInput = GhostUI.input('Leave empty to use path basename');
  nameGroup.appendChild(nameInput);
  body.appendChild(nameGroup);

  const branchGroup = GhostUI.h('div', { className: 'form-group' });
  branchGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Branch'));
  const branchInput = GhostUI.input('main');
  branchInput.value = 'main';
  branchGroup.appendChild(branchInput);
  body.appendChild(branchGroup);

  GhostUI.modal('Install from GitHub', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      const owner = ownerInput.value.trim();
      const repo = repoInput.value.trim();
      const path = pathInput.value.trim();
      if (!owner || !repo || !path) { GhostUI.toast('Please fill in owner, repo, and path.'); return; }
      try {
        const payload = { owner, repo, path };
        if (nameInput.value.trim()) payload.name = nameInput.value.trim();
        if (branchInput.value.trim()) payload.branch = branchInput.value.trim();
        const res = await GhostAPI.post('/api/admin/skills/install', payload);
        GhostUI.toast(res.message || 'Skill installed.');
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Failed to install skill.');
      }
    }}, 'Install')
  ]);
}

GhostApp.registerSection('skills', loadSkills);
