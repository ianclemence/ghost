/* Ghost Section: Activity — what Ghost has done */
'use strict';

async function loadActivity(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-activity' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Activity'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'What Ghost has been doing.'));

  // Filters
  const filters = GhostUI.h('div', { style: 'display:flex;gap:var(--space-sm);margin-bottom:var(--space-xxl);flex-wrap:wrap' });
  const filterBtns = ['All', 'Tasks', 'Memory', 'Automations'];
  let activeFilter = 'All';
  for (const f of filterBtns) {
    const b = GhostUI.h('button', {
      className: `ghost-btn ghost-btn-sm ${f === activeFilter ? 'ghost-btn-primary' : 'ghost-btn-ghost'}`,
      onClick: (e) => {
        activeFilter = f;
        filters.querySelectorAll('button').forEach(btn => {
          btn.className = `ghost-btn ghost-btn-sm ${btn.textContent === f ? 'ghost-btn-primary' : 'ghost-btn-ghost'}`;
        });
      }
    }, f);
    filters.appendChild(b);
  }
  section.appendChild(filters);

  // Activity list
  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/sessions');
    const sessions = data.sessions || [];

    if (sessions.length === 0) {
      section.appendChild(GhostUI.emptyState('Nothing to report.', 'Activity will appear here as Ghost works.'));
    }

    // Group by day
    const groups = {};
    for (const s of sessions) {
      const d = s.last_activity ? new Date(s.last_activity) : new Date();
      const day = d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' });
      if (!groups[day]) groups[day] = [];
      groups[day].push({ ...s, date: d });
    }

    for (const [day, items] of Object.entries(groups)) {
      listEl.appendChild(GhostUI.h('div', { className: 'section-label', style: 'margin-top:var(--space-lg)' }, day));
      for (const item of items) {
        const r = GhostUI.h('div', { className: 'ghost-row' });
        const c = GhostUI.h('div', { className: 'ghost-row-content' });
        c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, item.title || item.id || 'Session'));
        c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, `${item.message_count || 0} messages`));
        r.appendChild(c);
        r.appendChild(GhostUI.h('span', { className: 'type-footnote text-tertiary' },
          item.date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        ));
        listEl.appendChild(r);
      }
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load activity.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

GhostApp.registerSection('activity', loadActivity);
