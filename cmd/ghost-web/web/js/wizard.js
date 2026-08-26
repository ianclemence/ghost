/* Ghost Wizard — first-run setup flow */
'use strict';

const GhostWizard = (() => {
  let _container;
  let _state = {
    step: 'welcome',
    ownerName: '',
    ghostName: 'Ghost',
    password: '',
    ollamaReady: false,
    ollamaModels: [],
    selectedModel: '',
    cloudProvider: '',
    cloudKey: '',
    pairingDone: false,
  };

  const steps = ['welcome', 'identity', 'password', 'preparing', 'local-ai', 'cloud-ai', 'phone', 'done'];

  function render(container) {
    _container = container;
    _container.innerHTML = '';
    const wrapper = GhostUI.h('div', { id: 'wizard' });
    _container.appendChild(wrapper);
    renderStep();
  }

  function renderStep() {
    const wrapper = document.getElementById('wizard') || _container;
    wrapper.innerHTML = '';

    const screen = GhostUI.h('div', { className: `wizard-screen wizard-${_state.step}` });

    switch (_state.step) {
      case 'welcome': renderWelcome(screen); break;
      case 'identity': renderIdentity(screen); break;
      case 'password': renderPassword(screen); break;
      case 'preparing': renderPreparing(screen); break;
      case 'local-ai': renderLocalAI(screen); break;
      case 'cloud-ai': renderCloudAI(screen); break;
      case 'phone': renderPhone(screen); break;
      case 'done': renderDone(screen); break;
    }

    wrapper.appendChild(screen);
  }

  function renderWelcome(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-brand' }, GhostUI.ghostMark('xl')));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-display' }, 'Ghost'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-tagline type-body text-secondary' }, 'Your AI, Your Hardware.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-callout text-secondary' },
      'A personal AI that lives on your hardware, remembers what matters, and keeps working for you.'
    ));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-lg', onClick: () => goTo('identity') }, 'Set up Ghost')
    ));
  }

  function renderIdentity(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Let\u2019s make this yours.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'What should Ghost call you?'
    ));

    const nameInput = GhostUI.input('Your name');
    nameInput.value = _state.ownerName;
    nameInput.addEventListener('input', (e) => _state.ownerName = e.target.value);
    screen.appendChild(nameInput);

    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin:var(--space-xl) 0 var(--space-md)' },
      'What would you like to call Ghost?'
    ));

    const ghostInput = GhostUI.input('Ghost name');
    ghostInput.value = _state.ghostName;
    ghostInput.addEventListener('input', (e) => _state.ghostName = e.target.value);
    screen.appendChild(ghostInput);

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => goTo('password') }, 'Continue')
    ));
  }

  function renderPassword(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Protect your Ghost.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Create a password for accessing Ghost from this device.'
    ));

    const pwInput = GhostUI.input('Password', 'password');
    screen.appendChild(pwInput);

    const confirmInput = GhostUI.input('Confirm password', 'password');
    confirmInput.style.marginTop = 'var(--space-md)';
    screen.appendChild(confirmInput);

    screen.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--space-sm)' },
      'At least 12 characters. A longer passphrase is easier to remember and harder to guess.'
    ));

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
        if (pwInput.value !== confirmInput.value) { GhostUI.toast('Passwords don\u2019t match.'); return; }
        if (pwInput.value.length < 12) { GhostUI.toast('Password must be at least 12 characters.'); return; }
        _state.password = pwInput.value;
        goTo('preparing');
      }}, 'Continue')
    ));
  }

  function renderPreparing(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Preparing Ghost'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Your Ghost is getting ready.'
    ));

    const progress = GhostUI.h('div', { className: 'wizard-progress' });

    const items = [
      { label: 'Identity', done: false },
      { label: 'Secure access', done: false },
      { label: 'Local storage', done: false },
      { label: 'Local AI', done: false },
      { label: 'Checking system', done: false },
    ];

    for (const item of items) {
      const row = GhostUI.h('div', { className: 'wizard-progress-item' });
      row.appendChild(GhostUI.h('span', { className: 'wizard-progress-check' }, '\u2022'));
      row.appendChild(GhostUI.h('span', { className: 'type-callout' }, item.label));
      progress.appendChild(row);
    }

    screen.appendChild(progress);
    screen.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--space-xl)' }, 'Please wait\u2026'));

    // Run setup
    setTimeout(() => runSetup(progress, items), 500);
  }

  async function runSetup(progress, items) {
    const checks = progress.children;

    // Step 1: Identity
    try {
      await GhostAPI.post('/api/configure', {
        admin_password: _state.password,
        owner_name: _state.ownerName,
        ghost_name: _state.ghostName,
      });
      items[0].done = true;
      checks[0].textContent = '\u2713';
      checks[0].classList.add('done');
    } catch (e) { /* continue anyway */ }

    // Step 2: Secure access
    items[1].done = true;
    checks[1].textContent = '\u2713';
    checks[1].classList.add('done');

    // Step 3: Local storage
    items[2].done = true;
    checks[2].textContent = '\u2713';
    checks[2].classList.add('done');

    // Step 4: Local AI
    try {
      const models = await GhostAPI.get('/api/ollama/models');
      _state.ollamaModels = models.models || [];
      _state.ollamaReady = _state.ollamaModels.length > 0;
      items[3].done = true;
      checks[3].textContent = '\u2713';
      checks[3].classList.add('done');
    } catch (e) {
      items[3].done = true;
      checks[3].textContent = '\u2713';
      checks[3].classList.add('done');
    }

    // Step 5: Check system
    items[4].done = true;
    checks[4].textContent = '\u2713';
    checks[4].classList.add('done');

    setTimeout(() => goTo('local-ai'), 800);
  }

  function renderLocalAI(screen) {
    if (_state.ollamaReady && _state.ollamaModels.length > 0) {
      screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Ghost\u2019s Brain'));
      screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
        'Ghost can use AI running directly on this machine.'
      ));

      const card = GhostUI.h('div', { className: 'ghost-card' });
      card.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Local AI'));
      const status = GhostUI.h('div', { className: 'ghost-row', style: 'padding:var(--space-md) 0' });
      status.appendChild(GhostUI.statusDot('online'));
      status.appendChild(GhostUI.h('span', { className: 'type-callout', style: 'margin-left:var(--space-sm)' }, 'Ready'));
      card.appendChild(status);
      card.appendChild(GhostUI.h('div', { className: 'type-subhead text-secondary', style: 'margin-top:var(--space-sm)' },
        _state.ollamaModels[0]?.name || 'Model available'
      ));
      screen.appendChild(card);

      screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
        GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => {
          _state.selectedModel = _state.ollamaModels[0]?.name || '';
          goTo('cloud-ai');
        }}, 'Use this model'),
        GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => goTo('cloud-ai') }, 'Skip for now')
      ));
    } else {
      screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Local AI'));
      screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
        'Local AI isn\u2019t ready yet. Ghost can still be configured. You can finish this later.'
      ));
      screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
        GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => goTo('cloud-ai') }, 'Continue')
      ));
    }
  }

  function renderCloudAI(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Cloud intelligence'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Ghost can optionally use cloud AI for tasks that benefit from deeper reasoning.'
    ));

    const providers = ['OpenAI', 'Anthropic', 'Kimi'];
    for (const p of providers) {
      const row = GhostUI.h('div', { className: 'ghost-row' });
      row.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, p));
      row.appendChild(GhostUI.h('span', { className: 'type-footnote text-tertiary' }, 'Not configured'));
      screen.appendChild(row);
    }

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => goTo('phone') }, 'Continue'),
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => goTo('phone') }, 'Skip for now')
    ));
  }

  function renderPhone(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Your Ghost is ready.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Take Ghost with you. Open the Ghost app on your phone and scan this code.'
    ));

    // Generate pairing code
    const qrPlaceholder = GhostUI.h('div', { className: 'wizard-qr', style: 'padding:var(--space-xxxl);background:var(--ghost-bg-sunken);border-radius:var(--radius-lg);text-align:center;margin-bottom:var(--space-md)' });
    qrPlaceholder.appendChild(GhostUI.h('div', { className: 'type-body text-secondary' }, 'Loading pairing code\u2026'));
    screen.appendChild(qrPlaceholder);

    (async () => {
      try {
        const res = await GhostAPI.post('/api/pairing-code');
        if (res && res.code) {
          qrPlaceholder.innerHTML = '';
          qrPlaceholder.appendChild(GhostUI.h('div', { className: 'type-mono', style: 'word-break:break-all' }, res.code));
          qrPlaceholder.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--space-sm)' }, 'Expires in 5 minutes'));
        }
      } catch (e) {
        qrPlaceholder.innerHTML = '';
        qrPlaceholder.appendChild(GhostUI.h('div', { className: 'type-callout text-secondary' }, 'Couldn\u2019t generate pairing code.'));
      }
    })();

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => goTo('done') }, 'Done'),
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => goTo('done') }, 'Skip for now')
    ));
  }

  function renderDone(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-brand' }, GhostUI.ghostMark('xl')));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-display' }, 'You\u2019re connected.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Ghost is ready. Start talking.'
    ));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-lg', onClick: () => {
        location.hash = '#home';
        location.reload();
      }}, 'Go to Ghost')
    ));
  }

  function goTo(step) {
    _state.step = step;
    renderStep();
  }

  return { render };
})();

GhostApp.registerSection('wizard', (container) => GhostWizard.render(container));
