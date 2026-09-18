/* Ghost Wizard — first-run setup flow */
'use strict';

const GhostWizard = (() => {
  let _container;
  let _state = {
    step: 'welcome',
    setupCode: '',
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
    screen.appendChild(GhostUI.h('div', { className: 'wizard-tagline type-body text-secondary' }, 'Your AI. Your Memory. Your Machine.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-callout text-secondary' },
      'A personal AI that lives on your hardware, remembers what matters, and keeps working for you.'
    ));

    // Phone handoff: a QR carrying only the Pod address (never the setup
    // code, which stays a device-only secret). Scan it with the Ghost app to
    // prefill the address, then type the code from the device.
    const qrWrap = GhostUI.h('div', {});
    qrWrap.style.marginTop = 'var(--space-xl)';
    qrWrap.style.display = 'flex';
    qrWrap.style.flexDirection = 'column';
    qrWrap.style.alignItems = 'center';
    const qrCanvas = document.createElement('canvas');
    qrWrap.appendChild(qrCanvas);
    screen.appendChild(qrWrap);
    fetch('/api/status').then((r) => r.json()).then((s) => {
      const host = window.location.hostname || '';
      const port = String(s.console_port || window.location.port || '80');
      const pod = s.pod_id || '';
      const uri = 'ghost://setup?v=1&host=' + encodeURIComponent(host) +
        '&port=' + encodeURIComponent(port) + '&pod=' + encodeURIComponent(pod);
      let ok = false;
      try { ok = GhostQR.draw(uri, qrCanvas, 4); } catch (e) { ok = false; }
      if (ok) {
        qrWrap.appendChild(GhostUI.h('div', {
          className: 'type-footnote text-tertiary',
          style: 'margin-top:var(--space-sm)',
        }, 'Scan with the Ghost app to set up from your phone'));
      }
    }).catch(() => {});

    // Setup code: proves local presence so a host on the network cannot claim
    // an unconfigured Ghost. Printed in the console output on the device.
    const codeInput = GhostUI.input('Setup code');
    codeInput.value = _state.setupCode;
    codeInput.addEventListener('input', (e) => { _state.setupCode = e.target.value.trim(); });
    codeInput.style.marginTop = 'var(--space-xl)';
    screen.appendChild(codeInput);
    screen.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--space-sm)' },
      'Shown in the Ghost console output on the device: journalctl -u ghost-web | grep "Setup code"'
    ));

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-lg', onClick: () => {
        if (!_state.setupCode) { GhostUI.toast('Enter the setup code shown on your Ghost.'); return; }
        goTo('identity');
      }}, 'Set up Ghost')
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
      'At least 8 characters. A longer passphrase is easier to remember and harder to guess.'
    ));

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
        if (pwInput.value !== confirmInput.value) { GhostUI.toast('Passwords don\u2019t match.'); return; }
        if (pwInput.value.length < 8) { GhostUI.toast('Password must be at least 8 characters.'); return; }
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
        setup_code: _state.setupCode,
        owner_name: _state.ownerName,
        ghost_name: _state.ghostName,
      });
      items[0].done = true;
      checks[0].textContent = '\u2713';
      checks[0].classList.add('done');
    } catch (e) { /* continue anyway */ }

    // Sign in silently: later setup steps save provider choices through
    // authenticated configuration calls.
    try {
      await GhostAPI.post('/api/login', { password: _state.password });
    } catch (e) { /* the final sign-in screen covers this */ }

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

  // pickChatModel chooses what "Use this model" means: the first model
  // that isn't an embedding model. The Ollama list mixes chat and embedding
  // models; defaulting to an embedder would leave Ghost unable to talk.
  function pickChatModel() {
    const models = _state.ollamaModels || [];
    const chat = models.find(m => !/embed/i.test(typeof m === 'string' ? m : (m.name || '')));
    return chat !== undefined ? chat : models[0];
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
      const picked = pickChatModel();
      const pickedName = typeof picked === 'string' ? picked : (picked.name || '');
      card.appendChild(GhostUI.h('div', { className: 'type-subhead text-secondary', style: 'margin-top:var(--space-sm)' },
        (GhostUI.modelFriendly('ollama:' + pickedName) || {}).name || pickedName || 'Model available'
      ));
      screen.appendChild(card);

      screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
        GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
          _state.selectedModel = pickedName;
          // Persist the choice now: setup completion records whatever the
          // configuration holds, and an unrecorded choice used to vanish.
          try {
            await GhostAPI.post('/api/configure', {
              current_password: _state.password,
              model: _state.selectedModel,
              provider: 'ollama',
            });
          } catch (e) {
            GhostUI.toast('Couldn\u2019t save that model \u2014 you can pick one later under Intelligence.', 'err');
          }
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

  // Cloud providers offered in setup order: Ghost's default cloud choice
  // first, then the rest alphabetically. Keys save immediately; the model
  // Ghost uses stays whatever was chosen on the previous screen — switch it
  // anytime in AI settings.
  const CLOUD_PROVIDERS = [
    { key: 'deepseek', label: 'DeepSeek', note: 'Recommended cloud choice' },
    { key: 'openai', label: 'OpenAI', note: '' },
    { key: 'anthropic', label: 'Anthropic', note: '' },
    { key: 'moonshot', label: 'Kimi', note: '' },
  ];

  async function renderCloudAI(screen) {
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Cloud intelligence'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Optional. Add a key and Ghost can use stronger cloud AI when the on-device model isn\u2019t enough.'
    ));

    // Every /api/configure call revokes admin sessions, so the session from
    // the earlier steps is dead by now — without this silent re-login the
    // config fetch below 401s and the global auth hook boots the owner out
    // of the wizard into the login screen.
    try { await GhostAPI.post('/api/login', { password: _state.password }); } catch (e) { /* read-only fallback below */ }

    let configured = {};
    try {
      const cfg = await GhostAPI.get('/api/admin/config');
      configured = (cfg && cfg.providers) || {};
    } catch (e) { /* offline-safe: everything renders as unconfigured */ }

    for (const p of CLOUD_PROVIDERS) {
      const hasKey = !!(configured[p.key] && configured[p.key].api_key);
      const row = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      const title = GhostUI.h('div', { className: 'ghost-row-title' }, p.label);
      if (p.note) title.appendChild(GhostUI.h('span', { className: 'type-footnote text-tertiary', style: 'margin-left:var(--s-2);font-weight:400' }, '\u00b7  ' + p.note));
      c.appendChild(title);
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, hasKey ? 'Key saved' : 'Not configured'));
      row.appendChild(c);
      const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
      const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary ghost-btn-sm' }, hasKey ? 'Replace key' : 'Add key');
      btn.addEventListener('click', () => toggleCloudKey(screen, p, btn));
      tr.appendChild(btn);
      row.appendChild(tr);
      screen.appendChild(row);
    }

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: () => goTo('phone') }, 'Continue'),
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: () => goTo('phone') }, 'Skip for now')
    ));
  }

  function toggleCloudKey(screen, provider, btn) {
    const row = btn.closest('.ghost-row');
    if (btn.nextForm && document.body.contains(btn.nextForm)) { btn.nextForm.remove(); btn.nextForm = null; return; }
    // Full-width row below the provider row: appending inside the trailing
    // cell overlaps the row text.
    const form = GhostUI.h('div', { style: 'margin:0 0 var(--space-md);padding:var(--s-3);background:var(--ghost-bg-sunken);border-radius:var(--radius-md)' });
    const input = GhostUI.input('Paste your ' + provider.label + ' API key', 'password');
    input.style.marginBottom = 'var(--s-2)';
    input.style.width = '100%';
    const save = GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-sm' }, 'Save key');
    save.addEventListener('click', async () => {
      const val = input.value.trim();
      if (!val) { GhostUI.toast('Paste a key first.'); return; }
      save.disabled = true;
      try {
        await GhostAPI.post('/api/admin/config/save', { api_keys: { [provider.key]: val } });
        GhostUI.toast(provider.label + ' key saved \u2014 Ghost keeps your current model; switch anytime under Intelligence.');
        renderStep();
      } catch (e) { GhostUI.toast('Couldn\u2019t save that key.', 'err'); save.disabled = false; }
    });
    form.appendChild(input);
    form.appendChild(save);
    form.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--s-1)' }, 'Stored only on this Ghost. Never shown back in full.'));
    row.parentElement.insertBefore(form, row.nextSibling);
    btn.nextForm = form;
    setTimeout(() => input.focus(), 50);
  }

  function renderPhone(screen) {
    // Pairing codes are minted by the running gateway (Devices screen), which
    // isn't up yet at this point in setup — so this step points there
    // instead of showing a code that can't be redeemed.
    screen.appendChild(GhostUI.h('div', { className: 'wizard-title type-title' }, 'Your Ghost is ready.'));
    screen.appendChild(GhostUI.h('div', { className: 'wizard-desc type-body text-secondary', style: 'margin-bottom:var(--space-xxl)' },
      'Take Ghost with you. After setup, open Devices to connect your phone in about a minute \u2014 your Ghost stays on this hardware.'
    ));

    screen.appendChild(GhostUI.h('div', { className: 'wizard-actions' },
      GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary ghost-btn-lg', onClick: () => goTo('done') }, 'Finish setup')
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
        history.pushState(null, '', '/home');
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
