/* Ghost Section: AI — How Ghost thinks */
'use strict';

async function loadAI(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-ai' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'AI'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'How Ghost thinks'));

  // Load data in parallel
  const [healthRes, modelRes, configRes, ollamaRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/model'),
    GhostAPI.get('/api/admin/config'),
    GhostAPI.get('/api/ollama/models')
  ]);

  const config = configRes.status === 'fulfilled' ? configRes.value : null;
  const ollamaModels = ollamaRes.status === 'fulfilled' ? (ollamaRes.value.models || []) : [];

  // Current model section
  const currentSection = GhostUI.h('div', { className: 'section-group' });
  currentSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Current model'));

  const currentCard = GhostUI.h('div', { className: 'ghost-card' });
  const currentRow = GhostUI.h('div', { className: 'ghost-row' });
  const currentContent = GhostUI.h('div', { className: 'ghost-row-content' });

  if (modelRes.status === 'fulfilled' && modelRes.value) {
    const model = modelRes.value;
    const providerName = model.provider || 'unknown';
    const modelName = model.active ? model.active.split('/').pop() : 'Not set';
    currentContent.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, providerName.charAt(0).toUpperCase() + providerName.slice(1)));
    currentContent.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, modelName));
    currentRow.appendChild(currentContent);
    currentRow.appendChild(GhostUI.badge('Active', 'success'));
  } else {
    currentContent.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'No model configured'));
    currentRow.appendChild(currentContent);
  }
  currentCard.appendChild(currentRow);
  currentSection.appendChild(currentCard);
  section.appendChild(currentSection);

  // Local models (Ollama)
  const localSection = GhostUI.h('div', { className: 'section-group' });
  localSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Local models'));

  const localCard = GhostUI.h('div', { className: 'ghost-card' });
  const localRow = GhostUI.h('div', { className: 'ghost-row' });
  const localContent = GhostUI.h('div', { className: 'ghost-row-content' });
  localContent.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Ollama'));
  const localStatus = healthRes.status === 'fulfilled' ? GhostUI.badge('Ready', 'success') : GhostUI.badge('Unavailable', 'warning');
  localRow.appendChild(localContent);
  localRow.appendChild(localStatus);
  localCard.appendChild(localRow);

  // List installed Ollama models
  if (ollamaModels.length > 0) {
    for (const model of ollamaModels) {
      const modelRow = GhostUI.h('div', { className: 'ghost-row', style: 'padding-left:var(--space-xl)' });
      const modelContent = GhostUI.h('div', { className: 'ghost-row-content' });
      modelContent.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, model));
      modelRow.appendChild(modelContent);
      // Delete button
      const deleteBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: async () => {
        if (!confirm(`Delete model ${model}?`)) return;
        try {
          await GhostAPI.post('/api/ollama/delete', { model });
          GhostUI.toast(`Model ${model} deleted.`);
          loadAI(container); // Reload
        } catch (e) {
          GhostUI.toast('Failed to delete model.');
        }
      }}, 'Delete');
      modelRow.appendChild(deleteBtn);
      localCard.appendChild(modelRow);
    }
  }

  localSection.appendChild(localCard);

  // Pull new model button
  const pullBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-secondary ghost-btn-sm', style: 'margin-top:var(--space-md)', onClick: () => showPullModelModal() }, 'Pull a model');
  localSection.appendChild(pullBtn);
  section.appendChild(localSection);

  // Cloud providers
  const cloudSection = GhostUI.h('div', { className: 'section-group' });
  cloudSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Cloud providers'));
  cloudSection.appendChild(GhostUI.h('div', { className: 'type-subhead text-secondary', style: 'margin-bottom:var(--space-md)' }, 'Optional. Requires API keys.'));

  const providerList = GhostUI.h('div', { className: 'ghost-list' });

  const cloudProviders = [
    { name: 'OpenAI', key: 'openai', envKey: 'OPENAI_API_KEY' },
    { name: 'Anthropic', key: 'anthropic', envKey: 'ANTHROPIC_API_KEY' },
    { name: 'OpenRouter', key: 'openrouter', envKey: 'OPENROUTER_API_KEY' },
    { name: 'Groq', key: 'groq', envKey: 'GROQ_API_KEY' },
    { name: 'DeepSeek', key: 'deepseek', envKey: 'DEEPSEEK_API_KEY' },
    { name: 'Gemini', key: 'gemini', envKey: 'GEMINI_API_KEY' },
    { name: 'Moonshot (Kimi)', key: 'moonshot', envKey: 'KIMI_API_KEY' },
    { name: 'Zhipu', key: 'zhipu', envKey: 'ZHIPU_API_KEY' },
  ];

  for (const p of cloudProviders) {
    const r = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, p.name));

    const providerData = config && config.providers ? config.providers[p.key] : null;
    const hasKey = providerData && providerData.api_key && providerData.api_key !== '' && !providerData.api_key.startsWith('not set');
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, hasKey ? 'Configured' : 'Not configured'));
    r.appendChild(c);

    const configureBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: () => showProviderConfigModal(p) }, 'Configure');
    r.appendChild(configureBtn);
    providerList.appendChild(r);
  }
  cloudSection.appendChild(providerList);
  section.appendChild(cloudSection);

  // Model settings
  if (config) {
    const settingsSection = GhostUI.h('div', { className: 'section-group' });
    settingsSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Model settings'));
    const settingsList = GhostUI.h('div', { className: 'ghost-list' });
    settingsList.appendChild(GhostUI.row('Temperature', config.temperature ? String(config.temperature) : '0.7'));
    settingsList.appendChild(GhostUI.row('Max tokens', config.max_tokens ? String(config.max_tokens) : '4096'));
    if (config.embedding_model) settingsList.appendChild(GhostUI.row('Embedding model', config.embedding_model));
    settingsSection.appendChild(settingsList);

    const editSettingsBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', style: 'margin-top:var(--space-md)', onClick: () => showModelSettingsModal(config) }, 'Edit settings');
    settingsSection.appendChild(editSettingsBtn);
    section.appendChild(settingsSection);
  }

  // Routing
  const routingSection = GhostUI.h('div', { className: 'section-group' });
  routingSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Routing'));
  const routingCard = GhostUI.h('div', { className: 'ghost-card' });
  routingCard.appendChild(GhostUI.h('div', { className: 'type-callout', style: 'margin-bottom:var(--space-sm)' },
    'Ghost normally prefers local intelligence. When a task needs deeper reasoning, Ghost may use an approved cloud model.'
  ));
  const routingRow = GhostUI.h('div', { className: 'ghost-row' });
  const localLabel = GhostUI.h('div', { className: 'ghost-row-content' });
  localLabel.appendChild(GhostUI.h('div', { className: 'type-callout text-primary' }, 'Local'));
  localLabel.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary' }, 'Private \u00b7 Fast \u00b7 Offline'));
  routingRow.appendChild(localLabel);
  routingCard.appendChild(routingRow);

  const routingRow2 = GhostUI.h('div', { className: 'ghost-row' });
  const cloudLabel = GhostUI.h('div', { className: 'ghost-row-content' });
  cloudLabel.appendChild(GhostUI.h('div', { className: 'type-callout text-primary' }, 'Cloud'));
  cloudLabel.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary' }, 'Deeper reasoning \u00b7 Optional'));
  routingRow2.appendChild(cloudLabel);
  routingCard.appendChild(routingRow2);

  routingSection.appendChild(routingCard);
  section.appendChild(routingSection);

  container.appendChild(section);
}

