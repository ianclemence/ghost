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

    main.addEventListener('click', (e) => {
      closeMobileSidebar();
    });

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
          dataset: { section: item.id },
          onClick: () => closeMobileSidebar()
        }, item.label);
        sidebar.appendChild(link);
      }
    }

    return sidebar;
  }

  function renderHeader() {
    const header = GhostUI.h('div', { id: 'ghost-header' });
    const hamburger = GhostUI.h('button', { id: 'sidebar-toggle', className: 'ghost-btn-ghost', onClick: (e) => { e.stopPropagation(); toggleMobileSidebar(); } }, '\u2630');
    header.appendChild(hamburger);
    const title = GhostUI.h('div', { id: 'header-title' });
    title.appendChild(GhostUI.h('span', { className: 'type-title' }, 'Ghost'));
    header.appendChild(title);
    return header;
  }

  function toggleMobileSidebar() {
    document.getElementById('ghost-sidebar').classList.toggle('open');
  }

  function closeMobileSidebar() {
    const sidebar = document.getElementById('ghost-sidebar');
    if (sidebar) sidebar.classList.remove('open');
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
    GhostAPI.setOnAuthExpired(() => showLoginScreen());
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
      // Check if session is valid before showing dashboard
      try {
        await GhostAPI.get('/api/admin/auth/check');
        GhostApp.start();
      } catch (e) {
        // Session invalid — show login screen
        showLoginScreen();
      }
    }
  } catch (e) {
    GhostApp.start();
  }
});

function showLoginScreen() {
  document.body.innerHTML = '';
  const container = GhostUI.h('div', { className: 'login-screen' });
  const card = GhostUI.h('div', { className: 'login-card' });

  card.appendChild(GhostUI.h('div', { className: 'login-logo' }, GhostUI.ghostMark('lg')));
  card.appendChild(GhostUI.h('div', { className: 'type-headline', style: 'margin-bottom:var(--space-lg)' }, 'Ghost'));
  card.appendChild(GhostUI.h('div', { className: 'type-body text-secondary', style: 'margin-bottom:var(--space-xl)' }, 'Enter your admin password to continue.'));

  const input = GhostUI.input('Admin password', 'password');
  input.style.marginBottom = 'var(--space-md)';
  card.appendChild(input);

  const errorMsg = GhostUI.h('div', { className: 'type-footnote', style: 'color:var(--color-error);margin-bottom:var(--space-md);min-height:1.2em' });
  card.appendChild(errorMsg);

  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', style: 'width:100%' }, 'Log in');
  card.appendChild(btn);

  const doLogin = async () => {
    errorMsg.textContent = '';
    const pw = input.value.trim();
    if (!pw) { errorMsg.textContent = 'Enter the admin password.'; return; }
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ password: pw }),
      });
      const data = await res.json();
      if (data.ok) {
        location.reload();
      } else {
        errorMsg.textContent = data.error || 'Login failed.';
      }
    } catch (e) {
      errorMsg.textContent = 'Login failed.';
    }
  };

  btn.addEventListener('click', doLogin);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter') doLogin(); });

  container.appendChild(card);
  document.body.appendChild(container);
  input.focus();
}
