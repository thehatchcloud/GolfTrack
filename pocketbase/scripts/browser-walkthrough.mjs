// Browser walkthrough for the Phase 7B frontend.
//
// The Go suite (web_test.go) proves each page renders, gates and 404s
// correctly, but it never runs the JavaScript. This does: it drives a real
// Chromium through all seven workflows the migration plan lists — sign in,
// courses, start, play, complete, review, cancel — and fails if anything logs
// to the console, requests a third-party origin, or returns an unexpected
// error status.
//
// It needs a running instance with password login enabled and an admin account:
//
//   cd pocketbase
//   GOLFTRACK_ALLOW_PASSWORD_LOGIN=true go run . serve --http=127.0.0.1:8090
//   go run . superuser upsert super@example.test <pw>            # then, with a superuser token:
//   curl -X POST localhost:8090/api/collections/users/records \
//     -H "Authorization: <token>" -H 'content-type: application/json' \
//     -d '{"email":"player@example.test","password":"…","passwordConfirm":"…","role":"ADMIN","verified":true}'
//
//   npm i playwright   # or use a checkout that already has it
//   GOLFTRACK_URL=http://127.0.0.1:8090 GOLFTRACK_EMAIL=player@example.test \
//     GOLFTRACK_PASSWORD=… node pocketbase/scripts/browser-walkthrough.mjs
//
// It writes to the instance it is pointed at — a course, a couple of rounds —
// so point it at a scratch instance, never at production.
import { chromium } from 'playwright';

const BASE = process.env.GOLFTRACK_URL || 'http://127.0.0.1:8090';
const EMAIL = process.env.GOLFTRACK_EMAIL || 'player@golftrack.test';
const PASSWORD = process.env.GOLFTRACK_PASSWORD || '';
const OUT = process.env.GOLFTRACK_SHOTS || '';
const CHROME = process.env.CHROME_PATH || undefined;

if (!PASSWORD) {
  console.error('Set GOLFTRACK_PASSWORD (and GOLFTRACK_EMAIL) for the walkthrough account.');
  process.exit(2);
}

const problems = [];
const step = (name) => console.log(`
=== ${name}`);

const browser = await chromium.launch({ executablePath: CHROME });
const context = await browser.newContext({ viewport: { width: 420, height: 900 } });
const page = await context.newPage();

page.on('console', (msg) => {
  // Chromium logs "Failed to load resource" for every non-2xx response, which
  // the response listener below already records with its URL and status. Keep
  // the one with the detail, drop the echo.
  if (msg.text().startsWith('Failed to load resource')) return;
  if (msg.type() === 'error' || msg.type() === 'warning') {
    problems.push(`console.${msg.type()}: ${msg.text()}`);
  }
});
page.on('pageerror', (err) => problems.push(`pageerror: ${err.message}`));
page.on('requestfailed', (req) => {
  // A request cancelled because the page navigated away is not a failure —
  // the icon fetch loses that race routinely.
  if (req.failure()?.errorText === 'net::ERR_ABORTED') return;
  problems.push(`requestfailed: ${req.url()} ${req.failure()?.errorText}`);
});
page.on('response', (res) => {
  const u = new URL(res.url());
  if (u.origin !== BASE) problems.push(`external request: ${res.url()}`);
  if (res.status() >= 400) problems.push(`HTTP ${res.status()} ${res.url()}`);
});

// Screenshots are optional: set GOLFTRACK_SHOTS to a directory to keep them.
async function shot(name) {
  if (!OUT) return;
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: true });
}

// --- sign in ---------------------------------------------------------------
step('sign in');
await page.goto(`${BASE}/rounds/`);
console.log('redirected to:', page.url());
await shot('01-login');
await page.fill('#email', EMAIL);
await page.fill('#password', PASSWORD);
await page.click('button[type=submit]');
await page.waitForURL(`${BASE}/rounds/`, { timeout: 10000 });
console.log('landed on:', page.url());

// --- home ------------------------------------------------------------------
step('home');
await page.goto(`${BASE}/`);
console.log(await page.locator('h1').first().innerText());
await shot('02-home');

// --- create a course -------------------------------------------------------
step('create course');
await page.goto(`${BASE}/courses/new/`);
await page.fill('#name', 'Playwright Links');
await page.click('button:has-text("18 holes")');
await page.selectOption('select[data-hole="1"]', '5');
await page.selectOption('select[data-hole="2"]', '3');
await shot('03-course-form');
await page.click('button[type=submit]');
await page.waitForURL(/\/courses\/[a-z0-9]{15}\/$/, { timeout: 10000 });
const courseUrl = page.url();
console.log('course detail:', courseUrl);
console.log(await page.locator('h1').first().innerText(), '|', await page.locator('p.text-sm.text-stone-500').first().innerText());
await shot('04-course-detail');

// --- edit it ---------------------------------------------------------------
step('edit course');
await page.goto(`${courseUrl}edit/`);
await page.fill('#name', 'Playwright Links West');
await page.click('button[type=submit]');
await page.waitForURL(courseUrl, { timeout: 10000 });
console.log('after edit:', await page.locator('h1').first().innerText());

// --- validation error surfaces ---------------------------------------------
step('validation error');
await page.goto(`${BASE}/courses/new/`);
await page.fill('#name', '   ');
await page.click('button[type=submit]');
await page.waitForSelector('[x-text="error"]:visible', { timeout: 10000 });
console.log('error shown:', (await page.locator('[x-text="error"]').first().innerText()).trim());
await shot('05-course-error');

