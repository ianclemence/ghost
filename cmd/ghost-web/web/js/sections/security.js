/* Ghost Section: Security */
'use strict';

async function loadSecurity(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-security' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Security'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'Protect your Ghost.'));

  // Owner access
  const ownerSection = GhostUI.h('div', { className: 'section-group' });
  ownerSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Owner access'));
  const ownerList = GhostUI.h('div', { className: 'ghost-list' });
  ownerList.appendChild(GhostUI.linkRow('Change password', 'Update your owner password', () => showChangePassword()));
  ownerSection.appendChild(ownerList);
  section.appendChild(ownerSection);

  // Devices
  const devSection = GhostUI.h('div', { className: 'section-group' });
  devSection.appendChild(GhostUI.h('div', { className: 'section-label' }, 'Devices'));
  const devList = GhostUI.h('div', { className: 'ghost-list' });
  devList.appendChild(GhostUI.linkRow('Manage devices', 'View and revoke paired devices', () => GhostApp.navigate('devices')));
  devSection.appendChild(devList);
  section.appendChild(devSection);

  container.appendChild(section);
}

function showChangePassword() {
  const body = GhostUI.h('div');

  const currentGroup = GhostUI.h('div', { className: 'form-group' });
  currentGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Current password'));
  const currentInput = GhostUI.input('Current password', 'password');
  currentGroup.appendChild(currentInput);
  body.appendChild(currentGroup);

  const newGroup = GhostUI.h('div', { className: 'form-group' });
  newGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'New password'));
  const newInput = GhostUI.input('New password', 'password');
  newGroup.appendChild(newInput);
  body.appendChild(newGroup);

  const confirmGroup = GhostUI.h('div', { className: 'form-group' });
  confirmGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Confirm password'));
  const confirmInput = GhostUI.input('Confirm password', 'password');
  confirmGroup.appendChild(confirmInput);
  body.appendChild(confirmGroup);

  GhostUI.modal('Change password', body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      if (newInput.value !== confirmInput.value) { GhostUI.toast('Passwords don\'t match.'); return; }
      if (newInput.value.length < 12) { GhostUI.toast('Password must be at least 12 characters.'); return; }
      try {
        await GhostAPI.post('/api/admin/password', { current: currentInput.value, new: newInput.value, confirm: confirmInput.value });
        GhostUI.toast('Password changed.');
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Couldn\'t change password.');
      }
    }}, 'Save')
  ]);
}

GhostApp.registerSection('security', loadSecurity);
