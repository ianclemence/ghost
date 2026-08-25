/* Ghost Section: System — technical information */
'use strict';

async function loadSystem(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-system' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'System'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Your Ghost Pod.'));

  // Load data from proxy endpoints (no auth required)
  const [statsRes, doctorRes, healthRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/stats'),
    GhostAPI.proxyGet('/v1/doctor'),
    GhostAPI.proxyGet('/v1/health')
  ]);

  // Version and info
  const infoSection = GhostUI.h('div', { className: 'section-group' });
  infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'About'));
  const infoList = GhostUI.h('div', { className: 'ghost-list' });

  // Version from health endpoint
  if (healthRes.status === 'fulfilled' && healthRes.value.version) {
    infoList.appendChild(GhostUI.row('Ghost version', healthRes.value.version));
  } else if (doctorRes.status === 'fulfilled' && doctorRes.value.version) {
    infoList.appendChild(GhostUI.row('Ghost version', doctorRes.value.version));
  }

  if (statsRes.status === 'fulfilled') {
    const s = statsRes.value;
    if (s.hostname) infoList.appendChild(GhostUI.row('Hostname', s.hostname));
    if (s.ip) infoList.appendChild(GhostUI.row('IP address', s.ip));
    if (s.memory) infoList.appendChild(GhostUI.row('Memory', s.memory));
    if (s.disk) infoList.appendChild(GhostUI.row('Disk', s.disk));
    if (s.cpu_temp) infoList.appendChild(GhostUI.row('Temperature', s.cpu_temp));
    if (s.load) infoList.appendChild(GhostUI.row('Load', s.load));
    if (s.uptime) infoList.appendChild(GhostUI.row('Uptime', s.uptime));
  }

  infoSection.appendChild(infoList);
  section.appendChild(infoSection);

  // Services
  const svcSection = GhostUI.h('div', { className: 'section-group' });
  svcSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Services'));
  const svcList = GhostUI.h('div', { className: 'ghost-list' });

  if (statsRes.status === 'fulfilled') {
    const s = statsRes.value;
    // Ghost service from stats
    const ghostActive = s.ghost_svc === 'active';
    const ghostRow = GhostUI.h('div', { className: 'ghost-row' });
    ghostRow.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Ghost'));
    ghostRow.appendChild(GhostUI.badge(ghostActive ? 'Running' : 'Stopped', ghostActive ? 'success' : 'error'));
    svcList.appendChild(ghostRow);

    // Ollama - check from doctor checks
    let ollamaOk = false;
    if (doctorRes.status === 'fulfilled') {
      const checks = doctorRes.value.checks || [];
      for (const c of checks) {
        if (c.name && c.name.includes('ollama')) {
          ollamaOk = c.status === 'ok';
        }
      }
    }
    const ollamaRow = GhostUI.h('div', { className: 'ghost-row' });
    ollamaRow.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Ollama'));
    ollamaRow.appendChild(GhostUI.badge(ollamaOk ? 'Running' : 'Unknown', ollamaOk ? 'success' : 'neutral'));
    svcList.appendChild(ollamaRow);

    // Ghost Web - always running if we can see this page
    const webRow = GhostUI.h('div', { className: 'ghost-row' });
    webRow.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, 'Ghost Web'));
    webRow.appendChild(GhostUI.badge('Running', 'success'));
    svcList.appendChild(webRow);
  }
  svcSection.appendChild(svcList);
  section.appendChild(svcSection);

  // Diagnostics
  if (doctorRes.status === 'fulfilled') {
    const doc = doctorRes.value;
    const diagSection = GhostUI.h('div', { className: 'section-group' });
    diagSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'System health'));
    const diagList = GhostUI.h('div', { className: 'ghost-list' });

    const checks = doc.checks || [];
    for (const check of checks) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      r.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, check.name));
      const variant = check.status === 'ok' ? 'success' : check.status === 'warning' ? 'warning' : 'error';
      r.appendChild(GhostUI.badge(check.status === 'ok' ? 'Healthy' : check.status === 'fail' ? 'Error' : check.status, variant));
      if (check.message) r.appendChild(GhostUI.h('div', { className: 'type-footnote text-tertiary', style: 'margin-left:auto' }, check.message));
      diagList.appendChild(r);
    }
    diagSection.appendChild(diagList);
    section.appendChild(diagSection);
  }

  // Actions
  const actions = GhostUI.h('div', { style: 'margin-top:var(--space-xxl);display:flex;gap:var(--space-sm)' });
  actions.appendChild(GhostUI.btn('Reboot', 'danger', async () => {
    if (!confirm('Reboot the system?')) return;
    try {
      await GhostAPI.post('/api/admin/reboot');
      GhostUI.toast('Rebooting...');
    } catch (e) {
      GhostUI.toast('Failed to reboot.');
    }
  }));
  section.appendChild(actions);

  container.appendChild(section);
}

GhostApp.registerSection('system', loadSystem);
