// SPDX-License-Identifier: MIT
// Builds the openrail site into dist/: the landing page, the docs (HTML and
// a Markdown copy of every page), a search index, /deploy, /llms.txt,
// /llms-full.txt, /install.sh, a sitemap and a 404 page. No dependencies
// beyond Node itself.
//
//   node build.mjs [--out DIR]
//
// The site reads the openrail source tree (pkg/api, templates, install) for
// the generated reference pages. It looks for it next to this folder (the
// site/ folder of a checkout), then in ../openrail, or wherever FIFTY_REPO
// points.

import { readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync, statSync, existsSync, copyFileSync } from 'node:fs';
import { join, dirname, relative, posix, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseFrontMatter, render, escapeHtml, inline } from './src/lib/markdown.mjs';
import { apiPage, serviceSettingsPage, cliPages, templatePages } from './src/lib/generate.mjs';
import { ORIGIN, REPO, expand } from './src/lib/config.mjs';
import { landing } from './src/pages/index.mjs';
import { shot } from './src/lib/media.mjs';
import { deployPage } from './src/pages/deploy.mjs';

const SITE = dirname(fileURLToPath(import.meta.url));

export function findRoot() {
  const candidates = [process.env.FIFTY_REPO, join(SITE, '..'), join(SITE, '..', 'openrail')].filter(Boolean);
  for (const c of candidates) if (existsSync(join(c, 'pkg/api/routes.go'))) return resolve(c);
  throw new Error(`cannot find the openrail source tree (looked in ${candidates.join(', ')}). Set FIFTY_REPO.`);
}
export const ROOT = findRoot();

const outArg = process.argv.indexOf('--out');
const OUT = outArg > 0 ? process.argv[outArg + 1] : join(SITE, 'dist');

function walk(dir) {
  const files = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) files.push(...walk(p));
    else files.push(p);
  }
  return files;
}

