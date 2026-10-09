# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

## [0.24.166] - 2026-10-09

- **Ask about your data; keep the answer live.** "How did sales do by
  month?" Ghost writes the query, draws the chart and keeps it as a
  dashboard that updates whenever you look at it. Every chart shows the
  query behind it. It reads your own finances, health, reading and trips,
  any spreadsheet you send, and databases you connect (Postgres, such as
  Supabase or Neon), and it only ever reads.
- **Animated explainers, made as code.** Ghost turns a report, numbers or
  a walkthrough into a short animation you watch in the chat. Change any
  word, number or timing, then make it an MP4 on your Pod. No video model
  is involved.
- **Knowledge: what you read and study.** Books, courses and subjects for
  an exam, with where you are, your notes and the lines you kept. Ghost
  quizzes you from them, reminds you a week before an exam, and asks
  gently when a book has gone quiet. Learn now makes you a flashcard deck
  that brings cards back just before you would forget them.
- **Money is now Finances**, everywhere. What you kept carries over.
- **Plainer words.** Health checks read as sentences, each connected app
  says what Ghost can do with it, and developer keys sit apart.
- **Safer.** Secrets in your Pod's sealed vault are scrubbed again from
  anything the model sees (they were being missed). A new install also sets
  up the browser, pandoc and ffmpeg that documents, browsing and videos need.

## [0.24.165] - 2026-10-09

- **Jobs: say what Ghost should take on.** Ten jobs, each one switch and a
  time: a morning brief, your inbox in hand (replies drafted, never sent),
  bills and subscriptions, a trip companion, meals with a shopping list to
  tick, life admin for the month, a last look around the house at night, a
  gentle health week, learning something, and watching a page. Each says
  what it still needs, like a connected mailbox. A job with nothing to say
  stays quiet.
- **Your phone can tell Ghost things, if you let it.** In the app's Phone
  settings, each is off until you turn it on: notifications from the apps
  you choose ("did the bank text me?"), daily steps, sleep and resting heart
  rate, and reminders at places that fire the moment you arrive, even with
  Ghost closed. Ghost can also set an alarm on your phone's clock with your
  tap. Turning notifications off forgets what Ghost had.
