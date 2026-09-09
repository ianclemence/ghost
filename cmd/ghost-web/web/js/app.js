/* Ghost App — router, app shell, auth gate, state.
   Loaded BEFORE the section files so GhostApp.registerSection is defined. */
'use strict';

const GhostApp = (() => {
  const sections = new Map();   // name -> { loader }
  const navTitles = new Map();  // name -> title
  let current = null;
  let root = null;

  // ── Navigation model (the product's information architecture) ──
  // Ghost is ONE persistent personal AI. There is no chat list, no "New
  // Chat", and no conversation switcher. Home is the relationship; Memory is
  // what Ghost knows; Activity is what Ghost has done; Routines are what
  // Ghost does automatically; Connected Apps are what Ghost can access;
  // Devices are where Ghost has hands. System holds owner/appliance config.
  const NAV = [
    {
      label: 'Ghost',
      items: [
        { name: 'home', title: 'Home', glyph: 'home' },
        { name: 'memory', title: 'Memory', glyph: 'memory' },
        { name: 'activity', title: 'Activity', glyph: 'activity' },
        { name: 'routines', title: 'Routines', glyph: 'automation' },
        { name: 'devices', title: 'Devices', glyph: 'device' },
        { name: 'integrations', title: 'Connected Apps', glyph: 'integrations' },
      ],
    },
    {
      label: 'Connect',
      items: [
        { name: 'channels', title: 'Channels', glyph: 'channel' },
      ],
    },
    {
      label: 'System',
      items: [
        { name: 'skills', title: 'Skills', glyph: 'skill' },
        { name: 'automations', title: 'Automations', glyph: 'automation' },
        { name: 'system', title: 'System', glyph: 'system' },
        { name: 'ai', title: 'AI & providers', glyph: 'ai' },
        { name: 'security', title: 'Security', glyph: 'security' },
        { name: 'help', title: 'Help', glyph: 'help' },
        { name: 'about', title: 'About', glyph: 'about' },
      ],
    },
  ];
  NAV.forEach(g => g.items.forEach(i => navTitles.set(i.name, i.title)));

  const GLYPHS = {
    home: '<path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/><polyline points="9 22 9 12 15 12 15 22" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    memory: '<path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/>',
    activity: '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    automation: '<polyline points="23 4 23 10 17 10" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polyline points="1 20 1 14 7 14" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    skill: '<path d="M20.24 12.24a6 6 0 0 0-8.49-8.49L5 10.5V19h8.5z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/><line x1="16" y1="8" x2="2" y2="22" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/><line x1="17.5" y1="15" x2="9" y2="15" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    device: '<rect x="5" y="2" width="14" height="20" rx="2" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round"/><line x1="12" y1="18" x2="12.01" y2="18" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    channel: '<path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    integrations: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    system: '<rect x="6" y="6" width="12" height="12" rx="2" fill="none" stroke="currentColor" stroke-width="1.7"/><rect x="10" y="10" width="4" height="4" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    ai: '<polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round"/>',
    security: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/>',
    help: '<circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/><line x1="12" y1="17" x2="12.01" y2="17" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    about: '<circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="1.7"/><line x1="12" y1="16" x2="12" y2="12" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/><line x1="12" y1="8" x2="12.01" y2="8" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    update: '<polyline points="23 4 23 10 17 10" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polyline points="1 20 1 14 7 14" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
    backup: '<path d="M3 7v10a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-6l-2-2H5a2 2 0 0 0-2 2z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" stroke-linecap="round"/><polyline points="9 14 11 16 15 12" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>',
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
    // Restart the section entrance animation (calm settle, not a bounce).
    view.classList.remove('view-enter');
    void view.offsetWidth;
    view.classList.add('view-enter');
    setActions(null);
    // loading state
    view.appendChild(GhostUI.loading('Loading…'));
    updateActiveNav(name);
    updateTitle(name);

    const sec = sections.get(name);
    let cancelled = false;

    Promise.resolve(sec.loader(view))
      .catch(err => {
        if (current !== name || cancelled) return;
        view.innerHTML = '';
        view.appendChild(GhostUI.errorState(
          "Something went wrong",
          (err && err.message) ? err.message : "This page couldn’t be loaded."
        ));
      });
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
    // Server-side sign-out first so the session cannot be reused; the
    // sign-in screen follows regardless of the outcome.
    GhostAPI.post('/api/logout', {}).catch(() => {}).finally(() => showLogin());
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

  function currentSection() { return current; }

  return { registerSection, navigate, start, setActions, render, lock, currentSection };
})();

window.addEventListener('DOMContentLoaded', () => GhostApp.start());
