import { chromium } from 'playwright';
import { spawn, spawnSync } from 'node:child_process';
import { mkdirSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { homedir } from 'node:os';

const here = dirname(fileURLToPath(import.meta.url));
const SOURCE = pathToFileURL(join(here, 'index.html')).href;
const OUT_DIR = join(here, 'out');
const SILENT = join(OUT_DIR, 'opendepot-promo-silent.mp4');
const FINAL = join(OUT_DIR, 'opendepot-promo.mp4');

// Grid derived from the measured kick analysis (150 BPM, drop on bar 1).
const FPS = 30;
const DURATION = 30;
const TRACK = process.env.TRACK_AUDIO || join(homedir(), 'Desktop', 'Tone Def - Sunday Vibes.wav');
const TRACK_START = Number(process.env.TRACK_START || '75.2155');

// ffmpeg is resolved from fixed absolute paths only; PATH is never consulted.
const FFMPEG_CANDIDATES = ['/opt/homebrew/bin/ffmpeg', '/usr/local/bin/ffmpeg', '/usr/bin/ffmpeg'];

function resolveFfmpeg() {
  const bin = FFMPEG_CANDIDATES.find((candidate) => existsSync(candidate));
  if (!bin) throw new Error(`ffmpeg not found; looked in ${FFMPEG_CANDIDATES.join(', ')}`);

  return bin;
}

const args = process.argv.slice(2);
const stillIdx = args.indexOf('--still');

mkdirSync(OUT_DIR, { recursive: true });

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 }, deviceScaleFactor: 1 });
await page.goto(SOURCE, { waitUntil: 'load' });
await page.evaluate(() => Promise.all(Array.from(document.images).map((img) => img.decode().catch(() => null))));
await page.evaluate(() => document.fonts.ready);

if (stillIdx !== -1) {
  const t = Number(args[stillIdx + 1]);
  const file = args[stillIdx + 2] || join(OUT_DIR, `still-${t}.png`);
  await page.evaluate((tt) => window.renderAt(tt), t);
  await page.screenshot({ path: file, type: 'png' });
  console.log(`wrote ${file}`);
  await browser.close();
  process.exit(0);
}

const frames = Math.round(DURATION * FPS);
const FFMPEG = resolveFfmpeg();
const ff = spawn(FFMPEG, [
  '-y', '-loglevel', 'error',
  '-f', 'image2pipe', '-framerate', String(FPS), '-c:v', 'png', '-i', '-',
  '-c:v', 'libx264', '-preset', 'slow', '-crf', '16', '-pix_fmt', 'yuv420p',
  '-movflags', '+faststart', SILENT,
], { stdio: ['pipe', 'inherit', 'inherit'] });

const ffDone = new Promise((res, rej) => ff.on('close', (code) => (code === 0 ? res() : rej(new Error(`ffmpeg exited ${code}`)))));

for (let i = 0; i < frames; i++) {
  const t = i / FPS;
  await page.evaluate((tt) => window.renderAt(tt), t);
  const png = await page.screenshot({ type: 'png' });
  if (!ff.stdin.write(png)) await new Promise((r) => ff.stdin.once('drain', r));
  if (i % 150 === 0) console.log(`frame ${i}/${frames}`);
}
ff.stdin.end();
await ffDone;
await browser.close();

if (!existsSync(TRACK)) throw new Error(`Audio track not found: ${TRACK}`);
const mux = spawnSync(FFMPEG, [
  '-y', '-loglevel', 'error',
  '-i', SILENT,
  '-ss', String(TRACK_START), '-i', TRACK,
  '-map', '0:v', '-map', '1:a',
  '-t', String(DURATION),
  // 27.2 s is a bar line (17 bars at 150 BPM); the fade ends at DURATION.
  '-af', `afade=t=out:st=27.2:d=${(DURATION - 27.2).toFixed(2)}`,
  '-c:v', 'copy',
  '-c:a', 'aac', '-b:a', '320k', '-ar', '48000',
  '-movflags', '+faststart', FINAL,
], { stdio: 'inherit' });
if (mux.status !== 0) throw new Error('mux failed');
console.log(`wrote ${FINAL}`);
