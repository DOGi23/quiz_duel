import {build} from 'esbuild';
import {mkdir, copyFile, readFile, writeFile} from 'node:fs/promises';

await mkdir('web/vendor', {recursive: true});
await build({
  entryPoints: ['web/material.js'],
  outfile: 'web/vendor/material.js',
  bundle: true,
  minify: true,
  format: 'esm',
  target: ['chrome100', 'safari16'],
  legalComments: 'eof',
});
const icons = ['arrow_back', 'group', 'sports_esports', 'leaderboard', 'school',
  'person_add', 'bolt', 'play_arrow', 'replay', 'check', 'close', 'emoji_events',
  'handshake', 'trending_up', 'calculate', 'science', 'code', 'history_edu',
  'public', 'biotech', 'menu_book', 'info', 'verified_user', 'edit', 'delete',
  'settings', 'history', 'flag', 'visibility', 'visibility_off', 'wifi_off', 'image'];
let symbols = '';
for (const name of icons) {
  const svg = await readFile('node_modules/@material-design-icons/svg/outlined/' + name + '.svg', 'utf8');
  symbols += svg.replace(/<svg[^>]*>/, '<symbol id="' + name + '" viewBox="0 0 24 24">').replace('</svg>', '</symbol>');
}
await writeFile('web/vendor/icons.svg', '<svg xmlns="http://www.w3.org/2000/svg">' + symbols + '</svg>');
let fonts = '';
for (const subset of ['cyrillic', 'latin']) {
  for (const weight of [400, 500, 700]) {
    const file = 'roboto-' + subset + '-' + weight + '-normal.woff2';
    await copyFile('node_modules/@fontsource/roboto/files/' + file, 'web/vendor/' + file);
  }
  // The package CSS supplies the correct unicode ranges and font metadata.
  for (const weight of [400, 500, 700]) {
    const css = await readFile('node_modules/@fontsource/roboto/' + weight + '.css', 'utf8');
    fonts += (css.match(/@font-face\s*\{[^}]*\}/g) || [])
      .filter(block => block.includes('/roboto-' + subset + '-' + weight + '-'))
      .join('\n').replaceAll('./files/', './')
      .replace(/, url\([^)]*\.woff\) format\('woff'\)/g, '');
  }
}
// Only weights used by this app are shipped.
fonts = fonts.replace(/@font-face\s*\{[^}]*\}/g, block =>
  /font-weight: (400|500|700);/.test(block) ? block : '');
await writeFile('web/vendor/fonts.css', fonts);
const notices = [];
for (const pkg of ['@material/web', 'lit', 'lit-html', 'lit-element', '@lit/reactive-element', '@material-design-icons/svg', '@fontsource/roboto']) {
  for (const file of ['LICENSE', 'LICENSE.txt']) {
    try {
      notices.push(pkg + '\n' + await readFile('node_modules/' + pkg + '/' + file, 'utf8'));
      break;
    } catch { /* Some packages use LICENSE.txt. */ }
  }
}
await writeFile('web/vendor/LICENSES.txt', notices.join('\n\n'));