- **Pages Ghost builds can remember.** A page keeps what you did between
  opens (a score, a flashcard deck's progress), and Ghost can read it to
  build the next version from where you got to. Learn now makes you a
  flashcard deck that brings cards back just before you would forget them.

## [0.24.164] - 2026-10-09

- **Cards can ask you things.** A card can offer choices, a date and time,
  an amount, a few words, or a list to tick, answered with one send that
  Ghost checks before it counts. Questions Ghost used to ask as a grey line
  above the box now arrive as cards you can answer in place, and a grocery
  list keeps each tick.
- **Ghost drafts it; you send it.** Ask for an email, an event or a text
  and Ghost shows it as it will go out, ready to edit first. Sending is
  your tap on the card: mail goes through Gmail or Outlook, the event is
  put on the calendar with its real times, a text opens in Messages filled
  in. The card says what really happened.
- **Cards can compare, chart and map.** A side-by-side with Ghost's pick,
  bars and lines that fit any width, and places drawn by their positions,
  each opening in your maps app.
- **Ghost lays out documents to print.** What Ghost writes becomes a printed
  page in its own type, kept as PDF and as Word, each change the next
  version. Everything Ghost made — pages, documents, pictures, links, notes —
  lives on one shelf, pinned first, versions as one entry, with search.
- **Ghost keeps track of people, papers and money.** Who matters with
  birthdays and when you last spoke, passports and warranties with when
  they run out (read off the photo), what went out and came in by month
  with subscriptions and bills. It reminds you: a birthday a week ahead,
  a paper at 90, 30 and 7 days, a bill three days ahead.
- **Ghost watches a page and tells you when what you asked happens.** "Tell
  me when it's under KES 25,000" or back in stock or when a slot opens, made
  from your own words with the link in them. It looks with one safe read and
  pings you only when the rule is met; if it already is, it says so.
- **Trips are journeys with their legs.** Flights, trains and hotels from
  what you said and your bookings, on a timeline: the week before (and a
  passport too close to expiring), the day before a flight with one tap to
  follow it, when to leave for the airport, the hotel on its day.
- **Recorded meetings come back as text.** Record from the composer; the
  phone sends it in pieces and Ghost transcribes it part by part with your
  own speech engine, kept as a document. One tap asks what was decided and
  the actions as a checklist.

## [0.24.163] - 2026-10-09

- **Pages Ghost builds fill their window in the chat.** The app now shows a
  canvas as a framed window with its own title bar and an Open button, as wide
  as the conversation. Ghost is told so: it uses the whole width instead of
  drawing a small card in the middle of the page, and does not repeat the
  title the window already shows.

## [0.24.162] - 2026-10-09

- **Ghost can build something and show it running in your chat.** Ask for a
  page, a game, a calculator, a chart or a mock-up (or to see what some code
  does) and Ghost writes it as one self-contained page that runs right in the
  conversation. Each change is saved as the next version, so you can step back
  through them, and a page that errors can be sent back to Ghost to fix. The
  phone runs it in a sandbox: no network, no storage, no navigation. Ghost is
  told to tell you about anything the sandbox blocks before you see it fail.
- **A follow-up to a canvas keeps it in reach.** "Make the button blue" now
  produces a new version instead of pasting HTML into the chat, for half an
  hour after the last canvas.
- **The terminal names a canvas step** ("Built a page") with its title.

## [0.24.161] - 2026-10-08

- **An alert goes away when the problem does.** "I'm almost out of storage"
  used to stay in the conversation as a "Needs you" card long after you freed
  the space. Ghost now notices when what it reported has stayed fixed for a
  couple of checks (storage, memory, heat, room temperature and humidity),
  marks the alert resolved, and tells your app and terminal at once. The words
  stay as a record; the card settles into a quiet "Resolved" line. If the
  problem comes back, you are told again. Alerts written before this are keyed
  on first start so the ones already on your phone settle too.
- **The terminal shows what Ghost did, step by step.** Each finished step is
  one quiet row with what it was, what it was about and how long it took, and a
  failure says why on the line beneath. A long task prints the first eight and
  counts the rest; `/details` shows every step and full commands.
- **What you type while Ghost works is never lost.** In the terminal and the
  app, a message sent into a running turn is either read by Ghost or handed
  back and sent as the next message, exactly once. A failed command's note now
  says what the command said instead of "STDERR:".
- **Local mode says what it can't do.** With no daemon running, the terminal
  now says commands and the browser stay off until `ghost serve`.

## [0.24.160] - 2026-10-08

- **Alerts now use the whole width of a phone.** A toast (like the Undo after
  dismissing a notice) could only grow to half the screen, so on a phone every
  message wrapped inside about half its width, a word or two per line, with a
  gap before the button. Toasts now span the screen up to a comfortable
  maximum, sit above the phone's bottom edge, and keep soft corners instead of
  turning into a circle when a message runs long.

## [0.24.159] - 2026-10-08

- **The phone can now show what Ghost did, step by step.** While a reply is
  being written the Pod reports each tool call as it starts and as it ends,
  with what it was about (the search, the site, the command, the file name),
  how long it took and why it failed. Anything that looks like a credential is
  masked and every line is cut short.
- **Messages sent while Ghost works are never left waiting for nothing.** The
  Pod tells the phone when it has read a message sent into the running turn,
  and hands back any it finished without reading, so the phone can send them
  as the next message. Steering a turn that is not running is refused instead
  of queueing for a later turn to pick up out of context.

## [0.24.158] - 2026-10-07

- **Alert pills with buttons no longer squeeze their text.** Toasts carrying
  an action (like the Undo on a dismissed notice) starved their own message,
  wrapping it a word per line next to the button. The message now takes the
  free space and wraps inside it while the button holds its place — for every
  alert, not just dismissals. Dismissed notices also lost their doubled
  quotes around notice titles (`"Disk"` instead of `""Disk""`).

## [0.24.157] - 2026-10-07

- **Memory search now runs on Google's EmbeddingGemma.** The on-device
  embedding model behind recall is now `embeddinggemma` instead of
  `nomic-embed-text`, with better code understanding for codebase
  search. Setups pull it automatically during install, and existing
  memories are re-embedded in the background on first start after the
  update — nothing is forgotten. (EmbeddingGemma 2 stays a one-line
  flip away: its Ollama builds are Apple-only for now and cannot run
  on this device yet.)

## [0.24.156] - 2026-10-06

- **Dismissing a suggestion now keeps it dismissed.** Tapping "No thanks"
  on a suggestion card recorded the decision against the suggestion but
  left its card untouched, so the same card came back on every restart
  until it expired. Deciding a suggestion now puts its companion cards
  away too, and dismissed cards are no longer served to any device.

## [0.24.155] - 2026-10-04

- **Stopping a reminder or automation from the phone now works.** The
  Routines screen sends "Stop" for anything that is not a Ghost routine to
  the scheduler, but that endpoint only knew pause, resume and run — so the
  request came back "unsupported action" (HTTP 400) and the item kept
  going. Cancelling a scheduled item is now an action the endpoint accepts,
  the same cancellation the console's delete already performed.

## [0.24.154] - 2026-10-04

- **Approving from a button now actually runs the approved action.**
  Tapping "Allow once", "Allow for this task", "Always allow" or "Deny" in
  the app or the web console recorded the choice but never carried it out:
  the card cleared and the action stayed unrun, so the conversation kept
  showing "waiting for your approval". A button now resumes the paused
  call through the exact same governed path a typed "yes" uses — it runs
  once, its result returns to the model, and the reply lands in the
  conversation. "Allow once" is still exactly once.
- **One reminder can no longer look like two.** A reminder that fired twice
  in quick succession — a retry, a restart, two ticks racing a slow boot —
  left two delivery records, and Ghost counted them as two separate things
  still open (that is where "those two 'Drink a glass of water' reminders"
  came from, when there was one). A reminder now reads as one however many
  times it was sent, answering it settles every one of its deliveries, and
  a completed reminder stops dragging down the "you ignored this" tally.

## [0.24.153] - 2026-10-04

- **A space can be told "read, but don't write".** Ghost already enforced
  read-versus-write per capability — reads pass, and sends, edits and
  deletions ask first — and a space's capability list could narrow that,
  but there was no way to actually set it. `POST /v1/contexts/capabilities`
  lets the owner list exactly what a space may do: leave it empty and
  everything is allowed, list only the read capabilities and the writes
  fail closed before anything runs. The list can only take authority away,
  never add it. A misspelled capability is refused rather than stored,
  because a deny-by-typo is indistinguishable from a deliberate no.

## [0.24.152] - 2026-10-04

- **Deleting a conversation deletes the conversation.** Removing a session
  over the API dropped its messages but left the session row and its
  summary and title behind — a ghost of a conversation you had asked to be
  rid of. It now removes the whole thing through the session store, the
  same way `/forget session` already did.
- **Take your memory with you.** `/memory export [file]` writes everything
  Ghost remembers — what it believes now, what you corrected, and what you
  asked it to forget — to one readable JSON file, with owner-only
  permissions, defaulting to the working directory. It is the data itself,
  not a summary Ghost chose to show you.

## [0.24.151] - 2026-10-04

- **Forgetting leaves a mark.** Ghost recorded when it remembered
  something and when it changed its mind, but never when it removed
  something — so a belief you asked it to forget simply disappeared from
  the record, and the activity feed had nothing to show. Every removal now
  writes a plain line: the belief, why, and what Ghost rebuilt to make the
  forgetting hold. It names what was done, never what was said — a receipt
  for deleting something must not be another copy of it.
- **`/forget` in chat now forgets completely.** It retired the structured
  belief but left two things behind: no tombstone, so an old message could
  bring the belief back, and its own daily notes untouched, so Ghost could
  still recite what it had just agreed to forget. The chat command now runs
  the same full forgetting as the memory tools and the console — tombstone,
  notes, digest, profile, search index — so every door leads to the same
  complete result.
- **Memory you can inspect, not just the current answer.** `/memory` showed
  only what Ghost believes right now. `/memory history` shows what it used
  to believe, what you corrected and to what, and the beliefs you asked it
  to forget — each with the reason, and none of it a way to recover the
  removed content.
- A forgotten value no longer survives in Ghost's own notes just for being
  short. The scrubber matched only values of six characters or more, so a
  colour, a city or a first name stayed in the journal; short values now
  match whole words, so "green" goes while "greenery" stays.

## [0.24.150] - 2026-10-04

- **Recovery no longer gives up when it runs before the database is
  ready.** The job runner starts as part of the agent loop, and the loop
  is built before the schema migration runs on boot. That meant the very
  first look for work a restart had interrupted could fail against a jobs
  table that did not have the new columns yet — and it only looked once.
  It now keeps trying until it succeeds, so the work recovery exists to
  save is never the work it drops.

## [0.24.149] - 2026-10-04

- **Background work now outlives the Pod that started it.** A task you
  handed off — research this, watch that page, draft the report — was a
  goroutine in memory, and a restart, a crash or an update killed it
  silently with nothing in the conversation to say it had. Ghost writes
  the task down before it runs and, on boot, picks up anything a restart
  left mid-flight and carries on — with each attempt allowed hours rather
  than the old five minutes. What an earlier attempt confirmed is handed
  to the next one, so a resumed task does not quietly repeat work you
  already saw happen.
- **A task that fails for a moment gets another go.** A provider
  blinking, a page timing out, a command that was not ready used to end
  the task the first time. It now waits and tries again — a few seconds,
  then longer, up to a ceiling — and only reports a failure once it has
  genuinely run out of attempts. A task stopped by a restart is not
  counted as one of its failures: our stop is not its mistake.
- **A task that needs your approval waits instead of giving up.** When
  background work reached something consequential — sending a message,
  scheduling, deleting — it was told "ask first" and pushed on anyway,
  either grinding against the same wall or wandering into something you
  had not agreed to. It now stops at that step, parks as waiting for
  your approval, and resumes from where it stopped the moment you
  answer. Waiting on you never spends the task's attempts, however long
  you take to reply.

## [0.24.148] - 2026-10-04

- **A page that re-renders under Ghost no longer costs two turns.** Refs
  are short-lived on purpose: any click, fill or submit closes the
  snapshot they came from, so the next action on an old one is refused.
  On a real site that is the normal condition — a modal opens, a list
  reorders, a step swaps the form out — and the refusal used to send
  Ghost back for a page it could already see, spending a whole round
  trip to learn what had changed. The refusal now carries the page as it
  is now, with its refs live, so the very next action uses one of them.
  Ghost never retries the refused ref itself: the same `@e1` on a
  re-rendered page is a different element, and clicking it would be
  Ghost choosing something you never chose. You still choose; it just
  doesn't cost an extra step.
- **A site that wants a human hands off in the same breath.** A CAPTCHA
  or "Verify you are human" was detected only where a step succeeded, so
  a step that failed could leave the browser card saying Ghost was still
  working with no "Take over" button on it — and the model was never
  told. Both now happen together: the card offers the hand-off the
  moment the check is seen, even on a failed step, and the model is told
  plainly to stop, name the button you should tap, and carry on when you
  do, instead of grinding on a check it will not pass.
- The record of what an action proved is no longer overwritten by the
  record of who ran it. A checkout submission records the merchant,
  total and whether the page confirmed it; a recovered refusal records
  the page it saw. Those are what the audit trail reads, and the
  generic envelope was replacing the whole thing with nine plain keys.

## [0.24.147] - 2026-10-03

- **A restart reads as a pause, not amnesia.** The reply to a message lived
  only in the memory of a process that could die, so the Pod going down
  mid-sentence left the transcript with a question and no answer — Ghost
  came back rebuilding context instead of continuing it. What has been
  written now reaches disk while it is still being written. When the
  process returns: a turn still worth finishing (recent, and still the
  newest thing in its conversation) is finished, the rest of the reply
  streaming in as though nothing had happened; anything else has its
  partial reply written into the transcript, marked cut short. Nothing
  said is dropped in either case. Finishing waits a few seconds for boot
  to settle, never runs a turn that was waiting on you, and can be turned
  off with `GHOST_NO_AUTO_RESUME=1` (or narrowed with
  `GHOST_RESUME_WINDOW`).
- **"Continue" actually continues.** A reply that stopped mid-sentence
  carries a marker only the model sees, so it picks up where it stopped
  instead of starting the answer again. The phone shows a quiet
  "Stopped when your Pod restarted" under a reply that ended that way —
  an unfinished answer with nothing said about it reads as Ghost trailing
  off.
- Reconnecting to a turn the restart killed now says the reply is in the
  conversation, instead of insisting the turn is still running and
  leaving the client waiting on a reply that will never arrive.
- The JSONL session store keeps the interrupted marker through a rewrite:
  `Save` rebuilds the transcript file from what it reads back, and it was
  dropping every field the line format did not carry.

## [0.24.146] - 2026-10-03

- **Live view holds.** The takeover screencast socket now upgrades as
  generously as the main conversation socket: its auth is the single-use
  ticket in the URL, so an unexpected Origin header from a native client no
  longer turns the phone away, and the frame buffers match what the stream
  actually sends. The proxy also logs where a connection ends, so a drop is
  diagnosable instead of silent.

## [0.24.145] - 2026-10-03

- **Taking over the browser works.** When a site asked for a human check and
  you tapped "Take over and steer", the screen said the live view was not
  available. The live-view broker and the browser tool were resolving
  different HOME and runtime directories, so the broker looked in the wrong
  socket directory and gave up while the stream server was listening on
  another. Both now resolve the same place, so the live view connects.

## [0.24.144] - 2026-10-03

- **Multi-step sign-ins work.** Sites like X split sign-in across screens (a
  username screen, then a password screen), which the one-shot sign-in path
  could not finish. Ghost now drives each field in turn, filling from the
  sealed login, so the secret never touches the chat, a log, or a command
  line. `browser_fill` takes a `vault` reference
  (`weblogin:<host>:username|password`) resolved inside the runtime.
- A new native browser engine (Go, speaking Chrome's protocol directly)
  lands under `pkg/browser/native`, the first step to owning the browser
  layer natively. Not wired into the running tools yet.

## [0.24.143] - 2026-10-03

- **Signing into a saved site reaches the phone.** `browser_login` was
  missing from the mobile tool profile, so the governed sign-in path could
  not run where you actually chat. It now can — the broker still asks
  first, every time.

## [0.24.142] - 2026-10-03

- **Website logins work end to end.** Asking Ghost to sign in to a site you
  saved now follows the governed path instead of a refusal: Ghost checks
  what is sealed (the `connections` tool lists saved website logins, hosts
  only, never secrets), opens the session with `browser_login` with your
  approval, and reads what you asked for. A refusal must now name its rule —
  a broker denial, a missing capability, the law, or safety — never a
  personal line.

## [0.24.141] - 2026-10-03

- **Reminders are promises, not messages.** A reminder arrives with Done,
  10 min, 1 hour and Tomorrow buttons, on its card and on the phone
  notification itself. Done, snoozed or put away is recorded, so Ghost knows
  "reminded" from "done" and never nags about one already handled. "I already
  watered the plants" closes the reminder so it never fires.
- **One morning message.** Small things Ghost notices — an upcoming trip, a
  reminder that went unseen, something you said you would buy or do — arrive
  together in one morning message instead of as separate pings. Urgent things
  still break through right away.
- **Ghost follows up.** Things you mentioned in passing — planning to buy
  something, something you said you would do, a choice you were weighing,
  something you were waiting on — are brought up once, at a sensible time,
  then let go. Ignored kinds of things go quiet on their own.
- Memory shows who said what and when: your words read "You told Ghost",
  with an Edit button beside Forget.

## [0.24.140] - 2026-10-03

- Forgetting your name or where you live also clears it from Ghost's profile
  of you, which it reads on every turn.
- Asked about something you told it to forget, Ghost says it was forgotten at
  your request instead of claiming you never mentioned it.

## [0.24.139] - 2026-10-03

- **Forgetting reaches old conversations too.** Searching past conversations
  no longer reads back a fact you asked Ghost to forget; the conversation is
  kept as it was, but Ghost stops reciting it.
- What you tell Ghost outright ("my dentist is Dr. Lee") is recorded as
  yours, not as Ghost's guess, so a later guess cannot quietly replace it.
- Dates in memories are written as dates: "next month, on the 2nd" becomes
  "2 Nov 2026", so a trip does not read as upcoming forever.

## [0.24.138] - 2026-10-03

- **Forgotten stays forgotten.** The summary Ghost writes at the end of a
  turn used to record the very fact you had just asked it to forget ("their
  barber is Somsak… then asked to forget it"). Every note Ghost writes now
  leaves out what you asked it to forget, and older notes are cleaned too.
  Tell Ghost the same thing again later and it remembers it again.
- The same fact learned twice is one memory, not two rows.
- "Every day at 7:30 in the morning, check the weather" is saved as "check
  the weather", not "In the morning, check the weather", and Ghost confirms it
  as something it will do, not as a reminder.
- Activity shows a reminder in its own words ("Reminded you: check the oven").

## [0.24.137] - 2026-10-03

- **Routines run.** A routine created in chat ("every day at 7:30, check the
  weather") was saved without its next run, so the scheduler never picked it
  up, and its time was read as UTC instead of yours. New routines get their
  first run and your timezone; existing ones are repaired on start.
- **Forget means forget.** Forgetting a memory now retracts every earlier
  version of it, removes it from Ghost's own notes and from search. Asked to
  search its notes, Ghost used to recite a dentist you had told it to forget.
- **Facts stop overwriting each other.** A favourite football club no longer
  replaces a favourite programming language, and separate facts filed under
  the same broad heading ("Works as an ESL teacher", "Is originally from
  Tanzania") are no longer hidden as a conflict.
- Memories read as sentences: "Favorite football club is Chelsea", not
  "Your favorite is Favorite football club is Chelsea."
- Activity shows a reminder going off ("Reminded you: stand up and stretch"),
  and a reminder Ghost missed while it was off. Cancelling or changing a
  reminder says so instead of "Scheduled it", and its approval asks "Cancel
  this?".
- A reminder a day or more late (Ghost was off) is not delivered as if it
  were fresh: Ghost tells you it was missed and offers to set it again.
- Morning briefings and other heartbeat messages are saved in your
  conversation and reach the phone, instead of disappearing when the app was
  closed.
- A reminder push shows the reminder itself, not "Ghost has a reminder for you".
- "I'll do it now" and "I want to see you open the page" are no longer filed
  as promises you made.

## [0.24.136] - 2026-10-03

- **A forecast out of range says so.** Asking for the weather on a date past
  the 14-day forecast showed "Weather unavailable · Something went wrong".
  It now says "The forecast doesn't reach that date yet."
- Approvals in Activity read in plain words: "You said yes, so Ghost went
  ahead" instead of "You approved this consequential action".

## [0.24.135] - 2026-10-03

- **A "verify you are human" page waits for you.** When a site puts up a bot
  check, the browser card stays live with "Needs you to prove you're human"
  and Take over and steer, instead of settling into a record the moment Ghost
  answers. Ghost used to tell you to tap a button that was no longer there.

## [0.24.134] - 2026-10-03

- **Approvals say what and where.** "Type on en.wikipedia.org?" instead of
  "Control the browser?", on the phone, in the console and in the terminal;
  the browser card says "Wants to type on en.wikipedia.org".
- A screenshot you ask for after a search is captioned with the page it
  shows, not "The page": the next step remembers which page Ghost was on.
- The message bar never suggests approving or denying something: that is
  always your deliberate choice, on the approval itself.
- Ghost asks Google Flights for one-way fares when you want one way.

## [0.24.133] - 2026-10-03

- **"Yes, run it" does what Ghost offered.** A short reply is read together
  with the offer it answers. Ghost used to pick its tools from your words
  alone, so "yes, run it" to "shall I search Google Flights?" got no browser,
  and Ghost asked to run a shell command through an outside page reader.
- **Flights, hotels and prices go to the browser.** "Cheapest one-way Bangkok
  to Shenzhen" names no website and used to get no browser at all. Ghost never
  reads a site through a shell command or someone else's reader service.
- A reminder you cancel or move is described in your time ("Sunday at
  6:00 PM"), not the server's: Ghost had told you the times disagreed.

## [0.24.132] - 2026-10-03

- **Follow-ups keep the browser.** "Send me a screenshot of that results
  page" right after a search got "I can't take screenshots": tools are picked
  from the words of each message, and that one names no site. A conversation
  that was just browsing now keeps its browser.
- **Ghost asks with a button, not in words.** When the next browser step needs
  your OK, Ghost tries it so your screen shows the approval; it no longer
  stops to ask in a sentence that leaves you nothing to tap.
- On the phone, a browser approval offers "Allow for this task" first, like
  the terminal: a search is several steps, and "once" covered one keystroke.
- The browser card's trail names the site ("Typed into en.wikipedia.org").

## [0.24.131] - 2026-10-03

- **Approving a browser step works.** Tapping Allow on "Ghost wants to type
  into the page" used to end in "the browser session expired": the turn that
  asked was recorded as finished, and finishing it closed the very browser
  your approval was meant to continue. The turn now waits for you, and the
  approved step picks up where Ghost stopped, on the phone and in the terminal.
- **Ghost's browser sits in the conversation, under your request.** While it
  works you see the page, the step it is on ("Opening news.ycombinator.com")
  and what it has done so far; approvals appear right on that card. When it
  is done, it settles into one line, "Browsed news.ycombinator.com", with the
  last picture a tap away. It no longer says "working" under a finished
  answer, flips to "couldn't finish" when one step misses, or shows an idle
  computer card that never goes away.
- **Fewer asks.** Ghost opens search and results pages by their address
  instead of typing into search boxes, so looking something up no longer
  stops for your OK.
- **A screenshot you ask for arrives as the picture**, captioned with the page
  and time, full screen on a tap. Screenshots Ghost takes to look at a page
  for itself no longer fill the conversation with "Browser screenshot" files.
- Every tool says what it is doing in words ("Checking your calendar…",
  "Checking the flight…") instead of "Working on it…".
- In the terminal, an approval you left unanswered no longer reopens after
  every later message, and Ghost's words above an approval are no longer cut
  off mid-sentence.

## [0.24.130] - 2026-10-03

- **A browser wait that runs out of time is no longer treated as a hung
  browser.** Ghost's own safety timeout reworded it as "timed out after 1m30s"
  and tore the browser session down, even for a 20-second wait Ghost chose
  itself. Only a step that really hits its own limit gets that treatment now.
- **On the phone, the live browser card stays.** It disappeared whenever you
  left the app and came back, because "couldn't reach the Pod for a moment"
  was read as "the browser is gone". It now asks the Pod what is running when
  you come back. Watching shows the page's picture only, never its raw
  accessibility text.

## [0.24.129] - 2026-10-03

- **No more false "My browser got stuck".** When Ghost waited for a page to
  show something and it didn't in time, Ghost treated the browser as broken:
  it threw away the page and told you so, every time, even mid-search. A wait
  that times out is now just that, and any other slow step first checks that
  the browser is alive. The "stuck" card appears only for a browser that
  really is stuck, and at most once every five minutes.
- On the phone, a message Ghost has already started answering no longer stays
  "Waiting to send", and finished browser cards and repeated notices no longer
  pile up under your replies.

## [0.24.128] - 2026-10-03

- **Searches and page reads show in Activity.** Only failures used to appear,
  so a turn that worked looked like one that broke. Looking things up on the
  web and reaching outside services now show as steps ("Searched the web").
- **Your Pod screen reads in words.** "Needs attention" says "Disk pressure"
  and "Skills", not `disk_pressure`, and the internal memory model
  (nomic-embed-text) is no longer listed as one you could use.

## [0.24.127] - 2026-10-03

- **The phone can see the browser Ghost is using.** Your phone's live
  connection was being refused whenever it reached the Pod by a name the Pod
  did not recognise as its own, so nothing live arrived: no browser card, no
  live reminders. A paired phone is now accepted by its device credentials.
- **Browser steps appear in Activity.** "Opened www.google.com" now shows when
  a browser step works, and a failed step says why in plain words, instead of a
  lone "Browser step didn't finish". The live browser card now appears when
  the browser starts, not after the first page finishes, and the status line
  says "Using the browser".
- **Models live in each provider's Configure sheet**, on the phone and in the
  console, with the list asked of the provider and a Use button per model.
- **`ghost status` no longer reports false errors** about AI and tools that
  only the running Ghost can check.

## [0.24.126] - 2026-10-03

- **Fine grain behind every screen.** A light film grain now sits over the
  black and the aurora, in the console and on the phone. The old console grain
  was invisible on black; this one is subtle but visible.

## [0.24.125] - 2026-10-03

- **Activity reads like sentences, in the console too.** A failed step used to
  show the tool's raw error ("query is required", a network trace) as its
  summary. Ghost now says what went wrong in plain words ("It took too long to
  answer", "The service refused access") and keeps the technical text behind a
  Details link. The same thing happening several times in a row is one line
  with a count, on the Activity page, on Home, and on the phone.

## [0.24.124] - 2026-10-03

- **The phone sees the models each provider really serves.** The list on the
  Intelligence screen used to be a fixed one built into Ghost. It now asks each
  connected provider for its own list, like the console does, and says where
  the answer came from: live from the provider, or Ghost's built-in list with
  the reason it could not reach it. This is what lets you pick a model after
  adding a key.

## [0.24.123] - 2026-10-03

- **Ghost can show an answer as a card.** A weather glance, a flight in
  progress, a plan for the day, a few options side by side: when a card says it
  better than a paragraph, Ghost presents one on your phone. A card is built
  only from a fixed set of blocks (a big number, facts, a list, a timeline, a
  progress bar, a note, a code snippet), and every field is checked before it is
  sent: nothing in a card can run, open a link or style itself. It can offer up
  to three choices, and a choice only ever sends a reply to Ghost, as if you had
  typed it, or puts the card away. Surfaces that cannot draw cards (the
  terminal, a text channel) get the same answer as plain text.
- **Cards stay where they were shown.** They are saved, so a conversation reads
  the same after a restart, and a card you have answered puts itself away on
  every device.
- New: `POST /v1/cards/resolve` records what you chose on a card.
- Console and terminal are unchanged.

## [0.24.122] - 2026-10-02

- **The menu button lines up with the page.** On a phone, the menu icon sat
  about 28px to the right of the page title and cards. Its lines now start on
  the same left edge as the content, and icon-only buttons are squares
  everywhere in the console.
- **No dark background when you hover an app or a channel.** Rows in Apps and
  Channels stay still under the mouse, like Approvals and Intelligence do; the
  buttons on them are unchanged.

## [0.24.121] - 2026-10-02

- **You can dismiss notices in "Needs you" on Home.** Each setup or health
  notice has a small dismiss button, and "Dismiss notices" clears them
  together; a toast offers Undo. Decisions Ghost is waiting on (approvals) are
  never dismissed, because you answer them, and a failure cannot be dismissed
  either: you can silence a nag but not an outage. A dismissed notice comes
  back if it changes, for example if a warning gets worse.
- **The status line stays honest.** If every warning behind "Ghost is up. A
  few things could use a look." is dismissed, it reads "Ghost is up. 3 notices
  dismissed." with a calm light instead of the warning one, and "Show" in the
  card brings them back. Dismissals are remembered in this browser.
- Toasts are dark glass like the rest of the console and can carry an action.

## [0.24.120] - 2026-10-02

- **A new look, shared by the phone, the web console and the terminal.** Pure
  black with one aurora of light (amber, magenta, violet and electric blue)
  fading out below the middle of the screen, dark glass surfaces with hairline
  edges, Inter for the interface and Instrument Serif for titles. The console
  has one dark theme; there is no light theme any more.
- **The primary button glows.** Everything you act on is a pill: the action is
  a glowing indigo, the alternative is dark glass, and danger stays soft red
  until you press it.
- **The terminal welcome is the Ghost mark**, an amber light beside the name in
  the aurora gradient, in place of the emoji banner.
- **The recovery page** uses the same palette.
- Console pages work better on a phone: no grey flash on tap, 16px fields so
  the page does not zoom, and room for the notch and home bar.

## [0.24.119] - 2026-10-02

- **The empty message box now suggests what you are likely to say next.** After
  Ghost speaks, it works out the most natural reply from the conversation as it
  stands and offers it where the placeholder used to be. If Ghost made an
  offer ("Want me to move it, or add a nudge?") the suggestion is the answer
  ("Yes, move it"); after a reminders list it is "Move the first one"; after a
  weather report, "What about tomorrow?". Otherwise a small background model
  call writes one short line, checked before it is shown, and only when Ghost
  is not busy with you. In the terminal, Tab or the right arrow takes it; on
  the phone, a "Use" button does. Taking a suggestion only fills the box;
  nothing is ever sent for you. With no suggestion, the box keeps its line for
  the time of day.
- Set `GHOST_SUGGEST=rules` to use only the instant rules (no model call), or
  `GHOST_SUGGEST=off` to turn suggestions off.

## [0.24.118] - 2026-10-02

- **The sign-in and setup screens keep the living background.** They used to
  paint a solid colour over it, so the light showed while the page loaded and
  then disappeared when the screen drew.
- **Activity is a tree.** Each day is a branch with a count, and what Ghost did
  that day hangs from it: a coloured node for the outcome, the time, what
  happened, and why. Days fold away with their caret.
- **The phone drawer is solid.** The page no longer shows through it.
- **The logo's status light sits on the rim of the mark again.** A style meant
  for the other status dots had pulled it inside the circle.
- **Routines no longer say everything twice.** When a routine's title and its
  description are the same words, they appear once; when the title was clipped,
  the full wording becomes the title.

## [0.24.117] - 2026-10-02

- **The terminal looks like the rest of Ghost.** Its colours now match the
  console and the app (warm ink, soft indigo, ember for Ghost), the faintest
  text is readable instead of nearly invisible, and the empty message box says
  something: a short line that follows the time of day and changes daily, in
  the same words the phone uses.
- **Console polish.** Rows of buttons wrap instead of running past their card
  on a phone, and form fields keep a faint edge on the dark background.

## [0.24.116] - 2026-10-02

- **The console has a new look, and now matches the phone app.** Warm paper by
  day and warm midnight by night (the same two worlds as the app), a serif
  voice for titles and big numbers, and a background of slow-drifting light
  with a touch of film grain. The typefaces Ghost shipped with were never
  actually loaded before; now they are. Pages arrive in order, buttons and
  tiles respond under your finger, and everything that moves stops moving if
  your device asks for reduced motion.
- **Ghost's presence is visible.** On Home and on the first setup screen, a
  soft living light breathes slowly when all is well and quickens when Ghost is
  waiting on you.
- **Files is a gallery.** Photos show as themselves (small copies made on the
  device, so a page of photos stays quick), other files get a tile that says
  what they are, and you can filter by photos, documents and other. Open one to
  read it: photos at full size, documents and text as readable pages, with
  arrow keys to move between files, plus Download and Delete.
- **Calmer danger buttons.** Delete, Deny and Stop are soft until you press
  them, and no longer shout at you from every row. Diagnostics and approval
  rows line up properly on a phone.

## [0.24.115] - 2026-10-02

- **You can correct a memory from the console.** Each item on the Memory page
  now has Correct next to Why? and Forget: type what is true now and Ghost
  replaces the belief, keeping the old version in its history and your words as
  the receipt.
- **Forgetting from the console now takes effect straight away.** It used to
  change the saved file while the running Ghost kept believing the forgotten
  fact until its next restart. Forget and Correct now go through the live
  Ghost.
- **Memory buttons are easier to see and tap**, full width on a phone.
- **Activity shows each outcome beside its item** instead of at the far edge of
  a wide screen.

## [0.24.114] - 2026-10-02

- **The console, reviewed at phone and desktop size.** Every page was checked at
  both widths; none overflowed or errored. What changed:
  - **Abilities has a search box**, so finding one of the seventy-odd things
    Ghost can do no longer means scrolling five screens, and Weather, air
    quality, currency, crypto and nearby places are listed with the everyday
    abilities instead of under "Custom and newly installed".
  - **Embedding models are no longer offered as chat models** under Local
    models; picking one could not have worked.
  - **"At a glance" stays two columns on a phone** instead of four tall cards.
  - **Setup says where to talk to Ghost.** The console is for looking after
    Ghost, not for chatting; the phone app and the terminal are where you talk
    to it. The setup screens used to say you could talk to it on that page.

## [0.24.113] - 2026-10-02

- **Setup page polish, checked in a browser.** The numbered and bulleted lists
  line up, the link to get an AI key looks like a link, the phone step and the
  final screen no longer both say "ready", and the cost note no longer promises
  a figure.

## [0.24.112] - 2026-10-02

- **The reminders list is shorter.** It shows the latest three completed
  reminders and says how many more fired earlier, instead of ten lines of
  history under what is still to come, and no longer cuts a long reminder
  title in the middle of a word.
- **When Ghost lists what it knows about you, it asks you to correct it.**

## [0.24.111] - 2026-10-02

- **Ghost can correct what it remembers, and asks first.** Say "the trip is on
  the 16th now, forget the 26th" and Ghost finds the belief, shows what it was
  and what it would become, and changes it once you have said so, keeping the
  old value in its history. If something you say only seems to disagree with
  what you told it, Ghost keeps what you stated, holds the new idea aside, and
  asks which is right. Asking "what do you know about my trip?" now also points
  out when two things it knows disagree.
- **Quick answers can suggest a next step.** The reminders list, the weather
  report and a flight status can now end with one short offer drawn from the
  answer itself (a reminder that failed to send, rain in the report, a flight
  nobody is watching), at most once every six hours.
- **Setup is written for people who have never set up an assistant.** It
  explains what an AI key is and links straight to where each provider creates
  one, asks which city you are in (so the weather and reminder times work from
  your first message), and ends with things to try instead of a claim that your
  phone is already connected.

## [0.24.110] - 2026-10-02

- **Ghost can give a forecast.** Ask about tomorrow, the weekend or a named day
  up to 16 days ahead and Ghost reads the daily high, low and chance of rain
  instead of saying it has no forecasts. Days beyond about a week are marked as
  a rough guide.
- **"Stop tracking my expenses" no longer ends your flight watches.** A stop
  command with no flight number used to close every watch. It now closes
  everything only for a plain "stop watching"; anything else goes to the model.
  Starting a watch likewise needs a command that opens the sentence and
  something to watch, so "I'm going to watch the game" is just a sentence.
- **Ghost looks before it explains an earlier mistake.** It now searches your
  past conversation first and describes only what it finds, instead of guessing.
- **A date with a 24-hour time keeps its time.** "2026-10-04 19:00" was stored
  as 09:00.
- **The weather reading's time is correct and says UTC.**

## [0.24.109] - 2026-10-02

- **A reminder uses the time from the sentence that asks for it.** "Forget the
  October 26 dates, remind me to book the flights by Sunday evening" used to be
  stored for October 26, the date you were dropping. Ghost now reads the time
  from the sentence that makes the request.
- **A question about the weather is no longer answered with the weather.** "What
  did you do wrong when I asked about the weather in Nairobi" got a Nairobi
  forecast. The weather and air-quality quick answer now fires only when the
  message is a weather request and nothing else.
- **Weather times say UTC.** The "observed" time was in UTC but looked local;
  it now says so.
- **Ghost knows about quiet hours.** It said there was no such setting. Quiet
  hours hold back what Ghost raises by itself; reminders you set still fire at
  their time.

## [0.24.108] - 2026-10-02

- **Ghost answers what you asked, not the keyword in it.** Quick answers for
  reminders, routines, jobs, health, watches and similar used to fire whenever
  a phrase appeared anywhere in your message, so "are there any reminders I
  forgot to look at?" got the full reminder list. A quick answer now fires only
  when it covers your whole message; anything with more in it goes to the
  model, which reads the question. Calendar and home-control "not connected"
  replies follow the same rule, and a year after "in" is no longer read as a
  flight number.

## [0.24.107] - 2026-10-02

- **Weather and air quality no longer misread your city.** Asking for "the
  weather in nairobi and its aqi" used to be taken as a place called "nairobi
  and its aqi", and "aqi of nairobi" was taken as naming no place at all, so
  Ghost asked "Which city should I check?" again and again. Ghost now asks for a
  city only when you really named none, stops reading the place at words like
  "and", and hands anything it cannot parse to the model, which reads the whole
  conversation, instead of refusing.
- **Ghost offers a sensible next step.** After an answer it may suggest one
  thing it can actually do about what you are discussing, such as the weather
  at a destination or a reminder before a trip. It only offers; it does not act
  until you say yes.

## [0.24.106] - 2026-10-02

- **`ghost update` now installs the release itself.** It used to look up the
  newest release and then rebuild whatever source was already on the Pod, so an
  update could report a new version while running the old code, and needed a
  `git pull` first. Each release now ships ready-made programs for Linux (arm64
  and amd64) and a signed list of their checksums. Ghost checks the signature
  against a key built into it, and each program against the list, before
  anything is replaced; an update that fails either check is refused with
  nothing changed. The recovery snapshot is taken,
  and the console and the service files are updated from the same release. A
  machine with no ready-made program gets the release's tagged source built
  instead.
- **If you are updating from 0.24.105 or earlier,** the first update still runs the
  old updater. After it, run `ghost update --force` once so the web console and
  service files are brought in line; every update after that is the new kind.

## [0.24.105] - 2026-10-01

- **Fixes `ghost pair` and the terminal's approvals on installs where Ghost runs
  as a system service.** 0.24.104 kept its new local key readable only by root,
  which locked out your own terminal on those installs. The key now belongs to
  whoever owns the config folder. Update and nothing else is needed.

## [0.24.104] - 2026-10-01

- **Approving, pairing and changing policy now need more than being on the Pod.**
  A command Ghost runs in its shell can reach the gateway on the Pod itself, and
  the gateway used to trust that. Approvals and grants, the permission mode,
  pairing, connecting apps and tool servers, skills, providers, reset, system
  update and live takeover now also need a local key kept where Ghost's commands
  cannot read it. The console, terminal and `ghost pair` use it automatically;
  nothing for you to do.
- **Skills are no longer fetched from a default address.** The registry Ghost
  looked at does not exist. Set `GHOST_SKILLS_REGISTRY` to a `skills.json`
  address if you keep one.
- **Groundwork for Ghost Connect**, the optional hosted way to reach your Pod
  away from home: sealed requests through a relay, and linking from the console.
  It is not offered yet, and nothing changes unless you link a Pod.

## [0.24.103] - 2026-10-01

- **Send several files at once and Ghost answers what you ask about them.** The
  app now takes up to ten files or photos in one message. Ghost is given a list
  of everything you sent, in order, photos included, so "compare these", "the
  second one" and "summarise them all" work. If you send files with no words, it
  says in a line what each one is and asks what you want done.

## [0.24.102] - 2026-10-01

- **The phone can now connect Google Calendar, Gmail, Outlook and Spotify.** The
  app only ever told you to "use the web console". The Pod now offers the same
  guided sign-in to the app as to the console: set up your own app with the
  provider once, sign in, and the app finishes the connection. Disconnecting
  one of these from the phone works too.

## [0.24.101] - 2026-10-01

- **You can now connect Google Calendar, Gmail, Outlook and Spotify.** They all
  failed with "isn't set up on this Ghost yet", and there was no way to set them
  up short of editing the server's environment. Connect now walks you through
  registering a free app with the provider once (the steps are on screen), stores
  its ID and secret sealed, and finishes the sign-in when you paste back the
  address your browser ended on. Calendar no longer depends on a separate helper
  that could not start. The buttons now say Connect and Disconnect.
- **Modals and buttons redesigned.** Consistent button sizes, a working state,
  clear focus, and a red button only where something can't be undone. Dialogs
  close with Escape or the X, keep focus inside, hand it back, and become a
  bottom sheet on a phone.
- **Help and About rewritten** to answer what people ask first, including the
  forgotten-password steps that Help still described the old way.
- **One description of Ghost everywhere.** The READMEs, the GitHub descriptions,
  the console and the app now say the same thing, and the README is a front door
  with the reference material moved into `docs/` (`GUIDE.md`, `COMMANDS.md`,
  `DEVELOPMENT.md`).
- Error messages that told Ghost to send you to "Ghost settings under
  Integrations" now name the real places: Apps in the web console, Connected
  apps in the app.

## [0.24.100] - 2026-10-01

- Ghost builds for Windows again. It had stopped compiling there.

## [0.24.99] - 2026-10-01

- **Ghost tells you when your Pod is too small, before it gets slow.** It now
  speaks at the first sign of low storage or memory, not only when critical, and
  says what to do. If memory stays short for half an hour it tells you the Pod is
  too small for how you use it. It also says so once if the Pod has less than
  4 GB of memory or a drive under 64 GB, and if the system runs from a memory
  card, which wears out.
- **Power cuts are noticed.** Ghost tells you when it restarted after being cut
  off instead of stopped, and that its memory checked out.
- New page: what a Pod needs (`docs/HARDWARE.md`).

## [0.24.98] - 2026-10-01

- **You can do the human steps.** When a site asks for something only a person
  can do (a "Verify you are human" box, a CAPTCHA, a code sent to your phone),
  Ghost stops and says so, and never tries to get past it. In the app, tap
  "Take over and steer" on the browser card: you see the real page, tap to
  click, type, and tap Done. Ghost carries on from where you leave it. Ghost's
  browser is also shown live while you watch, not as still pictures.
- **Only you can steer.** The Pod drops any click or keystroke sent to its
  browser unless you hold the takeover of that browser.
- Groundwork for a private relay: the sealed envelope that will make relayed
  traffic unreadable to the relay is built and tested. It is not switched on yet.
- Removed the unused Docker files and a leftover folder.

## [0.24.97] - 2026-10-01

- **Ghost works in your language.** Recall already crossed languages (tell it
  something in English and ask in Swahili, French, Thai, Chinese or Spanish and
  it answers correctly, in that language). Now confirming with "sí", "oui",
  "ndiyo", "sawa", "好的", "ตกลง" or "はい" counts as a yes, and "non", "hapana",
  "不要", "ไม่" as a no. A reminder asked for in another language is scheduled
  correctly, with the time checked against the number you wrote.
- **You can cancel a reminder or a recurring routine by talking.** "Cancel the
  vitamins reminder" stops that one, says which it stopped, and if two things
  match it asks which instead of guessing.
- Weather questions in languages where one word means both weather and air now
  give the conditions and mention the air quality, not only the air.
- Weekday names read "every Monday", not "every monday".

## [0.24.96] - 2026-10-01

- **Recurring reminders and routines understand how people talk.** "Remind me
  every weekday at 8am to take my vitamins" used to be misread (Ghost asked
  "what should happen?" about a request that already said). It now understands
  weekdays, weekends, several days ("Mondays and Thursdays"), "every morning",
  "8am every day", "every other day", "every 3 days", "hourly" and more, and
  says the time back, including one it assumed. The confirmation is addressed to
  you ("take your vitamins"), a question waiting for an answer no longer swallows
  an unrelated new request, and "give me a brief" counts as an ask.
- **A recurring reminder is delivered like a one-off one.** No AI call: it
  arrives on time even if the model provider is down, and says so if the Pod was
  off. A routine's result carries a "Routine" label.
- **Clearer prompts.** Ghost's core instructions were rewritten: they had
  corrupted text and a stray template marker, and "be professional" fought the
  warm voice everywhere else. New rules: say what it can't do without being asked,
  know its own situation, handle rudeness and distress well, and take corrections.
- **A cleaner build.** About thirty unused functions and one unused file were
  removed after checking each against every place it could be used.
- The app labels reminders, notices, alerts and routine results and shows them the
  moment they happen (it previously only saw them after a reload), true-black dark
  mode and a new palette, and a smoother approval card.

## [0.24.95] - 2026-10-01

- **Updates and backups can no longer be stopped by a private file.** Ghost's
  service account could leave a screenshot folder that only it could read, and
  the safety backup before an update then (correctly) refused to continue.
  Files and folders Ghost creates now belong to the workspace's owner, and Ghost
  repairs any it finds.

## [0.24.94] - 2026-10-01

- **Your terminal no longer stops working after you save a login.** When Ghost
  saved a credential from the app or console it handed the settings file to the
  system account, and your own terminal then failed with "permission denied".
  Saves now keep the file's owner.
- **Follow a reply from any device.** Send a message from the terminal and open
  the app: the question is there and the answer arrives as it is written, and
  opening the app halfway through shows what has been written so far. The app
  also now sends its credentials on its live connection; without them a phone on
  your home network never received anything live.
- **Reminders and notices look like reminders and notices.** They show up in an
  open terminal the moment they happen, with their own label, and they stay
  marked in your history. The terminal's "working" indicator is a calm
  five-dot glide.
- **Connecting an app now checks it.** A made-up GitHub key or a Home Assistant
  address of "not a url" used to say "connected". Keys are tried against the
  service first; a refusal is explained and nothing is saved, and a service that
  can't be reached is saved with an honest note.
- **Tool servers (MCP) without a terminal.** Add one by address and key on the
  console's Apps page. The key is sealed on the Pod, the connection is tested
  before anything is saved, and the tools appear at once. Ghost now speaks
  the current "streamable HTTP" style as well as the older one.
- **Sign in to a website you saved.** Saved logins actually work now (the
  browser rejected names with a dot, so none ever did), Ghost finds the login
  page from the front page, and if the page asks for a human check it says so
  plainly instead of waiting.
- **Forgot the console password? No terminal needed.** Get a one-time code in
  the app under Your Pod, or leave a file on the SD card, and set a new password
  on the sign-in page.
- **Files.** Ghost can delete and move files in its own workspace, asking before
  it deletes. It can never delete or move its own default files and folders,
  through the tools or the shell. Screenshots and downloads live in the
  workspace, not in temporary folders.
- **A new look.** True black in dark mode and a cool neutral palette throughout
  the terminal and the console. The Abilities page is at `#abilities`, and Activity
  and Abilities lose their divider lines.
- **Fixes.** A message with several asks is no longer answered with one of them;
  a stale suggestion can no longer take over an "allow once" meant for a browser
  approval; approvals for browser control default to "this task"; wide tables in
  the terminal show as readable cards instead of breaking; output order can no
  longer scramble when a message is queued; Ghost keeps working past 20 steps
  and uses the browser before saying it couldn't find something online.

## [0.24.93] - 2026-09-30

- **Approvals and permissions fit on a phone.** In the console, each approval is
  now a warm card like the ones on Home: the question, what it means, then the
  four choices in a two-by-two grid. Under System, Permissions no longer sits
  as a card inside a card, so nothing is squeezed or runs off the screen.

## [0.24.92] - 2026-09-30

- **Ghost is not an "assistant."** It says so about itself, and the word is
  gone from the banner, the service descriptions, the README, the skills and
  every prompt that shapes how Ghost talks about itself. It is a personal AI
  that lives on your machine and keeps learning you.

## [0.24.91] - 2026-09-30

- **Backups now back up the right Ghost, and can be restored.** `sudo ghost
  state backup` used to save an empty workspace and report success. It now
  targets the running Ghost, and restoring a snapshot no longer asks for a
  passphrase nobody has. `ghost state backup` also says plainly that the backup
  lives on the Pod and that the Pod's key is needed to open it. Recovery steps
  are in the user guide.
- **If Ghost was off, it says what it skipped.** A routine that came due while
  the Pod was off is no longer replayed hours late (a morning brief at 9pm);
  Ghost skips it, tells you, and carries on at the next time. Reminders still
  arrive late with a note.
- **Approval cards say what you are approving.** Reading your inbox no longer
  asks "Send this email?", scheduling says so, and the internal label under the
  title is now a sentence.
- Ghost re-reads the clock each turn, so a change of timezone mid-conversation
  no longer leaves it repeating an earlier time.

## [0.24.90] - 2026-09-30

- **Ghost tells you what you need to know, unasked.** If its balance runs out
  or a key is rejected while it is working in the background, if the Pod is
  overheating or nearly out of storage, if the room gets unusually hot, cold,
  damp or dry, or if a newer Ghost is out, it says so once, and pushes it to
  your phone even when the app is closed.
- **It notices when someone is poking at it.** Repeated attempts to reach Ghost
  without valid credentials, wrong pairing codes, or a sweep across your network
  are reported with where they came from. Everything is turned away first.
- **It can feel the room.** With a temperature and humidity sensor attached,
  "what's the temperature in here?" gets a real reading. With none, Ghost says
  so and reports the Pod's own temperature instead of inventing one.
- **Messaging channels are closed until you open them.** Telegram, Discord,
  Slack, WhatsApp and email used to answer anyone who found the bot. They now
  answer only the people listed under Channels in the console, and Ghost tells
  you when a stranger writes. **If a channel goes quiet after this update, add
  your own ID there.**
- **A relay can no longer act as you.** Traffic replayed by the relay is treated
  like any other remote request and needs the device's credentials.
- **Setting up a Pod with no screen.** The setup code is also written to the SD
  card's boot partition (and a factory can preset one), so it can be read from
  any computer. Finishing setup removes it.
- Memories are filed under the right kind and domain more reliably, and a
  memory is no longer lost when two are saved at the same moment.

## [0.24.89] - 2026-09-30

- **Ghost understands before it files.** Memories used to be sorted into a
  handful of buckets and mostly landed under "Other". Ghost now works out who a
  statement is about (you, Jas, your dog), whether it will last or is tied to a
  date, whether it is private, and what specifically it is, and keeps your
  own words as the receipt. People, dates and events, health, things you own,
  skills, views and habits each have a place, and different facts about
  different things no longer overwrite each other.
- Health, money and similar memories are marked private, and a dated memory
  (a trip, an appointment) retires itself after the day.

## [0.24.88] - 2026-09-30

- **See what Ghost knows, right in the terminal.** `/memory` lists what is
  actually stored (and `/memory forget 2` removes one), `/activity` shows what
  Ghost did and why, `/devices` shows paired phones and can remove one. They
  read the real records, not a model's recollection.
- **Update from your phone.** Your Pod screen shows when a newer release is out
  and can install it after you confirm.
- **Late reminders say so.** If the Pod was off when a reminder came due, it
  arrives with "This was due at 2:39 PM; I was offline then."
- **Fewer wrong memories.** A frustrated aside is no longer stored as your home
  town, and a shopping errand or a permission claim is no longer kept as a fact
  about you.

## [0.24.87] - 2026-09-30

- **Reminders actually reach you.** A reminder that came due was worked out and
  then filed in a private corner of Ghost that no screen looks at, so nothing
  ever appeared. Reminders, and anything Ghost does on a timer, now arrive in your
  conversation, live and in history, and as a notification when the app is
  closed. A reminder no longer needs the AI to be reachable: it says
  "Reminder: stretch." on time even if your model provider is down.

## [0.24.84] - 2026-09-30

- **Ghost can reach your phone when the app is closed.** Reminders, questions,
  approvals and finished tasks now arrive as push notifications, worded the same
  fixed way every time and never carrying what was said. They wait for quiet
  hours if it is only an update, and never double up while the app is open.
- **Closed phones are noticed.** Ghost now hears when a phone's connection drops
  instead of counting it as connected, which would have silenced notifications.
- **Screenshots come back to you.** Ask Ghost for a screenshot of the browser and
  it appears as an image in the conversation and in Files, instead of a file name
  you could not open.

## [0.24.83] - 2026-09-30

- **Shops let Ghost in.** Ghost's browser announced itself as a headless robot,
  and Amazon answered with an error page instead of results. It now presents
  as an ordinary Chrome, so searching and reading a shop's results works.

## [0.24.82] - 2026-09-30

- **The browser works.** Every browser action was dying instantly because
  Ghost's service had no writable place for the browser to keep its files.
  Ghost can now open pages, read them, fill forms and click.
- **Ask for things the way you would say them.** "Search Amazon for a keyboard"
  used to get "I can't browse Amazon": Ghost only reached for the browser when a
  message contained a web address or the word "website". It now also does for
  well-known shops and sites and for shopping, booking and logging in.
- **It answers the whole question.** A message with two asks ("disk space and
  uptime, run the commands") got only the first half from a built-in shortcut.
  Compound or work-requesting messages now go to Ghost itself, and the quick
  health answer says how long the machine has been up.

## [0.24.81] - 2026-09-30

- **Lists and paragraphs stay apart.** When the model sent a line break as a
  piece of its own, Ghost threw it away, so bullets ran together on one line
  and a closing sentence stuck to the last bullet. It happened in the terminal
  and on the phone. Line breaks now arrive as written.
- **Open and preview the files you send.** In the app, tap a file under
  Files to see a photo, the text, or what Ghost reads out of a PDF, Word
  document or spreadsheet, and open the original in your phone's own viewer.
- **`ghost update` stops repeating itself.** It used to replay every past
  release note each time; now it shows what is new once, and clears an old copy
  of Ghost that kept your terminal on the previous version.

## [0.24.80] - 2026-09-30

- **Send Ghost files, and they work.** Photos and documents you send are kept on
  your Pod with their real names, identified by what they are, and can be
  listed and deleted from the app, the console and the terminal
  (`/attach`, `/files`). Ghost reads PDFs, Word documents, spreadsheets, CSV
  and text, and says plainly when it can't open something.
- **Photos from the phone now arrive.** They were being rejected.

## [0.24.78] - 2026-09-30

- **Approve "for this task".** A new answer between once and always: Ghost may
  repeat the action while it keeps working, and it ends after ten quiet
  minutes or an hour.
- **Updates from the console work.** The update button and Ghost's own update
  tool now run as their own background job, which can actually install.
- **Safer by default.** Ghost's image tool is confined to your workspace, and
  Home Assistant control needs an explicit grant.

## [0.24.77] - 2026-09-30

- **A blank Pod can be set up.** The setup screen now asks what Ghost should
  think with before it claims the Pod, checks a cloud key with the provider, and
  leaves nothing half-done if something is refused.
- **Secrets never reach the model.** Anything Ghost reads or runs is scrubbed of
  your saved passwords and keys before the model sees it.

## [0.24.59] - 2026-09-27

- **The last "One caveat:" is gone.** The rule was in the prompt, but the model
  still opened an answer with a labelled note about which pages it had looked
  at on roughly one news answer in fifteen. The decision now lives where it can
  be made properly: the runtime knows whether anything failed during the turn,
  so it drops a trailing note about the *shape* of the sources and leaves alone
  anything that names what went wrong, a conflict between sources, an outcome
  it could not confirm, or a single source.
- **Ghost's own evaluator stopped grading honesty as a lie.** Under pressure
  it read an honest refusal — "this is certainly not proof that your passwords
  were sent" — as a claim that Ghost had sent them, and failed the run for a
  success that never happened. It was doing the same thing to an explanation of
  the approval flow: "you approve it there, once, and it's done" was graded as
  something already finished. Both are fixed, and the controls that prove the
  evaluator cannot go permissive are part of the suite.

## [0.24.58] - 2026-09-27

- **Ghost stopped explaining its own plumbing.** Asking what was happening
  somewhere no longer ends with a note about which pages loaded. The answer
  leads, sources are named where they belong, and the retrieval mechanics stay
  where they belong — inside Ghost.
- **It only mentions a limitation when it changes the answer.** If you asked
  for a specific publication and Ghost couldn't read it, it says so. If sources
  disagree, it says that. If what it has is thin, it says "early reports
  indicate" and moves on. A page that wouldn't parse while other outlets
  confirm the same facts is Ghost's business, not yours.
- **No more "Caveat:" blocks.** The guidance that used to tell Ghost to flag
  thin sourcing and pair it with an offer has been replaced with the rule
  itself: be honest, attribute naturally, and don't narrate how you got
  anything.
- **Answers end when they end.** The automatic "Want me to…?" after an
  information answer is gone; it survives only when something actually failed
  and a retry is a real next step.
- **No throat-clearing.** "I don't have a live news feed wired up for this, so
  let me search" used to arrive as if it were the answer. Ghost now holds back
  the sentence a model says before it goes and gets something, and shows you
  the answer itself.
- **Activity shows provenance, not apologies.** Entries read like "Searched the
  web: …" with the sources listed, instead of commentary on what Ghost could
  not reach.
- Reading a page goes through the page reader, not a shell command, so a
  routine lookup no longer turns into an approval prompt.
- Internally, Ghost still records everything about the shape of its evidence —
  what was read, how, and what stood behind it — for audit, activity and
  evaluation. It just no longer says it all out loud.

## [0.24.56] - 2026-09-26

- **Ghost stopped doing work it did not need.** A local memory lookup cost
  about three seconds and ran on every message, including "hey Ghost". Ghost
  now looks things up only when what you said actually depends on something
  you told it before; a greeting, a request, or a question about right now
  goes straight to the answer.
- **Questions Ghost already knows the answer to are answered instantly.**
  "What reminders do I have?", "what needs me?", "is my routine okay?", "how
  much disk space is left?", "which model are you using?", "what did you just
  do?" come from the records Ghost already keeps — no model call, no waiting.
  Every answer is drawn only from those records, so it cannot invent a name,
  a time or a count.
- **The prompt is a third smaller.** The behaviour and safety rules ship in
  full; the operational reference — the long per-tool manual, credentials
  setup, browser and channel details — is read on demand when a task needs it
  instead of being sent on every message.
- **Ghost tells you what it is doing.** While it is retrieving memory or
  waiting on the model, the app is told that, so you are not watching a still
  screen. It never says something is finished before it is.
- **Background work waits its turn.** Reminders, journaling and upkeep no
  longer compete with you while you are talking to Ghost, and the half-hourly
  heartbeat only runs the parts of its checklist that are actually due —
  instead of spending a large model call every 30 minutes to report that
  nothing needed doing.
- **Memory still settles, just not in your way.** Reading a message for things
  worth remembering now happens after you have your answer; the work is queued
  durably, retried, and recovered if Ghost restarts.
- Every turn now records how long it took, how long until the first word, and
  whether the token counts are measured or estimated — locally, with no
  telemetry leaving the device.

## [0.24.53] - 2026-09-26

- **Ghost keeps your promises.** Tell Ghost "I need to send Alex those
  photos Friday" in passing and it is no longer a sentence that dies in
  the transcript. Ghost records the promise — your own words, kept
  verbatim as the receipt — along with the day it resolves to. When that
  day arrives, or when an undated promise has been sitting long enough,
  Ghost offers exactly one thing: *"You said you'd send Alex those photos
  Friday and it's still open. Want me to send it?"*
- **It only records real promises.** "I might send those Friday", "maybe I
  should email the landlord", a question, a request to Ghost, or an actual
  "remind me to…" never become promises. When Ghost asks a model to help
  read a message, every field it returns — the quote, the person, the time
  — must appear in your own words or it is thrown away. Ghost never invents
  who you meant or when.
- **Ghost notices immediately, not half an hour later.** Meaningful change
  — a promise you just made, a routine that just failed, a task that just
  stopped — wakes the proactive engine straight away instead of waiting for
  the next reconciliation pass. It is a local, deterministic check with no
  model call, and repeated events collapse into one evaluation.
- **Asking for permission happens when you approve, not when Ghost
  suggests.** A suggestion no longer carries an authorization that quietly
  expires while you are away. Approving mints and records the permission
  decision at that moment, through the same broker as every other action, so
  a suggestion you get to late still works instead of failing with "sorry,
  that expired."
- **One suggestion, one card.** A proactive offer is now a single
  interaction on every surface: no duplicate approval card beside it, and
  the phone decides it by identity rather than by a token.
- **What actually happened, stated honestly.** Outcomes carry an evidence
  level — verified (read back from real state), acknowledged, dispatched,
  unavailable, or failed — and the sentence you read is derived from it. A
  promise is only marked kept when the work really succeeded, a failure
  leaves it open, and dismissing it never pretends it was done.
- **Talking is not doing.** When Ghost takes on a promise through a real
  turn, the promise is only marked kept if a mutating capability actually
  ran. A turn that asks you for the missing photo or address records the
  promise as *blocked* — still open, with what is missing written down —
  and never as done. It comes back to you later, this time saying what it
  needs.
- The Ideas screen lists the promises Ghost is holding, with the words you
  said, and lets you close one.

## [0.24.52] - 2026-09-26

- **Ghost notices things.** Ghost now watches the state it already
  keeps — reminders that never reached you, routines that keep failing,
  routines waiting on your approval, goals that went quiet, background
  tasks parked on a human — and turns what it finds into one concrete
  offer: *"Your reminder "send Alex the document" was due 2 hours ago
  and never reached you. Want me to run it now?"* Each one shows the
  exact rows behind it and why it matters now. Nothing is invented:
  if Ghost cannot point at the evidence, there is no suggestion.
- **Ghost asks, then does it, then proves it.** Approving a suggestion
  goes through the same Permission Broker as any other action; Ghost
  then runs it through the same capability path, checks the result
  against real state, and tells you what actually happened — never
  what it hoped happened. Approve, Later, and No thanks all work from
  chat, the phone card, and the Web console.
- **It will not nag.** One live suggestion per situation, a cooldown
  after you dismiss one, a backoff after a failure, and a hard stop
  when the situation changes: if your routine recovers or the reminder
  fires before you answer, the offer is withdrawn instead of executed.
  Quiet hours, a daily limit, per-category controls, and a master
  switch all live in `PROACTIVE_PREFERENCES.md` and are shown on the
  Home screen.
- **Nothing about this costs a model call.** Deciding whether to
  interrupt you is entirely local and deterministic. The model is only
  involved if you approve something that needs judgement — and even
  then, anything consequential still goes through the broker.
- The Ideas screen is now "what needs you": open suggestions with their
  evidence and one action each, followed by what Ghost recently handled.

## [0.24.51] - 2026-09-26

- Ghost no longer asks "Which city should I check?" when the answer is
  already in the conversation. Talking about Phuket and then asking
  about the weather "there" used to bounce straight back into that
  question, because the quick path that handles a weather ask never
  looked at what had just been said. A request that points back at the
  conversation now reaches Ghost with the conversation attached, so the
  place you just named is the place it uses. A plain "what's the
  weather?" with no place anywhere still asks once, as before.
- Section titles are no longer cut short. In a narrow window, every
  heading in a reply came back truncated behind an ellipsis with the
  rest of the line dropped — no way to expand it. Titles now wrap like
  the rest of the text, so nothing is lost.

## [0.24.50] - 2026-09-26

- Fixed Ghost answering with raw web page markup. Approving a read of
  a web page used to hand the fetched bytes straight back as Ghost's
  reply, so an owner who said yes got a wall of HTML and JSON-LD
  streamed and saved as something Ghost said — and never got the
  answer that was promised. The approved run's output now returns to
  the model, which replies in its own words; the raw bytes are the
  evidence for that turn, never the turn itself.
- The approval reply ("always allow") is no longer treated as
  something the owner said: it is not saved as a chat message and is
  not fed to memory, journaling, note capture, or the follow-up
  question tracker.
- The receipt shown when a resumed run cannot be written up is now
  bounded, so a long payload can never be pasted at the owner again.

## [0.24.49] - 2026-09-26

- `docs/` is now exactly the architecture chain and nothing else:
  this README plus the nine layer documents (User → Ghost →
  Intent/Reasoning → Capability → Permission Broker → Execution →
  Evidence → Canonical Event → Memory/Activity/Routines/Artifacts).
  Retired the side documents — agent CLI contract, Desk, golden run
  report, and Pod home — and the code comments that pointed at them
  now stand on their own.
- Removed `ghost eval release-notes`. Its only job was assembling
  `docs/VERIFICATION-<version>.md`, which no longer belongs in the
  docs directory; `verify`, `benchmark`, `golden`, and `replay` are
  unchanged.

## [0.24.48] - 2026-09-26

- Ghost's operating contract is now a complete system prompt.
  `GHOST.md` gained a per-family operating manual — status, web
  reading, live lookups, connected apps, memory, files, commands,
  routines, artifacts, delegation, clarifying, skills — each with
  when to reach for it, how to call it, and what to say on success
  or failure. Plus a dedicated skills section, channel-by-channel
  presentation rules (phone vs console vs CLI), reply-formatting
  guidance, and a "common failures to avoid" list drawn from the
  bugs that actually shipped: date-label leaks, "I can't run
  commands", success claimed without evidence, timezone mistaken
  for location, and permission theater.
- Persona and workspace notes deepened to match: `SOUL.md` now
  carries the values layer (truth, respect, warmth, play) and a
  voice card; `AGENTS.md` the skill, state-boundary, and
  reporting conventions for agentic work.
- Docs cleanup: retired evaluation and audit artifacts removed
  (demo evaluation, interaction audit, live eval, restore drill,
  tool-skill map, verification reports, security posture).

## [0.24.47] - 2026-09-26

- Date-stamp fix reaches the live chat. v0.24.46 stripped the
  label from saved replies, but on a streaming reply the internal
  dump filter still ate the label's opening "[" before the gate
  could recognize it — the stream showed "2026-09-2610:04] …" as a
  bare date fragment. The label gate now runs first and the filter
  sees only what survives, so the live transcript, the saved reply,
  and history reads all agree: no date prefixes, anywhere.

## [0.24.46] - 2026-09-26

- No more stray date stamps. When a model echoed the internal
  history label in a reply — with its characteristic dropped space
  ("[2026-09-2610:04] Approval needed…") — it slipped past the
  stripper and showed up as a visible date prefix on your chat.
  Both label shapes now strip at every output boundary: the live
  stream, the final reply, and history reads.

## [0.24.45] - 2026-09-26

- "Weather in my city" works instead of failing. The place
  extractor was capturing the phrase "my city" literally and handing
  it to a geocoder that could never resolve it — a plain weather read
  came back "That didn't complete. Nothing was changed." Here-phrases
  (my city / my location / my town / near me) now resolve through
  the location Ghost has on file, and a genuinely unknown place says
  so plainly: "I couldn't find a place called …"
- Weather reads speak read language. A failed lookup never claims
  something "was changed" — it says the weather service didn't come
  back with a reading, or that the place couldn't be found.
- Asking for a command offers the command tool. When your message
  explicitly asks Ghost to run something ("run df -h", "run the
  command uname -a", a bare "ls -la"), the command tool is offered
  for that turn. Visibility only — the permission broker still asks
  before anything runs, exactly as before.

## [0.24.44] - 2026-09-26

- The status tool actually reaches the model now. It was registered
  in 0.24.43 but the per-turn intent filter kept it off the offered
  list, so Ghost still said "that tool isn't available in this
  session". It joins the always-offered core set — status phrasings
  vary too much to keyword-gate, and a lost offer is a refused
  question.
- The persona now names `exec` for owner-commanded runs. "Attempt
  the call" wasn't enough without saying which call: hidden means
  unadvertised, not unavailable, so Ghost calls it and the approval
  gate does the rest.

## [0.24.43] - 2026-09-26

- Machine status and free disk space in one call. Ghost now has a
  read-only `system_status` check — temperature, memory, load,
  uptime, and df-style disk rows with real free space — available on
  every surface including subagents. Status questions never needed
  shell, and free space never needed df: it comes from statvfs
  directly.
- Asked to run a command? Ghost attempts it. When you explicitly ask
  for a command (or grant permission for one), the call is made and
  the runtime shows an approval — it runs on your yes. Ghost no
  longer refuses on principle while claiming the capability "isn't
  exposed"; surfaces that truly forbid a tool still say so plainly.
- Direct requests win over narration rules. When you ask for command
  output, a path, or a raw number on your own machine, you get it —
  the "no machinery" style rule is how Ghost narrates its own work,
  never a reason to refuse what you asked for.
- A timezone is not a location. The clock zone is labeled as time
  only in Ghost's context, and the persona rule now forbids
  inferring a city or country from a zone — no more "08:25 in
  Bangkok" from Asia/Bangkok.
- "Weather here / in my city / in my location" resolves from the
  location on file (asked once, remembered), and declarations like
  "my city is Bangkok" are stored as your location too.
- Website sign-ins offer Website logins. Asking Ghost to sign in to
  a site now points at the sealed-login flow (you seal it in
  Connected Apps; the password never passes through chat) instead
  of a flat refusal.

## [0.24.42] - 2026-09-26

- The location you give once now really sticks. Answering "bangkok" to
  "which city should I check?" remembers it, so further "what's the
  weather here" questions answer straight away. (The remember step sat
  on a path that resumed answers deliberately skip; it now lives where
  the answer is resolved, which every location reply passes through.)

## [0.24.41] - 2026-09-26

- "Here" is remembered. Answer "bangkok" once to "which city should I
  check?", and Ghost keeps it — the next "what's the weather here"
  answers straight away instead of asking again. (The first save
  silently did nothing before: the store only knew how to replace an
  existing location, not create one.)
- Weather shows the emoji you expect from wttr. wttr's JSON carries no
  emoji — only its text mode does — so Ghost maps the condition to the
  glyph itself, which works for every provider.

## [0.24.40] - 2026-09-26

- No more "allow once" that gets refused. Shell (`exec`) is hidden by
  default, but the tool list still offered it — so Ghost would ask you to
  approve a shell command from the phone, then the runtime refused it
  ("disabled for this channel/session"). Hidden tools are never offered
  now, a committed skill that needs one promotes it so it actually runs,
  and Ghost says plainly what it will use instead.
- Reading a website goes through web search, page fetch, or the browser —
  never a shell command.

## [0.24.38] - 2026-09-26

- Browser sign-ins now stick. Ghost keeps a per-context browser profile
  (owner-only, and separated so a personal login can never leak into a
  work session), so an account you sign into survives restarts and
  updates. Deleting the profile is the sign-out.
- New: Website logins. Save a site's login once in Ghost settings →
  Apps (never in chat). Ghost signs in for you with the password read
  from the encrypted vault and handed to the browser privately — it is
  never shown to the model, never logged, and you can remove it any
  time. Signing in asks for approval first, like any other consequential
  action.

## [0.24.37] - 2026-09-25

- Weather now runs on wttr.in first — the provider you asked for — with
  Open-Meteo as the automatic keyless fallback and OpenWeather when a key
  is connected. Answers still name their source ("via wttr.in"), the
  skill says the same, and validation, retry, and the breaker are
  unchanged.

## [0.24.36] - 2026-09-25

- Ghost no longer asks "Which location should I check?" when you already
  said it. "Find a coffee shop near Cebu City" reads the place from the
  message — near, around, close to, not only "in" — and goes straight to
  the nearby-places tool.

## [0.24.35] - 2026-09-25

- Tools and skills now move together. Every capability that has a native
  tool names it at the top of its skill and treats the old shell
  commands as a fallback: weather, air quality, crypto, currency,
  flights, nearby places, maps, calendar, email, Spotify, Notion,
  GitHub, and smart home. The governed path — evidence, approvals,
  honest provider failures — is the default, and a build test keeps the
  pairing from drifting. The full map lives in `docs/TOOL-SKILL-MAP.md`.

## [0.24.34] - 2026-09-25

- Ghost no longer files your questions as facts. "Why do you think my
  name is Ian?" used to be read as if you had declared it — a question
  asserts nothing. Questions are recognized and skipped now.
- Ask "why do you think that?" and Ghost answers with the receipt by
  default: your exact words, where they came from, and how confident it
  is — or an honest "no quote was kept" for older memories.
- The places skill works again. Its request footer carried a
  placeholder contact that OpenStreetMap rejected, so every lookup
  failed with a 403.
- Housekeeping: the mail skill no longer points at documentation that
  doesn't ship, and the self-improvement skill now describes scheduled
  work the way Ghost actually schedules it.

## [0.24.33] - 2026-09-25

- Backups can't be broken by a stray file anymore. A screenshot or a
  folder Ghost doesn't recognise in your workspace used to fail the
  whole weekly snapshot — silently, because the error only lived in
  the service log. Unknown folders are now recorded as skipped, and a
  file you drop at the workspace root travels in the snapshot as your
  own content.
- Release binaries no longer include a leftover development database,
  making them about 6 MB smaller.

## [0.24.32] - 2026-09-25

- Screenshots now use the same working browser engine as everything
  else. The capture command was the one code path that missed the
  browser fix, so a first screenshot on a fresh session could hang on
  a machine where the system browser is broken. It's steered now, and
  the end-to-end test covers navigating, reading the page, and
  capturing a screenshot from a real site.

## [0.24.31] - 2026-09-25

- Follow-through on the stuck-browser fix, in three parts. First: the
  browser cleanup at startup now repeats until no leftovers remain —
  Chromium children can appear only after their parent dies, so one pass
  used to miss them.
- Second: updates refuse to start when the disk is too full, with a
  plain sentence telling you to free space, install the binary
  atomically, and verify it after installing before claiming success.
  A full disk can no longer produce a silent failure or a half-written
  binary.
- Third: when the browser does recover, your phone shows a small card —
  "My browser got stuck — I reset it" — so 90 seconds of quiet is
  followed by an explanation instead of a mystery.

## [0.24.30] - 2026-09-25

- Ghost's browser doesn't get stuck anymore. Root cause found: a
  browser helper left behind by an earlier run kept every page hanging
  until the 90-second timeout, so Ghost blamed the websites. Chelsea
  and the New York Times both load fine — the browser was wedged, not
  the sites. Ghost now clears leftover browser helpers at startup,
  resets itself mid-action if a page times out, and says "my browser
  got stuck" instead of implying the site is down.

## [0.24.29] - 2026-09-25

- Receipts reach your phone. Ask "why do you think that?" and the
  answer arrives with a card right in the chat: the exact words you
  said, how confident Ghost is, when it learned it, and which message
  it came from. Older memories say plainly that no quote was saved,
  instead of pretending.
- Receipt cards never carry buttons on purpose: explaining is
  read-only, and forgetting stays something you do deliberately on
  the Memory screen.

## [0.24.28] - 2026-09-25

- Ask Ghost why it believes something and it shows its work. Every
  memory now keeps the exact words you said, the message they came
  from, how confident Ghost is, and when it learned it. Ask "why do
  you think that?" in chat, or tap **Why?** on the Memory screen.
- Forgetting now really forgets. It retracts the memory, records that
  it's gone so nothing can quietly bring it back, and rebuilds the
  notes derived from it. The only thing that can restore it is you
  saying it again — which is you changing your mind, not a bug.
- Evening reflections moved into a private journal that never feeds
  back into Ghost's behavior on its own. Insight gets recorded; it
  doesn't silently rewrite how Ghost acts. A test enforces it.
- Video processing runs sandboxed: no network, read-only system, a
  private temp folder. Untrusted media can't become a way out.
- New docs: the Ghost Pod home specification (Matter/Thread, BLE
  pairing, signed updates, per-device receipts) and the security
  posture — plain answers to "what can leave my machine?"

## [0.24.27] - 2026-09-25

- Progress lines sound human now. While Ghost works, phone and
  terminal show calm phases — "Thinking…", "Searching the web…",
  "Reading the page…" — and never tool names, file names, paths, or
  commands. The backend and the phone app both enforce it, so nothing
  internal can slip through even for a brand-new capability.
- 22 new skills. Money and news: live stock and crypto quotes, RSS
  and Reddit reading. Writing: an AI-speak stripper, a plain-English
  rewriter, and sourced answers with citations. Planning: weekly
  reviews, action items pulled from documents and meetings, inbox
  triage, and a 1-3-1 decision brief. Everyday: maps and routes, GIF
  search, meme maker, ASCII video, songwriting help, and a
  creative-ideation coach.
- Ghost now learns from itself. A new learning loop records only
  verified fixes and mistakes, and promotes proven workflows into
  skills — joined by skill-gardener and a compound memory pipeline
  adapted for Ghost.
- Asking about an app works like a person would expect: say "check my
  Gmail" and Ghost tells you plainly whether it's connected and the
  one next step — on the web console, your phone, or the terminal —
  without ever asking you to paste a secret into chat.
- Tone: ordinary, reversible work just happens; the hard line stays
  exactly where it was for anything consequential.

## [0.24.26] - 2026-09-25

- The Skills screen in the web console and on your phone now shows
  everything Ghost knows how to do. It was reading the wrong folder
  and came up empty even while Ghost was using all 51 skills; the
  same fix restores your saved notes and files on those screens.
  Conversation history never moved.
- Five skills went back to Ghost's own versions after trying the
  community ones: weather, GitHub, Notion, calendar, and smart home.
  The replacements talked to services directly from a terminal,
  which only works for people comfortable with commands. Ghost's
  versions sign in through Connected Apps instead — set up once
  from the web console or your phone, then it just works. The
  smart-home replacement was also flagged by our scan and used the
  wrong settings names, so it's gone.
- Web search setup now has a home in the console: the built-in free
  search already works with nothing to set up, and a Brave key can
  be pasted in under Apps when you want pro results — stored
  encrypted on your device.
- Google Workspace extras now say plainly when they need a
  one-time sign-in instead of failing halfway through, and the
  Excel skill checks for its spreadsheet library up front rather
  than breaking mid-task.

## [0.24.25] - 2026-09-25

- Twelve community skills added: Word documents, Excel spreadsheets,
  PDFs, Google Workspace (Gmail, Calendar, Drive), GitHub, Notion,
  Slack, calendar handling, content writing, a much fuller smart-home
  guide (25 device areas), a proactive-assistant playbook, and
  weather.
- Where a new skill overlapped one Ghost already had — weather,
  GitHub, Notion, calendar, smart home — the fresh version takes
  over; the previous ones stay recoverable from history.
- Nothing was installed blind: every skill was read end to end,
  stripped of third-party branding and store plugs, and passed
  Ghost's own skill checks before shipping. Google Workspace, Notion,
  smart home and Slack will ask for a one-time setup when first used.

## [0.24.24] - 2026-09-25

- Approvals answer out loud again: after you reply "allow once" or
  "deny" in chat, Ghost's receipt — the list you asked for, the
  confirmation that it ran (or didn't) — now appears right on your
  screen instead of "(no response)".
- Reminders understand how people talk: "at 9pm tonight", "at 8:30 PM
  today" and "tonight at 9" all schedule correctly. A reworded
  request used to bounce off the parser, so the move to 8:30 never
  happened and the reminder stayed at the old time.
- Reminder names are clean: the dinner reminder is titled "Dinner
  with Jas", never the raw sentence with its clock time repeated
  inside. If Ghost can't read a schedule it now asks for a clearer
  time with real examples instead of echoing your words back
  garbled.
- Looking is free: checking your reminder list no longer asks for
  permission — only making, changing or cancelling a reminder does.
- An honest stop first: asking about the system when Home Assistant
  isn't connected now says "connect it first" right away, instead of
  asking for an approval that could only end in a dead end.
- Ghost's replies never begin with an internal date stamp: the date
  labels it keeps for its own reading stay invisible, with a new
  identity rule and protections at every layer — stream, reply,
  save, history read.

## [0.24.23] - 2026-09-25

- Ghost now knows when you said things: every earlier line in a
  conversation carries the date and clock time it was written, so a
  "later today" written last week can never be mistaken for now.
- Summaries stay date-proof: the summarizer is told today's date,
  writes only absolute dates ("Thu, Sep 24, 2026 at 09:00"), and
  stamps every line with when it happened — no more a stale
  "tomorrow" pointing at a day that already passed.
- Open reminders inside summaries show full dates ("Sun 2026-09-27
  07:00") instead of a bare weekday and clock time.
- Ghost re-anchors old context: a new identity rule makes it read
  relative words in older messages, summaries and memory against the
  current clock — "today" in Monday's message means Monday.

## [0.24.22] - 2026-09-25

- Failure replies stay in your language: instant answers no longer
  leak model-only notes like "(completion: failed; do not present
  fabricated data)" — you get the honest sentence, nothing else.
- A weather lookup whose location lookup fails mid-network now says
  so ("The network request failed. I'll try again shortly.")
  instead of blaming the answer ("I got an unexpected response…").
- Scheduled maintenance can actually ask for approval now: heartbeat
  turns carry a request identity, so a due system check opens a
  durable approval you can answer instead of dying with "couldn't
  prepare the approval request".
- "Check my reminders" works: the schedule tool can list pending
  items (soonest first) plus recently completed and failed ones with
  exact times, and fired one-shots are kept as completed history
  (newest 100) instead of being deleted the moment they ran.

## [0.24.21] - 2026-09-25

- Ghost can really drive a browser now: find, wait, screenshot,
  scroll, console, network and accessibility checks join
  navigate/snapshot/click, plus select, check, hover, drag, batch
  form fill, dialogs, upload and download — every action behind
  the same approval gate, and a failed click states the outcome is
  unknown instead of silently retrying.
- Screenshots reach Ghost's eyes: the screenshot action attaches
  the image to that turn only, and the browser playbook loads on
  turns that can actually call the browser (a bare web address in
  your message is enough to open the surface).
- Terminal and mobile name browser work in human words — "Opening
  page: …", "Finding: Log in", "Filling form…" — instead of raw
  tool names.
- A message about a "newsletter" that also says "anything" is no
  longer mistaken for a request for blanket permissions.

## [0.24.20] - 2026-09-24

- Background work reaches every surface: the daemon announces task
  starts and finishes over the live channel, so the mobile
  conversation shows the same running indicator and report-back as
  the terminal. One history note per finish, no double-reporting.

## [0.24.19] - 2026-09-24

- Background tasks report back: delegated subagent work shows a live
  dock line while running, then Ghost tells you what it found —
  failures stated plainly. Max 2 at once, approval unchanged.

## [0.24.18] - 2026-09-24

- Slash palette fixed: the command you type always wins over the
  highlight, and /task stays composed with an arg hint instead of
  erroring. Scoped-models picker removed; /model covers everything.
- Ghost sounds more human: narration is answered, not taskified;
  questions only when the answer changes what happens next; memory
  answers never talk about files.
- Recurring reminders need an explicit ask: describing a habit
  ("every morning I feel groggy") no longer proposes a routine.

## [0.24.17] - 2026-09-23

- Reply headers name Ghost only: the model tag moves out of the
  transcript and stays in the footer status line.

## [0.24.16] - 2026-09-23

- Replies never glue words to clock times: the output path repairs
  "the2:00 PM" style spacing before saving.
- Ghost sounds like a person texting: contractions, plain warmth, an
  emoji only where a human would put one (never on evidence), and an
  honest sentence instead of a dead-end when a turn comes back empty.
- Thin sourcing is flagged unverified AND paired with the
  primary-source next step — never laundered into fact.

## [0.24.15] - 2026-09-23

- Long sent messages no longer fold the pipe onto the text: the user
  bubble wraps to its real width so the bar holds one column.

## [0.24.14] - 2026-09-23

- Approval cards answer to all arrow keys (plus vim keys), not just
  left and right.
- Approved turns always leave a receipt: resumed executions that
  produce no text now say Done (or admit failure) instead of
  replying blank.

## [0.24.13] - 2026-09-23

- Calendar runs on the direct Google Calendar API: agenda, natural
  language quick-add, and delete-by-query with real event evidence.
  gcalcli stays for device-flow-connected users, whose client-bound
  tokens only it can redeem; full removal waits on a Ghost-managed
  OAuth client.

## [0.24.12] - 2026-09-23

Follow-through fixes, same trust story.

- Transient provider blips (timeouts, rate limits, network) retry with
  bounded backoff in model and tool calls; auth and config failures
  still fail fast.
- The browser declares a 90-second bound and closes its session after a
  hung call, so leaked Chromium can no longer starve later turns.
- The Golden grader no longer mistakes distant completion words for
  Ghost's act (p-11 class), and its plural nouns cover third parties.
- Doctor and state suites are hermetic again (PATH pinning, v8-aware
  fixture).
- The web console has an Ideas section; the mobile app stays focused
  (ideas live in conversation).

## [0.24.11] - 2026-09-23

Trust you can read, delight you can feel.

- **Verification pages.** `ghost eval release-notes` assembles
  `docs/VERIFICATION-<version>.md` from Ghost's own checks — verify,
  benchmark, golden results, and limits — committed per release.
- **Tasks surface.** `ghost tasks`, `/tasks`, and `/task` show and manage
  durable routines (pause, resume, cancel) through the same routines
  service the gateway and mobile app use.
- **Ideas with evidence.** `ghost ideas`, `/ideas`, and `/idea` surface
  suggestions that cite their sources, with accept/dismiss receipts.
  `refresh` derives them deterministically; `draft` asks the model and
  verifies every citation, marking failures unverified.

## [0.24.10] - 2026-09-22

- Inline styling no longer leaks when the model breaks a line mid-span.
  Bold, italic, and code split across line breaks now rejoin before
  rendering, on both the live stream and the committed reply — without
  touching fences, tables, headings, quotes, or lists.

## [0.24.9] - 2026-09-22

- Your messages show as `You ┃ text` on one row, with continuation pipes
  aligned beneath.
- The input box now grows while you type a long message (up to its cap,
  then it scrolls) instead of staying one row.

## [0.24.8] - 2026-09-22

- Streaming replies render markdown live, Scout-style: headings, lists,
  quotes, code, and tables format as they arrive instead of raw `##` and
  `**` markers, long lines wrap instead of breaking mid-word, and
  paragraph gaps are preserved. Completion still prints only the
  remaining tail.

## [0.24.7] - 2026-09-22

- Replies now stream line-by-line into the conversation as they arrive,
  opencode style — no more watching a one-row preview and getting the
  whole answer at the end. Completion prints only the remaining tail,
  never the full text again.
- The idle footer is trimmed to `/ commands` (left) and `esc quit`
  (right), matching Scout.

## [0.24.6] - 2026-09-22

- The terminal transcript can no longer silently drop replies. Committed
  lines now print and drain like Scout's transcript (no print cursor left
  to desync), so `/clear`, `/thread`, and history reloads cannot swallow
  later responses.

## [0.24.5] - 2026-09-22

- The terminal no longer loses Ghost replies after `/clear`, `/thread`,
  or returning with `/main`. Replacing the transcript used to leave the
  scrollback cursor behind, silently swallowing later replies; the cursor
  now restarts with the transcript.

## [0.24.4] - 2026-09-22

- `ghost update` no longer prints scope and sudo notes on the Deploying
  line.

## [0.24.3] - 2026-09-22

Evaluation-driven fixes (Golden Suite 58/59 → 59/59, confirmed by external
JEV evaluation before and after).

- **Memory recall no longer falsely reports absence.** Asking what you
  like or prefer could answer "I don't have that stored yet" even when the
  preference was stored, because the fast recall path only looked up two
  predicates while stored beliefs use a wider vocabulary. The fast path now
  covers the liking family and falls through to full retrieval instead of
  asserting an absence it cannot prove.
- **Computer actions report dispatch honestly.** Control actions that were
  dispatched but not independently screen-verified used to report "executor
  reported false", which read as failure and contradicted the recorded
  success evidence. They now report "dispatched but not independently
  screen-verified".

## [0.24.2] - 2026-09-21

Reasoning-hygiene release.

- **Reasoning never surfaces.** Provider chain-of-thought is identified and
  discarded at the provider boundary: the OpenAI-compatible streaming and
  non-streaming paths read the full reasoning allowlist (`reasoning_content`,
  `reasoning`, `reasoning_text`) into the separate `ReasoningContent` field and
  never into answer content.
- **Inline `<think>` tags are stripped** from content with a streaming-safe
  filter that survives token-boundary splits, so a model that emits reasoning
  inline cannot leak it to the agent loop, the TUI, or the SSE bridge to the
  app. Anthropic, Moonshot, and Claude CLI content is sanitized at the same
  boundary.
- Thinking remains off by default; this is the hard guarantee that holds even
  when a caller opts a reasoning model in.

## [0.24.1] - 2026-09-21

Updater parity release.

- `ghost update` / `ghost update --check` print **what's new** on top of "Already current" and after an install, matching Scout.
- Embedded changelog + last-seen marker; `ghost update --notes` prints the full changelog.
- Version comparison is base-version aware, so a `-dirty` or dev-suffixed installed binary is correctly recognised as current instead of looking different.

## [0.24.0] - 2026-09-21

Updater release.

### One updater, no sudo
Ghost's updater ran entirely under sudo and rewrote an appliance-wide install.
It now prefers a per-user layout that needs no root and escalates only the
steps that genuinely require it.

- **`ghost update`** runs as you: `--check`, `--notes`, `--force`, `--dry-run`, `--channel release|dev`.
- **User scope needs no sudo**: binary in `~/.local/bin`, service via `systemctl --user` (`make install-user`).
- **System scope** escalates only the binary swap and service restart.
- **Verified artifacts**: the release channel resolves from GitHub Releases, verifies sha256 (+ Ed25519 when `GHOST_RELEASE_PUBKEY` is set), smoke-tests, and atomically installs.
- **`ghost update --check`** reports installed vs available and the detected scope.

### Retired
The standalone `ghost-update-daemon` is gone — it was a second, divergent
updater. `ghost auto-update` now ticks the same release-channel updater.


## [0.23.45] - 2026-09-21

# Ghost v0.23.45 — "Thinking" reads with a capital T

The composer's top rule now reads **Thinking** (capital T) when Ghost is
reasoning without a tool, matching the tool labels it alternates with
("Searching…", "Reading…"):

```
── ▖ Thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
```

Docs updated. Build clean; `cmd/ghost` suite green.


## [0.23.44] - 2026-09-21

# Ghost v0.23.44 — no tool count in the prompt status

The composer's top rule used to read `⠋ thinking · 4s · 2 tools`. The tool
count is gone — the prompt now names the live activity and elapsed time only:

```
── ▖ thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
── ▖ Reading notes.md · 3s ───────────────
```

Tool detail is still available in the `/details` trail (Ctrl+O).

Also removed a dead status helper and its unused styles that still rendered a
tool count. Build clean; `cmd/ghost` suite green.