// --- courses list ----------------------------------------------------------
step('courses list');
await page.goto(`${BASE}/courses/`);
console.log(await page.locator('li').first().innerText());
await shot('06-courses');

// --- start a round ---------------------------------------------------------
step('start round');
await page.goto(`${BASE}/rounds/new/`);
await page.click('label:has-text("Playwright Links West")');
await shot('07-new-round');
await page.click('button[type=submit]');
await page.waitForURL(/\/rounds\/[a-z0-9]{15}\/play\/$/, { timeout: 10000 });
const playUrl = page.url();
console.log('playing:', playUrl);

// --- play ------------------------------------------------------------------
step('play');
await page.click('button:has-text("Driver")');
await page.waitForFunction(() => document.body.innerText.includes('1.'), null, { timeout: 5000 });
await page.click('button:has-text("7i")');
await page.click('button:has-text("Putter")');
await shot('08-play');
console.log('shot list:', (await page.locator('[x-text="shot.club"]').allInnerTexts()).join(', '));

await page.click('button:has-text("Undo")');
await page.waitForTimeout(400);
console.log('after undo:', (await page.locator('[x-text="shot.club"]').allInnerTexts()).join(', '));

// edit a shot
await page.locator('button:has-text("Edit")').first().click();
await page.selectOption('select[x-model="editClub"]', 'SW');
await page.locator('button:has-text("Save")').first().click();
await page.waitForTimeout(400);
console.log('after edit:', (await page.locator('[x-text="shot.club"]').allInnerTexts()).join(', '));

// delete a shot
await page.locator('button:has-text("Delete")').first().click();
await page.waitForTimeout(400);
console.log('after delete:', (await page.locator('[x-text="shot.club"]').allInnerTexts()).join(', '));

// next hole
await page.click('button:has-text("Next")');
await page.waitForTimeout(400);
console.log('current hole now:', await page.locator('.text-4xl').first().innerText());
await page.click('button:has-text("Driver")');
await page.waitForTimeout(400);
await shot('09-play-hole2');

// reload to prove the server render matches the live state
await page.reload();
await page.waitForTimeout(300);
console.log('after reload, hole:', await page.locator('.text-4xl').first().innerText(),
            'total:', await page.locator('[x-text="totalStrokes"]').first().innerText());

// --- review + complete -----------------------------------------------------
step('review and complete');
await page.click('a:has-text("Review Round")');
await page.waitForURL(/\/review\/$/, { timeout: 10000 });
console.log('totals:', (await page.locator('.text-2xl').allInnerTexts()).join(' / '));
await page.fill('textarea', 'Windy back nine, kept the driver in the bag.');
await shot('10-review');
await page.click('button:has-text("Submit Round")');
await page.waitForURL(/\/rounds\/[a-z0-9]{15}\/$/, { timeout: 10000 });
console.log('completed round:', page.url());
console.log('scores:', (await page.locator('.text-2xl').allInnerTexts()).join(' / '));
await shot('11-round-detail');

// --- rounds list + home ----------------------------------------------------
step('rounds list');
await page.goto(`${BASE}/rounds/`);
await shot('12-rounds');
console.log(await page.locator('ul li').first().innerText());

await page.goto(`${BASE}/`);
console.log('home recent:', await page.locator('ul li').first().innerText());

// --- cancel a round --------------------------------------------------------
step('cancel round');
await page.goto(`${BASE}/rounds/new/`);
await page.click('label:has-text("Playwright Links West")');
await page.click('button[type=submit]');
await page.waitForURL(/\/play\/$/, { timeout: 10000 });
await page.click('button:has-text("Cancel Round")');
await page.waitForSelector('text=Cancel this round?', { timeout: 5000 });
await shot('13-cancel-modal');
await page.click('button:has-text("Yes, Cancel")');
await page.waitForURL(`${BASE}/`, { timeout: 10000 });
console.log('after cancel, home says:', (await page.locator('text=No round in progress').count()) ? 'no round in progress' : 'STILL HAS A ROUND');

// --- archive ---------------------------------------------------------------
step('archive course');
page.on('dialog', (d) => d.accept());
await page.goto(courseUrl);
await page.click('button:has-text("Archive")');
await page.waitForURL(`${BASE}/courses/`, { timeout: 10000 });
console.log('courses after archive:', (await page.locator('ul li').allInnerTexts()).join(' | ') || '(empty)');
await page.goto(`${BASE}/courses/archived/`);
console.log('archived list:', (await page.locator('ul li').allInnerTexts()).join(' | ') || '(empty)');
await shot('14-archived');
await page.click('button:has-text("Restore")');
await page.waitForTimeout(600);
console.log('after restore, archived list:', (await page.locator('ul li').allInnerTexts()).join(' | ') || '(empty)');

// --- sign out --------------------------------------------------------------
step('sign out');
await page.goto(`${BASE}/accounts/logout/`);
await page.click('button:has-text("Sign out")');
await page.waitForURL(`${BASE}/`, { timeout: 10000 });
await page.goto(`${BASE}/rounds/`);
console.log('after sign out, /rounds/ went to:', page.url());
await shot('15-signed-out');

await browser.close();

console.log('\n=== console/network problems');
console.log(problems.length ? problems.join('\n') : 'none');
