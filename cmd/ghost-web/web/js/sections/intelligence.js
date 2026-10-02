/* Ghost Section: Intelligence — default model, providers, routing, health. */
'use strict';

async function loadAI(container) {
  if (GhostApp.currentSection() !== 'intelligence') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Intelligence'));
  head.appendChild(GhostUI.h('p', {}, 'Which AI Ghost runs on, and whether it is healthy.'));
  container.appendChild(head);

  const [providerModelsRes, cfgRes, ollamaRes, modelRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/providers/models'),
    GhostAPI.get('/api/admin/config'),
    GhostAPI.get('/api/ollama/models'),
    GhostAPI.proxyGet('/v1/model'),
  ]);

  const pm = providerModelsRes.status === 'fulfilled' ? providerModelsRes.value : null;
  const cfg = cfgRes.status === 'fulfilled' ? cfgRes.value : null;
  // Embedding models (nomic-embed-text and the like) cannot hold a conversation:
  // listing one with a "Use" button invites picking a model that cannot answer.
  const ollamaModels = (ollamaRes.status === 'fulfilled' ? (ollamaRes.value.models || []) : []).filter(m => !/embed/i.test(m));
  const activeModel = modelRes.status === 'fulfilled' ? modelRes.value : null;

  const currentProvider = (pm && pm.provider) || (cfg && cfg.provider) || '';
  const currentModel = (pm && pm.model) || (cfg && cfg.model) || '';
  const providerModels = (pm && pm.providers) || {};

  // ── Default model ──
  const defPanel = GhostUI.h('div', { className: 'panel' });
  renderDefaultModel(defPanel, currentProvider, currentModel, providerModels, ollamaModels);
  container.appendChild(defPanel);

  // ── Providers ──
  const provPanel = GhostUI.h('div', { className: 'panel' });
  renderProviders(provPanel, cfg, providerModels, ollamaModels);
  container.appendChild(provPanel);

  // ── Routing ──
  const routingPanel = GhostUI.h('div', { className: 'panel' });
  const routingCfg = cfg ? (cfg.routing || {}) : {};
  renderRouting(routingPanel, routingCfg);
  container.appendChild(routingPanel);

  // ── Local models ──
  if (ollamaModels.length > 0) {
    const localPanel = GhostUI.h('div', { className: 'panel' });
    renderLocal(localPanel, ollamaModels, activeModel ? activeModel.active : '');
    container.appendChild(localPanel);
  }

  // ── AI Health ──
  const diagPanel = GhostUI.h('div', { className: 'panel' });
  renderDiag(diagPanel);
  container.appendChild(diagPanel);
}

// ── Default model panel ──

function renderDefaultModel(panel, currentProvider, currentModel, providerModels, ollamaModels) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, 'Default model'));
  text.appendChild(GhostUI.h('p', {}, 'The model Ghost normally uses for conversations and everyday tasks.'));
  h.appendChild(text);
  panel.appendChild(h);

  if (currentProvider && currentModel) {
    const f = GhostUI.modelFriendly(currentProvider + ':' + currentModel);
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-size:var(--t-body);font-weight:600' }, f.name));
    const sub = GhostUI.h('div', { className: 'ghost-row-subtitle' });
    sub.appendChild(document.createTextNode(f.provName + (f.provName ? ' \u00b7 ' : '') + f.model));
    if (f.provider === 'ollama' || f.provider === 'vllm') {
      sub.appendChild(document.createTextNode(' \u00b7 Local'));
    }
    c.appendChild(sub);
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: () => {
      changeDefaultModal(currentProvider, currentModel, providerModels, ollamaModels);
    } }, 'Change'));
    row.appendChild(tr);
    panel.appendChild(row);
  } else {
    panel.appendChild(GhostUI.emptyState('No model selected', 'Pick a model below so Ghost knows how to think.'));
  }
}

