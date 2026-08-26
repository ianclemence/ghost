/* Ghost Section: Updates */
'use strict';

async function loadUpdates(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-updates' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Updates'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Keep your Ghost current.'));

  try {
    const health = await GhostAPI.proxyGet('/v1/health');
    const version = health.version || 'Unknown';

    const infoSection = GhostUI.h('div', { className: 'section-group' });
    infoSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Current version'));
    const infoList = GhostUI.h('div', { className: 'ghost-list' });
    infoList.appendChild(GhostUI.row('Version', version));
    infoSection.appendChild(infoList);
    section.appendChild(infoSection);

    // Update actions
    const actions = GhostUI.h('div', { style: 'margin-top:var(--space-xxl);display:flex;gap:var(--space-sm)' });
    actions.appendChild(GhostUI.btn('Check for updates', 'primary', async () => {
      try {
        GhostUI.toast('Checking for updates...');
        const res = await GhostAPI.post('/api/admin/update');
        if (res.ok) {
          // Poll for update status — show toast only once
          let attempts = 0;
          let shownProgress = false;
          const poll = setInterval(async () => {
            attempts++;
            try {
              const status = await GhostAPI.get('/api/admin/update/status');
              if (status.running) {
                if (!shownProgress) {
                  GhostUI.toast('Update in progress...');
                  shownProgress = true;
                }
              } else {
                clearInterval(poll);
                if (status.success) {
                  GhostUI.toast('Update complete! Refreshing...');
                  setTimeout(() => location.reload(), 2000);
                } else {
                  GhostUI.toast('Update failed. Check logs for details.');
                }
              }
            } catch (e) {
              clearInterval(poll);
              GhostUI.toast('Ghost may have restarted. Refreshing...');
              setTimeout(() => location.reload(), 3000);
            }
            if (attempts > 60) {
              clearInterval(poll);
              GhostUI.toast('Update is taking longer than expected.');
            }
          }, 500);
        } else {
          GhostUI.toast('Failed to start update.');
        }
      } catch (err) {
        GhostUI.toast('Failed to check for updates.');
      }
    }));
    section.appendChild(actions);

  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t check version.', e.message));
  }

  container.appendChild(section);
}

GhostApp.registerSection('updates', loadUpdates);
