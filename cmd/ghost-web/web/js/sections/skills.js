/* Ghost Section: Skills — what Ghost knows how to do */
'use strict';

async function loadSkills(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-skills' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Skills'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost knows how to do.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/skills');
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
      if (skill.enabled === 'false') tags.appendChild(GhostUI.badge('Disabled', 'warning'));
      if (skill.user_modified === 'true') tags.appendChild(GhostUI.badge('Modified', 'warning'));
      r.appendChild(tags);
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load skills.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

GhostApp.registerSection('skills', loadSkills);