function changeDefaultModal(currentProvider, currentModel, providerModels, ollamaModels) {
  const body = GhostUI.h('div');
  const currentVal = currentProvider + ':' + currentModel;
  let selected = currentVal;
  let query = '';

  // Groups come from what each provider says it serves (the server asks
  // the provider APIs). The current default's provider leads; any other
  // configured provider follows in a stable order.
  function buildGroups(pmap) {
    const groups = [];
    if (ollamaModels.length > 0) {
      groups.push({ provider: 'ollama', label: 'Ollama \u00b7 Local', models: ollamaModels, local: true });
    }
    const known = ['openai', 'anthropic', 'moonshot', 'groq', 'deepseek', 'qwen', 'gemini', 'zhipu', 'openrouter', 'nvidia', 'vllm'];
    const order = known.concat(Object.keys(pmap).filter((k) => !known.includes(k) && k !== 'ollama').sort());
    order.sort((a, b) => (a === currentProvider ? -1 : 0) - (b === currentProvider ? -1 : 0));
    for (const key of order) {
      const pm = pmap[key];
      if (!pm || !pm.configured || !pm.models || pm.models.length === 0) continue;
      const label = key.charAt(0).toUpperCase() + key.slice(1);
      groups.push({ provider: key, label, models: pm.models, local: key === 'vllm', catalog: pm.source === 'catalog', error: pm.error || '' });
    }
    return groups;
  }
  let groups = buildGroups(providerModels);

  const search = GhostUI.h('input', { className: 'ghost-input', type: 'search', placeholder: 'Search models or providers', 'aria-label': 'Search models' });
  const list = GhostUI.h('div', { style: 'max-height:55vh;overflow-y:auto;margin-top:var(--s-2)' });
  body.appendChild(search);
  body.appendChild(list);

  function render() {
    list.innerHTML = '';
    const q = query.trim().toLowerCase();
    let shown = 0;
    for (const g of groups) {
      const models = q ? g.models.filter((m) => m.toLowerCase().includes(q) || g.provider.includes(q) || g.label.toLowerCase().includes(q)) : g.models;
      if (models.length === 0) continue;
      list.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-top:var(--s-3);margin-bottom:var(--s-1);font-weight:600;text-transform:uppercase;letter-spacing:0.05em' }, g.label));
      if (g.catalog) {
        // Honest provenance: the provider couldn't be asked, so these are
        // Ghost's built-in suggestions, not the provider's own list.
        list.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-1)' },
          'Built-in list \u2014 couldn\u2019t reach ' + g.label + (g.error ? ' (' + g.error + ')' : '')));
      }
      for (const m of models) {
        shown++;
        list.appendChild(modelOptionRow(g, m, selected, currentVal, (val) => { selected = val; render(); }));
      }
    }
    if (groups.length === 0) {
      list.appendChild(GhostUI.emptyState('No models available', 'Configure a provider first.'));
    } else if (shown === 0) {
      list.appendChild(GhostUI.emptyState('No matches', 'Try a different search.'));
    }
  }
  search.addEventListener('input', () => { query = search.value; render(); });
  render();

  const refreshBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: async () => {
    refreshBtn.disabled = true;
    refreshBtn.textContent = 'Refreshing\u2026';
    try {
      const res = await GhostAPI.get('/api/admin/providers/models?refresh=1');
      groups = buildGroups((res && res.providers) || {});
      render();
    } catch (err) {
      GhostUI.toast('Couldn\u2019t refresh model lists.', 'err');
    }
    refreshBtn.disabled = false;
    refreshBtn.textContent = 'Refresh lists';
  } }, 'Refresh lists');

  GhostUI.modal('Change default model', body, [
    refreshBtn,
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const [provider, ...modelParts] = selected.split(':');
      const model = modelParts.join(':');
      if (!provider || !model) return;
      e.target.disabled = true;
      try {
        // Use the live model endpoint for immediate effect
        await GhostAPI.proxyPost('/v1/model', { model: provider + ':' + model });
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast('Default model set to ' + model);
        loadAI(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Couldn\u2019t set model.', 'err'); e.target.disabled = false; }
    } }, 'Set default'),
  ]);
  setTimeout(() => search.focus(), 0);
}

