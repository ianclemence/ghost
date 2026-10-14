---
name: canvas-design
description: Design guidance for anything visual Ghost builds. Build it with the canvas tool (never publish_artifact, which needs an approval and shows nothing running): websites, landing pages, portfolios, pricing pages, dashboards, mock-ups, tools, games and animations. Invoke before building or restyling such a page, and when the owner says it looks plain, generic, dated, cramped or "AI-made". Covers layout, type, colour, states, motion and a pre-send checklist. Based on Emil Kowalski's design-engineering craft.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [design, canvas, website, landing-page, ui, css, animation, polish]
---

# Canvas design

Build what a person would want to keep, not what a model produces by default.
A page is finished when it has a point of view, every state is handled, and no
detail makes a thumb or an eye work harder than it should.

This file has what a good first build needs. Build from it, let the canvas check the page, then improve. The references go deeper and are optional: read at most one, and only when you are stuck or the owner asks for more polish (`references/site-patterns.md`, `references/motion.md`, `references/anti-slop.md`).

## Pick the shape

- **Landing page:** hero (what it is, for whom, one action) → one proof → what is offered (list or menu with prices, not three identical cards) → story or how it works → practical details (where, when, how to reach) → the action again.
- **Pricing:** two or three tiers named by who they are for; the recommended one differs by weight and contrast; same price and action position in each; stack on a phone.
- **Portfolio:** the work leads (one large piece, then a grid with title, year, one line); a short first-person about; one way to get in touch. Drawn placeholders with real aspect ratios, never hotlinked photos.
- **Dashboard or tracker:** the answer the owner opens it for is the first thing on screen, as one large number with its unit and trend; breakdown below; real empty and first-run states; remember entries with `ghost.save`.
- **Tool or calculator:** one input, one result, shown as you type, result in the largest type; remembers the last values.
- **Game:** works in three seconds with no instructions; feedback on every input; score kept with `ghost.save`; one-tap restart.

## The five decisions, in order

1. **Who is it for, and what is the one thing they should do?** One primary action per screen. Write it down before any markup.
2. **What is the point of view?** Pick one memorable idea and commit: editorial and warm, instrument-panel precise, playful and tactile, quiet and premium. A coffee roaster is not a fintech app. Every later choice (type, colour, radius, motion) serves that idea.
3. **What is real?** Use the owner's real name, place, products and numbers. Never lorem ipsum, "Your text here", "Feature 1", or invented testimonials and statistics presented as true. Build first: when you lack content, write honest, specific copy, say it is a draft, and ask for the real details after you have shown the page. Never hold the build back with questions. A page that needs the owner's own data (sleep, spending, habits) is still built now: fill it with clearly labelled sample data and give it inputs for the real entries, kept with `ghost.save`; never refuse to build because the data is not connected yet.
4. **What are the states?** Empty, loading, error, success, disabled, long text, one item, a hundred items. Design them, do not leave them to the browser.
5. **What is the motion budget?** Decide what animates and why (see `references/motion.md`). Most things should not.

## The canvas you are building in

- It is one self-contained HTML document on a phone, about 360px wide in chat and full screen when opened. Design mobile first; add a `@media (min-width: 720px)` layout so it also holds up on a tablet or desktop.
- The page is the window. Do not wrap everything in one outer card, and do not use `min-height: 100vh` centring. Use `min-height: 100dvh` only when the page genuinely fills the screen (a game, a tool).
- No network calls, no `localStorage`. Keep state in memory, or call `ghost.save(value)` and read `ghost.saved` to remember between opens.
- Scripts and styles load only from cdnjs.cloudflare.com, cdn.jsdelivr.net, unpkg.com, fonts.googleapis.com. Prefer no libraries. Add one only when it earns its weight (Chart.js for a real chart, Three.js for real 3D).
- Imagery: draw it. Inline SVG, CSS gradients, shapes and type make better, lighter, always-loading pictures than hotlinked photos. Use a photo only when the owner supplied its address.

## Start from the tokens that are already there

The page already has a dark base, a system font and these variables. Use them, extend them, and override them to give the page its own identity (a warm light theme is as valid as dark):

```
--bg --surface --fg --muted --accent --ok --warn --bad --line --radius
--ease-out --ease-in-out --ease-drawer        motion curves
--fast (140ms) --base (200ms)                 durations
--s1..--s8 (4 8 12 16 24 32 48 72 px)         spacing scale
--shadow                                      one elevation
```

