// Helderberg Social: post the day's batch in the Facebook groups the page
// belongs to, unattended.
//
// The API is the planner: GET /api/fb/rota hands out the due groups with the
// post text written for each; POST /api/fb/rota/result records what happened.
// This script is the hands: it opens each group in a browser profile that is
// signed in as the page, pastes the text, presses Post, and reports back.
//
//   node post.mjs --login          open the browser so a person can sign in
//                                  as the page (once; the profile persists)
//   node post.mjs                  post to today's batch (per_day groups)
//   node post.mjs --limit 12       post to up to 12 due groups
//   node post.mjs --limit all      catch-up run through everything due
//   node post.mjs --dry-run        open each due group and report whether the
//                                  page could post there; posts nothing
//
// Settings come from scripts/fb-groups/.env (never committed) or the
// environment: HS_API_URL, HS_FB_ROTA_TOKEN, HS_FB_PROFILE_DIR,
// HS_FB_HEADLESS=1, HS_FB_PAUSE_MIN / HS_FB_PAUSE_MAX (seconds between
// groups, default 60-150, a human pace).
//
// It never types a password (the sign-in is a person's job), never posts as
// a person (the composer must say the page's name or the group is skipped as
// "failed"), and stops the whole run the moment Facebook asks for a login,
// a checkpoint or a captcha. Read docs/facebook.md before changing it.

import { chromium } from "playwright";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
loadDotEnv(path.join(here, ".env"));

const API = (process.env.HS_API_URL || "https://api.helderbergsocial.co.za").replace(/\/+$/, "");
const TOKEN = process.env.HS_FB_ROTA_TOKEN || "";
const PROFILE = process.env.HS_FB_PROFILE_DIR || path.join(process.env.LOCALAPPDATA || process.env.HOME || here, "helderberg-social", "fb-profile");
const HEADLESS = process.env.HS_FB_HEADLESS === "1";
const PAUSE_MIN = num(process.env.HS_FB_PAUSE_MIN, 60);
const PAUSE_MAX = num(process.env.HS_FB_PAUSE_MAX, 150);
const PAGE_NAME = process.env.HS_FB_PAGE_NAME || "Helderberg Social"; // what the composer must say
const PAGE_URL = (process.env.HS_FB_PAGE_URL || "").replace(/\/+$/, ""); // what /me must resolve to; empty = any URL containing "helderberg"
const LOGS = path.join(here, "logs");

const args = process.argv.slice(2);
const flag = (n) => args.includes(n);
const opt = (n, d) => { const i = args.indexOf(n); return i >= 0 && args[i + 1] ? args[i + 1] : d; };

fs.mkdirSync(LOGS, { recursive: true });
const logFile = path.join(LOGS, new Date().toISOString().slice(0, 10) + ".log");
function log(...parts) {
  const line = `${new Date().toISOString()} ${parts.join(" ")}`;
  console.log(line);
  fs.appendFileSync(logFile, line + "\n");
}

function loadDotEnv(p) {
  if (!fs.existsSync(p)) return;
  for (const raw of fs.readFileSync(p, "utf8").split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const eq = line.indexOf("=");
    if (eq < 1) continue;
    const k = line.slice(0, eq).trim();
    let v = line.slice(eq + 1).trim();
    if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) v = v.slice(1, -1);
    if (!(k in process.env)) process.env[k] = v;
  }
}
function num(v, d) { const n = parseInt(v, 10); return Number.isFinite(n) && n >= 0 ? n : d; }
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const jitter = (a, b) => a + Math.floor(Math.random() * Math.max(0, b - a + 1));

async function api(pathname, init = {}) {
  const res = await fetch(API + pathname, {
    ...init,
    headers: { Authorization: "Bearer " + TOKEN, "Content-Type": "application/json", ...(init.headers || {}) },
  });
  const text = await res.text();
  let body;
  try { body = JSON.parse(text); } catch { body = { ok: false, error: text.slice(0, 200) }; }
  if (!res.ok || !body.ok) throw new Error(`${pathname}: ${res.status} ${body.error || text.slice(0, 200)}`);
  return body;
}

async function report(g, outcome, note) {
  try {
    const r = await api("/api/fb/rota/result", { method: "POST", body: JSON.stringify({ id: g.id, outcome, note }) });
    log(`  -> recorded ${outcome} (posts=${r.posts}, next=${r.next_due || "-"}, enabled=${r.enabled})`);
  } catch (e) {
    log(`  !! could not record ${outcome} for #${g.id}: ${e.message}`);
  }
}