// modelOptionRow renders a selectable model in the "Change default model" modal.
// The model id is the title (a provider can list dozens, and a coarse class
// like "Fast" can't tell them apart); the class is the secondary line.
function modelOptionRow(g, m, selected, currentVal, onPick) {
  const val = g.provider + ':' + m;
  const f = GhostUI.modelFriendly(val);
  const row = GhostUI.h('div', { className: 'ghost-row', style: 'cursor:pointer;padding:var(--s-2) var(--s-3)' });
  const dot = GhostUI.h('span', { className: 'status-dot ' + (val === selected ? 'ready' : 'neutral'), style: 'flex-shrink:0' });
  row.appendChild(dot);
  const content = GhostUI.h('div', { style: 'margin-left:var(--s-2);min-width:0;flex:1' });
  content.appendChild(GhostUI.h('div', { className: 'ghost-row-title', style: 'font-size:var(--t-body);overflow-wrap:anywhere;font-weight:' + (val === selected ? '600' : '400') }, f.model));
  content.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, f.name));
  row.appendChild(content);
  if (val === currentVal) {
    row.appendChild(GhostUI.h('span', { className: 'type-foot text-tertiary', style: 'margin-left:auto' }, 'Current'));
  }
  row.addEventListener('click', () => onPick(val));
  return row;
}

// ── Providers panel ──

function renderProviders(panel, cfg, providerModels, ollamaModels) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, 'Providers'));
  text.appendChild(GhostUI.h('p', {}, 'The AI services Ghost can think with. It only uses what you connect.'));
  h.appendChild(text);
  panel.appendChild(h);

  const all = ['ollama', 'openai', 'anthropic', 'moonshot', 'groq', 'deepseek', 'qwen', 'gemini', 'zhipu', 'openrouter', 'nvidia'];
  const currentProvider = cfg ? (cfg.provider || '') : '';
  const labelOf = k => ({ openai: 'OpenAI', deepseek: 'DeepSeek', openrouter: 'OpenRouter', nvidia: 'NVIDIA', zhipu: 'Zhipu' }[k] || (k.charAt(0).toUpperCase() + k.slice(1)));
  const isLocal = k => k === 'ollama' || k === 'vllm';
  const connected = all.filter(k => { const pm = providerModels[k]; return isLocal(k) ? ollamaModels.length > 0 : !!(pm && pm.configured); })
    .sort((a, b) => (a === currentProvider ? -1 : 0) - (b === currentProvider ? -1 : 0));
  const available = all.filter(k => !connected.includes(k));

  if (connected.length === 0) {
    panel.appendChild(GhostUI.emptyState('No provider connected', 'Connect one below so Ghost has something to think with.'));
  }
  for (const key of connected) {
    const pm = providerModels[key];
    const modelCount = isLocal(key) ? ollamaModels.length : (pm && pm.models ? pm.models.length : 0);
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    const title = GhostUI.h('div', { className: 'ghost-row-title' }, labelOf(key));
    if (key === currentProvider) title.appendChild(GhostUI.h('span', { className: 'state-chip state-chip-ok', style: 'margin-left:var(--s-2)' }, 'Default'));
    c.appendChild(title);
    const sub = modelCount > 0 ? modelCount + ' model' + (modelCount !== 1 ? 's' : '') + (isLocal(key) ? ' on your Pod' : '') : 'Connected';
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, sub + (pm && pm.source === 'catalog' && !isLocal(key) ? ' · built-in list (couldn’t reach the provider)' : '')));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary ghost-btn-sm', onClick: () => configureProviderModal(key, labelOf(key), isLocal(key), cfg) }, 'Configure'));
    row.appendChild(tr);
    panel.appendChild(row);
  }

  if (available.length > 0) {
    panel.appendChild(GhostUI.h('div', { className: 'provider-add-label' }, connected.length ? 'Add another' : 'Add a provider'));
    const chips = GhostUI.h('div', { className: 'provider-add' });
    for (const key of available) {
      chips.appendChild(GhostUI.h('button', {
        className: 'provider-chip',
        onClick: () => configureProviderModal(key, labelOf(key), isLocal(key), cfg),
      }, '+ ' + labelOf(key)));
    }
    panel.appendChild(chips);
  }
}

