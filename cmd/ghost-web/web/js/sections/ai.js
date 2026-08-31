/* Ghost Section: AI — local intelligence, cloud intelligence, routing. */
'use strict';

async function loadAI(container) {
  if (GhostApp.currentSection() !== 'ai') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'AI'));
  head.appendChild(GhostUI.h('p', {}, 'How Ghost thinks.'));
  container.appendChild(head);

  const [modelRes, cfgRes, ollamaRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/model'),
    GhostAPI.get('/api/admin/config'),
    GhostAPI.get('/api/ollama/models'),
  ]);

  const active = modelRes.status === 'fulfilled' ? (modelRes.value.active || '') : '';
  const cfg = cfgRes.status === 'fulfilled' ? cfgRes.value : null;
  const ollamaModels = ollamaRes.status === 'fulfilled' ? (ollamaRes.value.models || []) : [];

  // ── Local intelligence ──
  const local = GhostUI.h('div', { className: 'panel' });
  local.appendChild(panelHead('Local intelligence', 'Runs on your hardware. Nothing leaves the device.'));
  renderLocal(local, ollamaModels, active);
  container.appendChild(local);

  // ── Cloud intelligence ──
  const cloud = GhostUI.h('div', { className: 'panel' });
  cloud.appendChild(panelHead('Cloud intelligence', 'Optional. Used only when a task benefits from it.'));
  renderCloud(cloud, cfg);
  container.appendChild(cloud);

  // ── Routing ──
  const routing = GhostUI.h('div', { className: 'panel' });
  routing.appendChild(panelHead('Routing', 'Ghost runs tasks locally when it can, and uses cloud AI only when a task needs it.'));
  const routingCfg = cfg ? (cfg.routing || {}) : {};
  renderRouting(routing, routingCfg);
  container.appendChild(routing);

  // ── Diagnose ──
  const diagPanel = GhostUI.h('div', { className: 'panel' });
  const diagHead = GhostUI.h('div', { className: 'panel-head' });
  const diagTitle = GhostUI.h('div');
  diagTitle.appendChild(GhostUI.h('h2', {}, 'AI health'));
  diagTitle.appendChild(GhostUI.h('p', {}, 'Quick check that your local and cloud AI are reachable.'));
  diagHead.appendChild(diagTitle);
  const diagBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Run check');
  diagBtn.addEventListener('click', () => runAIDiag(diagBody, diagBtn));
  diagHead.appendChild(diagBtn);
  diagPanel.appendChild(diagHead);
  const diagBody = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
  diagBody.appendChild(GhostUI.emptyState('Not run yet', 'Run a check to see how your AI providers are doing.'));
  diagPanel.appendChild(diagBody);
  container.appendChild(diagPanel);
}

function panelHead(title, sub) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, title));
  if (sub) text.appendChild(GhostUI.h('p', {}, sub));
  h.appendChild(text);
  return h;
}

function renderLocal(panel, models, active) {
  if (models.length === 0) {
    panel.appendChild(GhostUI.emptyState('No local models installed', 'Ghost can still run, but local AI needs a model. Install one to get started.'));
    return;
  }
  for (const m of models) {
    const row = GhostUI.h('div', { className: 'model-row' });
    const main = GhostUI.h('div', { className: 'model-main' });
    main.appendChild(GhostUI.h('div', { className: 'model-name' }, m));
    if (m === active) main.appendChild(GhostUI.h('div', { className: 'model-sub' }, 'Active model'));
    row.appendChild(main);
    const trailing = GhostUI.h('div', { className: 'ghost-row-trailing' });
    if (m !== active) {
      trailing.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
        try { await GhostAPI.proxyPost('/v1/model', { model: m }); GhostUI.toast('Default model updated'); loadAI(document.getElementById('view')); }
        catch (e) { GhostUI.toast('Couldn’t set model.', 'err'); }
      } }, 'Use'));
    } else {
      trailing.appendChild(GhostUI.statusDot('ready'));
    }
    trailing.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-icon', title: 'Remove model', onClick: async () => {
      if (!(await GhostUI.confirmModal('Remove this model?', m + ' will be deleted from this device. This can’t be undone.', 'Remove'))) return;
      try { await GhostAPI.post('/api/ollama/delete', { model: m }); GhostUI.toast('Model removed'); loadAI(document.getElementById('view')); }
      catch (e) { GhostUI.toast('Couldn’t remove model.', 'err'); }
    } }, '✕'));
    row.appendChild(trailing);
    panel.appendChild(row);
  }

  // Install row
  const install = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-4)' });
  const input = GhostUI.input('Model name');
  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Install');
  btn.addEventListener('click', async () => {
    const name = input.value.trim();
    if (!name) return;
    btn.disabled = true; input.disabled = true;
    try { await GhostAPI.post('/api/ollama/pull', { model: name }); GhostUI.toast('Download started — this can take a while'); }
    catch (e) { GhostUI.toast('Couldn’t start download.', 'err'); btn.disabled = false; input.disabled = false; }
  });
  install.appendChild(input); install.appendChild(btn);
  panel.appendChild(install);
  panel.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-2)' }, 'A small model (0.5\u20131B) is plenty for everyday tasks.'));
}

