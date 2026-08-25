/* Ghost Section: Channels — how Ghost reaches you */
'use strict';

async function loadChannels(container) {
  container.innerHTML = '';
  const section = GhostUI.h('div', { id: 'section-channels' });

  section.appendChild(GhostUI.h('div', { className: 'page-title' }, 'Channels'));
  section.appendChild(GhostUI.h('div', { className: 'page-subtitle' }, 'How Ghost can reach you.'));

  const listEl = GhostUI.h('div', { className: 'ghost-list' });

  try {
    const data = await GhostAPI.proxyGet('/v1/channels/status');
    const channelNames = ['telegram', 'discord', 'slack', 'email', 'whatsapp'];

    for (const name of channelNames) {
      const ch = data[name] || {};
      const r = GhostUI.h('div', { className: 'ghost-row' });
      const c = GhostUI.h('div', { className: 'ghost-row-content' });
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-title' }, name.charAt(0).toUpperCase() + name.slice(1)));

      const running = ch.running || false;
      const enabled = ch.enabled || false;
      let statusText = 'Not configured';
      let dotState = 'offline';
      if (running) { statusText = 'Connected'; dotState = 'online'; }
      else if (enabled) { statusText = 'Configured'; dotState = 'warning'; }
      c.appendChild(GhostUI.h('div', { className: 'ghost-row-subtitle' }, statusText));
      r.appendChild(c);

      const trailing = GhostUI.h('div', { style: 'display:flex;align-items:center;gap:var(--space-sm)' });
      trailing.appendChild(GhostUI.statusDot(dotState));
      const configBtn = GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost ghost-btn-sm', onClick: () => showChannelConfigModal(name, ch) }, 'Configure');
      trailing.appendChild(configBtn);
      r.appendChild(trailing);
      listEl.appendChild(r);
    }
  } catch (e) {
    section.appendChild(GhostUI.errorState('Couldn\'t load channels.', e.message));
  }

  section.appendChild(listEl);
  container.appendChild(section);
}

