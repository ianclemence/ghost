/* Ghost Section: System — technical information */
'use strict';

async function loadSystem(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-system' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'System'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Your Ghost Pod.'));

  // Load data
  const [statsRes, doctorRes] = await Promise.allSettled([
    GhostAPI.get('/api/admin/status'),
    GhostAPI.get('/api/admin/doctor')
  ]);

  // Version and info
  const infoSection = GhostUI.h('div', { className: 'section-group' });
  infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'About'));
  const infoList = GhostUI.h('div', { className: 'ghost-list' });

  if (statsRes.status === 'fulfilled') {
    const s = statsRes.value;
    if (s.version) infoList.appendChild(GhostUI.row('Ghost version', s.version));
    if (s.hostname) infoList.appendChild(GhostUI.row('Hostname', s.hostname));
    if (s.ip) infoList.appendChild(GhostUI.row('IP address', s.ip));
    if (s.memory) {
      const usedGB = (s.memory.used / (1024 * 1024 * 1024)).toFixed(1);
      const totalGB = (s.memory.total / (1024 * 1024 * 1024)).toFixed(1);
      infoList.appendChild(GhostUI.row('Memory', `${usedGB}GB / ${totalGB}GB`));
    }
    if (s.disk) {
      const usedGB = (s.disk.used / (1024 * 1024 * 1024)).toFixed(1);
      const totalGB = (s.disk.total / (1024 * 1024 * 1024)).toFixed(1);
      infoList.appendChild(GhostUI.row('Disk', `${usedGB}GB / ${totalGB}GB`));
    }
    if (s.cpu_percent) infoList.appendChild(GhostUI.row('CPU', `${s.cpu_percent.toFixed(1)}%`));
    if (s.load) infoList.appendChild(GhostUI.row('Load', `${s.load.one} ${s.load.five} ${s.load.fifteen}`));
  }

  infoSection.appendChild(infoList);
  section.appendChild(infoSection);

  // Services
  const svcSection = GhostUI.h('div', { className: 'section-group' });
  svcSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Services'));
  const svcList = GhostUI.h('div', { className: 'ghost-list' });

  if (statsRes.status === 'fulfilled' && statsRes.value.services) {
    for (const svc of statsRes.value.services) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      r.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, svc.name));
      const badgeText = svc.active ? 'Running' : 'Stopped';
      const badgeVariant = svc.active ? 'success' : 'error';
      r.appendChild(GhostUI.badge(badgeText, badgeVariant));
      svcList.appendChild(r);
    }
  } else {
    // Fallback
    const services = [
      { name: 'Ghost', active: true },
      { name: 'Ollama', active: true },
      { name: 'Ghost Web', active: true },
    ];
    for (const svc of services) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      r.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, svc.name));
      r.appendChild(GhostUI.badge(svc.active ? 'Running' : 'Unknown', svc.active ? 'success' : 'neutral'));
      svcList.appendChild(r);
    }
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