function showProviderConfigModal(provider) {
  const body = GhostUI.h('div');

  const keyGroup = GhostUI.h('div', { className: 'form-group' });
  keyGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'API Key'));
  const keyInput = GhostUI.input('Enter API key');
  keyInput.type = 'password';
  keyGroup.appendChild(keyInput);
  body.appendChild(keyGroup);

  GhostUI.modal(`Configure ${provider.name}`, body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      const apiKey = keyInput.value.trim();
      if (!apiKey) { GhostUI.toast('Please enter an API key.'); return; }
      try {
        await GhostAPI.post('/api/admin/config/save', {
          api_keys: { [provider.key]: apiKey }
        });
        GhostUI.toast(`${provider.name} configured.`);
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Failed to save configuration.');
      }
    }}, 'Save')
  ]);
}

function showPullModelModal() {
  const body = GhostUI.h('div');
  const group = GhostUI.h('div', { className: 'form-group' });
  group.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Model name'));
  const input = GhostUI.input('e.g. llama3.2, qwen3:0.6b');
  group.appendChild(input);
  body.appendChild(group);
  body.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-top:var(--space-sm)' }, 'This may take a few minutes depending on model size.'));

  GhostUI.modal('Pull a model', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      const model = input.value.trim();
      if (!model) { GhostUI.toast('Please enter a model name.'); return; }
      try {
        await GhostAPI.post('/api/ollama/pull', { model });
        GhostUI.toast(`Download started for ${model}.`);
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Failed to start download.');
      }
    }}, 'Pull')
  ]);
}

function showModelSettingsModal(config) {
  const body = GhostUI.h('div');

  const tempGroup = GhostUI.h('div', { className: 'form-group' });
  tempGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Temperature'));
  const tempInput = GhostUI.input('0.0 - 2.0');
  tempInput.type = 'number';
  tempInput.step = '0.1';
  tempInput.min = '0';
  tempInput.max = '2';
  tempInput.value = config.temperature || 0.7;
  tempGroup.appendChild(tempInput);
  body.appendChild(tempGroup);

  const maxTokensGroup = GhostUI.h('div', { className: 'form-group' });
  maxTokensGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Max tokens'));
  const maxTokensInput = GhostUI.input('4096');
  maxTokensInput.type = 'number';
  maxTokensInput.min = '256';
  maxTokensInput.max = '128000';
  maxTokensInput.value = config.max_tokens || 4096;
  maxTokensGroup.appendChild(maxTokensInput);
  body.appendChild(maxTokensGroup);

  const embeddingGroup = GhostUI.h('div', { className: 'form-group' });
  embeddingGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Embedding model'));
  const embeddingInput = GhostUI.input('e.g. nomic-embed-text');
  embeddingInput.value = config.embedding_model || '';
  embeddingGroup.appendChild(embeddingInput);
  body.appendChild(embeddingGroup);

  GhostUI.modal('Model settings', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      try {
        await GhostAPI.post('/api/admin/config/save', {
          temperature: parseFloat(tempInput.value) || 0.7,
          max_tokens: parseInt(maxTokensInput.value) || 4096,
          embedding_model: embeddingInput.value.trim()
        });
        GhostUI.toast('Settings saved.');
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Failed to save settings.');
      }
    }}, 'Save')
  ]);
}

GhostApp.registerSection('ai', loadAI);