function showChannelConfigModal(name, current) {
  const body = GhostUI.h('div');

  // Enabled toggle
  const enableGroup = GhostUI.h('div', { className: 'form-group', style: 'display:flex;align-items:center;gap:var(--space-md)' });
  enableGroup.appendChild(GhostUI.h('label', { className: 'form-label', style: 'margin:0' }, 'Enabled'));
  let isEnabled = current.enabled || false;
  const toggleEl = GhostUI.toggle(isEnabled, (val) => { isEnabled = val; });
  enableGroup.appendChild(toggleEl);
  body.appendChild(enableGroup);

  // Channel-specific fields
  if (name === 'telegram') {
    const tokenGroup = GhostUI.h('div', { className: 'form-group' });
    tokenGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Bot Token'));
    const tokenInput = GhostUI.input('Enter Telegram bot token');
    tokenInput.type = 'password';
    if (current.token) tokenInput.value = current.token;
    tokenGroup.appendChild(tokenInput);
    body.appendChild(tokenGroup);
  } else if (name === 'discord') {
    const tokenGroup = GhostUI.h('div', { className: 'form-group' });
    tokenGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Bot Token'));
    const tokenInput = GhostUI.input('Enter Discord bot token');
    tokenInput.type = 'password';
    if (current.token) tokenInput.value = current.token;
    tokenGroup.appendChild(tokenInput);
    body.appendChild(tokenGroup);
  } else if (name === 'slack') {
    const botGroup = GhostUI.h('div', { className: 'form-group' });
    botGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Bot Token'));
    const botInput = GhostUI.input('Enter Slack bot token');
    botInput.type = 'password';
    if (current.bot_token) botInput.value = current.bot_token;
    botGroup.appendChild(botInput);
    body.appendChild(botGroup);

    const appGroup = GhostUI.h('div', { className: 'form-group' });
    appGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'App Token'));
    const appInput = GhostUI.input('Enter Slack app token');
    appInput.type = 'password';
    if (current.app_token) appInput.value = current.app_token;
    appGroup.appendChild(appInput);
    body.appendChild(appGroup);
  } else if (name === 'email') {
    const smtpHostGroup = GhostUI.h('div', { className: 'form-group' });
    smtpHostGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'SMTP Host'));
    const smtpHostInput = GhostUI.input('smtp.example.com');
    smtpHostInput.value = current.smtp_host || '';
    smtpHostGroup.appendChild(smtpHostInput);
    body.appendChild(smtpHostGroup);

    const smtpPortGroup = GhostUI.h('div', { className: 'form-group' });
    smtpPortGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'SMTP Port'));
    const smtpPortInput = GhostUI.input('587');
    smtpPortInput.type = 'number';
    smtpPortInput.value = current.smtp_port || 587;
    smtpPortGroup.appendChild(smtpPortInput);
    body.appendChild(smtpPortGroup);

    const userGroup = GhostUI.h('div', { className: 'form-group' });
    userGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Username'));
    const userInput = GhostUI.input('Email username');
    userInput.value = current.username || '';
    userGroup.appendChild(userInput);
    body.appendChild(userGroup);

    const passGroup = GhostUI.h('div', { className: 'form-group' });
    passGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'Password'));
    const passInput = GhostUI.input('Email password');
    passInput.type = 'password';
    passGroup.appendChild(passInput);
    body.appendChild(passGroup);

    const fromGroup = GhostUI.h('div', { className: 'form-group' });
    fromGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'From Address'));
    const fromInput = GhostUI.input('ghost@example.com');
    fromInput.value = current.from || '';
    fromGroup.appendChild(fromInput);
    body.appendChild(fromGroup);

    const toGroup = GhostUI.h('div', { className: 'form-group' });
    toGroup.appendChild(GhostUI.h('label', { className: 'form-label' }, 'To Address'));
    const toInput = GhostUI.input('you@example.com');
    toInput.value = current.to || '';
    toGroup.appendChild(toInput);
    body.appendChild(toGroup);
  } else if (name === 'whatsapp') {
    body.appendChild(GhostUI.h('div', { className: 'type-callout text-secondary', style: 'margin-bottom:var(--space-md)' }, 'WhatsApp requires a bridge server. Configure the bridge URL in Advanced settings.'));
  }

  const channelPayload = { [name]: {} };

  GhostUI.modal(`Configure ${name.charAt(0).toUpperCase() + name.slice(1)}`, body, [
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-ghost', onClick: (e) => e.target.closest('.ghost-modal-backdrop').remove() }, 'Cancel'),
    GhostUI.h('button', { className: 'ghost-btn ghost-btn-primary', onClick: async () => {
      try {
        channelPayload[name].enabled = isEnabled;
        if (name === 'telegram' && body.querySelector('input')?.value) {
          channelPayload[name].token = body.querySelector('input').value;
        } else if (name === 'discord' && body.querySelector('input')?.value) {
          channelPayload[name].token = body.querySelector('input').value;
        } else if (name === 'slack') {
          const inputs = body.querySelectorAll('input');
          channelPayload[name].bot_token = inputs[0]?.value || undefined;
          channelPayload[name].app_token = inputs[1]?.value || undefined;
        } else if (name === 'email') {
          const inputs = body.querySelectorAll('input');
          channelPayload[name].smtp_host = inputs[0]?.value || undefined;
          channelPayload[name].smtp_port = parseInt(inputs[1]?.value) || undefined;
          channelPayload[name].username = inputs[2]?.value || undefined;
          channelPayload[name].password = inputs[3]?.value || undefined;
          channelPayload[name].from = inputs[4]?.value || undefined;
          channelPayload[name].to = inputs[5]?.value || undefined;
        }
        await GhostAPI.post('/api/admin/channels/save', channelPayload);
        GhostUI.toast(`${name.charAt(0).toUpperCase() + name.slice(1)} saved.`);
        e.target.closest('.ghost-modal-backdrop').remove();
      } catch (err) {
        GhostUI.toast('Failed to save channel settings.');
      }
    }}, 'Save')
  ]);
}

GhostApp.registerSection('channels', loadChannels);
