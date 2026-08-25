/* Ghost Section: Automations — scheduled tasks */
'use strict';

async function loadAutomations(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-automations' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Automations'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost does on its own.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/cron/jobs');
    const jobs = data.jobs || [];

    if (jobs.length === 0) {
      section.appendChild(GhostUI.emptyState('No automations yet.', 'Tell Ghost to remind you about something, or schedule a regular task.'));
    }

    for (const job of jobs) {
      const r = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, job.name || 'Unnamed automation'));

      // Human-readable schedule
      let scheduleText = '';
      if (job.schedule) {
        if (job.schedule.kind === 'cron') scheduleText = job.schedule.expr || '';
        else if (job.schedule.kind === 'every') {
          const mins = Math.round((job.schedule.every_ms || 0) / 60000);
          scheduleText = mins >= 60 ? `Every ${Math.round(mins/60)}h` : `Every ${mins}m`;
        }
        else if (job.schedule.kind === 'at') scheduleText = 'One-time';
      }
      if (scheduleText) c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, scheduleText));
      r.appendChild(c);

      const badge = job.enabled !== false ? GhostUI.badge('Active', 'success') : GhostUI.badge('Paused', 'neutral');
      r.appendChild(badge);
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load automations.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

GhostApp.registerSection('automations', loadAutomations);
