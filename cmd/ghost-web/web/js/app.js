/* Ghost App — router, state, app shell */
'use strict';

const GhostApp = (() => {
  let _currentSection = 'home';
  let _sectionLoaders = {};
  let _wizardMode = false;

  function registerSection(name, loader) {
    _sectionLoaders[name] = loader;
  }

  function getSection() {
    const hash = location.hash.replace('#', '') || 'home';
    return _sectionLoaders[hash] ? hash : 'home';
  }

  function navigate(section) {
    location.hash = '#' + section;
  }

  function renderShell() {
    document.body.innerHTML = '';
    const app = GhostUI.h('div', { id: 'ghost-app' });
    app.appendChild(renderSidebar());
    const main = GhostUI.h('div', { id: 'ghost-main' });
    main.appendChild(renderHeader());
    const content = GhostUI.h('div', { id: 'ghost-content' });
    main.appendChild(content);
    app.appendChild(main);
    document.body.appendChild(app);
    loadCurrentSection();
  }

  function renderSidebar() {
    const sidebar = GhostUI.h('nav', { id: 'ghost-sidebar' });
    const logo = GhostUI.h('div', { id: 'sidebar-logo' });
    logo.appendChild(GhostUI.ghostMark('lg'));
    logo.appendChild(GhostUI.h('span', { className: 'type-headline' }, 'Ghost'));
    sidebar.appendChild(logo);

    const sections = [
      { group: 'MAIN', items: [
        { id: 'home', label: 'Home' },
        { id: 'ai', label: 'AI' },
        { id: 'memory', label: 'Memory' },
        { id: 'activity', label: 'Activity' },
        { id: 'automations', label: 'Automations' },
        { id: 'skills', label: 'Skills' },
      ]},
      { group: 'CONNECTIONS', items: [
        { id: 'devices', label: 'Devices' },
        { id: 'channels', label: 'Channels' },
      ]},
      { group: 'SYSTEM', items: [
        { id: 'system', label: 'System' },
        { id: 'updates', label: 'Updates' },
        { id: 'backups', label: 'Backups' },
        { id: 'security', label: 'Security' },
      ]},
    ];

    for (const group of sections) {
      sidebar.appendChild(GhostUI.h('div', { className: 'sidebar-group-label section-label' }, group.group));
      for (const item of group.items) {
        const link = GhostUI.h('a', {
          className: 'sidebar-link',
          href: '#' + item.id,
          dataset: { section: item.id }
        }, item.label);
        sidebar.appendChild(link);
      }
    }

    return sidebar;
  }

  function renderHeader() {
    const header = GhostUI.h('div', { id: 'ghost-header' });
    const hamburger = GhostUI.h('button', { id: 'sidebar-toggle', className: 'ghost-btn-ghost', onClick: toggleMobileSidebar }, '\u2630');
    header.appendChild(hamburger);
    const title = GhostUI.h('div', { id: 'header-title' });
    title.appendChild(GhostUI.h('span', { className: 'type-title' }, 'Ghost'));
    header.appendChild(title);
    return header;
  }

  function toggleMobileSidebar() {
    document.getElementById('ghost-sidebar').classList.toggle('open');
  }

  function updateSidebarActive(section) {
    document.querySelectorAll('.sidebar-link').forEach(link => {
      link.classList.toggle('active', link.dataset.section === section);
    });
  }

  function loadCurrentSection() {
    const section = getSection();
    _currentSection = section;
    updateSidebarActive(section);
    const content = document.getElementById('ghost-content');
    if (!content) return;
    content.innerHTML = '';
    if (_sectionLoaders[section]) {
      _sectionLoaders[section](content);
    } else {
      content.appendChild(GhostUI.emptyState('Nothing here yet.', 'This section is coming soon.'));
    }
  }

  function start() {
    window.addEventListener('hashchange', loadCurrentSection);
    renderShell();
  }

  function startWizard() {
    _wizardMode = true;
    if (_sectionLoaders['wizard']) {
      _sectionLoaders['wizard'](document.body);
    }
  }

  return { registerSection, navigate, start, startWizard, getSection };
})();

/* Init on DOM ready */
document.addEventListener('DOMContentLoaded', async () => {
  try {
    const status = await GhostAPI.get('/api/status');
    if (status.needs_setup) {
      GhostApp.startWizard();
    } else {
      GhostApp.start();
    }
  } catch (e) {
    GhostApp.start();
  }
});
