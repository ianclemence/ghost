/* Ghost Section: Skills — what Ghost knows how to do */
'use strict';

async function loadSkills(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-skills' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Skills'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost knows how to do.'));

  // Header with count and actions
  const header = GhostUI.h('div', { className: 'skills-header' });
  const countEl = GhostUI.h('span', { className: 'type-subhead text-tertiary' });
  header.appendChild(countEl);

  // Dropdown for actions
  const dropdown = GhostUI.h('div', { className: 'skills-dropdown' });
  const dropBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary ghost-btn-sm' }, 'Install skill');
  const dropMenu = GhostUI.h('div', { className: 'skills-dropdown-menu hidden' });

  dropMenu.appendChild(GhostUI.h('div', { className: 'skills-dropdown-item', onClick: () => { dropMenu.classList.add('hidden'); showClawHubSearchModal(); } }, 'From ClawHub'));
  dropMenu.appendChild(GhostUI.h('div', { className: 'skills-dropdown-item', onClick: () => { dropMenu.classList.add('hidden'); showGitHubInstallModal(); } }, 'From GitHub'));
  dropMenu.appendChild(GhostUI.h('div', { className: 'skills-dropdown-divider' }));
  dropMenu.appendChild(GhostUI.h('div', { className: 'skills-dropdown-item', onClick: async () => {
    dropMenu.classList.add('hidden');
    try {
      const res = await GhostAPI.post('/api/admin/skills/sync');
      GhostUI.toast(res.report || 'Skills synced.');
      loadSkills(container);
    } catch (e) {
      GhostUI.toast('Failed to sync skills.');
    }
  }}, 'Sync bundled skills'));

  dropBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    dropMenu.classList.toggle('hidden');
  });
  document.addEventListener('click', () => dropMenu.classList.add('hidden'));

  dropdown.appendChild(dropBtn);
  dropdown.appendChild(dropMenu);
  header.appendChild(dropdown);
  section.appendChild(header);

  // Skills list
  const listEl = GhostUI.h('div', { className: 'skills-grid' });

  try {
    const data = await GhostAPI.proxyGet('/v1/skills');
    const skills = data.skills || [];

    countEl.textContent = `${skills.length} skill${skills.length !== 1 ? 's' : ''}`;

    if (skills.length === 0) {
      section.appendChild(GhostUI.emptyState('No skills installed.', 'Ghost can learn new skills from the community.'));
    }

    for (const skill of skills) {
      const card = GhostUI.h('div', { className: 'skill-card' });

      // Top row: name + badges
      const topRow = GhostUI.h('div', { className: 'skill-card-top' });
      const nameEl = GhostUI.h('div', { className: 'skill-card-name' }, skill.name);
      topRow.appendChild(nameEl);

      const badges = GhostUI.h('div', { className: 'skill-card-badges' });
      if (skill.bundled === 'true') badges.appendChild(GhostUI.badge('Built-in', 'neutral'));
      const isEnabled = skill.enabled !== 'false';
      if (!isEnabled) badges.appendChild(GhostUI.badge('Disabled', 'warning'));
      if (skill.user_modified === 'true') badges.appendChild(GhostUI.badge('Modified', 'warning'));
      topRow.appendChild(badges);
      card.appendChild(topRow);

      // Description
      if (skill.description) {
        const desc = GhostUI.h('div', { className: 'skill-card-desc' }, skill.description);
        card.appendChild(desc);
      }

      // Actions row
      const actions = GhostUI.h('div', { className: 'skill-card-actions' });

      // Read button
      actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: () => showSkillReader(skill.name) }, 'Read'));

      // Toggle (non-bundled only)
      if (skill.bundled !== 'true') {
        actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: async (e) => {
          e.stopPropagation();
          try {
            await GhostAPI.post('/api/admin/skills/toggle', { name: skill.name, enabled: !isEnabled });
            GhostUI.toast(`Skill ${isEnabled ? 'disabled' : 'enabled'}.`);
            loadSkills(container);
          } catch (e) {
            GhostUI.toast('Failed to toggle skill.');
          }
        }}, isEnabled ? 'Disable' : 'Enable'));

        actions.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm skill-remove-btn', onClick: async (e) => {
          e.stopPropagation();
          if (!confirm(`Remove skill ${skill.name}?`)) return;
          try {
            await GhostAPI.post('/api/admin/skills/remove', { name: skill.name });
            GhostUI.toast('Skill removed.');
            loadSkills(container);
          } catch (e) {
            GhostUI.toast('Failed to remove skill.');
          }
        }}, 'Remove'));
      }

      card.appendChild(actions);

      // Click card to read
      card.addEventListener('click', () => showSkillReader(skill.name));
      card.style.cursor = 'pointer';

      listEl.appendChild(card);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load skills.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

