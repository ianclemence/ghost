# Site and page patterns

Pick the pattern, then bend it to the owner's idea. Every pattern assumes the tokens and rules in `../SKILL.md`.

## Landing page for a business or product

Order, top to bottom, on a phone:

1. **Hero.** What it is, for whom, in one line of at most 10 words, set large (`clamp(2rem, 9vw, 3.25rem)`), then one sentence of support, then one primary action. A single strong visual: an inline SVG, a type treatment, or a shape built in CSS. No stock-photo placeholder.
2. **Proof.** One concrete thing that makes it believable: a real number, a place, a year founded, a named process. If you have no real proof, skip the section rather than inventing it.
3. **What you offer.** A menu, list or short rows with the price at the right edge. Not three identical cards with emoji.
4. **How it works or the story.** Two to four steps, or one honest paragraph in the owner's voice.
5. **Practical details.** Where, when, how to reach them. Hours in a readable table, an address, a tap-to-copy action (the canvas cannot open links or maps).
6. **Final action.** Repeat the primary action with the context it needs (a form that works in the page, or the thing to copy).

Skeleton:

```html
<header class="hero">
  <p class="eyebrow">Chiang Mai · roasted weekly</p>
  <h1>Coffee that tastes like the hill it grew on</h1>
  <p class="lede">Small lots from Doi Chang, roasted every Tuesday.</p>
  <button class="btn primary" id="cta">See this week's beans</button>
</header>
```

```css
.hero { padding: var(--s7) var(--s4) var(--s6); }
.eyebrow { color: var(--accent); font-size: .8125rem; letter-spacing: .08em; text-transform: uppercase; }
h1 { font-size: clamp(2rem, 9vw, 3.25rem); margin: var(--s3) 0; }
.lede { color: var(--muted); max-width: 34ch; }
.btn.primary { background: var(--accent); color: #fff; border-color: transparent; padding: 0 var(--s5); }
```

## Pricing page

Two or three tiers. Make the recommended tier visibly different by weight and contrast, not by a ribbon. Name each tier by who it is for. List what is included in plain outcomes. Show the billing period once. Make comparison easy on a phone: stack the tiers, keep the price and the action in the same place in each. Use a toggle (monthly/yearly) only if both prices are real; state the saving as a number.

## Portfolio

The work is the design. Lead with one great piece, large, then a grid. Each piece: title, year, one line about the problem and the result. A short about section in first person, a short list of what the person does, one way to get in touch. Restraint: one typeface, neutral ground, the work supplies the colour. For a photographer or artist, draw framed placeholders with the exact aspect ratios and say clearly that images go there; never hotlink photographs.

## Dashboard or tracker

Answer the question the owner opens it with in the first screen: a single large number with its trend and its unit, then supporting breakdowns. Labels sit with their numbers. Use tabular numerals. A chart earns its place only if it shows change over time or comparison; label axes and mark the latest value. Always design the empty state ("Nothing logged yet. Add your first entry") and the first-run state. Persist what the person enters with `ghost.save`.

## Small tool, calculator, converter

One input, one result, immediately. Show the result as you type. Large touch targets, a numeric keypad (`inputmode="decimal"`), a clear reset, remembers the last values. The result has the largest type on screen and says what it is in words.

## Game or toy

The first interaction must work in under three seconds with no instructions. Feedback on every input (press, sound-free confirmation through motion and colour). A score that is kept with `ghost.save`. A clear end state and a one-tap restart. Respect `prefers-reduced-motion` and never rely on colour alone.

## Writing copy

- Headlines say the thing: what it is and why it matters. Avoid "Welcome to", "Revolutionize", "Seamless", "Elevate".
- Verbs on buttons: "See this week's beans", "Book a table", not "Submit" or "Click here".
- Numbers with units, dates with the year, prices with the currency.
- Errors say what happened and how to fix it. Empty states say what to do next.
- Write for one person, in the owner's voice and language. If the owner writes in Thai, the page is in Thai.
