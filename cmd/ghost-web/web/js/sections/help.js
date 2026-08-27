/* Ghost Section: Help — honest guidance for what exists. */
'use strict';

async function loadHelp(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Help'));
  head.appendChild(GhostUI.h('p', {}, 'How Ghost works.'));
  container.appendChild(head);

  const prose = GhostUI.h('div', { className: 'panel prose' });
  prose.innerHTML = GhostUI.md(`
## Connecting devices
Open **Devices** and choose *Connect another device*. Ghost shows a code that expires after a few minutes and can be used once. Scan it with the Ghost app on your phone. Once paired, that device can reach your Ghost — but your Ghost itself stays on this hardware.

## AI
Ghost runs a small model on your hardware for everyday tasks (*Local intelligence*). You can optionally add a cloud provider for harder reasoning. Ghost decides where each task runs based on capability, privacy, latency, cost, and availability.

## Memory
Ghost remembers things that matter as plain notes on this device. Open **Memory** to browse, read, and forget them. Forgetting deletes a note from your Ghost.

## Skills
Skills are capabilities Ghost has installed. Built-ins come with Ghost; you can add more from a GitHub repository. Disable a skill to turn it off without deleting it.

## Automations
Automations are tasks Ghost runs on a schedule — a morning briefing, a weekly research roundup. Create one with a name, what it should do, and when it should run.

## Backups
A backup is a download containing your memory, skills, configuration, and automations. Secrets are kept out of backups for safety. Store the file somewhere you trust.

## Diagnostics
If something seems off, open **System** and run *Diagnostics*. It checks Ghost, its services, storage, and connections, and tells you what’s healthy and what isn’t.

## Recovery
If you’re locked out, you can re-run setup from the Ghost service with the force flag to reset owner access. Your memory and skills are preserved.
  `);
  container.appendChild(prose);
}

GhostApp.registerSection('help', loadHelp);