// rel turns an absolute site path into one relative to the page at fromSlug,
// so the site works at the domain root and under a GitHub Pages project path.
export function rel(fromSlug, href) {
  if (!href.startsWith('/') || href.startsWith('//')) return href;
  const [pathPart, hash = ''] = href.split('#');
  const fromDir = fromSlug === '' ? '' : fromSlug;
  let target = pathPart.replace(/^\//, '');
  const isFile = /\.[a-z0-9]+$/i.test(target);
  if (!isFile && target !== '' && !target.endsWith('/')) target += '/';
  let r = posix.relative(fromDir || '.', target || '.');
  if (r === '') r = '.';
  if (!isFile && !r.endsWith('/')) r += '/';
  if (r === './') r = './';
  return r + (hash ? `#${hash}` : '');
}

const sidebar = JSON.parse(readFileSync(join(SITE, 'data/sidebar.json'), 'utf8'));

function loadPages() {
  const pages = [];
  const docsDir = join(SITE, 'src/content/docs');
  for (const file of walk(docsDir)) {
    if (!file.endsWith('.md')) continue;
    const src = expand(readFileSync(file, 'utf8'), file);
    const { data, body } = parseFrontMatter(src);
    const slug = 'docs/' + relative(docsDir, file).replace(/\\/g, '/').replace(/\.md$/, '').replace(/\/index$/, '');
    if (!data.title || !data.description) throw new Error(`${file}: front matter needs title and description`);
    pages.push({ slug, title: data.title, navTitle: data.nav || '', description: data.description, body, source: `site/${relative(SITE, file).replace(/\\/g, '/')}` });
  }
  const api = apiPage(ROOT);
  pages.push({ slug: 'docs/reference/api', title: 'API', description: 'The openrail REST API serves everything the console and the CLI do, with the same roles and audit log.', body: api.md, source: 'pkg/api/routes.go' });
  const ss = serviceSettingsPage(ROOT);
  pages.push({ slug: 'docs/reference/service-settings', title: 'Service settings', description: 'Every setting a service has, with its default and the Railway setting it matches.', body: ss.md, source: 'pkg/api/types.go' });
  for (const c of cliPages()) pages.push({ slug: c.slug, title: c.title, description: c.description, body: expand(c.md, c.slug), source: 'site/data/cli.json' });
  const tpl = templatePages(ROOT);
  for (const t of tpl) pages.push({ slug: t.slug, title: t.title, description: t.description, body: expand(t.md, t.slug), source: `templates/${t.id}/template.yaml` });
  if (!sidebar.some((g) => g.label === 'Templates')) {
    const at = sidebar.findIndex((g) => g.label === 'CLI');
    sidebar.splice(at, 0, { label: 'Templates', items: tpl.map((t) => t.slug).sort() });
  }
  return pages;
}

function sidebarHtml(pages, current) {
  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  return sidebar
    .map((g) => {
      const items = g.items
        .map((slug) => {
          const p = bySlug.get(slug);
          if (!p) throw new Error(`sidebar lists ${slug}, which has no page`);
          const cur = slug === current ? ' aria-current="page"' : '';
          return `<li><a href="${rel(current, '/' + slug)}"${cur}>${escapeHtml(p.navTitle || p.title)}</a></li>`;
        })
        .join('');
      const open = g.items.includes(current) || g.open ? ' open' : '';
      return `<details${open}><summary>${escapeHtml(g.label)}</summary><ul>${items}</ul></details>`;
    })
    .join('');
}

// The theme is applied before first paint so nothing flashes (K9).
// Visitor counts: GoatCounter (no cookies, no personal data), on the live
// site only. GOATCOUNTER= (empty) turns it off; a code picks another account.
const GOATCOUNTER = process.env.GOATCOUNTER ?? '5050clooud';
const ANALYTICS = GOATCOUNTER
  ? `<script>if(/^(www\\.)?openrail\\.com$/.test(location.hostname)){var g=document.createElement('script');g.async=true;g.src='https://gc.zgo.at/count.js';g.dataset.goatcounter='https://${GOATCOUNTER}.goatcounter.com/count';document.head.appendChild(g);}</script>`
  : '';
const THEME_BOOT = `<script>(function(){var d=document.documentElement;d.classList.add('js');var t=null;try{t=localStorage.getItem('5050site.theme');}catch(e){}var m=/[?&]theme=(light|dark)/.exec(location.search);if(m)t=m[1];if(t==='light'||t==='dark')d.setAttribute('data-theme',t);})();</script>`;

// The openrail lockup. The word takes the text colour, so it follows the theme.
const LOGO = `<svg class="logo" viewBox="0 0 829.5 182" role="img" aria-hidden="true" focusable="false"><path fill="currentColor" fill-rule="evenodd" d="M56 147.2Q38.4 147.2 25.9 141.2Q13.4 135.2 6.7 122.9Q0 110.6 0 92Q0 73.4 6.7 61.2Q13.4 49 25.9 43Q38.4 37 56 37Q73.8 37 86.3 43Q98.8 49 105.5 61.2Q112.2 73.4 112.2 92Q112.2 110.6 105.5 122.9Q98.8 135.2 86.3 141.2Q73.8 147.2 56 147.2ZM56 123.4Q64.2 123.4 69.3 120.2Q74.4 117 76.7 110.9Q79 104.8 79 95.8V88.4Q79 79.4 76.7 73.2Q74.4 67 69.3 63.9Q64.2 60.8 56 60.8Q48 60.8 42.9 63.9Q37.8 67 35.5 73.2Q33.2 79.4 33.2 88.4V95.8Q33.2 104.8 35.5 110.9Q37.8 117 42.9 120.2Q48 123.4 56 123.4ZM132.2 182V39.4H159L161.8 54H163.2Q168.6 45 176.9 41Q185.2 37 195.4 37Q208.8 37 218.5 43Q228.2 49 233.4 61.2Q238.6 73.4 238.6 92.2Q238.6 110.8 233.4 123Q228.2 135.2 218.6 141.2Q209 147.2 196 147.2Q189.6 147.2 184 145.8Q178.4 144.4 173.9 141.4Q169.4 138.4 166.2 134H165V182ZM185.2 121.6Q192.6 121.6 197.1 118.6Q201.6 115.6 203.6 109.8Q205.6 104 205.6 95.6V88.6Q205.6 80.2 203.6 74.3Q201.6 68.4 197.1 65.5Q192.6 62.6 185.2 62.6Q178 62.6 173.5 65.9Q169 69.2 166.9 75.3Q164.8 81.4 164.8 89.6V94.4Q164.8 100.4 165.9 105.4Q167 110.4 169.5 114Q172 117.6 175.9 119.6Q179.8 121.6 185.2 121.6ZM310.2 147.2Q292.2 147.2 279.6 141.3Q267 135.4 260.3 123.2Q253.6 111 253.6 92Q253.6 73.4 260.2 61.2Q266.8 49 279.2 43Q291.6 37 309 37Q326.2 37 338.1 42.8Q350 48.6 356.2 60.6Q362.4 72.6 362.4 91.6V98.6H286.8Q286.8 107 289.2 112.8Q291.6 118.6 296.6 121.6Q301.6 124.6 310 124.6Q315 124.6 318.8 123.4Q322.6 122.2 325.2 119.9Q327.8 117.6 329.2 114.4Q330.6 111.2 330.6 107.2H362.4Q362.4 116.8 358.9 124.3Q355.4 131.8 348.6 136.9Q341.8 142 332.1 144.6Q322.4 147.2 310.2 147.2ZM287 80.4H329Q329 75.2 327.7 71.3Q326.4 67.4 323.9 64.8Q321.4 62.2 317.9 60.9Q314.4 59.6 309.8 59.6Q302.6 59.6 297.8 62Q293 64.4 290.4 69Q287.8 73.6 287 80.4ZM382.4 144.8V39.4H409.4L411.8 54.4H413.2Q417.2 49 422.4 45Q427.6 41 434.2 39Q440.8 37 448.2 37Q459.6 37 467.5 41.2Q475.4 45.4 479.6 53.8Q483.8 62.2 483.8 75.2V144.8H451V79.8Q451 75.4 449.9 72.3Q448.8 69.2 446.8 67Q444.8 64.8 441.8 63.8Q438.8 62.8 435 62.8Q429.2 62.8 424.8 65.5Q420.4 68.2 417.8 72.7Q415.2 77.2 415.2 83.2V144.8Z"/><path fill="currentColor" fill-rule="evenodd" d="M506.2 39.4H539V144.8H506.2Z"/><path fill="#d5348a" fill-rule="evenodd" d="M543.26 99.68V54.93Q550.69 49.66 559.21 46.06Q574.95 39.4 592.16 39.4L602.7 39.4V69.58L592.16 69.58Q569.6 69.58 553.64 85.53Q547.12 92.05 543.26 99.68Z"/><path fill="currentColor" fill-rule="evenodd" d="M645.3 147.2Q638.7 147.2 632.4 145.9Q626.1 144.6 621 141.3Q615.9 138 612.9 132.2Q609.9 126.4 609.9 117.6Q609.9 106.2 614.7 99Q619.5 91.8 628.6 87.7Q637.7 83.6 650.6 82Q663.5 80.4 679.5 80.4V73.8Q679.5 69.2 677.7 66.1Q675.9 63 672.4 61.3Q668.9 59.6 663.1 59.6Q657.9 59.6 654 60.8Q650.1 62 648.1 64.3Q646.1 66.6 646.1 69.8V71.6H614.1Q613.9 70.6 613.9 69.9Q613.9 69.2 613.9 68Q613.9 58.6 619.8 51.6Q625.7 44.6 636.7 40.8Q647.7 37 663.3 37Q677.9 37 688.8 40.6Q699.7 44.2 706 51.6Q712.3 59 712.3 71V116Q712.3 119.4 714.2 121.4Q716.1 123.4 718.9 123.4H725.7V143.6Q723.5 144.6 718.8 145.9Q714.1 147.2 707.5 147.2Q700.7 147.2 695.6 145.5Q690.5 143.8 687.2 140.7Q683.9 137.6 682.7 133.4H681.3Q677.5 137.4 672.4 140.6Q667.3 143.8 660.6 145.5Q653.9 147.2 645.3 147.2ZM657.5 124.6Q662.3 124.6 666.3 123.2Q670.3 121.8 673.2 119.3Q676.1 116.8 677.8 113.2Q679.5 109.6 679.5 105.4V98.4Q668.1 98.4 659.9 100Q651.7 101.6 647.3 105Q642.9 108.4 642.9 113.6Q642.9 117.4 644.8 119.9Q646.7 122.4 649.9 123.5Q653.1 124.6 657.5 124.6ZM739.1 25.2V0H771.9V25.2ZM739.1 144.8V39.4H771.9V144.8ZM796.7 144.8V0H829.5V144.8Z"/></svg>`;

const OG = existsSync(join(SITE, 'public/og/index.json')) ? JSON.parse(readFileSync(join(SITE, 'public/og/index.json'), 'utf8')) : {};
function ogImage(slug) {
  const key = slug === '' ? 'home' : slug.replace(/^docs\//, '');
  return OG[key] || OG.home || null;
}

export function shell({ slug, title, description, main, head = '', bodyClass = '', sheetExtra = '' }) {
  const r = (h) => rel(slug, h);
  const links = `<a href="${r('/docs')}"${slug.startsWith('docs') ? ' aria-current="true"' : ''}>Docs</a>
    <a href="${r('/#cost')}">Pricing</a>
    <a href="${r('/docs/compare/railway')}">Compare</a>
    <a href="${REPO}" rel="noopener">GitHub</a>`;
  const og = ogImage(slug);
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<meta name="color-scheme" content="light dark">
<link rel="canonical" href="${ORIGIN}/${slug}${slug ? '/' : ''}">
<link rel="icon" href="${r('/favicon.ico')}" sizes="48x48">
<link rel="icon" href="${r('/favicon.svg')}" type="image/svg+xml">
<link rel="apple-touch-icon" href="${r('/apple-touch-icon.png')}">
<link rel="manifest" href="${r('/site.webmanifest')}">
<meta name="theme-color" content="#f6f6f8" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#13111c" media="(prefers-color-scheme: dark)">
<meta property="og:type" content="website">
<meta property="og:site_name" content="openrail">
<meta property="og:title" content="${escapeHtml(title)}">
<meta property="og:description" content="${escapeHtml(description)}">
<meta property="og:url" content="${ORIGIN}/${slug}${slug ? '/' : ''}">
${og ? `<meta property="og:image" content="${ORIGIN}/og/${og}">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta name="twitter:card" content="summary_large_image">` : ''}
<link rel="preload" href="${r('/fonts/inter-latin-wght.woff2')}" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="${r('/fonts/ibm-plex-serif-latin-500.woff2')}" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="${r('/style.css')}">
${THEME_BOOT}
${ANALYTICS}
${head}
</head>
<body class="${bodyClass}" data-root="${r('/')}">
<a class="skip" href="#main">Skip to content</a>
<header class="top">
  <a class="brand" href="${r('/')}" aria-label="openrail home">${LOGO}</a>
  <nav class="nav" aria-label="Main">
    ${links}
  </nav>
  <div class="tools">
    <button class="search-open" type="button" hidden><span>Search docs</span> <kbd>/</kbd></button>
    <button class="theme-toggle" type="button" hidden>Theme</button>
    <a class="nav-action" href="${r('/docs/guides/install')}">Install on a VM</a>
    <button class="menu-open" type="button" aria-expanded="false" aria-controls="sheet">Menu</button>
  </div>
</header>
<div class="sheet" id="sheet" hidden>
  <div class="sheet-head"><a class="brand" href="${r('/')}" aria-label="openrail home">${LOGO}</a><button class="menu-close" type="button">Close</button></div>
  <nav class="sheet-nav" aria-label="Main">${links}</nav>
  <p class="sheet-tools"><button class="search-open" type="button" hidden>Search docs</button> <button class="theme-toggle" type="button" hidden>Theme</button></p>
  ${sheetExtra}
</div>
${main}
<footer class="foot">
  <p class="foot-name"><a class="brand" href="${r('/')}" aria-label="openrail home">${LOGO}</a></p>
  <p class="foot-links"><a href="${REPO}" rel="noopener">GitHub</a> <a href="${r('/docs/quick-start')}">Docs</a> <a href="${REPO}/blob/main/LICENSING.md" rel="noopener">License</a> <a href="${REPO}/blob/main/SECURITY.md" rel="noopener">Security policy</a> <a href="${REPO}/releases/tag/v0.1.0" rel="noopener">Release v0.1.0</a> <a href="${r('/llms.txt')}">llms.txt</a></p>
  <p class="foot-note">AGPL-3.0 server, MIT CLI, API types, templates and installer. Not affiliated with Railway; Railway is a trademark of its owner.</p>
</footer>
<dialog class="search" id="search" aria-label="Search the docs">
  <form method="dialog" class="search-form" role="search">
    <label for="q">Search the docs</label>
    <input id="q" name="q" type="search" autocomplete="off" spellcheck="false" placeholder="custom domain, backups, railway alias">
    <button type="submit" class="search-close">Close</button>
  </form>
  <p class="search-status" aria-live="polite"></p>
  <ol class="search-results"></ol>
</dialog>
<p class="sr-only" aria-live="polite" id="announce"></p>
<script src="${r('/site.js')}" defer></script>
</body>
</html>
`;
}

export const MEDIA = JSON.parse(readFileSync(join(SITE, 'data/media.json'), 'utf8'));

// findDemo is the build-time check for the full demo: the section exists only
// when a finished render was copied into public/demo by
// `node scripts/media.mjs --demo <dir> --reviewed`. A missing or partial file
// leaves the section out; the page never shows a placeholder.
export function findDemo(dir = join(SITE, 'public/demo')) {
  const need = ['openrail-demo.mp4', 'openrail-demo.webm', 'openrail-demo.en.vtt', 'demo.json'];
  if (!need.every((f) => existsSync(join(dir, f)) && statSync(join(dir, f)).size > 0)) return { ready: false };
  // A finished MP4 has its index (moov) written; an encode still running does not.
  const mp4 = readFileSync(join(dir, 'openrail-demo.mp4'));
  if (mp4.indexOf('moov') < 0) return { ready: false };
  const meta = JSON.parse(readFileSync(join(dir, 'demo.json'), 'utf8'));
  let chapters = null;
  if (existsSync(join(dir, 'chapters.vtt'))) {
    const vtt = readFileSync(join(dir, 'chapters.vtt'), 'utf8');
    chapters = [...vtt.matchAll(/(\d\d):(\d\d):(\d\d)\.(\d{3}) --> [^\n]+\n([^\n]+)/g)].map((m) => {
      const start = Number(m[1]) * 3600 + Number(m[2]) * 60 + Number(m[3]) + Number(m[4]) / 1000;
      return { start: start.toFixed(2), time: `${Math.floor(start / 60)}:${String(Math.floor(start % 60)).padStart(2, '0')}`, text: m[5].trim() };
    });
  }
  return { ready: true, duration: meta.duration, poster: meta.poster, chapters };
}

function flatOrder() {
  return sidebar.flatMap((g) => g.items);
}

function docPage(page, pages) {
  const linkFn = (h) => rel(page.slug, h);
  // A line `![alt](shot:networking "caption")` places a console screenshot,
  // light or dark with the theme.
  const shots = [];
  const body = page.body.replace(SHOT_LINE, (m, alt, name, cap1, cap2) => {
    const cap = cap1 || cap2;
    if (!MEDIA.stills[`${name}-light`]) throw new Error(`${page.slug}: no screenshot named ${name}`);
    shots.push({ alt, name, cap });
    return `SHOTMARKER${shots.length - 1}`;
  });
  const rendered = render(body, linkFn);
  const toc = rendered.toc;
  const html = rendered.html.replace(/<p>SHOTMARKER(\d+)<\/p>/g, (m, i) => {
    const x = shots[Number(i)];
    return `<figure class="doc-shot"><div class="shot">${shot(linkFn, MEDIA, x.name, { alt: x.alt, sizes: '(min-width: 1100px) 760px, calc(100vw - 32px)' })}</div>${x.cap ? `<figcaption>${escapeHtml(x.cap)}</figcaption>` : ''}</figure>`;
  });
  const tocItems = toc.filter((t) => t.level === 2);
  const tocHtml = tocItems.length > 2
    ? `<nav class="toc" aria-label="On this page"><p class="toc-title">On this page</p><ul>${tocItems.map((t) => `<li><a href="#${t.id}">${escapeHtml(t.text)}</a></li>`).join('')}</ul></nav>`
    : '<div class="toc" aria-hidden="true"></div>';
  const order = flatOrder();
  const i = order.indexOf(page.slug);
  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  const prev = i > 0 ? bySlug.get(order[i - 1]) : null;
  const next = i >= 0 && i < order.length - 1 ? bySlug.get(order[i + 1]) : null;
  const pager = (prev || next)
    ? `<nav class="pager" aria-label="Previous and next">${prev ? `<a class="prev" href="${linkFn('/' + prev.slug)}"><span>Previous</span> ${escapeHtml(prev.navTitle || prev.title)}</a>` : '<span></span>'}${next ? `<a class="next" href="${linkFn('/' + next.slug)}"><span>Next</span> ${escapeHtml(next.navTitle || next.title)}</a>` : ''}</nav>`
    : '';
  const md = rel(page.slug, `/${page.slug}.md`);
  const side = sidebarHtml(pages, page.slug);
  const main = `<div class="docs">
<nav class="sidebar" aria-label="Docs">${side}</nav>
<main id="main" class="content">
<article>
<h1>${escapeHtml(page.title)}</h1>
${html}
</article>
${pager}
<p class="meta"><a href="${md}">View as Markdown</a> <a href="${REPO}/edit/main/${page.source}" rel="noopener">Edit this page</a></p>
</main>
${tocHtml}
</div>`;
  return shell({ slug: page.slug, title: `${page.title} | openrail docs`, description: page.description, main, bodyClass: 'docs-page', sheetExtra: `<nav class="sheet-docs" aria-label="Docs">${side}</nav>` });
}

// Both `![alt](shot:name "caption")` and `![alt](shot:name) "caption"` are accepted.
const SHOT_LINE = /^!\[([^\]]+)\]\(shot:([a-z-]+)(?: "([^"]+)")?\)(?: "([^"]+)")?$/gm;

function mdExport(page) {
  const abs = page.body.replace(SHOT_LINE, (m, alt, name) => `![${alt}](/media/${name}-light-1600.png)`).replace(/\]\((\/[^)\s]*)\)/g, `](${ORIGIN}$1)`);
  return `# ${page.title}\n\n> ${page.description}\n\n${abs.trim()}\n`;
}

// searchIndex is what the docs search reads: title, path, description,
// headings and the start of the text of every page.
export function searchIndex(pages) {
  const plain = (md) => md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[`*_>#|]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  return pages.map((p) => ({
    t: p.title,
    u: p.slug + '/',
    d: p.description,
    h: [...p.body.matchAll(/^#{2,3} (.+)$/gm)].map((m) => m[1].replace(/[`*]/g, '')),
    x: plain(p.body).slice(0, 700),
  }));
}