function renderRouting(panel, routing) {
  const prefs = [
    { key: 'prefer_local', label: 'Prefer local AI', desc: 'Always try the local model first, even for complex tasks.' },
    { key: 'allow_cloud', label: 'Allow cloud AI', desc: 'Let Ghost use cloud providers when you have one configured.' },
    { key: 'cloud_when_local_fails', label: 'Fall back to cloud', desc: 'If the local model fails or is unavailable, try cloud instead.' },
  ];
  for (const p of prefs) {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, p.label));
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-sub' }, p.desc));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    const sw = GhostUI.toggle(routing[p.key] || false, async (on) => {
      const next = { ...routing, [p.key]: on };
      try {
        await GhostAPI.post('/api/admin/config/save', { routing: next });
        Object.assign(routing, next);
        GhostUI.toast(p.label + (on ? ' on' : ' off'));
      } catch (err) {
        sw.classList.toggle('on', !on);
        GhostUI.toast('Couldn\u2019t save', 'err');
      }
    });
    tr.appendChild(sw);
    row.appendChild(tr);
    panel.appendChild(row);
  }
  panel.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-2)' }, 'Defaults: prefer local, allow cloud, fall back if local fails.'));
}

function renderCloud(panel, cfg) {
  if (!cfg) { panel.appendChild(GhostUI.errorState('Couldn’t load providers', 'Try again in a moment.')); return; }
  const providers = cfg.providers || {};
  const order = ['openai', 'anthropic', 'moonshot', 'groq', 'deepseek', 'gemini', 'zhipu', 'openrouter'];
  let hasAny = false;
  for (const key of order) {
    const p = providers[key];
    if (!p || key === 'ollama') continue;
    hasAny = true;
    const name = key.charAt(0).toUpperCase() + key.slice(1);
    const configured = p.api_key && p.api_key.length > 0;
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, name));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    if (configured) {
      tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot('ready'), 'Configured'));
      tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => editCloud(key, name, p.api_key) }, 'Edit'));
    } else {
      tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot('neutral'), 'Not configured'));
      tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => editCloud(key, name, '') }, 'Configure'));
    }
    row.appendChild(tr);
    panel.appendChild(row);
  }
  if (!hasAny) {
    panel.appendChild(GhostUI.emptyState('No cloud providers', 'Cloud AI is optional. Add a provider when you need deeper reasoning.'));
  }
}

function editCloud(key, name, current) {
  const body = GhostUI.h('div');
  const field = GhostUI.h('div', { className: 'field' });
  field.appendChild(GhostUI.h('label', {}, name + ' API key'));
  const input = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: current ? 'Enter a new key to replace the current one' : 'Paste your API key' });
  field.appendChild(input);
  body.appendChild(field);
  body.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Stored only in your Ghost’s secrets file. Never shown back in full.'));
  GhostUI.modal('Configure ' + name, body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const val = input.value.trim();
      try {
        await GhostAPI.post('/api/admin/config/save', { api_keys: { [key]: val } });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast(name + ' saved');
        loadAI(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Couldn’t save.', 'err'); }
    } }, 'Save'),
  ]);
}

async function runAIDiag(body, btn) {
  btn.disabled = true;
  btn.textContent = 'Checking…';
  body.innerHTML = '';
  body.appendChild(GhostUI.loading('Running AI health check…'));
  let res;
  try { res = await GhostAPI.proxyGet('/v1/doctor'); }
  catch (e) {
    body.innerHTML = '';
    body.appendChild(GhostUI.errorState('Diagnostics unavailable', 'The gateway may be starting.'));
    btn.disabled = false; btn.textContent = 'Run check';
    return;
  }
  body.innerHTML = '';
  const checks = (res.checks || []).filter(c => c.name === 'provider' || c.name === 'ollama api');
  if (checks.length === 0) {
    body.appendChild(GhostUI.emptyState('Nothing to report', 'No AI checks returned.'));
    btn.disabled = false; btn.textContent = 'Run check';
    return;
  }
  const grid = GhostUI.h('div', { className: 'diag-grid' });
  for (const ch of checks) {
    const row = GhostUI.h('div', { className: 'diag-row' });
    const st = ch.status === 'ok' ? 'ready' : ch.status === 'warn' ? 'warn' : 'bad';
    row.appendChild(GhostUI.h('span', { className: 'status-dot ' + st }));
    row.appendChild(GhostUI.h('div', { className: 'diag-name' }, ch.name));
    const msg = GhostUI.h('div', { className: 'diag-msg' });
    msg.textContent = ch.message || '';
    row.appendChild(msg);
    grid.appendChild(row);
  }
  body.appendChild(grid);
  btn.disabled = false; btn.textContent = 'Run check';
}

GhostApp.registerSection('ai', loadAI);
