/* Ghost — dashboard.js
   Five sections: Overview, Assistant, Connections, Skills, Settings. */
(function () {
    'use strict';

    const $ = window.Ghost.$;
    const show = window.Ghost.show;
    const hide = window.Ghost.hide;
    const msg = window.Ghost.msg;
    const api = window.Ghost.api;
    const esc = window.Ghost.esc;

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

    let currentTab = 'overview';
    let logsTimer = null;

    const toggle = {
        set(id, on) {
            const el = $(id);
            if (!el) return;
            el.classList.toggle('on', !!on);
            el.setAttribute('aria-checked', on ? 'true' : 'false');
        },
        isOn(id) { return $(id) && $(id).classList.contains('on'); },
    };

    /* ── session / shell ── */
    window.Ghost.onSessionExpired = function () {
        window.Ghost.toast.err('Session expired. Please log in again.');
        logout();
    };

    function open() {
        hide($('wizard'));
        show($('dashboard'));
        switchTab('overview');
        loadAll();
    }

    function logout() {
        hide($('dashboard'));
        show($('wizard'));
        currentTab = 'overview';
        stopLogs();
        if (window.Wizard) window.Wizard.goto('login');
        const pw = $('login-password');
        if (pw) pw.value = '';
    }

    const PAGE_TITLES = {
        overview: 'Overview',
        assistant: 'Assistant',
        connections: 'Connections',
        skills: 'Skills',
        settings: 'Settings',
    };

    function switchTab(name) {
        currentTab = name;
        document.querySelectorAll('.dash-nav__item, .dash-side__nav-item').forEach((b) => {
            b.classList.toggle('active', b.dataset.tab === name);
        });
        document.querySelectorAll('.tab-panel').forEach((p) => {
            p.classList.toggle('active', p.id === 'tab-' + name);
        });
        var titleEl = document.getElementById('dash-page-title');
        if (titleEl) titleEl.textContent = PAGE_TITLES[name] || name;
        window.scrollTo(0, 0);
        if (name === 'settings') startLogs(); else stopLogs();
    }

    function loadAll() {
        loadOverview();
        loadAssistant();
        loadConnections();
        loadSkills();
        loadSettings();
    }

    /* ════════════════════════════════════ OVERVIEW ════════════════════════════════════ */
    async function loadOverview() {
        try {
            const data = await api('/api/admin/status');
            setEmber('online');
            $('dash-status-text').textContent = 'Online';
            $('dash-status-dot').classList.remove('offline', 'unknown');
            var mobileStatusText = $('dash-status-text-mobile');
            var mobileStatusDot = $('dash-status-dot-mobile');
            if (mobileStatusText) mobileStatusText.textContent = 'Online';
            if (mobileStatusDot) mobileStatusDot.classList.remove('offline', 'unknown');

            $('stat-cpu').textContent = (data.cpu_percent || 0).toFixed(0) + '%';
            const memPct = data.memory.total ? Math.round((data.memory.used / data.memory.total) * 100) : 0;
            $('stat-mem').innerHTML = memPct + '% <small>' + Ghost.fmtBytes(data.memory.used) + '</small>';
            const diskPct = data.disk.total ? Math.round((data.disk.used / data.disk.total) * 100) : 0;
            $('stat-disk').innerHTML = diskPct + '% <small>' + Ghost.fmtBytes(data.disk.used) + '</small>';
            $('stat-uptime').textContent = data.uptime || '\u2014';

            $('services').innerHTML = (data.services || []).map((s) =>
                '<div class="service-row"><span>' + esc(s.name) + '</span>' +
                '<span class="badge ' + (s.active ? 'badge--ok' : 'badge--fail') + '">' + (s.active ? 'Running' : 'Stopped') + '</span></div>'
            ).join('') || '<p class="help-text">No services reported.</p>';

            $('cfg-host').textContent = data.hostname || '\u2014';
            $('cfg-ip').textContent = data.ip || '\u2014';
            $('cfg-ver').textContent = data.version || '\u2014';
            $('cfg-provider').textContent = data.provider || '\u2014';
            $('cfg-model').textContent = data.model || '\u2014';
            $('cfg-ollama').textContent = data.ollama_url || '\u2014';

            const offlineServices = (data.services || []).filter((s) => !s.active);
            if (offlineServices.length) {
                $('overview-title').textContent = 'Some services are stopped.';
                $('overview-sub').textContent = offlineServices.map((s) => s.name).join(', ') + (offlineServices.length === 1 ? ' needs attention.' : ' need attention.');
            } else {
                $('overview-title').textContent = 'Your Ghost is online.';
                $('overview-sub').textContent = 'Everything looks good on this device.';
            }
        } catch (e) {
            setEmber('offline');
            $('dash-status-text').textContent = 'Offline';
            $('dash-status-dot').classList.add('offline');
            var mobileStatusText = $('dash-status-text-mobile');
            var mobileStatusDot = $('dash-status-dot-mobile');
            if (mobileStatusText) mobileStatusText.textContent = 'Offline';
            if (mobileStatusDot) mobileStatusDot.classList.add('offline');
        }
    }

    function setEmber(state) {
        const orbs = [$('overview-ember'), $('header-ember'), $('header-ember-mobile')];
        orbs.forEach((o) => {
            if (!o) return;
            o.classList.remove('ember--offline', 'ember--thinking');
            if (state === 'offline') o.classList.add('ember--offline');
            if (state === 'thinking') o.classList.add('ember--thinking');
        });
    }

    async function runDoctor() {
        const box = $('doctor-results');
        box.innerHTML = '<div class="loading"><span class="spinner"></span>Running checks&hellip;</div>';
        try {
            const data = await api('/api/admin/doctor');
            box.innerHTML = (data.checks || []).map((c) =>
                '<div class="check-row"><span class="badge ' +
                (c.status === 'ok' ? 'badge--ok' : c.status === 'fail' ? 'badge--fail' : 'badge--warn') + '">' +
                esc(c.status) + '</span><span class="check-row__name">' + esc(c.name) + '</span>' +
                '<span class="check-row__msg">' + esc(c.message || '') + '</span></div>'
            ).join('') || '<p class="help-text">No checks.</p>';
        } catch (e) {
            box.innerHTML = '<p class="error-text">Couldn\u2019t run checks.</p>';
        }
    }

    async function startUpdate() {
        const btn = $('update-btn');
        const log = $('update-log');
        btn.disabled = true;
        show(log);
        log.textContent = 'Starting update\u2026';
        try {
            await api('/api/admin/update', 'POST');
            const poll = setInterval(async () => {
                try {
                    const data = await api('/api/admin/update/status');
                    log.textContent = data.log || '';
                    log.scrollTop = log.scrollHeight;
                    if (!data.running) {
                        clearInterval(poll);
                        btn.disabled = false;
                        Ghost.toast(data.success ? 'Update complete.' : 'Update failed.', data.success ? 'ok' : 'err');
                        loadOverview();
                    }
                } catch (e) { clearInterval(poll); btn.disabled = false; }
            }, 1000);
        } catch (e) {
            btn.disabled = false;
            log.textContent = 'Couldn\u2019t start update.';
        }
    }

    /* ════════════════════════════════════ ASSISTANT ════════════════════════════════════ */
    let mcpServers = {};

    async function loadAssistant() {
        if (!$('ai-provider').options.length) {
            $('ai-provider').innerHTML = Object.keys(PROVIDERS)
                .map((k) => '<option value="' + k + '">' + PROVIDERS[k] + '</option>').join('');
        }
        loadAIConfig();
        loadPersonality();
        loadToolsets();
        loadOllamaModels();
    }

    async function loadAIConfig() {
        try {
            const data = await api('/api/admin/config');
            if (!data.ok) return;
            $('ai-provider').value = data.provider || 'ollama';
            $('ai-model').value = data.model || '';
            $('ai-fallback').value = (data.fallback_models || []).join(', ');
            $('ai-embedding').value = data.embedding_model || '';
            const p = data.providers || {};
            $('ai-ollama').value = (p.ollama && p.ollama.api_base) || 'http://localhost:11434';
            $('ai-maxtokens').value = data.max_tokens || '';
            $('ai-temperature').value = data.temperature || '';

            const keysBox = $('api-keys');
            const keys = ['moonshot', 'anthropic', 'openai', 'openrouter', 'groq', 'deepseek', 'gemini', 'zhipu'];
            keysBox.innerHTML = keys.map((name) => {
                const current = (p[name] && p[name].api_key) || '';
                return '<div class="field">' +
                    '<label for="key-' + name + '">' + PROVIDERS[name] + ' ' +
                    (current ? '<span class="muted">(set)</span>' : '<span class="muted">(not set)</span>') + '</label>' +
                    '<input class="input" type="password" id="key-' + name + '" placeholder="' +
                    (current ? 'Leave blank to keep current' : 'Enter API key') + '" autocomplete="off">' +
                    '</div>';
            }).join('');
        } catch (e) {}
    }

    async function saveAssistant() {
        const fallback = $('ai-fallback').value.split(',').map((s) => s.trim()).filter(Boolean);
        try {
            const data = await api('/api/admin/config/save', 'POST', {
                provider: $('ai-provider').value,
                model: $('ai-model').value,
                fallback_models: fallback,
                embedding_model: $('ai-embedding').value,
                ollama_url: $('ai-ollama').value,
                max_tokens: parseInt($('ai-maxtokens').value) || 0,
                temperature: parseFloat($('ai-temperature').value) || 0,
            });
            if (data.ok) {
                Ghost.toast.ok('Model settings saved.');
                loadOverview();
            } else {
                Ghost.toast.err(data.error || 'Failed to save model settings.');
            }
        } catch (e) { Ghost.toast.err('Failed to save model settings.'); }
    }

    async function saveKeys() {
        const keys = {};
        ['moonshot', 'anthropic', 'openai', 'openrouter', 'groq', 'deepseek', 'gemini', 'zhipu'].forEach((n) => {
            const el = $('key-' + n);
            if (el && el.value) keys[n] = el.value;
        });
        if (!Object.keys(keys).length) { Ghost.toast.err('Enter at least one API key first.'); return; }
        try {
            const data = await api('/api/admin/config/save', 'POST', { api_keys: keys });
            if (data.ok) {
                Ghost.toast.ok('API keys saved.');
                document.querySelectorAll('[id^="key-"]').forEach((el) => (el.value = ''));
                loadAIConfig();
            } else {
                Ghost.toast.err(data.error || 'Failed to save keys.');
            }
        } catch (e) { Ghost.toast.err('Failed to save keys.'); }
    }

    async function loadOllamaModels() {
        const box = $('ollama-models');
        try {
            const data = await api('/api/ollama/models');
            if (!data.ok || !data.models || !data.models.length) {
                box.innerHTML = '<p class="help-text">No local models yet. Pull one below.</p>';
                return;
            }
            box.innerHTML = data.models.map((m) =>
                '<div class="row"><div class="row__meta"><div class="row__title">' + esc(m) + '</div></div>' +
                '<button class="btn btn--danger-ghost btn--sm" type="button" data-model="' + esc(m) + '">Delete</button></div>'
            ).join('');
            box.querySelectorAll('[data-model]').forEach((b) => {
                b.addEventListener('click', () => deleteOllamaModel(b.dataset.model));
            });
        } catch (e) {
            box.innerHTML = '<p class="help-text">Ollama isn\u2019t reachable right now.</p>';
        }
    }

    async function pullOllamaModel() {
        const name = $('ollama-new-model').value.trim();
        if (!name) { Ghost.toast.err('Enter a model name first.'); return; }
        try {
            await api('/api/ollama/pull', 'POST', { model: name });
            $('ollama-new-model').value = '';
            Ghost.toast.ok('Download started — it may take a while.');
            msg('ollama-error', '');
            setTimeout(loadOllamaModels, 15000);
        } catch (e) { msg('ollama-error', 'Couldn\u2019t start the download.'); }
    }

    async function deleteOllamaModel(name) {
        const yes = await Ghost.confirm({ title: 'Delete model?', body: name + ' will be removed from this device.', okLabel: 'Delete', danger: true });
        if (!yes) return;
        try { await api('/api/ollama/delete', 'POST', { model: name }); loadOllamaModels(); }
        catch (e) { Ghost.toast.err('Failed to delete model.'); }
    }

    /* personality */
    async function loadPersonality() {
        try {
            const data = await api('/api/admin/personality');
            if (!data.ok) return;
            const active = data.active || 'default';
            const all = [...(data.builtins || []), ...(data.custom || [])];
            const customNames = (data.custom || []).map((c) => c.name);
            $('personality-list').innerHTML = all.map((p) =>
                '<div class="row" data-name="' + esc(p.name) + '">' +
                '<button class="choice-card__radio' + (p.name === active ? ' selected' : '') + '" type="button" aria-label="Set ' + esc(p.name) + '"></button>' +
                '<div class="row__meta" style="cursor:pointer"><div class="row__title">' + esc(p.name) + '</div>' +
                '<div class="row__desc">' + esc(p.description || '') + '</div></div>' +
                (p.name === active ? '<span class="row__tag">Active</span>' : '') +
                (customNames.includes(p.name) ? '<button class="link-btn" type="button" data-delete="' + esc(p.name) + '">Remove</button>' : '') +
                '</div>'
            ).join('');

            $('personality-list').querySelectorAll('.row').forEach((row) => {
                const name = row.dataset.name;
                row.querySelector('.choice-card__radio').addEventListener('click', () => setPersonality(name));
                row.querySelector('.row__meta').addEventListener('click', () => setPersonality(name));
                const del = row.querySelector('[data-delete]');
                if (del) del.addEventListener('click', () => deletePersonality(name));
            });
        } catch (e) {}
    }

    async function setPersonality(name) {
        try {
            const data = await api('/api/admin/personality/save', 'POST', { active: name });
            if (data.ok) { Ghost.toast.ok('Personality set.'); loadPersonality(); }
        } catch (e) {}
    }

    async function createPersonality() {
        const name = $('new-personality-name').value.trim();
        const desc = $('new-personality-desc').value.trim();
        const content = $('new-personality-content').value.trim();
        if (!name || !content) { Ghost.toast.err('Name and system prompt are required.'); return; }
        try {
            const data = await api('/api/admin/personality/create', 'POST', { name, description: desc, content });
            if (data.ok) {
                $('new-personality-name').value = '';
                $('new-personality-desc').value = '';
                $('new-personality-content').value = '';
                Ghost.toast.ok('Personality created.');
                loadPersonality();
            } else { Ghost.toast.err(data.error || 'Failed to create personality.'); }
        } catch (e) { Ghost.toast.err('Failed to create personality.'); }
    }

    async function deletePersonality(name) {
        const yes = await Ghost.confirm({ title: 'Remove personality?', body: name + ' will be deleted.', okLabel: 'Remove', danger: true });
        if (!yes) return;
        try { await api('/api/admin/personality/delete', 'POST', { name }); loadPersonality(); }
        catch (e) {}
    }

    /* toolsets */
    async function loadToolsets() {
        try {
            const data = await api('/api/admin/toolsets');
            if (!data.ok) return;
            const active = data.active || 'default';
            $('toolsets-list').innerHTML = (data.toolsets || []).map((t) =>
                '<div class="row">' +
                '<button class="choice-card__radio' + (t.name === active ? ' selected' : '') + '" type="button" data-name="' + esc(t.name) + '" aria-label="Set ' + esc(t.name) + '"></button>' +
                '<div class="row__meta" style="cursor:pointer" data-name="' + esc(t.name) + '"><div class="row__title">' + esc(t.name) + '</div>' +
                '<div class="row__desc">' + esc(t.description || '') + '</div></div>' +
                (t.name === active ? '<span class="row__tag">Active</span>' : '') +
                '</div>'
            ).join('');
            $('toolsets-list').querySelectorAll('[data-name]').forEach((el) => {
                el.addEventListener('click', () => setToolset(el.dataset.name));
            });
        } catch (e) {}
    }

    async function setToolset(name) {
        try {
            const data = await api('/api/admin/toolsets/save', 'POST', { active: name });
            if (data.ok) { Ghost.toast.ok('Toolset set.'); loadToolsets(); }
        } catch (e) {}
    }

    /* ════════════════════════════════════ CONNECTIONS ════════════════════════════════════ */
    async function loadConnections() {
        try {
            const data = await api('/api/admin/channels');
            if (!data.ok) return;
            const c = data.channels || {};
            const tg = c.telegram || {}, ds = c.discord || {}, sl = c.slack || {};
            const wa = c.whatsapp || {}, em = c.email || {}, hb = data.heartbeat || {};

            toggle.set('tg-toggle', !!tg.enabled);
            $('tg-token').placeholder = tg.token ? 'Keep current (masked)' : 'Bot token';
            toggle.set('ds-toggle', !!ds.enabled);
            $('ds-token').placeholder = ds.token ? 'Keep current (masked)' : 'Bot token';
            toggle.set('sl-toggle', !!sl.enabled);
            $('sl-bot-token').placeholder = sl.bot_token ? 'Keep current (masked)' : 'Bot token';
            $('sl-app-token').placeholder = sl.app_token ? 'Keep current (masked)' : 'App token';
            toggle.set('wa-toggle', !!wa.enabled);
            $('wa-bridge-url').value = wa.bridge_url || '';
            toggle.set('em-toggle', !!em.enabled);
            $('em-host').value = em.smtp_host || '';
            $('em-port').value = em.smtp_port || '';
            $('em-user').value = em.username || '';
            $('em-pass').placeholder = em.password ? 'Keep current (masked)' : 'App password';
            $('em-from').value = em.from || '';
            $('em-to').value = em.to || '';
            toggle.set('hb-toggle', !!hb.enabled);
            $('hb-interval').value = hb.interval || 30;
        } catch (e) {}
    }

    async function saveChannel(id) {
        const bodies = {
            tg: { telegram: { enabled: toggle.isOn('tg-toggle'), token: $('tg-token').value } },
            ds: { discord: { enabled: toggle.isOn('ds-toggle'), token: $('ds-token').value } },
            sl: {
                slack: {
                    enabled: toggle.isOn('sl-toggle'),
                    bot_token: $('sl-bot-token').value,
                    app_token: $('sl-app-token').value,
                },
            },
            wa: { whatsapp: { enabled: toggle.isOn('wa-toggle'), bridge_url: $('wa-bridge-url').value } },
            em: {
                email: {
                    enabled: toggle.isOn('em-toggle'),
                    smtp_host: $('em-host').value,
                    smtp_port: parseInt($('em-port').value) || 0,
                    username: $('em-user').value,
                    password: $('em-pass').value,
                    from: $('em-from').value,
                    to: $('em-to').value,
                },
            },
            hb: { heartbeat: { enabled: toggle.isOn('hb-toggle'), interval: parseInt($('hb-interval').value) || 30 } },
        };
        try {
            const data = await api('/api/admin/channels/save', 'POST', bodies[id]);
            if (data.ok) {
                Ghost.toast.ok('Saved.');
                if (id === 'tg') $('tg-token').value = '';
                if (id === 'ds') $('ds-token').value = '';
                if (id === 'sl') { $('sl-bot-token').value = ''; $('sl-app-token').value = ''; }
                if (id === 'em') $('em-pass').value = '';
                loadConnections();
            } else {
                Ghost.toast.err(data.error || 'Failed to save channel settings.');
            }
        } catch (e) { Ghost.toast.err('Failed to save channel settings.'); }
    }

    /* ════════════════════════════════════ SKILLS ════════════════════════════════════ */
    let editingSkill = null;
    let editingFiles = [];
    let editingActive = 0;

    async function loadSkills() {
        try {
            const data = await api('/api/admin/skills');
            const box = $('skills-list');
            if (!data.skills || !data.skills.length) {
                box.innerHTML = '<p class="help-text">No skills installed yet.</p>';
                return;
            }
            box.innerHTML = data.skills.map((s) => {
                const badges = [];
                if (s.bundled === 'true') badges.push('<span class="badge badge--ok">bundled</span>');
                if (s.user_modified === 'true') badges.push('<span class="badge badge--warn">edited</span>');
                return '<div class="row"><div class="row__meta"><div class="row__title">' + esc(s.name) +
                    (badges.length ? ' <span style="display:inline-flex;gap:6px;margin-left:6px;">' + badges.join('') + '</span>' : '') +
                    '</div><div class="row__desc">' + esc(s.description || '') + '</div></div>' +
                    '<div class="row__actions">' +
                    '<button class="btn btn--secondary btn--sm" type="button" data-edit="' + esc(s.name) + '">Edit</button>' +
                    '<button class="btn btn--secondary btn--sm" type="button" data-toggle="' + esc(s.name) + '">Disable</button>' +
                    '<button class="btn btn--danger-ghost btn--sm" type="button" data-remove="' + esc(s.name) + '">Remove</button>' +
                    '</div></div>';
            }).join('');
            box.querySelectorAll('[data-edit]').forEach((b) => {
                b.addEventListener('click', () => openSkillEditor(b.dataset.edit));
            });
            box.querySelectorAll('[data-toggle]').forEach((b) => {
                b.addEventListener('click', () => setSkillEnabled(b.dataset.toggle, false));
            });
            box.querySelectorAll('[data-remove]').forEach((b) => {
                b.addEventListener('click', () => removeSkill(b.dataset.remove));
            });
        } catch (e) {}
    }

    async function setSkillEnabled(name, enabled) {
        try { await api('/api/admin/skills/toggle', 'POST', { name, enabled }); loadSkills(); }
        catch (e) {}
    }

    async function removeSkill(name) {
        const yes = await Ghost.confirm({ title: 'Remove skill?', body: name + ' will be uninstalled.', okLabel: 'Remove', danger: true });
        if (!yes) return;
        try { await api('/api/admin/skills/remove', 'POST', { name }); loadSkills(); }
        catch (e) {}
    }

    /* ── skill editor ── */
    async function openSkillEditor(name) {
        try {
            const data = await api('/api/admin/skills/read?name=' + encodeURIComponent(name));
            if (!data.ok || !data.files || !data.files.length) { Ghost.toast.err('Could not read skill.'); return; }
            editingSkill = name;
            editingFiles = data.files;
            editingActive = 0;
            const root = $('skill-editor');
            $('skill-editor-title').textContent = name;
            $('sk-badge-bundled').classList.toggle('hidden', data.bundled !== true);
            $('sk-badge-modified').classList.toggle('hidden', data.user_modified !== true);
            renderSkillFileTabs();
            root.classList.remove('hidden');
            $('sk-editor').focus();
        } catch (e) { Ghost.toast.err('Could not read skill.'); }
    }

    function closeSkillEditor() {
        $('skill-editor').classList.add('hidden');
        editingSkill = null;
        editingFiles = [];
    }

    function renderSkillFileTabs() {
        const tabs = $('sk-file-tabs');
        tabs.innerHTML = editingFiles.map((f, i) =>
            '<button class="skill-editor__file' + (i === editingActive ? ' is-active' : '') + '" type="button" data-file="' + i + '" role="tab">' + esc(f.path) + '</button>'
        ).join('');
        tabs.querySelectorAll('[data-file]').forEach((b) => {
            b.addEventListener('click', () => {
                editingActive = parseInt(b.dataset.file, 10);
                renderSkillFileTabs();
                $('sk-editor').focus();
            });
        });
        $('sk-editor').value = editingFiles[editingActive] ? editingFiles[editingActive].content : '';
    }

    async function saveSkillEdits() {
        if (!editingSkill) return;
        editingFiles[editingActive].content = $('sk-editor').value;
        try {
            const data = await api('/api/admin/skills/save', 'POST', {
                name: editingSkill,
                files: editingFiles.map((f) => ({ path: f.path, content: f.content }))
            });
            if (data.ok) {
                Ghost.toast.ok('Saved — this skill is now protected from bundled updates.');
                closeSkillEditor();
                loadSkills();
            } else {
                Ghost.toast.err(data.error || 'Failed to save skill edits.');
            }
        } catch (e) { Ghost.toast.err('Failed to save skill edits.'); }
    async function resyncSkills() {
        const note = $('skills-sync-note');
        note.classList.remove('hidden');
        note.textContent = 'Reconciling bundled skills\u2026';
        try {
            const data = await api('/api/admin/skills/sync', 'POST', {});
            if (!data.ok) { note.textContent = 'Sync failed: ' + (data.error || 'unknown error'); return; }
            const r = data.report || {};
            const parts = [];
            if (r.seeded && r.seeded.length) parts.push(r.seeded.length + ' new');
            if (r.updated && r.updated.length) parts.push(r.updated.length + ' updated');
            if (r.user_modified && r.user_modified.length) parts.push(r.user_modified.length + ' edited (preserved)');
            if (r.unchanged && r.unchanged.length) parts.push(r.unchanged.length + ' unchanged');
            if (!parts.length) parts.push('already in sync');
            note.textContent = 'Sync complete — ' + parts.join(', ') + '.';
            loadSkills();
        } catch (e) { note.textContent = 'Sync failed.'; }
    }

    async function searchClawHub() {
        const q = $('clawhub-search').value.trim();
        const box = $('clawhub-results');
        if (!q) { box.innerHTML = ''; return; }
        box.innerHTML = '<div class="loading"><span class="spinner"></span>Searching&hellip;</div>';
        try {
            const data = await api('/api/admin/skills/clawhub/search?q=' + encodeURIComponent(q));
            if (!data.ok || !data.results || !data.results.length) {
                box.innerHTML = '<p class="help-text">No results for \u201c' + esc(q) + '\u201d.</p>';
                return;
            }
            box.innerHTML = data.results.map((r) =>
                '<div class="row"><div class="row__meta"><div class="row__title">' + esc(r.display_name || r.slug) +
                ' <span class="muted">v' + esc(r.version || '?') + '</span></div>' +
                '<div class="row__desc">' + esc(r.summary || '') + '</div></div>' +
                '<button class="btn btn--primary btn--sm" type="button" data-slug="' + esc(r.slug) + '">Install</button></div>'
            ).join('');
            box.querySelectorAll('[data-slug]').forEach((b) => {
                b.addEventListener('click', () => installFromClawHub(b.dataset.slug));
            });
        } catch (e) {
            box.innerHTML = '<p class="error-text">Search failed.</p>';
        }
    }

    async function installFromClawHub(slug) {
        try {
            const data = await api('/api/admin/skills/clawhub/install', 'POST', { slug });
            if (data.ok) { Ghost.toast.ok('Skill installed.'); loadSkills(); searchClawHub(); }
            else { Ghost.toast.err(data.error || 'Install failed.'); }
        } catch (e) {}
    }

    /* ════════════════════════════════════ SETTINGS ════════════════════════════════════ */
    async function loadSettings() {
        loadSystem();
        loadGateway();
        loadAdvanced();
        loadLogs();
    }

    async function loadSystem() {
        try {
            const data = await api('/api/admin/network');
            if (!data.ok) return;
            $('net-host').textContent = data.hostname || '\u2014';
            $('net-ip').textContent = data.ip || '\u2014';
            $('net-wifi').textContent = data.connected || 'wired/unknown';
        } catch (e) {}
    }

    async function setHostname() {
        const host = $('sys-hostname').value.trim();
        if (!host) { Ghost.toast.err('Enter a hostname first.'); return; }
        try {
            const data = await api('/api/admin/hostname', 'POST', { hostname: host });
            if (data.ok) {
                $('sys-hostname').value = '';
                msg('hostname-msg', 'Device name updated.', true);
                loadSystem();
            } else { Ghost.toast.err(data.error || 'Failed to set hostname.'); }
        } catch (e) { Ghost.toast.err('Failed to set hostname.'); }
    }

    async function downloadBackup() {
        try {
            const res = await fetch('/api/admin/backup', { method: 'POST' });
            if (!res.ok) throw new Error('failed');
            const blob = await res.blob();
            const a = document.createElement('a');
            a.href = URL.createObjectURL(blob);
            a.download = 'ghost-backup.tar.gz';
            document.body.appendChild(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(a.href), 4000);
            Ghost.toast.ok('Backup downloaded.');
        } catch (e) {
            Ghost.toast.err('Couldn\u2019t create a backup.');
        }
    }

    async function changePassword() {
        const current = $('pw-current').value;
        const next = $('pw-new').value;
        if (!current || !next) { Ghost.toast.err('Fill in both fields.'); return; }
        try {
            const data = await api('/api/admin/password', 'POST', { current, new: next });
            if (data.ok) {
                $('pw-current').value = '';
                $('pw-new').value = '';
                msg('password-msg', 'Password updated.', true);
            } else { Ghost.toast.err(data.error || 'Failed to change password.'); }
        } catch (e) { Ghost.toast.err('Failed to change password.'); }
    }

    async function regenBridge() {
        const yes = await Ghost.confirm({
            title: 'Regenerate pairing secret?',
            body: 'Every connected app will need to be paired again.',
            okLabel: 'Regenerate',
            danger: true,
        });
        if (!yes) return;
        try {
            const data = await api('/api/admin/bridge', 'POST');
            if (data.ok) {
                msg('bridge-msg', 'New pairing secret: ' + (data.secret || '') + ' — re-pair your app with it.', true);
            } else { Ghost.toast.err(data.error || 'Failed to regenerate pairing secret.'); }
        } catch (e) { Ghost.toast.err('Failed to regenerate pairing secret.'); }
    }

    async function loadGateway() {
        try {
            const data = await api('/api/admin/gateway');
            if (!data.ok) return;
            $('gw-host').value = data.host || '0.0.0.0';
            $('gw-port').value = data.port || 8766;
        } catch (e) {}
    }

    async function saveGateway() {
        try {
            const data = await api('/api/admin/gateway/save', 'POST', {
                host: $('gw-host').value,
                port: parseInt($('gw-port').value) || 8766,
            });
            if (data.ok) { msg('gateway-msg', 'Gateway saved.', true); }
            else { Ghost.toast.err(data.error || 'Failed to save gateway settings.'); }
        } catch (e) { Ghost.toast.err('Failed to save gateway settings.'); }
    }

    async function loadAdvanced() {
        try {
            const data = await api('/api/admin/advanced');
            if (!data.ok) return;
            const rag = data.rag || {};
            toggle.set('rag-toggle', !!rag.enabled);
            $('rag-m').value = rag.m || 16;
            $('rag-ef-const').value = rag.ef_construction || 200;
            $('rag-ef-search').value = rag.ef_search || 10;

            const nudge = data.nudge || {};
            toggle.set('nudge-toggle', !!nudge.enabled);
            $('nudge-memory').value = nudge.memory_interval || 20;
            $('nudge-skill').value = nudge.skill_interval || 15;

            const dev = data.devices || {};
            toggle.set('devices-toggle', !!dev.enabled);
            toggle.set('devices-usb-toggle', !!dev.monitor_usb);

            toggle.set('adv-search-toggle', !!data.search_enabled);
            toggle.set('adv-workspace-toggle', !!data.restrict_to_workspace);
            $('adv-max-iter').value = data.max_tool_iterations || 20;
            $('adv-light-model').value = (data.routing || {}).light_model || '';
            $('adv-threshold').value = (data.routing || {}).threshold || 0.35;

            const mcp = data.mcp || {};
            toggle.set('mcp-toggle', !!mcp.enabled);
            mcpServers = mcp.servers || {};
            renderMCPServers();
        } catch (e) {}
    }

    async function saveAdvanced() {
        try {
            const data = await api('/api/admin/advanced/save', 'POST', {
                rag: {
                    enabled: toggle.isOn('rag-toggle'),
                    m: parseInt($('rag-m').value) || 16,
                    ef_construction: parseInt($('rag-ef-const').value) || 200,
                    ef_search: parseInt($('rag-ef-search').value) || 10,
                },
                nudge: {
                    enabled: toggle.isOn('nudge-toggle'),
                    memory_interval: parseInt($('nudge-memory').value) || 20,
                    skill_interval: parseInt($('nudge-skill').value) || 15,
                },
                devices: {
                    enabled: toggle.isOn('devices-toggle'),
                    monitor_usb: toggle.isOn('devices-usb-toggle'),
                },
                search_enabled: toggle.isOn('adv-search-toggle'),
                restrict_to_workspace: toggle.isOn('adv-workspace-toggle'),
                max_tool_iterations: parseInt($('adv-max-iter').value) || 20,
                routing: {
                    light_model: $('adv-light-model').value,
                    threshold: parseFloat($('adv-threshold').value) || 0.35,
                },
            });
            if (data.ok) { msg('advanced-msg', 'Advanced settings saved.', true); }
            else { Ghost.toast.err(data.error || 'Failed to save advanced settings.'); }
        } catch (e) { Ghost.toast.err('Failed to save advanced settings.'); }
    }

    /* MCP */
    function renderMCPServers() {
        const el = $('mcp-servers-list');
        const names = Object.keys(mcpServers);
        if (!names.length) {
            el.innerHTML = '<p class="help-text">No MCP servers configured.</p>';
            return;
        }
        el.innerHTML = names.map((name) => {
            const s = mcpServers[name] || {};
            const cmd = s.http_url || (s.command || '?') + ' ' + (s.args || []).join(' ');
            return '<div class="row"><div class="row__meta"><div class="row__title">' + esc(name) + '</div>' +
                '<div class="row__desc">' + esc(cmd) + '</div></div>' +
                '<button class="link-btn" type="button" data-remove="' + esc(name) + '">Remove</button></div>';
        }).join('');
        el.querySelectorAll('[data-remove]').forEach((b) => {
            b.addEventListener('click', () => removeMCPServer(b.dataset.remove));
        });
    }

    async function addMCPServer() {
        const name = $('mcp-new-name').value.trim();
        if (!name) { Ghost.toast.err('Give the server a name.'); return; }
        const httpMode = toggle.isOn('mcp-http-toggle');
        const argsStr = $('mcp-new-args').value.trim();
        mcpServers[name] = httpMode
            ? { enabled: true, http: true, http_url: $('mcp-new-url').value.trim(), command: '', args: [] }
            : { enabled: true, command: $('mcp-new-cmd').value.trim(), args: argsStr ? argsStr.split(/\s+/) : [], http: false };
        await saveMCPConfig();
        ['mcp-new-name', 'mcp-new-cmd', 'mcp-new-args', 'mcp-new-url'].forEach((id) => ($(id).value = ''));
    }

    async function removeMCPServer(name) {
        delete mcpServers[name];
        await saveMCPConfig();
    }

    async function saveMCPConfig() {
        try {
            await api('/api/admin/advanced/save', 'POST', {
                mcp: { enabled: toggle.isOn('mcp-toggle'), servers: mcpServers },
            });
            renderMCPServers();
        } catch (e) {}
    }

    /* logs */
    function startLogs() {
        if (logsTimer) return;
        logsTimer = setInterval(loadLogs, 10000);
    }
    function stopLogs() {
        if (logsTimer) { clearInterval(logsTimer); logsTimer = null; }
    }

    async function loadLogs() {
        const el = $('logs-output');
        if (!el) return;
        try {
            const lines = $('log-lines').value;
            const data = await api('/api/admin/logs?lines=' + lines);
            if (data.ok) {
                el.textContent = (data.logs || []).join('\n');
                el.scrollTop = el.scrollHeight;
            }
        } catch (e) {}
    }

    /* reboot */
    async function rebootDevice() {
        const yes = await Ghost.confirm({
            title: 'Reboot this device?',
            body: 'Ghost will restart. It takes about a minute to come back.',
            okLabel: 'Reboot',
            danger: true,
        });
        if (!yes) return;
        try {
            await api('/api/admin/reboot', 'POST');
            Ghost.toast.ok('Rebooting\u2026 the page will stop responding for a minute.');
        } catch (e) {}
    }

    /* ════════════════════════════════════ BIND ════════════════════════════════════ */
    function bind() {
        document.querySelectorAll('.dash-nav__item, .dash-side__nav-item').forEach((b) => {
            b.addEventListener('click', () => switchTab(b.dataset.tab));
        });

        $('btn-doctor').addEventListener('click', runDoctor);
        $('update-btn').addEventListener('click', startUpdate);

        $('btn-ai-save').addEventListener('click', saveAssistant);
        $('btn-ai-reload').addEventListener('click', loadAIConfig);
        $('btn-save-keys').addEventListener('click', saveKeys);
        $('btn-ollama-pull').addEventListener('click', pullOllamaModel);
        $('btn-create-personality').addEventListener('click', createPersonality);

        $('btn-tg-save').addEventListener('click', () => saveChannel('tg'));
        $('btn-ds-save').addEventListener('click', () => saveChannel('ds'));
        $('btn-sl-save').addEventListener('click', () => saveChannel('sl'));
        $('btn-wa-save').addEventListener('click', () => saveChannel('wa'));
        $('btn-em-save').addEventListener('click', () => saveChannel('em'));
        $('btn-hb-save').addEventListener('click', () => saveChannel('hb'));

        $('btn-clawhub-search').addEventListener('click', searchClawHub);
        $('clawhub-search').addEventListener('keydown', (e) => { if (e.key === 'Enter') searchClawHub(); });
        $('btn-skills-sync').addEventListener('click', resyncSkills);
        $('btn-sk-cancel').addEventListener('click', closeSkillEditor);
        $('btn-sk-save').addEventListener('click', saveSkillEdits);
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && !$('skill-editor').classList.contains('hidden')) closeSkillEditor();
        });

        $('btn-set-hostname').addEventListener('click', setHostname);
        $('btn-backup').addEventListener('click', downloadBackup);
        $('btn-change-password').addEventListener('click', changePassword);
        $('btn-regen-bridge').addEventListener('click', regenBridge);
        $('btn-save-gateway').addEventListener('click', saveGateway);
        $('btn-refresh-logs').addEventListener('click', loadLogs);
        $('btn-save-advanced').addEventListener('click', saveAdvanced);
        $('btn-add-mcp').addEventListener('click', addMCPServer);
        $('btn-reboot').addEventListener('click', rebootDevice);

        $('mcp-http-toggle').addEventListener('click', () => {
            $('mcp-http-url-group').classList.toggle('hidden', !toggle.isOn('mcp-http-toggle'));
        });

        document.querySelectorAll('.toggle').forEach((t) => {
            t.addEventListener('click', () => {
                const on = t.classList.contains('on');
                t.classList.toggle('on', !on);
                t.setAttribute('aria-checked', (!on).toString());
            });
        });
    }

    /* ── expose ── */
    window.Dashboard = {
        open,
        logout,
        switchTab,
        loadOverview,
        bind,
    };

    document.addEventListener('DOMContentLoaded', bind);
})();