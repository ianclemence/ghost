/* Ghost Section: Updates */
'use strict';

async function loadUpdates(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-updates' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Updates'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Keep your Ghost current.'));

  try {
    const health = await GhostAPI.proxyGet('/v1/health');
    const version = health.version || 'Unknown';

    const infoSection = GhostUI.h('div', { className: 'section-group' });
    infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Ghost'));
    const infoList = GhostUI.h('div', { className: 'ghost-list' });
    infoList.appendChild(GhostUI.row('Current version', version));
    infoSection.appendChild(infoList);
    section.appendChild(infoSection);
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t check version.', e.message));
  }

  container.appendChild(section);
}

GhostApp.registerSection('updates', loadUpdates);