async function openBrowser() {
  fs.mkdirSync(PROFILE, { recursive: true });
  return chromium.launchPersistentContext(PROFILE, {
    headless: HEADLESS,
    viewport: { width: 1380, height: 900 },
    locale: "en-ZA",
    timezoneId: "Africa/Johannesburg",
    args: ["--disable-blink-features=AutomationControlled"],
  });
}

// Facebook's front door redirects a fresh profile once or twice (locale,
// login, checkpoint), which Chromium reports as net::ERR_ABORTED on the
// first navigation. That is not a failure; settle and carry on.
async function home(page) {
  try {
    await page.goto("https://www.facebook.com/", { waitUntil: "domcontentloaded", timeout: 45000 });
  } catch (e) {
    if (!/ERR_ABORTED|interrupted by another navigation/i.test(String(e.message))) throw e;
    await page.waitForLoadState("domcontentloaded", { timeout: 30000 }).catch(() => {});
  }
  await sleep(2000);
}

async function signedIn(ctx) {
  const cookies = await ctx.cookies("https://www.facebook.com");
  return cookies.some((c) => c.name === "c_user");
}

async function login() {
  const ctx = await openBrowser();
  const page = ctx.pages()[0] || (await ctx.newPage());
  await home(page);
  console.log("\nSign in to Facebook in the window, then switch to the page profile (top-right avatar -> See all profiles -> the Helderberg Social page).");
  console.log("This script never types a password. It reports who the session is every few seconds; close the window once it says the page.\n");
  const until = Date.now() + 15 * 60 * 1000;
  let last = "";
  while (Date.now() < until && !page.isClosed()) {
    if (await signedIn(ctx)) {
      const who = await identity(ctx);
      if (who !== last) {
        last = who;
        const isPage = PAGE_URL ? who === PAGE_URL : /helderberg/i.test(who);
        console.log(`Signed in as ${who}${isPage ? "  <- the page, good; close the window" : "  <- a person: switch to the page profile now"}`);
      }
    }
    await sleep(12000);
  }
  await ctx.close().catch(() => {});
  console.log("Profile saved at " + PROFILE);
}

