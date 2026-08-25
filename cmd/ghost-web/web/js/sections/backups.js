/* Ghost Section: Backups */
'use strict';

async function loadBackups(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-backups' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Backups'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Your Ghost\'s identity and memory.'));

  const infoSection = GhostUI.h('div', { className: 'section-group' });
  infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'What is backed up'));

  const items = GhostUI.h('div', { className: 'ghost-list' });
  const backups = ['Identity', 'Memory', 'Skills', 'Automations', 'Configuration'];
  for (const b of backups) {
    items.appendChild(GhostUI.row(b, 'Included in backup'));
  }
  infoSection.appendChild(items);
  section.appendChild(infoSection);

  // Actions
  const actions = GhostUI.h('div', { style: 'margin-top:var(--space-xxl);display:flex;gap:var(--space-sm)' });
  actions.appendChild(GhostUI.btn('Create backup', 'primary', async () => {
    try {
      const res = await GhostAPI.request('/api/admin/backup', { method: 'POST' });
      if (res.ok) GhostUI.toast('Backup downloaded.');
      else GhostUI.toast('Backup failed.');
    } catch (e) {
      GhostUI.toast('Backup failed.');
    }
  }));
  section.appendChild(actions);

  container.appendChild(section);
}

GhostApp.registerSection('backups', loadBackups);
