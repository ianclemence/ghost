/* Ghost — wizard.js
   Setup flow: welcome → wifi → password → model → pairing.
   Plus login and "already set up" states. */
(function () {
    'use strict';

    const $ = window.Ghost.$;
    const show = window.Ghost.show;
    const hide = window.Ghost.hide;
    const msg = window.Ghost.msg;

    const SCREENS = ['welcome', 'wifi', 'password', 'model', 'done', 'login', 'already'];
    const STEPS = { wifi: 1, password: 2, model: 3, done: 4 };

    let current = 'welcome';
    let selectedWifi = null;
    let adminPassword = '';
    let currentPassword = '';
    let rerunMode = false;

    const PROVIDERS = {
        ollama: 'Ollama (Local)',
        moonshot: 'Moonshot (Kimi)',
        anthropic: 'Anthropic (Claude)',
        openai: 'OpenAI',
        openrouter: 'OpenRouter',
        groq: 'Groq',
        deepseek: 'DeepSeek',
        gemini: 'Google Gemini',
        zhipu: 'Zhipu (GLM)',
    };

    const CHOICES = [
        { key: 'qwen3:0.6b', title: 'Light', desc: 'Fast and quiet. Perfect for everyday chats on small hardware.' },
        { key: 'qwen3:1.7b', title: 'Balanced', desc: 'Smarter answers, still quick. A great all-rounder.', recommended: true },
        { key: 'qwen3:4b', title: 'Smart', desc: 'The most capable. Best on devices with 8GB+ of memory.' },
    ];

    /* ── screen switching ── */
    function goto(name) {
        if (!SCREENS.includes(name)) return;
        document.querySelectorAll('.screen').forEach((s) => s.classList.remove('active'));
        $('screen-' + name).classList.add('active');
        current = name;
        updateProgress();
        if (name === 'wifi') scanWifi();
        if (name === 'password') setTimeout(() => $('admin-password').focus(), 220);
        if (name === 'login') setTimeout(() => $('login-password').focus(), 220);
    }

    function updateProgress() {
        const step = STEPS[current];
        const bar = $('wizard-progress');
        if (!step) { hide(bar); return; }
        show(bar);
        $('wizard-progress-fill').style.width = (step / 4) * 100 + '%';
        $('wizard-progress-label').textContent = 'Step ' + step + ' of 4';
        const names = { 1: 'Connect', 2: 'Secure', 3: 'Choose', 4: 'Done' };
        $('wizard-progress-name').textContent = names[step];
    }

    /* ── init ── */
    async function init() {
        renderChoices();
        renderProviders();
        bindEvents();

        let status = null;
        try { status = await (await fetch('/api/status')).json(); } catch (e) {}

        if (!status) { goto('already'); return; }
        if (status.needs_setup || !status.admin_configured) {
            goto('welcome');
        } else if (status.force) {
            goto('login');
        } else {
            goto('already');
        }
    }

    function bindEvents() {
        $('btn-welcome').addEventListener('click', () => goto('wifi'));
        $('btn-wifi-connect').addEventListener('click', connectWifi);
        $('btn-wifi-skip').addEventListener('click', () => goto('password'));
        $('wifi-password').addEventListener('keydown', (e) => { if (e.key === 'Enter') connectWifi(); });

        $('btn-pw-back').addEventListener('click', () => goto('wifi'));
        $('btn-pw-next').addEventListener('click', validatePassword);
        $('admin-password').addEventListener('keydown', (e) => { if (e.key === 'Enter') validatePassword(); });

        $('btn-model-back').addEventListener('click', () => goto('password'));
        $('btn-model-next').addEventListener('click', () => { goto('done'); complete(); });
        $('btn-advanced-model').addEventListener('click', toggleAdvanced);

        $('pairing-code').addEventListener('click', copyPairingCode);
        $('btn-done').addEventListener('click', enterDashboard);

        $('btn-login').addEventListener('click', login);
        $('btn-login-cancel').addEventListener('click', () => location.reload());
        $('login-password').addEventListener('keydown', (e) => { if (e.key === 'Enter') login(); });
        $('btn-forgot').addEventListener('click', showForgotModal);

        $('btn-already').addEventListener('click', () => location.reload());
    }

    /* ── model choices ── */
    function renderChoices() {
        const wrap = $('model-choices');
        wrap.innerHTML = CHOICES.map((c, i) =>
            '<button class="choice-card' + (i === 1 ? ' selected' : '') + '" type="button" data-model="' + c.key + '">' +
            '<span class="choice-card__radio" aria-hidden="true"></span>' +
            '<span>' +
            '<span class="choice-card__title">' + c.title + (c.recommended ? ' <span class="badge badge--ok">Recommended</span>' : '') + '</span>' +
            '<span class="choice-card__desc">' + c.desc + '</span>' +
            '</span></button>'
        ).join('');
        wrap.querySelectorAll('.choice-card').forEach((el) => {
            el.addEventListener('click', () => {
                wrap.querySelectorAll('.choice-card').forEach((x) => x.classList.remove('selected'));
                el.classList.add('selected');
            });
        });
    }

    function toggleAdvanced() {
        const box = $('advanced-model');
        const hidden = box.classList.contains('hidden');
        if (hidden) {
            show(box);
            $('btn-advanced-model').textContent = 'Hide advanced options';
            // Sync advanced selects to the recommended choice.
            $('provider').value = 'ollama';
            updateModels();
        } else {
            hide(box);
            $('btn-advanced-model').textContent = 'Show advanced options';
        }
    }

    function renderProviders() {
        $('provider').innerHTML = Object.keys(PROVIDERS)
            .map((k) => '<option value="' + k + '">' + PROVIDERS[k] + '</option>').join('');
        $('ai-provider').innerHTML = $('provider').innerHTML;
    }

    function updateModels() {
        const provider = $('provider').value;
        const sel = $('model');
        const opts = {
            ollama: ['qwen3:0.6b|Qwen 3 0.6B (Fastest, Light)', 'qwen3:1.7b|Qwen 3 1.7B (Balanced)', 'qwen3:4b|Qwen 3 4B (Smart, Needs 8GB+)'],
            moonshot: ['kimi-k2.5|Kimi K2.5'],
            anthropic: ['claude-sonnet-4-20250514|Claude Sonnet 4', 'claude-3-5-haiku-20241022|Claude 3.5 Haiku (Fast)'],
            openai: ['gpt-4o|GPT-4o', 'gpt-4o-mini|GPT-4o Mini (Fast)'],
            groq: ['llama-3.3-70b-versatile|Llama 3.3 70B', 'llama-3.1-8b-instant|Llama 3.1 8B (Fast)'],
        };
        const list = opts[provider] || [];
        sel.innerHTML = list.length
            ? list.map((o) => { const [v, l] = o.split('|'); return '<option value="' + v + '">' + l + '</option>'; }).join('')
            : '<option value="">Select a model</option>';
    }

    /* ── wifi ── */
    async function scanWifi() {
        try {
            const data = await (await fetch('/api/scan-wifi')).json();
            hide($('wifi-loading'));
            show($('wifi-list'));
            if (data.networks && data.networks.length) {
                $('wifi-count').textContent = data.networks.length + ' network' + (data.networks.length !== 1 ? 's' : '') + ' found';
                $('wifi-networks').innerHTML = data.networks.map((n) => {
                    const lit = (b) => n.signal >= b ? ' lit' : '';
                    return '<button class="wifi-item" type="button" data-ssid="' + Ghost.esc(n.ssid) + '" data-secured="' + n.encrypted + '">' +
                        '<span class="wifi-signal" aria-hidden="true"><span class="wifi-bar' + lit(25) + '"></span><span class="wifi-bar' + lit(50) + '"></span><span class="wifi-bar' + lit(75) + '"></span><span class="wifi-bar' + lit(95) + '"></span></span>' +
                        '<span class="wifi-ssid">' + Ghost.esc(n.ssid) + '</span>' +
                        (n.encrypted ? '<span class="wifi-lock" aria-hidden="true">&#128274;</span>' : '') +
                        '</button>';
                }).join('');
                $('wifi-networks').querySelectorAll('.wifi-item').forEach((el) => {
                    el.addEventListener('click', () => selectWifi(el));
                });
            } else {
                $('wifi-networks').innerHTML = '<p class="wifi__skip">No networks found — try the scan again.</p>';
            }
        } catch (e) {
            hide($('wifi-loading'));
            $('wifi-loading').innerHTML = '<p class="error-text">Couldn\'t scan for networks.</p>';
        }
    }

    function selectWifi(el) {
        selectedWifi = el.dataset.ssid;
        document.querySelectorAll('.wifi-item').forEach((x) => x.classList.remove('selected'));
        el.classList.add('selected');
        if (el.dataset.secured === 'true') {
            show($('wifi-password-form'));
            $('wifi-password').focus();
        } else {
            connectWifi();
        }
    }

    async function connectWifi() {
        if (!selectedWifi) return;
        const password = $('wifi-password').value;
        msg('wifi-error', '');
        try {
            const res = await fetch('/api/connect-wifi', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ssid: selectedWifi, password }),
            });
            const data = await res.json();
            if (data.ok) {
                goto('password');
            } else {
                msg('wifi-error', data.error || 'Failed to connect.');
            }
        } catch (e) {
            msg('wifi-error', 'Connection failed.');
        }
    }

    /* ── password ── */
    function validatePassword() {
        const pw = $('admin-password').value;
        const confirm = $('admin-password-confirm').value;
        msg('password-error', '');

        if (pw === '' && rerunMode) {
            adminPassword = '';
            goto('model');
            return;
        }
        if (pw.length < 12) {
            msg('password-error', 'Password must be at least 12 characters.');
            return;
        }
        if (pw !== confirm) {
            msg('password-error', 'Passwords don\u2019t match.');
            return;
        }
        adminPassword = pw;
        goto('model');
    }

    /* ── login ── */
    async function login() {
        const pw = $('login-password').value;
        const rememberMe = $('login-remember') && $('login-remember').checked;
        msg('login-error', '');
        if (!pw) { msg('login-error', 'Enter the admin password.'); return; }
        try {
            const res = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password: pw, remember_me: rememberMe }),
            });
            const data = await res.json();
            if (data.ok) {
                currentPassword = pw;
                window.Dashboard.open();
            } else {
                msg('login-error', data.error || 'Login failed.');
            }
        } catch (e) {
            msg('login-error', 'Login failed.');
        }
    }

    /* ── forgot password modal ── */
    function showForgotModal() {
        const root = $('modal-root');
        const backdrop = document.createElement('div');
        backdrop.className = 'modal-backdrop';
        backdrop.innerHTML =
            '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="forgot-modal-title">' +
            '<h3 class="modal__title" id="forgot-modal-title">Forgot your password?</h3>' +
            '<p class="forgot-modal__body">Set a new one over SSH, or from the recovery panel if Ghost won\'t start.</p>' +
            '<div class="forgot-modal__option">' +
            '<span class="forgot-modal__option-label">Over SSH (Ghost running)</span>' +
            '<span class="forgot-modal__option-desc">Connect to the device and run:</span>' +
            '<code class="code">sudo ghost reset-password --force</code>' +
            '</div>' +
            '<div class="forgot-modal__option">' +
            '<span class="forgot-modal__option-label">Recovery panel (Ghost won\'t start)</span>' +
            '<span class="forgot-modal__option-desc">Reboot the device, then open <code class="code">http://ghost.local:8766</code> within 15 minutes and choose "Reset admin password".</span>' +
            '</div>' +
            '<div class="modal__actions">' +
            '<button class="btn btn--primary" id="forgot-modal-close" type="button">Close</button>' +
            '</div>' +
            '</div>';

        const close = () => {
            document.removeEventListener('keydown', onKey);
            backdrop.remove();
        };
        const onKey = (e) => { if (e.key === 'Escape') close(); };
        backdrop.addEventListener('click', (e) => { if (e.target === backdrop) close(); });
        backdrop.querySelector('#forgot-modal-close').addEventListener('click', close);
        document.addEventListener('keydown', onKey);
        root.appendChild(backdrop);
        backdrop.querySelector('#forgot-modal-close').focus();
    }

    /* ── complete ── */
    async function complete() {
        hide($('setup-error'));
        hide($('pairing-box'));
        hide($('btn-done'));
        show($('setup-status'));

        const selected = document.querySelector('#model-choices .choice-card.selected');
        let model = selected ? selected.dataset.model : ($('model').value || '');
        if (!model && $('provider').value === 'ollama') model = 'qwen3:0.6b';

        const setupData = {
            admin_password: adminPassword,
            current_password: currentPassword,
            model: model,
            provider: $('provider').value,
            ollama_url: 'http://localhost:11434',
        };

        try {
            const res = await fetch('/api/configure', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(setupData),
            });
            const data = await res.json();
            if (!data.ok) {
                hide($('setup-status'));
                msg('setup-error', data.error || 'Failed to configure.');
                return;
            }
            let code = null;
            try {
                const c = await (await fetch('/api/pairing-code')).json();
                if (c.ok) code = c.code;
            } catch (e) {}
            hide($('setup-status'));
            show($('pairing-box'));
            if (code) $('pairing-code').textContent = code;
            show($('btn-done'));
        } catch (e) {
            hide($('setup-status'));
            msg('setup-error', 'Failed to configure. Please try again.');
        }
    }

    async function enterDashboard() {
        // After a fresh setup there is no session yet, so log in with the
        // password just chosen before the dashboard loads its admin data.
        if (adminPassword) {
            try {
                await fetch('/api/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ password: adminPassword }),
                });
            } catch (e) {}
        }
        window.Dashboard.open();
    }

    function copyPairingCode() {
        const btn = $('pairing-code');
        const code = btn.textContent;
        navigator.clipboard.writeText(code).then(() => {
            btn.classList.add('copied');
            btn.textContent = 'Copied';
            setTimeout(() => {
                btn.classList.remove('copied');
                btn.textContent = code;
            }, 1400);
        }).catch(() => {});
    }

    /* ── expose ── */
    window.Wizard = {
        init,
        goto,
        complete,
        getCurrentPassword: () => currentPassword,
        isRerun: () => rerunMode,
    };

    document.addEventListener('DOMContentLoaded', init);
})();