function write(path, content) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, content);
}

export function build(out = OUT) {
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  const pages = loadPages();
  const slugs = new Set();
  for (const p of pages) {
    if (slugs.has(p.slug)) throw new Error(`duplicate page ${p.slug}`);
    slugs.add(p.slug);
  }
  for (const f of walk(join(SITE, 'public'))) {
    const dest = join(out, relative(join(SITE, 'public'), f));
    mkdirSync(dirname(dest), { recursive: true });
    copyFileSync(f, dest);
  }
  const installer = join(ROOT, 'install/install.sh');
  if (existsSync(installer)) copyFileSync(installer, join(out, 'install.sh'));
  else throw new Error('install/install.sh is missing; the site must serve /install.sh');
  const cliInstaller = join(ROOT, 'install/install-cli.sh');
  if (existsSync(cliInstaller)) copyFileSync(cliInstaller, join(out, 'install-cli.sh'));

  for (const p of pages) {
    write(join(out, p.slug, 'index.html'), docPage(p, pages));
    write(join(out, `${p.slug}.md`), mdExport(p));
  }
  write(join(out, 'search.json'), JSON.stringify(searchIndex(pages)));

  const bySlug = new Map(pages.map((p) => [p.slug, p]));
  const groupHtml = sidebar.map((g) => `<section class="docs-group"><h2>${escapeHtml(g.label)}</h2><ul>${g.items.map((sl) => { const pg = bySlug.get(sl); return `<li><a href="${rel('docs', '/' + sl)}">${escapeHtml(pg.navTitle || pg.title)}</a><span>${inline(pg.description, (h) => rel('docs', h))}</span></li>`; }).join('')}</ul></section>`).join('');
  const indexMain = `<main id="main" class="docs-home"><h1>Documentation</h1><p class="lead">Install openrail on a VM, deploy your first service, and look up every setting, command and endpoint. Every page is also plain Markdown, listed in <a href="${rel('docs', '/llms.txt')}">llms.txt</a>.</p><p class="docs-home-start"><a class="btn primary" href="${rel('docs', '/docs/quick-start')}">Read the quick start</a> <a href="${rel('docs', '/docs/coming-from-railway')}">Coming from Railway</a> <a href="${rel('docs', '/docs/reference/honest-limits')}">Honest limits</a></p><div class="docs-groups">${groupHtml}</div></main>`;
  write(join(out, 'docs/index.html'), shell({ slug: 'docs', title: 'openrail docs', description: 'Documentation for openrail: install, set up, deploy, and reference for the CLI and API.', main: indexMain }));

  const claims = JSON.parse(readFileSync(join(SITE, 'claims.json'), 'utf8'));
  const pricing = JSON.parse(readFileSync(join(SITE, 'data/pricing.json'), 'utf8'));
  const benchPath = join(SITE, 'data/benchmark.json');
  const bench = existsSync(benchPath) ? JSON.parse(readFileSync(benchPath, 'utf8')) : null;
  const reportPath = process.env.REPORT_JSON || join(SITE, 'data/report.json');
  const report = existsSync(reportPath) ? JSON.parse(readFileSync(reportPath, 'utf8')) : null;
  write(join(out, 'index.html'), landing({ shell, rel, claims, pricing, bench, media: MEDIA, report, demo: findDemo() }));
  write(join(out, 'deploy/index.html'), deployPage({ shell, rel }));
  write(join(out, '404.html'), shell({ slug: '', title: 'Page not found | openrail', description: 'This page does not exist on the openrail site.', main: '<main id="main" class="content narrow"><h1>Page not found</h1><p>This page does not exist. Try the <a href="/docs/quick-start/">Quick start</a> or the <a href="/">home page</a>.</p></main>' }));

  // llms.txt: an index, grouped like the sidebar. llms-full.txt: every page.
  const lines = ['# openrail', '', "> Railway's workflow on your own servers. Open source and self-hosted: one command turns any Linux VM into a canvas of services with git deploys, databases, logs and rollbacks. You pay only for the VM.", ''];
  for (const g of sidebar) {
    lines.push(`## ${g.label}`, '');
    for (const s of g.items) {
      const p = bySlug.get(s);
      lines.push(`- [${p.title}](${ORIGIN}/${s}.md): ${p.description}`);
    }
    lines.push('');
  }
  write(join(out, 'llms.txt'), lines.join('\n'));
  write(join(out, 'llms-full.txt'), sidebar.flatMap((g) => g.items.map((s) => mdExport(bySlug.get(s)))).join('\n---\n\n'));
  const urls = ['', 'deploy', 'docs', ...pages.map((p) => p.slug)];
  write(join(out, 'sitemap.xml'), `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls.map((u) => `<url><loc>${ORIGIN}/${u}${u ? '/' : ''}</loc></url>`).join('\n')}\n</urlset>\n`);
  write(join(out, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${ORIGIN}/sitemap.xml\n`);
  return { pages, out };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  const { pages, out } = build();
  console.log(`Built ${pages.length} docs pages into ${relative(process.cwd(), out) || out}`);
}