// identity is the profile the session acts as: /me lands on its URL
// (a bare HTTP request gets a 400, so it has to be a real navigation). It
// uses its own tab so the visible one is left alone during --login.
async function identity(ctx) {
  const p = await ctx.newPage();
  try {
    await p.goto("https://www.facebook.com/me", { waitUntil: "domcontentloaded", timeout: 30000 }).catch(() => {});
    await sleep(4000);
    return p.url().replace(/[?#].*$/, "").replace(/\/+$/, "");
  } catch {
    return "";
  } finally {
    await p.close().catch(() => {});
  }
}

// What the group page says about the page's standing there.
async function standing(page) {
  const body = (await page.locator("body").innerText().catch(() => "")) || "";
  if (/\/login|\/checkpoint|\/recover/.test(page.url())) return { state: "signed-out" };
  if (/security check|confirm it's you|enter the code|verify your identity/i.test(body) && /checkpoint/i.test(page.url() + body)) return { state: "checkpoint" };
  if (/request to participate is pending approval|membership is pending|your request to join is pending/i.test(body)) return { state: "retry", note: "participation pending" };
  if (/this content isn't available|content isn't available right now|group not found/i.test(body)) return { state: "blocked", note: "group not available" };
  const joinBtn = page.getByRole("button", { name: /^Join group$|^Join$/ });
  const joined = page.getByRole("button", { name: /^Joined$/ });
  if ((await joined.count()) === 0 && (await joinBtn.count()) > 0) return { state: "blocked", note: "page is not a member" };
  return { state: "ok" };
}

async function postTo(page, g, dry) {
  await page.goto(g.url, { waitUntil: "domcontentloaded", timeout: 60000 });
  await sleep(jitter(4000, 8000));
  const s = await standing(page);
  if (s.state !== "ok") return s;
  const composer = page.getByRole("button", { name: /Write something|What's on your mind|Write a public post|Create a public post/i }).first();
  if ((await composer.count()) === 0) {
    // Some groups hide the box until the page scrolls; try once.
    await page.mouse.wheel(0, 400);
    await sleep(1500);
    if ((await composer.count()) === 0) return { state: "retry", note: "no composer on the group page" };
  }
  if (dry) return { state: "dry", note: "could post" };

  await composer.click();
  const dialog = page.getByRole("dialog", { name: /Create post/i }).first();
  await dialog.waitFor({ state: "visible", timeout: 20000 });
  await sleep(jitter(800, 1500));
  const head = (await dialog.innerText().catch(() => "")) || "";
  if (!head.toLowerCase().includes(PAGE_NAME.toLowerCase())) {
    await page.keyboard.press("Escape").catch(() => {});
    return { state: "failed", note: `composer is not the page (${head.split("\n").slice(0, 3).join(" | ").slice(0, 80)})` };
  }
  const box = dialog.locator('[contenteditable="true"][role="textbox"]').first();
  await box.click();
  await sleep(300);
  const lines = g.text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    if (lines[i]) await page.keyboard.type(lines[i], { delay: 2 });
    if (i < lines.length - 1) await page.keyboard.press("Enter");
  }
  await sleep(jitter(3000, 6000)); // let the link preview attach
  const btn = dialog.getByRole("button", { name: /^Post$/ });
  await btn.waitFor({ state: "visible", timeout: 10000 });
  const disabled = await btn.getAttribute("aria-disabled");
  if (disabled === "true") return { state: "failed", note: "Post button stayed disabled" };
  await btn.click();
  // The dialog closes on success; a "pending" banner or the post itself then appears.
  try {
    await dialog.waitFor({ state: "hidden", timeout: 60000 });
  } catch {
    const err = (await dialog.innerText().catch(() => "")) || "";
    return { state: "failed", note: "dialog did not close: " + err.replace(/\s+/g, " ").slice(0, 120) };
  }
  const lead = g.text.split("\n")[0].slice(0, 40);
  const until = Date.now() + 25000;
  while (Date.now() < until) {
    const body = (await page.locator("body").innerText().catch(() => "")) || "";
    if (/your post is pending|post is awaiting approval|pending approval/i.test(body)) return { state: "posted", note: "awaiting the group's admins" };
    if (body.includes(lead)) return { state: "posted", note: "live" };
    await sleep(2000);
  }
  return { state: "failed", note: "no confirmation after posting" };
}

async function run() {
  if (!TOKEN) throw new Error("HS_FB_ROTA_TOKEN is not set (scripts/fb-groups/.env)");
  const dry = flag("--dry-run");
  const limit = opt("--limit", "");
  const rota = await api("/api/fb/rota" + (limit ? "?limit=" + encodeURIComponent(limit) : ""));
  log(`rota ${rota.today}: ${rota.groups.length} of ${rota.due_total} due groups in this run${dry ? " (dry run)" : ""}`);
  if (rota.groups.length === 0) return;

  const ctx = await openBrowser();
  const page = ctx.pages()[0] || (await ctx.newPage());
  try {
    await home(page);
    await sleep(3000);
    if (!(await signedIn(ctx))) throw new Error("the browser profile is not signed in; run: node post.mjs --login");
    const who = await identity(ctx);
    log(`session is ${who || "unknown"}`);
    if (PAGE_URL ? who !== PAGE_URL : !/helderberg/i.test(who)) {
      throw new Error(`the session is a person, not the page (${who}); run: node post.mjs --login and switch to the page profile`);
    }

    let n = 0;
    for (const g of rota.groups) {
      n++;
      log(`[${n}/${rota.groups.length}] #${g.id} ${g.name} (${g.kind}) ${g.url}`);
      let r;
      try {
        r = await postTo(page, g, dry);
      } catch (e) {
        r = { state: "failed", note: (e.message || String(e)).replace(/\s+/g, " ").slice(0, 150) };
      }
      log(`  ${r.state}${r.note ? ": " + r.note : ""}`);
      if (r.state === "signed-out" || r.state === "checkpoint") {
        await page.screenshot({ path: path.join(LOGS, `stop-${Date.now()}.png`) }).catch(() => {});
        log("  !! Facebook wants a person; stopping the run. Sign in again with: node post.mjs --login");
        break;
      }
      if (r.state === "failed") await page.screenshot({ path: path.join(LOGS, `fail-${g.id}-${Date.now()}.png`), fullPage: false }).catch(() => {});
      if (!dry && r.state !== "dry") await report(g, r.state, r.note || "");
      if (n < rota.groups.length) {
        const wait = dry ? jitter(3, 6) : jitter(PAUSE_MIN, PAUSE_MAX);
        log(`  pausing ${wait}s`);
        await sleep(wait * 1000);
      }
    }
  } finally {
    await ctx.close().catch(() => {});
  }
}

(flag("--login") ? login() : run()).catch((e) => {
  log("!! " + (e.message || e));
  process.exitCode = 1;
});
