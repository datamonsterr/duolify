// Run from the repository root: mise exec node -- node docs/design/prototype/check.mjs
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';

const root = new URL('../../../', import.meta.url);
const htmlUrl = new URL('docs/design/prototype/index.html', root);
const require = createRequire(new URL('apps/client/package.json', root));

// E2E contract first: real file:// browsing with all network requests blocked.
async function browserChecks() {
  const { chromium } = require('@playwright/test');
  const browser = await chromium.launch({ headless: true });
  const errors = [];
  const requests = [];
  try {
    for (const width of [320, 390, 768, 1440]) {
      const context = await browser.newContext({ viewport: { width, height: 1000 }, offline: true });
      await context.route('**/*', route => {
        if (route.request().url().startsWith('file:')) return route.continue();
        requests.push(route.request().url());
        return route.abort();
      });
      const page = await context.newPage();
      page.on('pageerror', error => errors.push(error.message));
      page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
      await page.goto(htmlUrl.href);
      await page.getByRole('heading', { name: 'A little closer to the cosmos.' }).waitFor();
      await page.keyboard.press('Tab');
      assert.equal(await page.evaluate(() => document.activeElement.textContent.trim()), 'Skip to content');
      const overflow = () => page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
      assert.equal(await overflow(), false, `path overflow at ${width}`);
      await page.getByRole('button', { name: 'Start lesson' }).click();
      await page.getByRole('heading', { name: 'Why the sky changes' }).waitFor();
      const frame = page.frameLocator('iframe[title="Motion HTML: Earth turns"]');
      await frame.getByText('Earth turns. Your view changes.').waitFor();
      assert.equal(await overflow(), false, `player overflow at ${width}`);
      await page.getByRole('button', { name: 'Play narration' }).click();
      await page.waitForTimeout(400);
      assert.ok(Number(await page.locator('#timeline').inputValue()) > 0);
      await page.getByRole('button', { name: 'Pause narration' }).click();
      await page.getByRole('button', { name: 'Replay slide' }).click();
      assert.equal(Number(await page.locator('#timeline').inputValue()), 0);
      await page.getByLabel('Narration speed').selectOption('1.5');
      await page.getByRole('button', { name: 'Mute narration' }).click();
      await page.getByRole('button', { name: 'Show captions' }).click();
      await page.getByRole('button', { name: 'Bookmark slide' }).click();
      await page.getByRole('button', { name: 'Ask tutor' }).click();
      await page.getByRole('dialog').waitFor();
      await page.getByLabel('Your question').fill('A demo draft');
      await page.keyboard.press('Shift+Tab');
      assert.equal(await page.getByRole('button', { name: 'Close dialog' }).evaluate(el => el === document.activeElement), true);
      await page.keyboard.press('Shift+Tab');
      assert.equal(await page.getByRole('button', { name: 'Keep draft and return' }).evaluate(el => el === document.activeElement), true);
      await page.keyboard.press('Escape');
      assert.equal(await page.getByRole('button', { name: 'Ask tutor' }).evaluate(el => el === document.activeElement), true);
      await page.getByRole('button', { name: 'Ask tutor' }).click();
      assert.equal(await page.getByLabel('Your question').inputValue(), 'A demo draft');
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Add note' }).click();
      await page.getByLabel('Your original note').fill('Earth turns, so our view changes.');
      await page.getByRole('button', { name: 'Save quick note' }).click();
      await page.getByRole('button', { name: 'Add note' }).click();
      assert.equal(await page.getByLabel('Your original note').inputValue(), 'Earth turns, so our view changes.');
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Take lesson test' }).click();
      assert.equal(await overflow(), false, `test overflow at ${width}`);
      await page.getByLabel('Earth stays still.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByText('A retry variant is queued for later.').waitFor();
      await page.getByRole('button', { name: 'Continue test' }).click();
      await page.getByRole('group', { name: 'What gives Earth its daylight?' }).waitFor();
      await page.getByLabel('Light from the Sun.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByRole('button', { name: 'Continue test' }).click();
      await page.getByText('Retry variant · Same idea, fresh wording').waitFor();
      await page.getByLabel('Because our planet rotates.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByRole('button', { name: 'Finish lesson' }).click();
      await page.getByRole('heading', { name: 'One idea clearer.' }).waitFor();
      await page.getByRole('button', { name: 'Back to learn path' }).click();
      await page.getByRole('button', { name: 'View credit options' }).first().click();
      await page.getByRole('heading', { name: 'Make room for your next idea.' }).waitFor();
      await page.getByLabel('Monthly subscription').check();
      await page.getByRole('button', { name: 'Preview checkout' }).click();
      await page.getByRole('dialog').waitFor();
      await page.getByRole('button', { name: 'Simulate cancelled payment' }).click();
      await page.getByText('Checkout cancelled. No credits were added.').waitFor();
      await page.getByRole('button', { name: 'Preview checkout' }).click();
      await page.getByRole('button', { name: 'Simulate pending payment' }).click();
      await page.getByText('Payment pending. Your balance has not changed.').waitFor();
      assert.equal(await overflow(), false, `shop overflow at ${width}`);
      await page.getByRole('button', { name: 'Back to learn path' }).click();
      // All-correct route and repeated mistake must both be supported.
      await page.getByRole('button', { name: 'Restart demo' }).click();
      await page.getByRole('button', { name: 'Start lesson' }).click();
      await page.getByRole('button', { name: 'Take lesson test' }).click();
      await page.getByLabel('Earth turns.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByRole('button', { name: 'Continue test' }).click();
      await page.getByLabel('Light from the Sun.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByRole('button', { name: 'Finish lesson' }).click();
      await page.getByRole('heading', { name: 'One idea clearer.' }).waitFor();
      await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
      await page.getByRole('button', { name: 'Back to learn path' }).click();
      assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--canvas').trim()), '#131f24');
      await page.getByRole('button', { name: 'Restart demo' }).click();
      await page.getByRole('button', { name: 'Start lesson' }).click();
      await page.getByRole('button', { name: 'Play narration' }).click();
      await page.waitForTimeout(200);
      await page.getByRole('button', { name: 'Pause narration' }).click();
      assert.equal(await page.frameLocator('#slide').locator('#earth').evaluate(el => el.style.transform), 'rotate(0deg)');
      await page.getByRole('button', { name: 'Take lesson test' }).click();
      for (const answer of ['Earth stays still.', 'Light from the Sun.', 'Because the Sun orbits us.']) {
        await page.getByLabel(answer).check();
        await page.getByRole('button', { name: 'Check answer' }).click();
        await page.getByRole('button', { name: 'Continue test' }).click();
      }
      await page.getByLabel('Because our planet rotates.').check();
      await page.getByRole('button', { name: 'Check answer' }).click();
      await page.getByRole('button', { name: 'Finish lesson' }).click();
      await page.getByRole('heading', { name: 'One idea clearer.' }).waitFor();
      await page.getByRole('button', { name: 'Back to learn path' }).click();
      await page.evaluate(() => { document.documentElement.style.fontSize = '32px'; });
      await page.waitForTimeout(100);
      assert.equal(await overflow(), false, `200% text overflow at ${width}`);
      await page.getByRole('button', { name: 'Restart demo' }).focus();
      await page.getByRole('button', { name: 'Restart demo' }).scrollIntoViewIfNeeded();
      assert.equal(await page.getByRole('button', { name: 'Restart demo' }).evaluate(el => {
        const nav = document.querySelector('nav');
        return getComputedStyle(nav).position !== 'fixed' || el.getBoundingClientRect().bottom <= nav.getBoundingClientRect().top;
      }), true, `bottom navigation covers final action at ${width}`);
      await page.evaluate(() => { document.documentElement.style.removeProperty('font-size'); });
      await context.close();
    }
    assert.deepEqual(errors, [], 'browser console/page errors');
    assert.deepEqual(requests, [], 'prototype attempted network access');
  } finally { await browser.close(); }
  console.log('PASS offline browser: 4 widths, wrong/repeated/all-correct answers, playback, sheets, paywall, dark/reduced motion');
}

// Integration checks: JSON, Tailwind snippet and standalone CSS must agree.
async function tokenChecks() {
  const tokens = JSON.parse(await readFile(new URL('docs/design/tokens.json', root), 'utf8'));
  const css = await readFile(new URL('docs/design/theme.css', root), 'utf8');
  const html = await readFile(htmlUrl, 'utf8');
  for (const group of ['space', 'radius', 'size']) {
    for (const [name, token] of Object.entries(tokens[group])) {
      const declaration = `--${group}-${name}: ${token.$value.value}px;`;
      assert.ok(css.includes(declaration), `${group}/${name} missing in CSS`);
      assert.ok(html.includes(declaration), `${group}/${name} missing in HTML`);
    }
  }
  for (const [name, token] of Object.entries(tokens.motion.duration)) {
    const declaration = `--duration-${name}: ${token.$value.value}ms;`;
    assert.ok(css.includes(declaration) && html.includes(declaration), `duration/${name} mismatch`);
  }
  const contrastRows = [];
  for (const mode of ['light', 'dark']) {
    const colors = tokens.color[mode];
    for (const [name, token] of Object.entries(colors)) {
      if (name.startsWith('$')) continue;
      assert.equal(token.$type, 'color');
      const hex = token.$value.hex.toLowerCase();
      const expected = token.$value.components.map(value => Math.round(value * 255));
      assert.deepEqual(expected, hex.match(/[a-f\d]{2}/gi).map(n => parseInt(n, 16)));
      assert.ok(css.includes(`--${name}: ${hex};`), `${mode}/${name} missing in CSS`);
      assert.ok(html.includes(`--${name}: ${hex};`), `${mode}/${name} missing in HTML`);
    }
    const pairs = [];
    for (const bg of ['canvas', 'surface', 'raised']) {
      for (const fg of ['text', 'muted', 'primary']) pairs.push([fg, bg, 4.5]);
      for (const fg of ['line', 'focus']) pairs.push([fg, bg, 3]);
    }
    pairs.push(['on-primary', 'primary', 4.5], ['on-primary', 'primary-hover', 4.5], ['on-accent', 'accent', 4.5]);
    for (const role of ['success', 'danger', 'warning']) pairs.push([role, `${role}-bg`, 4.5]);
    pairs.push(['on-secondary', 'secondary', 4.5], ['info', 'info-bg', 4.5], ['credit', 'credit-bg', 4.5], ['streak', 'streak-bg', 4.5]);
    for (const [fg, bg, threshold] of pairs) {
      const a = colors[fg].$value.hex, b = colors[bg].$value.hex;
      const ratio = contrast(a, b);
      assert.ok(ratio >= threshold, `${mode}: ${fg}/${bg} = ${ratio}`);
      contrastRows.push(`| ${mode} | ${fg} / ${bg} | ${a} / ${b} | ${ratio.toFixed(2)}:1 | ${threshold}:1 |`);
    }
  }
  assert.ok(css.includes('@theme inline'));
  console.log(contrastRows.join('\n'));
  console.log('PASS semantic token parity and all permitted contrast pairs');
}

function contrast(a, b) {
  const luminance = hex => hex.slice(1).match(/../g).map(n => parseInt(n, 16) / 255)
    .map(n => n <= 0.04045 ? n / 12.92 : ((n + 0.055) / 1.055) ** 2.4)
    .reduce((sum, n, i) => sum + n * [0.2126, 0.7152, 0.0722][i], 0);
  const x = luminance(a), y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

// Unit fixtures for the measurement, not a mirror of component code.
assert.equal(contrast('#000000', '#ffffff'), 21);
assert.equal(contrast('#5942b5', '#5942b5'), 1);
assert.equal(contrast('#ffffff', '#000000'), contrast('#000000', '#ffffff'));
if (process.argv.includes('--tokens')) await tokenChecks();
else if (process.argv.includes('--browser')) await browserChecks();
else { await tokenChecks(); await browserChecks(); }
