import {readFile, writeFile} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';

// Lists Go modules used by the application and the locked UI build packages.
const goMod = await readFile('go.mod', 'utf8');
const goVersion = goMod.match(/^go\s+(\S+)/m)[1];
const goModules = [...new Set(execFileSync('go', ['list', '-deps', '-f', '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}', '.'], {encoding:'utf8'}).split(/\r?\n/).map(line => line.trim()).filter(Boolean))].sort().join('\n');
const lock = JSON.parse(await readFile('package-lock.json', 'utf8'));
const packages = Object.entries(lock.packages).filter(([name]) => name).map(([name, pkg]) => `${name.replace(/^node_modules\//, '')} ${pkg.version}${pkg.dev ? ' [dev]' : ''}${pkg.optional ? ' [optional/platform-specific]' : ''}`).sort();
const text = `Квиз-Дуэль: зависимости и версии

Проект написан на Go и JavaScript. Python не используется.
Этот файл является описанием зависимостей для хакатона, а не входом pip install.
Машиночитаемые источники: go.mod, go.sum, package.json, package-lock.json.

Среда:
- Go ${goVersion} (сборочный Docker-образ golang:${goVersion})
- Debian bookworm-slim (runtime); точный образ и пакеты: dist/IMAGE.json
- Docker Engine с Linux-контейнерами; Compose v2 или новее
- Node.js 24.15.0 (нужен только для пересборки UI)
- CGO_ENABLED=0; отдельный сервер SQLite/PostgreSQL/MySQL не требуется

Go: модули, используемые при сборке приложения, включая косвенные зависимости
${goModules}

JavaScript: все пакеты package-lock.json, включая dev и платформенные варианты
${packages.join('\n')}

Шрифты:
- Press Start 2P 3.000: web/brand/press-start-2p.ttf; лицензия web/brand/PRESS-START-OFL.txt
- Open Sans 3.003: web/brand/open-sans.ttf; лицензия web/brand/OPEN-SANS-OFL.txt
Файлы шрифтов включены в исходники и Docker-образ; SHA-256: dist/IMAGE.json.
- Roboto: @fontsource/roboto, версия указана выше.

Внешний API: MAX Bot API, через версионированный Go SDK выше.
Внешний клиентский скрипт: https://st.max.ru/js/max-web-app.js (предоставляется MAX без закреплённой версии).
Лицензии встроенных UI-библиотек: web/vendor/LICENSES.txt.
`;
await writeFile('requirements.txt', text);
