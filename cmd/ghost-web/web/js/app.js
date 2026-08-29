/* Ghost App — router, app shell, auth gate, state.
   Loaded BEFORE the section files so GhostApp.registerSection is defined. */
'use strict';

const GhostApp = (() => {
  const sections = new Map();   // name -> { loader }
  const navTitles = new Map();  // name -> title
  let current = null;
  let root = null;

  // ── Navigation model (the product's information architecture) ──
  const NAV = [
    {
      label: 'Main',
      items: [
        { name: 'home', title: 'Home', glyph: 'home' },
        { name: 'ai', title: 'AI', glyph: 'ai' },
        { name: 'memory', title: 'Memory', glyph: 'memory' },
        { name: 'activity', title: 'Activity', glyph: 'activity' },
        { name: 'automations', title: 'Automations', glyph: 'automation' },
        { name: 'skills', title: 'Skills', glyph: 'skill' },
      ],
    },
    {
      label: 'Connections',
      items: [
        { name: 'devices', title: 'Devices', glyph: 'device' },
        { name: 'channels', title: 'Channels', glyph: 'channel' },
      ],
    },
    {
      label: 'System',
      items: [
        { name: 'system', title: 'System', glyph: 'system' },
        { name: 'security', title: 'Security', glyph: 'security' },
        { name: 'help', title: 'Help', glyph: 'help' },
        { name: 'about', title: 'About', glyph: 'about' },
      ],
    },
  ];
  NAV.forEach(g => g.items.forEach(i => navTitles.set(i.name, i.title)));

  const GLYPHS = {
    home: '<path d="M3 11l9-8 9 8M5 9.5V20h5v-5h4v5h5V9.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" stroke-linecap="round"/>',
    ai: '<path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1M12 8.5a3.5 3.5 0 100 7 3.5 3.5 0 000-7z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/>',
    memory: '<path d="M6 4h12v16H6zM9 8h2M13 8h2M9 12h2M13 12h2M9 16h2M13 16h2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>',
    activity: '<path d="M3 12h4l2 6 4-14 2 8h6" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>',
    automation: '<circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M12 8v4l3 2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>',
    skill: '<path d="M12 3l2.2 4.6L19 8.3l-3.5 3.4.8 4.9L12 14.8 7.7 16.6l.8-4.9L5 8.3l4.8-.7L12 3z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>',
    device: '<rect x="7" y="3" width="10" height="18" rx="2.2" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 18h3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
    channel: '<path d="M4 6h16v9H9l-4 4V6z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>',
    system: '<circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.5 5.5l2 2M16.5 16.5l2 2M18.5 5.5l-2 2M7.5 16.5l-2 2" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
    update: '<path d="M4 12a8 8 0 0113.7-5.7L20 8M20 4v4h-4M20 12a8 8 0 01-13.7 5.7L4 16M4 20v-4h4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>',
    backup: '<path d="M5 7h9l4 4v9H5zM14 7v4h4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><path d="M8.5 13.5l2.5 2.5 4-4.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>',
    security: '<path d="M12 3l7 3v6c0 4.4-3 7.5-7 9-4-1.5-7-4.6-7-9V6l7-3z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/><path d="M9 12l2 2 4-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>',
    help: '<circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M9.5 9.5a2.5 2.5 0 113.5 2.3c-.8.4-1 .9-1 1.7M12 16.5v.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
    about: '<circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.5"/><path d="M12 11v5M12 8v.5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>',
  };

  function glyph(name) {
    return '<span class="nav-glyph"><svg viewBox="0 0 24 24">' + (GLYPHS[name] || '') + '</svg></span>';
  }

  function registerSection(name, loader) {
    sections.set(name, { loader });
  }

  function navigate(name) {
    if (!sections.has(name)) name = 'home';
    if (location.hash !== '#' + name) {
      location.hash = name;
    } else {
      render(name);
    }
  }

  function render(name) {
    current = name;
    const view = document.getElementById('view');
    if (!view) return;
    view.innerHTML = '';
    setActions(null);
    // loading state
    view.appendChild(GhostUI.loading('Loading…'));
    updateActiveNav(name);
    updateTitle(name);

    const sec = sections.get(name);
    // Defer so the loading state paints first
    setTimeout(() => {
      view.innerHTML = '';
      Promise.resolve()
        .then(() => sec.loader(view))
        .catch(err => {
          view.innerHTML = '';
          view.appendChild(GhostUI.errorState(
            "Something went wrong",
            (err && err.message) ? err.message : "This page couldn't be loaded."
          ));
        });
    }, 30);
  }

  function setActions(node) {
    const a = document.getElementById('topbar-actions');
    if (a) { a.innerHTML = ''; if (node) a.appendChild(node); }
  }

  function updateActiveNav(name) {
    document.querySelectorAll('.nav-item').forEach(el => {
      el.classList.toggle('active', el.dataset.nav === name);
    });
  }

  function updateTitle(name) {
    const t = document.getElementById('topbar-title');
    const s = document.getElementById('topbar-sub');
    if (t) t.textContent = navTitles.get(name) || 'Ghost';
    if (s) s.textContent = '';
  }

  // ── Shell construction ──
  function buildShell() {
    root.innerHTML = '';
    const shell = GhostUI.h('div', { className: 'shell' });

    const nav = GhostUI.h('nav', { className: 'shell-nav', id: 'shell-nav' });
    const brand = GhostUI.h('div', { className: 'shell-brand' });
    brand.appendChild(GhostUI.ghostMark('md'));
    brand.appendChild(GhostUI.h('span', { className: 'shell-brand-name' }, 'Ghost'));
    nav.appendChild(brand);

    NAV.forEach(group => {
      const g = GhostUI.h('div', { className: 'nav-group' });
      g.appendChild(GhostUI.h('div', { className: 'nav-group-label' }, group.label));
      group.items.forEach(item => {
        const btn = GhostUI.h('button', {
          className: 'nav-item', dataset: { nav: item.name },
          onClick: () => navigate(item.name),
        });
        btn.innerHTML = glyph(item.glyph);
        btn.appendChild(GhostUI.h('span', {}, item.title));
        g.appendChild(btn);
      });
      nav.appendChild(g);
    });

    const foot = GhostUI.h('div', { className: 'shell-nav-foot' });
    const lockBtn = GhostUI.h('button', { className: 'nav-item', onClick: () => lock() }, 'Lock');
    lockBtn.innerHTML = '<span class="nav-glyph"><svg viewBox="0 0 24 24"><path d="M6 11V8a6 6 0 1112 0v3M5 11h14v9H5z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg></span><span>Lock</span>';
    foot.appendChild(lockBtn);
    nav.appendChild(foot);

    const main = GhostUI.h('div', { className: 'shell-main' });
    const topbar = GhostUI.h('header', { className: 'topbar' });
    const left = GhostUI.h('div', { className: 'row-flex' });
    const toggle = GhostUI.h('button', { className: 'ghost-btn ghost-btn-icon nav-toggle', onClick: () => toggleNav(), html: '<svg viewBox="0 0 24 24" width="20" height="20"><path d="M4 7h16M4 12h16M4 17h16" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>' });
    left.appendChild(toggle);
    left.appendChild(GhostUI.h('div', {}, GhostUI.h('div', { className: 'topbar-title', id: 'topbar-title' }, 'Ghost'), GhostUI.h('div', { className: 'topbar-sub', id: 'topbar-sub' })));
    topbar.appendChild(left);
    topbar.appendChild(GhostUI.h('div', { className: 'topbar-actions', id: 'topbar-actions' }));
    main.appendChild(topbar);

    const view = GhostUI.h('main', { className: 'view', id: 'view' });
    main.appendChild(view);

    const scrim = GhostUI.h('div', { className: 'scrim', id: 'scrim', onClick: () => toggleNav(false) });

    shell.appendChild(nav);
    shell.appendChild(main);
    root.appendChild(shell);
    root.appendChild(scrim);
  }

  function toggleNav(force) {
    const nav = document.getElementById('shell-nav');
    const scrim = document.getElementById('scrim');
    if (!nav) return;
    const open = force === undefined ? !nav.classList.contains('open') : force;
    nav.classList.toggle('open', open);
    scrim.classList.toggle('show', open);
  }

  // ── Auth flow ──
  function showLogin() {
    root.innerHTML = '';
    const wrap = GhostUI.h('div', { className: 'locked' });
    const card = GhostUI.h('div', { className: 'locked-card' });
    const brand = GhostUI.h('div', { className: 'locked-brand' });
    brand.appendChild(GhostUI.ghostMark('md'));
    brand.appendChild(GhostUI.h('span', { className: 'shell-brand-name' }, 'Ghost'));
    card.appendChild(brand);
    card.appendChild(GhostUI.h('div', { className: 'type-callout text-tertiary', style: 'margin-bottom:var(--s-5)' }, 'Enter your password to continue.'));

    const pw = GhostUI.input('Owner password', 'password');
    pw.style.marginBottom = 'var(--s-2)';
    const remember = GhostUI.h('label', { className: 'row-flex', style: 'font-size:var(--t-foot);color:var(--ink-faint);margin-bottom:var(--s-4);gap:var(--s-2)' });
    const cb = GhostUI.h('input', { type: 'checkbox', id: 'remember' });
    cb.style.width = 'auto';
    remember.appendChild(cb);
    remember.appendChild(GhostUI.h('span', {}, 'Keep me signed in on this device'));

    const err = GhostUI.h('div', { className: 'type-foot', style: 'color:var(--bad);min-height:18px;margin-bottom:var(--s-3)' });

    const submit = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-lg', style: 'width:100%;justify-content:center' }, 'Continue');
    const doLogin = async () => {
      err.textContent = '';
      submit.disabled = true;
      try {
        await GhostAPI.post('/api/login', { password: pw.value, remember_me: cb.checked });
        start();
      } catch (e) {
        err.textContent = 'That password isn’t right.';
        submit.disabled = false;
      }
    };
    submit.addEventListener('click', doLogin);
    pw.addEventListener('keydown', e => { if (e.key === 'Enter') doLogin(); });

    card.appendChild(pw);
    card.appendChild(remember);
    card.appendChild(err);
    card.appendChild(submit);
    wrap.appendChild(card);
    root.appendChild(wrap);
    setTimeout(() => pw.focus(), 50);
  }

  function lock() {
    // Client-side lock: return to the sign-in screen. The session cookie remains,
    // but the console is no longer visible until the password is entered again.
    showLogin();
  }

  async function start() {
    root = document.getElementById('app') || document.body;
    let status = { needs_setup: false };
    try { status = await GhostAPI.get('/api/status'); } catch (e) { /* offline */ }

    if (status.needs_setup) {
      // First run — show the setup wizard, not the console.
      buildWizardScreen();
      return;
    }

    // Setup complete: require a valid session.
    let authed = false;
    try { authed = (await GhostAPI.get('/api/admin/auth/check')).ok === true; } catch (e) { authed = false; }
    if (!authed) { showLogin(); return; }

    buildShell();
    window.addEventListener('hashchange', () => {
      const name = location.hash.replace('#', '') || 'home';
      if (sections.has(name)) render(name);
    });
    // Close mobile nav on navigation
    document.addEventListener('click', e => {
      const nav = document.getElementById('shell-nav');
      if (nav && nav.classList.contains('open') && !nav.contains(e.target) && !e.target.closest('.nav-toggle')) toggleNav(false);
    });
    render(location.hash.replace('#', '') || 'home');
  }

  function buildWizardScreen() {
    root.innerHTML = '';
    const view = GhostUI.h('main', { className: 'view', id: 'view', style: 'max-width:520px' });
    root.appendChild(view);
    if (sections.has('wizard')) sections.get('wizard').loader(view);
    else view.appendChild(GhostUI.emptyState('Setting up', 'Ghost is preparing for first use.'));
  }

  GhostAPI.setOnAuthExpired(() => { showLogin(); });

  return { registerSection, navigate, start, setActions, render, lock };
})();

window.addEventListener('DOMContentLoaded', () => GhostApp.start());
