/* Ghost Section: AI — How Ghost thinks */
'use strict';

async function loadAI(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-ai' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'AI'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'How Ghost thinks'));

  // Load data
  const [healthRes, modelRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/model')
  ]);

  // Local intelligence
  const localSection = GhostUI.h('div', { className: 'section-group' });
  localSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Local intelligence'));

  const localCard = GhostUI.h('div', { className: 'ghost-card' });
  const localRow = GhostUI.h('div', { className: 'ghost-row' });
  const localContent = GhostUI.h('div', { className: 'ghost-row-content' });
  localContent.appendChild(GhostUI.h('div', { className: 'type-headline' }, 'Ollama'));
  if (modelRes.status === 'fulfilled' && modelRes.value.active) {
    const modelName = modelRes.value.active.split('/').pop() || modelRes.value.active;
    localContent.appendChild(GhostUI.h('div', { className: 'type-callout text-secondary' }, modelName));
  } else {
    localContent.appendChild(GhostUI.h('div', { className: 'type-callout text-secondary' }, 'Not configured'));
  }
  localRow.appendChild(localContent);
  const localStatus = healthRes.status === 'fulfilled' ? GhostUI.badge('Ready', 'success') : GhostUI.badge('Unavailable', 'warning');
  localRow.appendChild(localStatus);
  localCard.appendChild(localRow);
  localSection.appendChild(localCard);
  localSection.appendChild(GhostUI.h('div', { style: 'margin-top:var(--space-md)' },
    GhostUI.h('a', { className: 'ghost-btn ghost-btn-secondary ghost-btn-sm', href: '#ai-models' }, 'Manage models')
  ));
  section.appendChild(localSection);

  // Cloud intelligence
  const cloudSection = GhostUI.h('div', { className: 'section-group' });
  cloudSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Cloud intelligence'));
  cloudSection.appendChild(GhostUI.h('div', { className: 'type-subhead text-secondary', style: 'margin-bottom:var(--space-md)' }, 'Optional'));

  const providers = [
    { name: 'OpenAI', key: 'openai' },
    { name: 'Anthropic', key: 'anthropic' },
    { name: 'Kimi', key: 'kimi' },
  ];

  const cloudList = GhostUI.h('div', { className: 'ghost-list' });
  for (const p of providers) {
    const r = GhostUI.h('div', { className: 'ghost-row' });
    const c = GhostUI.h('div', { className: 'ghost-row-content' });
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, p.name));
    c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, 'Not configured'));
    r.appendChild(c);
    r.appendChild(GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm' }, 'Configure'));
    cloudList.appendChild(r);
  }
  cloudSection.appendChild(cloudList);
  section.appendChild(cloudSection);

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

GhostApp.registerSection('ai', loadAI);