function configureProviderModal(key, name, isLocal, cfg) {
  const body = GhostUI.h('div');
  let keyInput = null;
  let urlInput = null;

  if (isLocal && key === 'ollama') {
    // Ollama setup
    body.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary', style: 'margin-bottom:var(--s-3)' }, 'Ollama runs on your device. No API key needed.'));
    const urlField = GhostUI.h('div', { className: 'field' });
    urlField.appendChild(GhostUI.h('label', {}, 'Host URL'));
    urlInput = GhostUI.h('input', { className: 'ghost-input', type: 'text', value: (cfg && cfg.providers && cfg.providers.ollama && cfg.providers.ollama.api_base) || 'http://localhost:11434' });
    urlField.appendChild(urlInput);
    body.appendChild(urlField);
  } else {
    // Cloud provider setup
    const currentKey = cfg && cfg.providers && cfg.providers[key] && cfg.providers[key].api_key || '';
    const keyField = GhostUI.h('div', { className: 'field' });
    keyField.appendChild(GhostUI.h('label', {}, 'API key'));
    keyInput = GhostUI.h('input', { className: 'ghost-input secret-field', type: 'password', placeholder: currentKey ? 'Leave empty to keep current key' : 'Paste your API key' });
    keyField.appendChild(keyInput);
    body.appendChild(keyField);
    if (currentKey) {
      body.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'A key is saved. Leave the field empty to keep it. Test connection uses the saved key.'));
    } else {
      body.appendChild(GhostUI.h('div', { className: 'type-foot text-tertiary' }, 'Stored securely in your Ghost\u2019s secrets file. Never shown back in full.'));
    }
  }

  // Test connection button
  const testRow = GhostUI.h('div', { style: 'margin-top:var(--s-3);display:flex;gap:var(--s-2);align-items:center' });
  const testBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Test connection');
  const testResult = GhostUI.h('span', { style: 'font-size:var(--t-caption)' });
  testBtn.addEventListener('click', async () => {
    testBtn.disabled = true;
    testBtn.textContent = 'Testing\u2026';
    testResult.textContent = '';
    testResult.className = '';
    try {
      const payload = { provider: key };
      const val = keyInput ? keyInput.value.trim() : '';
      if (val) payload.api_key = val;
      const res = await GhostAPI.post('/api/admin/providers/test', payload);
      if (res.ok) {
        testResult.textContent = '\u2713 ' + (res.message || 'Connected');
        testResult.style.color = 'var(--ok)';
      } else {
        testResult.textContent = '\u2717 ' + (res.message || 'Failed');
        testResult.style.color = 'var(--bad)';
      }
    } catch (e) {
      const msg = e.message || 'Unknown error';
      testResult.textContent = '\u2717 ' + (msg.includes('Session expired') ? 'Session expired \u2014 please log in again' : msg.length > 80 ? msg.substring(0, 80) + '\u2026' : msg);
      testResult.style.color = 'var(--bad)';
    }
    testBtn.disabled = false;
    testBtn.textContent = 'Test connection';
  });
  testRow.appendChild(testBtn);
  testRow.appendChild(testResult);
  body.appendChild(testRow);

  GhostUI.modal('Configure ' + name, body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async (e) => {
      const val = keyInput ? keyInput.value.trim() : '';
      const payload = { api_keys: { [key]: val || undefined } };
      if (isLocal && key === 'ollama') {
        payload.ollama_url = urlInput.value.trim();
      }
      e.target.disabled = true;
      try {
        await GhostAPI.post('/api/admin/config/save', payload);
        e.target.closest('.ghost-modal-backdrop').remove();
        GhostUI.toast(name + ' saved');
        loadAI(document.getElementById('view'));
      } catch (err) { GhostUI.toast('Couldn\u2019t save.', 'err'); e.target.disabled = false; }
    } }, 'Save'),
  ]);
}

// ── Routing panel ──

function renderRouting(panel, routing) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, 'Routing'));
  text.appendChild(GhostUI.h('p', {}, 'Ghost automatically chooses the best model when a task requires something different.'));
  h.appendChild(text);
  panel.appendChild(h);

  const prefs = [
    { key: 'prefer_local', label: 'Prefer local AI', desc: 'Always try the local model first, even for complex tasks.' },
    { key: 'allow_cloud', label: 'Allow cloud AI', desc: 'Let Ghost use cloud providers when you have one configured.' },
    { key: 'cloud_when_local_fails', label: 'Fall back to cloud', desc: 'If the local model fails or is unavailable, try cloud instead.' },
  ];
  for (const p of prefs) {
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, p.label));
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, p.desc));
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
}

// ── Local models panel ──