Overriding looks like this, and is encouraged when the idea calls for it:

```css
:root { --bg:#f6efe4; --surface:#fffaf1; --fg:#2a1d14; --muted:#7a6552; --accent:#b4531f; --line:rgba(42,29,20,.14); color-scheme: light; }
```

If you change the ground to a light colour, set `color-scheme: light` and check every text/background pair for contrast (4.5:1 for body text, 3:1 for large text and icons).

## Layout

- Use the spacing scale, never arbitrary pixels. Generous space around the primary action; tight, consistent space inside groups. Proximity says what belongs together.
- One column on phones. Cards are for things that are actually separate objects, not a default wrapper for every block. Three identical boxes in a row is the most common tell of a generic page (see `references/anti-slop.md`).
- Alternate density: a loud hero, a quiet list, a dense table. Vary section rhythm instead of stacking equal blocks.
- Touch targets are at least 44×44px with 8px between neighbours. Put the thumb zone to work: primary actions low and reachable on a phone.
- Never hide meaning in hover. Touch has no hover. Gate hover polish behind `@media (hover: hover) and (pointer: fine)`.

## Type

- Two faces at most, one of them the system stack. A single distinctive display face (loaded from fonts.googleapis.com) with the system face for body is enough. Name the fallback.
- Scale by ratio, not by feel: 14 / 16 / 20 / 28 / 40 / clamp for the hero. Body text 16px minimum, line-height 1.5–1.65, line length 45–75 characters.
- Headings: tight line-height (1.1–1.2), slight negative tracking on large sizes, `text-wrap: balance`. Paragraphs: `text-wrap: pretty`. Numerals in tables and counters: `font-variant-numeric: tabular-nums`.
- Form inputs are at least 16px, or the phone zooms the page when they are focused.

## Colour

- One accent, used for the primary action and almost nothing else. Neutrals do the rest. Semantic colours (`--ok --warn --bad`) only for meaning.
- Build depth from layered neutrals and one soft shadow, not from gradients on everything. A gradient is a decision, not a default.
- Never convey state by colour alone: pair with an icon, text or shape.

## Components, finished

- **Buttons:** a clear primary and a quiet secondary. Press feedback (`transform: scale(0.97)` on `:active`, 100–160ms). A visible `:focus-visible` ring. A disabled and a loading state.
- **Forms:** a visible label above each field (placeholders are not labels), the right `type`/`inputmode`/`autocomplete`, an inline error that says how to fix it, a success state that confirms what happened. Since the canvas cannot submit anywhere, make the form do something real in the page (show the result, add to a list, keep it with `ghost.save`) rather than a dead button.
- **Lists and tables:** a real empty state with the next step, sensible truncation (`min-width: 0`, `text-overflow: ellipsis`, or wrapping), and a layout that survives a 40-character name and a 6-digit number.
- **Navigation inside a single page:** anchor sections with `id` and in-page scrolling (`scroll-behavior: smooth`, respecting reduced motion). Links that go outside do not navigate in the canvas, so do not build a site whose pages are separate links; build sections or tabs in one document.

## Before you send it

Walk this list against the code you wrote. Fix, then call canvas.

| Check | Fix |
| --- | --- |
| `transition: all` | Name the properties: `transition: transform var(--fast) var(--ease-out)` |
| `ease-in` on anything the user triggers | `var(--ease-out)`; ease-in feels sluggish |
| `scale(0)` entrance | Start from `scale(0.95)` with `opacity: 0` |
| A press with no feedback | `:active { transform: scale(0.97) }` |
| Hover effect with no hover query | Wrap in `@media (hover: hover) and (pointer: fine)` |
| Animation with no reduced-motion rule | Keep opacity and colour; drop movement under `prefers-reduced-motion` |
| Anything animating `width`, `height`, `top`, `margin` | Animate `transform` and `opacity` only |
| UI animation longer than 300ms | 150–250ms; entrances up to 500ms only for sheets and modals |
| Placeholder copy or fake numbers | Real, specific content from the owner, or an honest draft note |
| Only the happy state designed | Add empty, error, loading and long-text states |
| Text smaller than 16px in a field | 16px, or the phone zooms |
| Tap target under 44px | Pad it up |
| Contrast below 4.5:1 | Darken the text or lighten the ground |

Then tell the owner in a sentence or two what you made and how to use it; do not paste the HTML. When they ask for changes, keep what works and change only what they named, as a new version with the same title.
