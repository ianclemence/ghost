/* Ghost Section: System — technical information */
'use strict';

async function loadSystem(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-system' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'System'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Your Ghost Pod.'));

  // Load data
  const [healthRes, statsRes, doctorRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/stats'),
    GhostAPI.proxyGet('/v1/doctor')
  ]);

  // Version and info
  const infoSection = GhostUI.h('div', { className: 'section-group' });
  infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'About'));
  const infoList = GhostUI.h('div', { className: 'ghost-list' });

  if (healthRes.status === 'fulfilled') {
    const h = healthRes.value;
    infoList.appendChild(GhostUI.row('Ghost version', h.version || 'Unknown'));
    if (h.uptime_s) {
      const days = Math.floor(h.uptime_s / 86400);
      const hours = Math.floor((h.uptime_s % 86400) / 3600);
      infoList.appendChild(GhostUI.row('Uptime', days > 0 ? `${days}d ${hours}h` : `${hours}h`));
    }
  }

  if (statsRes.status === 'fulfilled') {
    const s = statsRes.value;
    if (s.hostname) infoList.appendChild(GhostUI.row('Hostname', s.hostname));
    if (s.memory) infoList.appendChild(GhostUI.row('Memory', s.memory));
    if (s.disk) infoList.appendChild(GhostUI.row('Disk', s.disk));
    if (s.cpu_temp) infoList.appendChild(GhostUI.row('Temperature', s.cpu_temp));
    if (s.ip) infoList.appendChild(GhostUI.row('IP address', s.ip));
  }

  infoSection.appendChild(infoList);
  section.appendChild(infoSection);

  // Services
  const svcSection = GhostUI.h('div', { className: 'section-group' });
  svcSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Services'));
  const svcList = GhostUI.h('div', { className: 'ghost-list' });

  const services = [
    { name: 'Ghost', status: statsRes.status === 'fulfilled' ? (statsRes.value.ghost_svc || 'active') : 'unknown' },
    { name: 'Ollama', status: healthRes.status === 'fulfilled' ? 'active' : 'unknown' },
    { name: 'Ghost Web', status: 'active' },
  ];

  for (const svc of services) {
    const r = GhostUI.h('div', { className: 'ghost-row' });
    r.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, svc.name));
    const badgeText = svc.status === 'active' ? 'Running' : 'Unknown';
    const badgeVariant = svc.status === 'active' ? 'success' : 'neutral';
    r.appendChild(GhostUI.badge(badgeText, badgeVariant));
    svcList.appendChild(r);
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
      r.appendChild(GhostUI.badge(check.status === 'ok' ? 'Healthy' : check.status, variant));
      diagList.appendChild(r);
    }
    diagSection.appendChild(diagList);
    section.appendChild(diagSection);
  }

  container.appendChild(section);
}

GhostApp.registerSection('system', loadSystem);