function renderLocal(panel, models, active) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, 'Local models'));
  text.appendChild(GhostUI.h('p', {}, 'Installed on your device via Ollama.'));
  h.appendChild(text);
  panel.appendChild(h);

  for (const m of models) {
    const f = GhostUI.modelFriendly('ollama:' + m);
    const row = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, f.name));
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle', 'aria-hidden': 'true' }, m));
    row.appendChild(c);
    const tr = GhostUI.h('div', { className: 'ghost-row-trailing' });
    if (m === active) {
      tr.appendChild(GhostUI.h('span', { className: 'status-pill' }, GhostUI.statusDot('ready'), 'Active'));
    } else {
      tr.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary', onClick: async () => {
        try { await GhostAPI.proxyPost('/v1/model', { model: 'ollama:' + m }); GhostUI.toast('Default model updated'); loadAI(document.getElementById('view')); }
        catch (e) { GhostUI.toast('Couldn\u2019t set model.', 'err'); }
      } }, 'Use'));
    }
    row.appendChild(tr);
    panel.appendChild(row);
  }

  // Install row
  const install = GhostUI.h('div', { className: 'row-flex', style: 'margin-top:var(--s-3)' });
  const input = GhostUI.input('Model name (e.g. qwen3:8b)');
  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Install');
  btn.addEventListener('click', async () => {
    const name = input.value.trim();
    if (!name) return;
    btn.disabled = true; input.disabled = true;
    try { await GhostAPI.post('/api/ollama/pull', { model: name }); GhostUI.toast('Download started \u2014 this can take a while'); }
    catch (e) { GhostUI.toast('Couldn\u2019t start download.', 'err'); btn.disabled = false; input.disabled = false; }
  });
  install.appendChild(input); install.appendChild(btn);
  panel.appendChild(install);
}

// ── AI Health panel ──

function renderDiag(panel) {
  const h = GhostUI.h('div', { className: 'panel-head' });
  const text = GhostUI.h('div');
  text.appendChild(GhostUI.h('h2', {}, 'AI health'));
  text.appendChild(GhostUI.h('p', {}, 'Quick check that your local and cloud AI are reachable.'));
  h.appendChild(text);
  const btn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary' }, 'Run check');
  h.appendChild(btn);
  panel.appendChild(h);
  const body = GhostUI.h('div', { style: 'margin-top:var(--s-3)' });
  body.appendChild(GhostUI.emptyState('Not run yet', 'Run a check to see how your AI providers are doing.'));
  panel.appendChild(body);

  btn.addEventListener('click', async () => {
    btn.disabled = true; btn.textContent = 'Checking\u2026';
    body.innerHTML = '';
    body.appendChild(GhostUI.loading('Running AI health check\u2026'));
    let res;
    try { res = await GhostAPI.proxyGet('/v1/doctor'); }
    catch (e) {
      body.innerHTML = '';
      body.appendChild(GhostUI.errorState('Diagnostics unavailable', 'The gateway may be starting.'));
      btn.disabled = false; btn.textContent = 'Run check';
      return;
    }
    body.innerHTML = '';
    const checks = (res.checks || []).filter(c => c.name === 'provider' || c.name === 'database' || c.name === 'gateway');
    if (checks.length === 0) {
      body.appendChild(GhostUI.emptyState('Nothing to report', 'No AI checks returned.'));
      btn.disabled = false; btn.textContent = 'Run check';
      return;
    }
    const grid = GhostUI.h('div', { className: 'diag-grid' });
    for (const ch of checks) {
      const row = GhostUI.h('div', { className: 'diag-row' });
      const st = ch.status === 'ok' ? 'ready' : ch.status === 'info' ? 'neutral' : ch.status === 'warn' ? 'warn' : 'bad';
      row.appendChild(GhostUI.h('span', { className: 'status-dot ' + st }));
      row.appendChild(GhostUI.h('div', { className: 'diag-name' }, ch.label || ch.name));
      const msg = GhostUI.h('div', { className: 'diag-msg' });
      msg.textContent = ch.message || '';
      row.appendChild(msg);
      grid.appendChild(row);
    }
    body.appendChild(grid);
    btn.disabled = false; btn.textContent = 'Run check';
  });
}

GhostApp.registerSection('intelligence', loadAI);
