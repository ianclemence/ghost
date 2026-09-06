/* Ghost Section: Conversations — past chats with Ghost. */
'use strict';

async function loadConversations(container) {
  if (GhostApp.currentSection() !== 'conversations') return;
  container.innerHTML = '';
  const head = GhostUI.h('div', { className: 'page-head' });
  head.appendChild(GhostUI.h('h1', {}, 'Conversations'));
  head.appendChild(GhostUI.h('p', {}, 'Past conversations with Ghost, newest first.'));
  container.appendChild(head);

  const listEl = GhostUI.h('div', { className: 'ghost-list', id: 'conv-list' });
  listEl.appendChild(GhostUI.loading('Loading conversations…'));
  container.appendChild(listEl);

  let res;
  try { res = await GhostAPI.proxyGet('/v1/sessions'); }
  catch (e) {
    if (!document.body.contains(container)) return;
    listEl.innerHTML = '';
    listEl.appendChild(GhostUI.errorState('Couldn\'t load conversations', 'Ghost may still be starting.'));
    return;
  }
  if (!document.body.contains(container)) return;
  const items = Array.isArray(res) ? res : (res.sessions || res.items || []);
  listEl.innerHTML = '';
  if (items.length === 0) {
    listEl.appendChild(GhostUI.emptyState('No conversations yet', 'Talk to Ghost and your chats will appear here.'));
    return;
  }
  items
    .slice()
    .sort((a, b) => (b.last_activity || 0) - (a.last_activity || 0))
    .forEach(s => {
      const parts = [];
      if (s.message_count != null) parts.push(GhostUI.fmtNum(s.message_count) + ' messages');
      if (s.last_activity) parts.push(GhostUI.timeAgo(s.last_activity));
      listEl.appendChild(GhostUI.row(s.title || 'Conversation', parts.join('  \u00b7  ')));
    });
}

GhostApp.registerSection('conversations', loadConversations);