async function showSkillReader(name) {
  const content = GhostUI.h('div', { id: 'section-skills-reader', className: 'skill-reader' });

  // Back link
  const back = GhostUI.h('div', { className: 'ghost-link-row', style: 'margin-bottom:var(--space-lg)', onClick: () => GhostApp.navigate('skills') });
  back.appendChild(GhostUI.h('span', { className: 'type-callout text-accent' }, '\u2190 Skills'));
  content.appendChild(back);

  // Title
  content.appendChild(GhostUI.h('div', { className: 'type-title', style: 'margin-bottom:var(--space-xl)' }, name));

  try {
    const data = await GhostAPI.get(`/api/admin/skills/read?name=${encodeURIComponent(name)}`);
    const files = data.files || [];

    if (files.length === 0) {
      content.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary' }, 'No files found.'));
    } else {
      // Find SKILL.md first, then show other files
      const skillMd = files.find(f => f.path === 'SKILL.md');
      const otherFiles = files.filter(f => f.path !== 'SKILL.md');

      if (skillMd) {
        const markdownEl = GhostUI.h('div', { className: 'skill-markdown' });
        markdownEl.innerHTML = renderMarkdown(skillMd.content || '');
        content.appendChild(markdownEl);
      }

      if (otherFiles.length > 0) {
        const filesSection = GhostUI.h('div', { className: 'skill-files-section', style: 'margin-top:var(--space-xxl)' });
        filesSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Other files'));

        for (const file of otherFiles) {
          const fileRow = GhostUI.h('div', { className: 'ghost-row' });
          const fileContent = GhostUI.h('div', { className: 'ghost-row-content' });
          fileContent.appendChild(GhostUI.h('div', { className: 'ghost-row-title type-mono' }, file.path));
          if (file.content) {
            const preview = file.content.substring(0, 120).replace(/\n/g, ' ');
            fileContent.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, preview + (file.content.length > 120 ? '...' : '')));
          }
          fileRow.appendChild(fileContent);
          filesSection.appendChild(fileRow);
        }
        content.appendChild(filesSection);
      }
    }
  } catch (e) {
    content.appendChild(GhostUI.errorState('Couldn\'t load skill.', e.message));
  }

  const appContent = document.getElementById('ghost-content');
  appContent.innerHTML = '';
  appContent.appendChild(content);
}

function renderMarkdown(md) {
  // Simple markdown renderer for skill content
  let html = md
    // Code blocks
    .replace(/```(\w*)\n([\s\S]*?)```/g, '<pre class="skill-code-block"><code>$2</code></pre>')
    // Inline code
    .replace(/`([^`]+)`/g, '<code class="skill-inline-code">$1</code>')
    // Headers
    .replace(/^### (.+)$/gm, '<h3 class="skill-h3">$1</h3>')
    .replace(/^## (.+)$/gm, '<h2 class="skill-h2">$1</h2>')
    .replace(/^# (.+)$/gm, '<h1 class="skill-h1">$1</h1>')
    // Bold and italic
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    // Lists
    .replace(/^- (.+)$/gm, '<li class="skill-li">$1</li>')
    .replace(/^(\d+)\. (.+)$/gm, '<li class="skill-li">$2</li>')
    // Links
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
    // Horizontal rules
    .replace(/^---$/gm, '<hr class="skill-hr">')
    // Paragraphs (double newlines)
    .replace(/\n\n/g, '</p><p class="skill-p">')
    // Single newlines to <br>
    .replace(/\n/g, '<br>');

  // Wrap consecutive li elements in ul
  html = html.replace(/(<li class="skill-li">.*?<\/li>(\s*<br>)*)+/g, (match) => {
    return '<ul class="skill-ul">' + match.replace(/<br>/g, '') + '</ul>';
  });

  return '<p class="skill-p">' + html + '</p>';
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
    results.innerHTML = '';
    results.appendChild(GhostUI.loading('Searching...'));
    try {
      const data = await GhostAPI.get(`/api/admin/skills/clawhub/search?q=${encodeURIComponent(q)}`);
      results.innerHTML = '';
      const items = data.results || [];
      if (items.length === 0) {
        results.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'padding:var(--space-lg)' }, 'No skills found.'));
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
      results.appendChild(GhostUI.h('div', { className: 'type-callout text-error', style: 'padding:var(--space-lg)' }, 'Search failed.'));
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
