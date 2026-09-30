/* Ghost Section: Help — honest guidance for what exists. */
'use strict';

async function loadHelp(container) {
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Help'));
  head.appendChild(GhostUI.h('p', {}, 'Answers to the things people ask first, in the order they tend to come up.'));
  container.appendChild(head);

  const prose = GhostUI.h('div', { className: 'panel prose' });
  prose.innerHTML = GhostUI.md(`
## Connect your phone
Open **Devices** and choose *Connect another device*. Ghost shows a QR code that works once and expires in five minutes. Scan it in the Ghost app. Each phone gets a credential of its own, and you can remove any phone from this page.

## Reach Ghost away from home
At home, your phone reaches Ghost directly. Away from home you need a way in, such as Tailscale on your phone and this Pod. Nothing is exposed to the public internet. Notifications work either way.

## Connect your apps
Open **Apps**. Keys (GitHub, Notion, weather and flights) are tried against the service before Ghost says *connected*. Google Calendar, Gmail, Outlook and Spotify use a sign-in, and each needs an app of your own registered with the provider, once. Ghost walks you through it and stores what you paste sealed. After you approve access, the last page says it can't be reached. That is expected: copy its address and paste it back.

**Website logins** are saved the same way, once, and Ghost signs in without the password passing through the AI. If a site asks for a human check, Ghost stops and tells you. Open its browser card in the app, tap *Take over and steer*, do that step, then tap *Done*.

## Choose how Ghost thinks
Open **Intelligence**. Ghost can think on this Pod, in the cloud, or a mix you control. Every reply says where it ran.

## What Ghost remembers
Open **Memory** to read, search and forget what Ghost knows. Forgetting removes it from this Pod. Each morning Ghost also writes a short daily briefing there from what it remembers, your reminders and the weather. Ask for one any time: *what should I know today?*

## Reminders and routines
A *reminder* tells you something at a time. A *routine* is a job Ghost does on its own and reports back, like *every Monday at 8, give me a brief of my week*. Tell Ghost in a conversation and it sets it up. Manage both under **Routines**.

## What Ghost is allowed to do
Anything consequential waits for your yes, in the app or here under **Permissions**. You can allow something once, for a task, or always, and take an *always* back at any time.

## Skills
Skills are things Ghost knows how to do. Some come with it, and you can add more from a GitHub repository. Turn one off without deleting it.

## Back up, and if you forget your password
**Security** offers an encrypted backup you can download. A backup is only readable with your passphrase, so keep it somewhere safe.

If you forget this console's password, nothing is lost. On the sign-in page choose *Forgot your password?* and use the one-time code from the Ghost app (**Your Pod**, then **Web console**), or put a file named \`ghost-reset-password\` with the new password on the memory card's \`boot\` drive. Your memory, files and paired phones are not touched.

## If something seems off
Ghost tells you about problems itself: low storage, a hot Pod, a failed update. To check on demand, open **System** and run *Diagnostics*.
  `);
  container.appendChild(prose);
}

GhostApp.registerSection('help', loadHelp);
