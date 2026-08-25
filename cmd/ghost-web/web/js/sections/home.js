/* Ghost Section: Home — "What is happening with my Ghost?" */
'use strict';

async function loadHome(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-home' });

  // Greeting
  const greeting = GhostUI.h('div', { className: 'home-greeting' });
  const hour = new Date().getHours();
  const timeGreet = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';
  const nameEl = GhostUI.h('div', { className: 'type-display' }, timeGreet + '.');
  greeting.appendChild(nameEl);
  const presence = GhostUI.h('div', { className: 'home-presence' });
  presence.appendChild(GhostUI.statusDot('online'));
  presence.appendChild(GhostUI.h('span', { className: 'home-presence-text' }, 'Ghost is running normally.'));
  greeting.appendChild(presence);
  section.appendChild(greeting);

  // Load real data
  const [health, cronRes, skillsRes] = await Promise.allSettled([
    GhostAPI.proxyGet('/v1/health'),
    GhostAPI.proxyGet('/v1/cron/jobs'),
    GhostAPI.proxyGet('/v1/skills')
  ]);

  // Ghost status card
  const statusCard = GhostUI.h('div', { className: 'home-stat' });
  statusCard.appendChild(GhostUI.h('div', { className: 'home-stat-label' }, 'Ghost'));
  const statusVal = GhostUI.h('div', { className: 'home-stat-value' });
  statusVal.appendChild(GhostUI.statusDot('online'));
  statusVal.appendChild(document.createTextNode(' Online'));
  statusCard.appendChild(statusVal);
  if (health.status === 'fulfilled' && health.value.uptime_s) {
    const days = Math.floor(health.value.uptime_s / 86400);
    const hours = Math.floor((health.value.uptime_s % 86400) / 3600);
    const uptimeText = days > 0 ? `Running for ${days}d ${hours}h` : `Running for ${hours}h`;
    statusCard.appendChild(GhostUI.h('div', { className: 'home-stat-sub' }, uptimeText));
  }
  section.appendChild(statusCard);

  // Activity section
  const activitySection = GhostUI.h('div', { className: 'home-activity' });
  activitySection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Recent activity'));

  const list = GhostUI.h('div', { className: 'ghost-list' });
  if (cronRes.status === 'fulfilled' && cronRes.value.jobs && cronRes.value.jobs.length > 0) {
    const jobs = cronRes.value.jobs.slice(0, 5);
    for (const job of jobs) {
      const item = GhostUI.h('div', { className: 'home-activity-item' });
      const left = GhostUI.h('div');
      left.appendChild(GhostUI.h('div', { className: 'home-activity-title' }, job.name || 'Automation'));
      if (job.state && job.state.last_run_at) {
        const t = new Date(job.state.last_run_at);
        left.appendChild(GhostUI.h('div', { className: 'home-activity-time' }, t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })));
      }
      item.appendChild(left);
      const statusBadge = job.enabled ? GhostUI.badge('Active', 'success') : GhostUI.badge('Paused', 'neutral');
      item.appendChild(statusBadge);
      list.appendChild(item);
    }
  } else {
    list.appendChild(GhostUI.h('div', { className: 'empty-state', style: 'min-height:auto;padding:var(--space-xl)' },
      GhostUI.h('div', { className: 'type-callout text-secondary' }, 'Nothing to report.')
    ));
  }
  activitySection.appendChild(list);
  section.appendChild(activitySection);

  // Your Ghost quick stats
  const statsSection = GhostUI.h('div');
  statsSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Your Ghost'));
  const stats = GhostUI.h('div', { className: 'home-stats' });

  // Skills
  const skillCount = skillsRes.status === 'fulfilled' ? (skillsRes.value.skills || []).length : '—';
  const skillStat = GhostUI.h('div', { className: 'home-stat' });
  skillStat.appendChild(GhostUI.h('div', { className: 'home-stat-label' }, 'Skills'));
  skillStat.appendChild(GhostUI.h('div', { className: 'home-stat-value' }, String(skillCount)));
  stats.appendChild(skillStat);

  // Automations
  const jobCount = cronRes.status === 'fulfilled' ? (cronRes.value.jobs || []).length : '—';
  const autoStat = GhostUI.h('div', { className: 'home-stat' });
  autoStat.appendChild(GhostUI.h('div', { className: 'home-stat-label' }, 'Automations'));
  autoStat.appendChild(GhostUI.h('div', { className: 'home-stat-value' }, String(jobCount)));
  stats.appendChild(autoStat);

  // AI status
  const aiStat = GhostUI.h('div', { className: 'home-stat' });
  aiStat.appendChild(GhostUI.h('div', { className: 'home-stat-label' }, 'Local AI'));
  aiStat.appendChild(GhostUI.h('div', { className: 'home-stat-value' }, health.status === 'fulfilled' ? 'Ready' : 'Checking\u2026'));
  stats.appendChild(aiStat);

  statsSection.appendChild(stats);
  section.appendChild(statsSection);

  container.appendChild(section);
}

GhostApp.registerSection('home', loadHome);
