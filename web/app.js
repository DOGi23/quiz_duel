'use strict';

let currentUser = { id: 0, role: 'student' };
let currentChatId = 0;
let currentChat = null;
let authData = '';
let startParam = '';
let chatMembers = [];
let currentDuel = null;
let activeQuiz = null;
let currentRoundNum = 1;
let activeQuizQIndex = 0;
let screen = 'home';
let timerInterval = null;
let pollTimeout = null;
let transitionTimeout = null;
let lobbyTimeout = null;
let generation = 0;
let answerPending = false;
let actionPending = false;
let displayedRound = '';
let questionViewportKey = '';
let questionViewportPending = '';
let serverOffset = 0;
let currentCategories = [];
let editingSubjectId = '';
let subjectImageURL = '', subjectImageTask = 0, subjectImageBusy = false, subjectSaving = false;
let lastFinishedDuelId = '';
let lobbyDuels = [];
let knownInvitations = new Set();
let lobbyLoaded = false;
let readinessPending = false;
let duelMutation = 0;
let loadAttemptKey = '';
let preferences = {theme: 'dark', palette: 'hackathon', font: '1', haptics: true};
let initialized = false, initializing = false, listenersBound = false;
let connectionLost = false, reconnectTimer = null, reconnectDelay = 1000, reconnecting = false;
let questionCatalog = [], editingQuestionId = '', historyOffset = 0, historyPending = false;
let reportTarget = null;
let loadingDuelId = '', loadingQuizId = '';
let catalogLimit = 6;
let gameSettingsRequest = 0;
let gameSettingsSaving = false;

const el = id => document.getElementById(id);
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fullName = user => `${user?.first_name || ''} ${user?.last_name || ''}`.trim() || user?.username || 'Игрок';
const serverNow = () => Date.now() + serverOffset;
function materialIcon(name, slot = '') {
  return `<md-icon${slot ? ` slot="${slot}"` : ''} aria-hidden="true"><svg viewBox="0 0 24 24"><use href="vendor/icons.svg#${name}"></use></svg></md-icon>`;
}
async function showMessage(text, title = 'Квиз-Дуэль', confirm = false) {
  const dialog = el('messageDialog');
  if (dialog.open) await dialog.close();
  el('messageTitle').textContent = title;
  el('messageText').textContent = text;
  el('messageCancel').classList.toggle('hidden', !confirm);
  el('messageConfirm').textContent = confirm ? 'Удалить' : 'Понятно';
  dialog.returnValue = '';
  await dialog.show();
  await new Promise(resolve => dialog.addEventListener('closed', resolve, {once: true}));
  return dialog.returnValue === 'confirm';
}

function defaultAvatarData(name = 'Игрок') {
  const initials = String(name).trim().split(/\s+/).slice(0, 2).map(part => Array.from(part)[0] || '').join('').toUpperCase();
  return 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect fill="#29113e" width="100" height="100"/><text x="50" y="52" dominant-baseline="middle" text-anchor="middle" fill="#efcfff" font-family="Arial,sans-serif" font-size="32">' + escapeHTML(initials) + '</text></svg>');
}

function avatarURL(value, name) {
  try { const url = new URL(value); if (url.protocol === 'https:') return url.href; } catch (_) { /* use default */ }
  return defaultAvatarData(name);
}

async function api(path, body, method) {
  const headers = {};
  if (authData) headers.Authorization = `Bearer ${authData}`;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let response;
  try { response = await fetch(path, { method: method || (body === undefined ? 'GET' : 'POST'), headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(12000) }); }
  catch (_) { setConnection(false); const error = new Error('Связь прервалась. Проверяем подключение…'); error.network = true; throw error; }
  setConnection(true);
  const stamp = Number(response.headers.get('X-Server-Time'));
  if (stamp > 0) serverOffset = stamp - Date.now();
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error || `Ошибка сервера (${response.status})`);
  if (data === null) { setConnection(false); const error = new Error('Не удалось прочитать состояние сервера. Восстанавливаем связь…'); error.network = true; throw error; }
  return data;
}

function showError(error) { el('antiAbuseStatus').textContent = error.message || String(error); el('antiAbuseStatus').classList.add('status-error'); }
function notifyError(error) { showError(error); void showMessage(error.message || String(error), 'Не удалось выполнить действие'); }

function triggerHaptic(type) {
  if (!preferences.haptics) return;
  const h = window.WebApp?.HapticFeedback;
  if (!h) return;
  try { if (type === 'impact') h.impactOccurred('medium'); else h.notificationOccurred(type); } catch (_) { /* optional bridge feature */ }
}

async function initializeApp() {
  if (initializing || initialized) return;
  initializing = true;
  loadPreferences();
  if (!listenersBound) {
    listenersBound = true;
    el('mainTabs').addEventListener('change', event => {
      if (event.target !== el('mainTabs')) return;
      const tab = el('mainTabs').activeTab;
      if (tab) switchScreen(tab.id.replace('nav-', ''));
    });
    el('teacherTabs').addEventListener('change', event => {
      if (event.target !== el('teacherTabs')) return;
      const tab = el('teacherTabs').activeTab;
      if (tab) switchTeacherTab(tab.id.replace('teacher-tab-', ''));
    });
    window.addEventListener('offline', () => setConnection(false));
    window.addEventListener('online', () => reconnectNow());
    document.addEventListener('visibilitychange', () => { if (!document.hidden) void reconnectNow(); });
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyPreferences);
    bindSwipeNavigation();
  }
  try {
    const bridge = window.WebApp;
    try { bridge?.ready?.(); bridge?.expand?.(); bridge?.BackButton?.onClick(handleBackNavigation); } catch (_) { /* optional */ }
    authData = bridge?.initData || new URLSearchParams(location.hash.slice(1)).get('WebAppData') || '';
    if (!authData) throw new Error('Приложение работает только внутри MAX. Откройте его кнопкой в чате бота.');
    const data = await api('/api/auth/validate', { initData: authData });
    currentUser = data.user;
    loadPreferences();
    currentChatId = data.chat_id || 0;
    currentChat = data.chat;
    startParam = data.start_param || '';
    updateUserUI();
    updateChatUI();
    await loadCategories();
    await setupHashRouting();
    if (currentChatId) {
      await Promise.all([loadChatMembers(), loadLeaderboard(), loadGameSettings()]);
      refreshLobby();
    } else {
      showError(new Error('Откройте Арену кнопкой бота в групповом чате.'));
    }
    initialized = true;
  } catch (error) {
    showError(error);
    if (!error.network) { el('connectionText').textContent = error.message; el('connectionRetry').classList.remove('hidden'); }
  } finally { initializing = false; }
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', initializeApp, {once: true});
else void initializeApp();

function updateUserUI() {
  el('userName').textContent = fullName(currentUser);
  el('homeTitle').textContent = currentUser.first_name ? `Твой ход, ${currentUser.first_name}` : 'Твой ход';
  el('userBadge').textContent = currentUser.role === 'teacher' ? 'Учитель' : 'Игрок';
  el('userLevel').textContent = `Уровень ${currentUser.level || 1}`;
  el('userXP').textContent = `${currentUser.xp || 0} XP`;
  el('userAvatar').src = avatarURL(currentUser.photo_url, fullName(currentUser));
  el('nav-teacher').classList.toggle('hidden', currentUser.role !== 'teacher');
  el('nav-teacher').disabled = currentUser.role !== 'teacher';
}

function updateChatUI() {
  const title = currentChat?.title || (currentChatId ? `Чат ${currentChatId}` : 'Откройте Арену из группы');
  el('chatContextSub').textContent = title;
  el('leaderboardSubheading').textContent = `${title} · ОБЩИЙ ПРОГРЕСС`;
  el('analyticsSubtitle').textContent = `Отчёт по ответам: ${title}`;
  el('homeChatTitle').textContent = title;
  el('teacherChatTitle').textContent = title;
  el('membersChatHint').textContent = title;
}

async function loadUserProfile() {
  try { currentUser = await api(`/api/me?chat_id=${currentChatId}`); updateUserUI(); } catch (error) { showError(error); }
}

function stopGameWork() {
  generation++;
  clearInterval(timerInterval);
  clearTimeout(pollTimeout);
  clearTimeout(transitionTimeout);
  displayedRound = '';
  answerPending = false;
  readinessPending = false;
  loadAttemptKey = '';
}

function switchScreen(name) {
  if (['teacher', 'reports'].includes(name) && currentUser.role !== 'teacher') return;
  if (!el(`screen-${name}`)) return;
  if (screen === 'duel' && name !== 'duel' && ['preparing', 'countdown'].includes(currentDuel?.status)) {
    void api('/api/duel/unready', {duel_id: currentDuel.id, round_num: currentDuel.current_round}).catch(() => {});
  }
  if (screen !== name) { stopGameWork(); questionViewportKey = ''; questionViewportPending = ''; }
  screen = name;
  document.body.dataset.screen = name;
  updateDuelAlert();
  document.querySelectorAll('.screen').forEach(item => item.classList.remove('active'));
  el(`screen-${name}`).classList.add('active');
  const tab = el(`nav-${name}`);
  if (tab) el('mainTabs').activeTabIndex = Array.from(el('mainTabs').children).indexOf(tab);
  try { if (name === 'home') window.WebApp?.BackButton?.hide(); else window.WebApp?.BackButton?.show(); } catch (_) { /* optional */ }
  if (name === 'leaderboard') loadLeaderboard();
  if (name === 'teacher') { loadTeacherAnalytics(); void loadGameSettings(); }
  if (name === 'home') void loadGameSettings();
  if (name === 'teacher' && el('tab-teacher-constructor').classList.contains('active')) void loadQuestions();
  if (name === 'members') loadChatMembers();
  window.scrollTo({ top: 0, behavior: 'auto' });
}

function handleBackNavigation() { switchScreen('home'); }

function navigateBySwipe(direction) {
  if (!initialized || !currentChatId || Array.from(document.querySelectorAll('md-dialog, md-outlined-select')).some(control => control.open)) return false;
  const pages = ['home', 'leaderboard', 'members'];
  if (currentUser.role === 'teacher') pages.push('teacher');
  const index = pages.indexOf(screen);
  const previous = {settings: 'home', history: 'home', review: 'history', reports: 'teacher', 'quiz-result': 'home'};
  const next = index >= 0 ? pages[index + (direction === 'next' ? 1 : -1)] : direction === 'previous' ? previous[screen] : null;
  if (!next) return false;
  if (next === 'history') openHistory();
  else switchScreen(next);
  return true;
}

function swipeStartsOnControl(event) {
  return event.composedPath().some(node => {
    if (!node.localName) return false;
    if (['input', 'textarea', 'select', 'form', 'md-outlined-text-field', 'md-outlined-select', 'md-radio', 'md-switch', 'md-tabs', 'md-dialog'].includes(node.localName) || node.isContentEditable) return true;
    // Leave horizontally scrolling content, including Material tab bars, alone.
    return node.scrollWidth > node.clientWidth + 2 && /auto|scroll/.test(getComputedStyle(node).overflowX);
  });
}

function bindSwipeNavigation() {
  const area = document.querySelector('.content-area');
  let gesture = null, suppressClickUntil = 0;
  area.addEventListener('touchstart', event => {
    gesture = null;
    if (event.touches.length !== 1 || swipeStartsOnControl(event)) return;
    const touch = event.touches[0];
    // Keep the screen edges available to the MAX client and system gestures.
    if (touch.clientX < 24 || touch.clientX > window.innerWidth - 24) return;
    gesture = {id: touch.identifier, x: touch.clientX, y: touch.clientY, time: Date.now(), screen};
  }, {passive: true});
  area.addEventListener('touchmove', event => {
    if (!gesture) return;
    const touch = Array.from(event.touches).find(t => t.identifier === gesture.id);
    if (event.touches.length !== 1 || !touch) { gesture = null; return; }
    const dx = touch.clientX - gesture.x, dy = touch.clientY - gesture.y;
    if (Math.abs(dy) > 20 && Math.abs(dy) > Math.abs(dx)) gesture = null;
  }, {passive: true});
  area.addEventListener('touchcancel', () => { gesture = null; }, {passive: true});
  area.addEventListener('touchend', event => {
    const start = gesture;
    gesture = null;
    if (!start || event.touches.length || screen !== start.screen || Date.now() - start.time > 700) return;
    const touch = Array.from(event.changedTouches).find(t => t.identifier === start.id);
    if (!touch) return;
    const dx = touch.clientX - start.x, dy = touch.clientY - start.y;
    if (Math.abs(dx) < 64 || Math.abs(dy) > 72 || Math.abs(dx) < Math.abs(dy) * 1.7) return;
    if (navigateBySwipe(dx < 0 ? 'next' : 'previous')) suppressClickUntil = Date.now() + 400;
  }, {passive: true});
  area.addEventListener('click', event => {
    if (Date.now() < suppressClickUntil) { event.preventDefault(); event.stopImmediatePropagation(); }
  }, {capture: true});
}

async function setupHashRouting() {
  const route = startParam || location.hash.slice(1);
  if (route.startsWith('duel-')) {
    // An open invitation is visible in the lobby; its questions require acceptance.
    const list = await api(`/api/duel/list?chat_id=${currentChatId}`);
    const duel = list.find(item => item.id === route);
    if (duel && (duel.status !== 'waiting' || duel.player1.id === currentUser.id)) await openDuel(route);
    else if (duel) { switchScreen('home'); showError(new Error('Примите вызов в списке дуэлей ниже.')); }
    else await openDuel(route);
  } else if (route.startsWith('quiz-')) {
    await openQuiz(route);
  } else if (['leaderboard', 'teacher', 'members'].includes(route)) {
    switchScreen(route);
  } else await resumeActiveDuel();
}

async function loadCategories() {
  if (!currentChatId) return;
  const categories = await api(`/api/categories?chat_id=${currentChatId}`);
  currentCategories = categories;
  for (const id of ['expressSubject', 'formCategory']) {
    const select = el(id);
    const previous = select.value;
    select.replaceChildren();
    const available = categories.filter(c => id !== 'expressSubject' || c.ready !== false);
    const selected = available.some(c => c.id === previous) ? previous : available[0]?.id || '';
    for (const category of categories) {
      const option = document.createElement('md-select-option');
      option.value = category.id;
      option.disabled = id === 'expressSubject' && category.ready === false;
      option.selected = category.id === selected;
      const label = document.createElement('span');
      label.slot = 'headline';
      label.textContent = category.name + (category.ready === false ? ' · черновик' : '');
      option.append(label);
      select.append(option);
    }
    await select.updateComplete;
    select.value = selected;
    select.disabled = !available.length;
  }
  el('expressLaunchButton').disabled = !categories.some(c => c.ready !== false);
  el('subjectsGrid').replaceChildren();
  el('subjectsCount').textContent = String(categories.length);
  for (const category of categories) {
    const button = document.createElement('md-list-item');
    button.type = 'button';
    button.disabled = category.ready === false;
    button.className = 'subject-tile';
    button.innerHTML = `<span slot="headline">${escapeHTML(category.name)}</span><span slot="supporting-text">${escapeHTML(category.ready === false ? subjectAvailability(category) : category.description || 'Проверить себя в дуэли')}</span>`;
    appendSubjectArtwork(button, category);
    button.onclick = () => startSubjectDuel(category.id);
    el('subjectsGrid').append(button);
  }
  if (!categories.length) el('subjectsGrid').textContent = 'Учитель скоро добавит предметы для этой группы.';
  renderSubjects();
  updateSubjectRequirementHint();
  updateQuestionSubjectFilter();
}

async function startQuickDuel(opponentId = 0, category = '') {
  if (actionPending || !currentUser.id) return;
  actionPending = true;
  try {
    const duel = await api('/api/duel/create', { chat_id: currentChatId, player2_id: opponentId, category });
    await openDuel(duel.id);
    if (duel.notification_warning) void showMessage(duel.notification_warning, 'Поиск соперника');
  } catch (error) { notifyError(error); } finally { actionPending = false; }
}
function startSubjectDuel(category) { return startQuickDuel(0, category); }
function showOpponentPicker() { switchScreen('members'); }

async function requestRematch() {
  if (actionPending || !lastFinishedDuelId) return;
  actionPending = true;
  el('rematchButton').disabled = true;
  try {
    const duel = await api('/api/duel/rematch', {duel_id: lastFinishedDuelId});
    await openDuel(duel.id);
    if (duel.notification_warning) void showMessage(duel.notification_warning, 'Реванш');
  } catch (error) { notifyError(error); }
  finally { actionPending = false; el('rematchButton').disabled = false; }
}

function updateDuelAlert() {
  const incoming = lobbyDuels.filter(d => d.status === 'waiting' && d.player1.id !== currentUser.id);
  el('duelAlert').classList.toggle('hidden', !incoming.length || ['home', 'duel', 'quiz'].includes(screen));
  el('duelAlertText').textContent = `Входящих вызовов: ${incoming.length} · Открыть`;
  const rematch = incoming.find(d => d.rematch_of === lastFinishedDuelId);
  el('rematchLabel').textContent = rematch ? 'Принять реванш' : 'Реванш';
}

async function refreshLobby() {
  clearTimeout(lobbyTimeout);
  try {
    const list = await api(`/api/duel/list?chat_id=${currentChatId}`);
    lobbyDuels = list;
    const incoming = list.filter(d => d.status === 'waiting' && d.player1.id !== currentUser.id);
    if (lobbyLoaded && incoming.some(d => !knownInvitations.has(d.id))) triggerHaptic('success');
    knownInvitations = new Set(incoming.map(d => d.id));
    lobbyLoaded = true;
    updateDuelAlert();
    el('duelInvites').classList.toggle('hidden', !list.length);
    const lobbyTitle = incoming.length ? `Ищут соперника: ${incoming.length}` : 'Ваши дуэли';
    if (el('duelLobbyTitle').textContent !== lobbyTitle) el('duelLobbyTitle').textContent = lobbyTitle;
    const container = el('duelLobby');
    container.replaceChildren();
    for (const duel of [...list].sort((a,b) => Number(b.player1.id !== currentUser.id) - Number(a.player1.id !== currentUser.id))) {
      const row = document.createElement('md-list-item');
      const text = document.createElement('span');
      text.className = 'leader-details';
      text.slot = 'headline';
      const canAccept = duel.status === 'waiting' && duel.player1.id !== currentUser.id;
      text.textContent = canAccept ? `${fullName(duel.player1)} ${duel.rematch_of ? 'предлагает реванш' : 'ищет соперника'}` : duel.status === 'waiting' ? 'Ваш вызов — ждём соперника' : 'Ваша дуэль продолжается';
      row.insertAdjacentHTML('afterbegin', materialIcon('sports_esports', 'start'));
      row.append(text);
      const details = document.createElement('span');
      details.slot = 'supporting-text';
      details.textContent = duel.player2 ? `${fullName(duel.player1)} · ${fullName(duel.player2)}` : `Любой участник группы · Вопросов: ${duel.question_count}, по 15 секунд`;
      row.append(details);
      const button = document.createElement(canAccept ? 'md-filled-button' : 'md-text-button');
      button.type = 'button';
      button.slot = 'end';
      button.textContent = canAccept ? 'Принять' : 'Продолжить';
      button.onclick = async () => {
        button.disabled = true;
        try {
          if (canAccept) await api('/api/duel/accept', { duel_id: duel.id, chat_id: currentChatId });
          await openDuel(duel.id);
        } catch (error) { notifyError(error); } finally { button.disabled = false; }
      };
      const actions = document.createElement('div'); actions.slot = 'end'; actions.className = 'invite-actions'; button.removeAttribute('slot'); actions.append(button);
      if (canAccept) {
        const decline = document.createElement('md-text-button'); decline.type = 'button'; decline.textContent = duel.player2 ? 'Отклонить' : 'Скрыть';
        decline.onclick = async () => { decline.disabled = true; try { await api('/api/duel/decline', {duel_id: duel.id, chat_id: currentChatId}); await refreshLobby(); } catch (error) { notifyError(error); } finally { decline.disabled = false; } };
        actions.append(decline);
      }
      row.append(actions);
      if (duel.status === 'waiting') { const until = Math.max(0, Math.ceil((Date.parse(duel.expires_at) - serverNow())/60000)); details.textContent += ` · ещё ${until} мин`; }
      container.append(row);
    }
  } catch (error) { showError(error); }
  lobbyTimeout = setTimeout(refreshLobby, 4000);
}

async function openDuel(id) {
  const refreshing = screen === 'duel' && currentDuel?.id === id;
  loadingDuelId = id;
  stopGameWork();
  if (!refreshing) {
    currentDuel = null;
    switchScreen('duel');
    el('questionText').textContent = 'Загрузка дуэли…';
    el('answersGrid').replaceChildren();
  }
  const token = generation;
  try {
    const duel = await api(`/api/duel/get?id=${encodeURIComponent(id)}`);
    if (token !== generation) return;
    currentDuel = duel;
    renderDuelState();
    pollDuel(token);
  } catch (error) { if (token === generation) { if (error.network) el('questionText').textContent = 'Восстанавливаем связь с дуэлью…'; else { notifyError(error); switchScreen('home'); } } }
}

function pollDuel(token) {
  if (token !== generation || screen !== 'duel' || !currentDuel) return;
  clearTimeout(pollTimeout);
  pollTimeout = setTimeout(async () => {
    try {
      if (token !== generation || screen !== 'duel' || !currentDuel || answerPending || readinessPending) return;
      const mutation = duelMutation;
      const duel = await api(`/api/duel/get?id=${encodeURIComponent(currentDuel.id)}`);
      if (token !== generation || mutation !== duelMutation || answerPending || readinessPending) return;
      currentDuel = duel;
      renderDuelState();
    } catch (error) { if (token === generation) showError(error); }
    finally {
      // Ready/loaded can begin while GET is in flight. Discard that stale
      // response, but always keep the room's polling loop alive.
      if (token === generation && screen === 'duel' && currentDuel) pollDuel(token);
    }
  }, ['preparing', 'countdown'].includes(currentDuel?.status) ? 400 : 1000);
}

async function acknowledgeDuelLoad() {
  if (readinessPending || currentDuel?.status !== 'preparing') return;
  const token = generation, id = currentDuel.id, round = currentDuel.current_round;
  loadAttemptKey = `${id}:${round}`;
  duelMutation++;
  readinessPending = true;
  try {
    await el('readyButton').updateComplete;
    await document.fonts.ready;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    if (token !== generation || screen !== 'duel') return;
    const duel = await api('/api/duel/loaded', {duel_id: id, round_num: round});
    if (token !== generation) return;
    currentDuel = duel;
  } catch (error) {
    if (token === generation) el('preparationHint').textContent = 'Не удалось подтвердить загрузку. Нажмите «Повторить загрузку».';
  } finally {
    if (token === generation) { readinessPending = false; renderDuelState(); }
  }
}

async function confirmDuelReady() {
  if (readinessPending || currentDuel?.status !== 'preparing') return;
  const round = currentDuel.rounds[currentDuel.current_round - 1];
  const loaded = currentDuel.player1.id === currentUser.id ? round.player1_loaded : round.player2_loaded;
  if (!loaded) { await acknowledgeDuelLoad(); return; }
  const token = generation;
  duelMutation++;
  readinessPending = true;
  el('readyButton').disabled = true;
  try {
    const duel = await api('/api/duel/ready', {duel_id: currentDuel.id, round_num: currentDuel.current_round});
    if (token === generation) currentDuel = duel;
  } catch (error) { if (token === generation) notifyError(error); }
  finally {
    if (token === generation) { readinessPending = false; renderDuelState(); }
  }
}

function renderPreparation(duel) {
  const round = duel.rounds[duel.current_round - 1];
  const mineFirst = duel.player1.id === currentUser.id;
  const loaded = mineFirst ? round.player1_loaded : round.player2_loaded;
  const ready = mineFirst ? round.player1_ready : round.player2_ready;
  const automatic = duel.current_round > 1;
  el('duelRoundIndicator').textContent = `Раунд ${duel.current_round} из ${duel.rounds.length}`;
  el('preparationTitle').textContent = duel.status === 'countdown' ? 'Оба готовы. Начинаем!' : 'Подготовка к раунду';
  for (const [prefix, user] of [['1', duel.player1], ['2', duel.player2]]) {
    el(`readyP${prefix}Name`).textContent = fullName(user) + (user.id === currentUser.id ? ' · Вы' : '');
    el(`readyP${prefix}Status`).textContent = round[`player${prefix}_ready`] ? 'Готов' : round[`player${prefix}_loaded`] ? automatic ? 'Загрузился' : 'Загрузился · ждём «Готов»' : 'Загружается';
  }
  const countdown = duel.status === 'countdown';
  el('duelCountdown').classList.toggle('hidden', !countdown);
  const loadFailed = !loaded && !readinessPending && loadAttemptKey === `${duel.id}:${duel.current_round}`;
  el('readyButton').classList.toggle('hidden', countdown || automatic && !loadFailed);
  el('readyButton').disabled = readinessPending || ready;
  el('readyButton').textContent = ready ? 'Вы готовы' : loaded ? 'Готов' : readinessPending ? 'Загрузка…' : 'Повторить загрузку';
  if (loaded || readinessPending || ready || countdown || automatic && !loadFailed) {
    el('preparationHint').textContent = countdown ? 'Вопрос и общий таймер откроются после отсчёта.' : automatic ? 'Следующий раунд начнётся автоматически после загрузки экранов обоих игроков.' : ready ? 'Ждём готовности соперника. Вопрос пока скрыт.' : 'Нажмите «Готов», когда готовы играть. Вопрос пока скрыт.';
  }
  if (countdown) {
    const key = `${duel.id}:${duel.current_round}:countdown:${round.starts_at}`;
    if (displayedRound !== key) {
      displayedRound = key;
      clearInterval(timerInterval);
      const tick = () => {
        const seconds = Math.max(0, Math.ceil((Date.parse(round.starts_at) - serverNow()) / 1000));
        const text = seconds ? String(seconds) : 'Старт';
        if (el('duelCountdown').textContent !== text) el('duelCountdown').textContent = text;
        if (!seconds) { clearInterval(timerInterval); void openDuel(duel.id); }
      };
      timerInterval = setInterval(tick, 100);
      tick();
    }
  } else {
    clearInterval(timerInterval);
    if (!loaded && loadAttemptKey !== `${duel.id}:${duel.current_round}`) void acknowledgeDuelLoad();
  }
}

function renderDuelState() {
  const duel = currentDuel;
  if (!duel) return;
  if (duel.status === 'declined') { void showMessage(closureText(duel), 'Приглашение закрыто'); switchScreen('home'); return; }
  if (duel.status === 'finished') { showResultScreen(); return; }
  const waiting = duel.status === 'waiting';
  el('duelReport').classList.add('hidden');
  const preparing = ['preparing', 'countdown'].includes(duel.status);
  el('duelCategorySeparator').classList.toggle('hidden', waiting || preparing);
  document.body.dataset.waiting = String(waiting || preparing);
  el('duelPreparation').classList.toggle('hidden', !preparing);
  el('duelWaiting').classList.toggle('hidden', !waiting);
  el('duelWaitingTitle').textContent = duel.rematch_of ? 'Ждём согласия на реванш' : duel.player2 ? 'Ждём соперника' : 'Ищем соперника';
  el('duelWaitingText').textContent = duel.player2 ? `Приглашение отправляется в группу. Принять его может только ${fullName(duel.player2)}. Вы откроете одну и ту же игру.` : 'Поиск запущен. Приглашение видно в Арене и отправляется в группу.';
  el('duelP1Name').textContent = duel.player1.first_name || fullName(duel.player1);
  el('duelP2Name').textContent = duel.player2 ? duel.player2.first_name || fullName(duel.player2) : 'Соперник';
  el('duelP1Avatar').src = avatarURL(duel.player1.photo_url, fullName(duel.player1));
  el('duelP2Avatar').src = avatarURL(duel.player2?.photo_url, fullName(duel.player2));
  el('duelP1Score').textContent = duel.rounds.reduce((sum, r) => sum + r.player1_score, 0);
  el('duelP2Score').textContent = duel.rounds.reduce((sum, r) => sum + r.player2_score, 0);
  el('duelScore').style.setProperty('--score-characters', el('duelP1Score').textContent.length + el('duelP2Score').textContent.length + 3);
  if (preparing) {
    el('duelCategoryBadge').textContent = '';
    el('answersGrid').replaceChildren();
    el('explanationBox').classList.add('hidden');
    renderPreparation(duel);
    return;
  }
  if (duel.status === 'waiting') {
    el('duelRoundIndicator').textContent = 'ОЖИДАНИЕ СОПЕРНИКА';
    el('duelCategoryBadge').textContent = '';
    el('duelTeacherBadge').classList.add('hidden');
    el('timerBar').value = 1;
    clearInterval(timerInterval);
    el('questionText').textContent = 'Ждём принятия вызова. Соперник увидит его на главной странице Арены.';
    el('questionTopic').textContent = 'Открытый вызов';
    el('answersGrid').replaceChildren();
    el('timerDigits').textContent = '—';
    el('explanationBox').classList.add('hidden');
    return;
  }
  currentRoundNum = duel.current_round;
  const round = duel.rounds[currentRoundNum - 1];
  const first = duel.player1.id === currentUser.id;
  const answered = first ? round.player1_answered : round.player2_answered;
  el('duelReport').classList.toggle('hidden', !answered);
  const key = `${duel.id}:${currentRoundNum}:${answered}`;
  if (displayedRound === key) return;
  displayedRound = key;
  el('duelRoundIndicator').textContent = `Раунд ${currentRoundNum} из ${duel.rounds.length}`;
  const question = round.question;
  el('duelCategoryBadge').textContent = categoryName(question.category);
  el('duelTeacherBadge').classList.toggle('hidden', question.multiplier <= 1);
  el('questionTopic').textContent = question.topic || '';
  el('questionText').textContent = question.text || 'Загрузка вопроса…';
  el('explanationBox').classList.add('hidden');
  renderAnswers('answersGrid', question.options || [], submitAnswer, answered);
  void revealNewQuestion(`${duel.id}:${currentRoundNum}`, 'duelQuestionStart', 'answersGrid');
  if (answered) {
    clearInterval(timerInterval);
    el('timerDigits').textContent = 'Ответ принят';
    el('explanationText').textContent = 'Ждём ответ соперника.';
    el('explanationBox').classList.remove('hidden');
  } else {
    const started = first ? round.player1_started_at : round.player2_started_at;
    startTimer(started, 'timerBar', 'timerDigits', () => submitAnswer(-1));
  }
}

async function revealNewQuestion(key, anchorId, answersId) {
  const token = generation;
  const pending = `${token}:${key}`;
  if (questionViewportKey === key || questionViewportPending === pending) return;
  questionViewportPending = pending;
  try {
    await Promise.all(Array.from(el(answersId).children, button => button.updateComplete));
    await document.fonts?.ready;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    if (token !== generation || questionViewportPending !== pending || !['duel', 'quiz'].includes(screen)) return;
    if (screen === 'duel' && currentDuel?.status !== 'in_progress') return;
    el(anchorId).scrollIntoView({block: 'start', behavior: 'auto'});
    questionViewportKey = key;
  } finally { if (questionViewportPending === pending) questionViewportPending = ''; }
}

function renderAnswers(containerId, options, handler, disabled = false) {
  const container = el(containerId);
  container.replaceChildren();
  options.forEach((option, index) => {
    const button = document.createElement('md-outlined-button');
    button.type = 'button';
    button.className = 'answer-btn';
    button.disabled = disabled;
    const text = document.createElement('span');
    text.className = 'answer-text';
    text.textContent = `${String.fromCharCode(65 + index)}. ${option}`;
    button.append(text);
    button.onclick = () => handler(index);
    container.append(button);
  });
}

function getCategoryIcon(cat) { return ({ algebra: 'calculate', physics: 'science', cs: 'code', history: 'history_edu', geography: 'public', chemistry: 'science', biology: 'biotech' })[cat] || 'menu_book'; }
function categoryName(cat) { return currentCategories.find(c => c.id === cat)?.name || cat || ''; }

function startTimer(startedAt, barId, digitsId, onTimeout) {
  clearInterval(timerInterval);
  const start = Date.parse(startedAt);
  const bar = el(barId), digits = el(digitsId);
  const tick = () => {
    const remaining = Math.max(0, Math.min(15, 15 - (serverNow() - start) / 1000));
    bar.value = remaining / 15;
    digits.textContent = `${remaining.toFixed(1).replace('.', ',')} с`;
    bar.classList.toggle('urgent', remaining <= 4);
    digits.classList.toggle('urgent', remaining <= 4);
    if (remaining <= 0) { clearInterval(timerInterval); onTimeout(); }
  };
  timerInterval = setInterval(tick, 100);
  tick();
}

async function submitAnswer(optionIdx) {
  if (answerPending || !currentDuel || screen !== 'duel') return;
  duelMutation++;
  answerPending = true;
  clearInterval(timerInterval);
  clearTimeout(pollTimeout);
  el('answersGrid').querySelectorAll('md-outlined-button').forEach(button => { button.disabled = true; });
  const token = generation, id = currentDuel.id, roundNum = currentRoundNum;
  try {
    const duel = await api('/api/duel/answer', { duel_id: id, round_num: roundNum, option_idx: optionIdx });
    if (token !== generation) return;
    currentDuel = duel;
    const round = duel.rounds[roundNum - 1];
    if (round.question.correct_index >= 0) {
      const buttons = el('answersGrid').querySelectorAll('md-outlined-button');
      buttons[round.question.correct_index]?.classList.add('correct');
      if (optionIdx !== round.question.correct_index) buttons[optionIdx]?.classList.add('wrong');
      for (const button of buttons) {
        if (button.classList.contains('correct')) button.insertAdjacentHTML('afterbegin', materialIcon('check', 'icon'));
        if (button.classList.contains('wrong')) button.insertAdjacentHTML('afterbegin', materialIcon('close', 'icon'));
      }
      el('explanationText').textContent = round.question.explanation || 'Раунд завершён.';
      el('explanationBox').classList.remove('hidden');
    }
    transitionTimeout = setTimeout(() => { if (token === generation) openDuel(id); }, 1200);
  } catch (error) {
    if (token === generation) {
      showError(error);
      el('explanationText').textContent = 'Не удалось получить подтверждение. Нажмите «Проверить ответ».';
      el('explanationBox').classList.remove('hidden');
      const retry = document.createElement('md-filled-tonal-button');
      retry.className = 'tab-btn active'; retry.textContent = 'Проверить ответ'; retry.onclick = () => openDuel(id);
      el('answersGrid').replaceChildren(retry);
    }
  } finally { if (token === generation) answerPending = false; }
}

async function cancelCurrentDuel() {
  if (!currentDuel) { switchScreen('home'); return; }
  try { await api('/api/duel/cancel', { duel_id: currentDuel.id }); currentDuel = null; switchScreen('home'); refreshLobby(); } catch (error) { notifyError(error); }
}

function showResultScreen() {
  lastFinishedDuelId = currentDuel.id;
  const first = currentDuel.player1.id === currentUser.id;
  const a = currentDuel.rounds.reduce((sum, r) => sum + r.player1_score, 0);
  const b = currentDuel.rounds.reduce((sum, r) => sum + r.player2_score, 0);
  const mine = first ? a : b, theirs = first ? b : a;
  el('resultScore').textContent = `${mine} : ${theirs} · ${fullName(first ? currentDuel.player2 : currentDuel.player1)}`;
  el('resultIcon').innerHTML = materialIcon(mine > theirs ? 'emoji_events' : mine === theirs ? 'handshake' : 'trending_up') + (mine > theirs ? 'Победа' : mine === theirs ? 'Ничья' : 'Ещё один шаг');
  el('resultTitle').textContent = mine > theirs ? 'Ты на шаг\nвпереди.' : mine === theirs ? 'На одной\nволне.' : 'Всё приходит\nс практикой.';
  const prefix = first ? 'player1' : 'player2';
  const rounds = currentDuel.rounds;
  const correct = rounds.filter(r => r[`${prefix}_answered`] && r[`${prefix}_time_sec`] < 15 && r.question.correct_index >= 0 && r[`${prefix}_answer`] === r.question.correct_index).length;
  const timed = rounds.filter(r => r[`${prefix}_answered`] && r[`${prefix}_answer`] >= 0 && Number.isFinite(r[`${prefix}_time_sec`]));
  el('resultCorrect').textContent = `${correct} из ${rounds.length}`;
  el('resultTime').textContent = timed.length ? `${(timed.reduce((sum, r) => sum + r[`${prefix}_time_sec`], 0) / timed.length).toFixed(1).replace('.', ',')} с` : '—';
  el('xpGainedAmount').textContent = `+${first ? currentDuel.player1_xp_delta : currentDuel.player2_xp_delta} XP`;
  el('xpReasonText').textContent = 'За ответы в дуэли с участником чата.';
  el('resultAntiAbuseNotice').textContent = currentDuel.reward_notice;
  loadUserProfile();
  switchScreen('result');
}

async function loadChatMembers() {
  if (!currentChatId) return;
  try {
    const data = await api(`/api/chat/members?chat_id=${currentChatId}`);
    chatMembers = data.members || [];
    if (data.chat) { currentChat = data.chat; updateChatUI(); }
    renderChatMembersScreen();
  } catch (error) { showError(error); }
}

function renderChatMembersScreen() {
  const container = el('membersList');
  container.replaceChildren();
  if (!chatMembers.length) { container.textContent = 'В чате пока нет доступных соперников.'; return; }
  for (const member of chatMembers) {
    const row = document.createElement('md-list-item');
    row.innerHTML = `<img slot="start" src="${escapeHTML(avatarURL(member.photo_url, fullName(member)))}" class="leader-avatar" alt=""><span slot="headline">${escapeHTML(fullName(member))}</span><span slot="supporting-text">Ур. ${Number(member.level) || 1} · ${Number(member.xp) || 0} XP</span>`;
    if (member.id !== currentUser.id) {
      const button = document.createElement('md-text-button');
      button.type = 'button';
      button.slot = 'end';
      button.className = 'tab-btn active'; button.textContent = 'Вызов'; button.setAttribute('aria-label', `Вызвать: ${fullName(member)}`); button.onclick = () => startQuickDuel(member.id); row.append(button);
    }
    container.append(row);
  }
}

async function loadLeaderboard() {
  if (!currentChatId) return;
  try { const data = await api(`/api/leaderboard?chat_id=${currentChatId}`); renderLeaderboardUI(data.leaders || []); renderTopReward(data.top_reward); } catch (error) { showError(error); }
}

function renderTopReward(text) {
  el('leaderRewardText').textContent = text || '';
  el('leaderReward').classList.toggle('hidden', !text);
}

function renderLeaderboardUI(list) {
  const leader = list[0];
  el('podiumContainer').innerHTML = leader ? `<md-list><md-list-item>${materialIcon('emoji_events', 'start')}<span slot="overline">Лидер чата</span><span slot="headline">${escapeHTML(leader.name)}</span><span slot="supporting-text">${Number(leader.xp) || 0} XP</span></md-list-item></md-list>` : '';
  el('leaderCount').textContent = String(list.length);
  el('leaderboardList').innerHTML = list.length ? list.slice(1).map(item => `<md-list-item><img slot="start" src="${escapeHTML(avatarURL(item.photo_url, item.name))}" class="leader-avatar" alt=""><span slot="headline">${Number(item.rank) || '—'}. ${escapeHTML(item.name)}${item.user_id === currentUser.id ? ' · Вы' : ''}</span><span slot="supporting-text">${escapeHTML(currentChat?.title || item.class_group || '')}</span><span slot="end">${Number(item.xp) || 0} XP</span></md-list-item>`).join('') : '<p class="muted">Пока нет участников.</p>';
}

function applyGameSettings(settings) {
  el('duelQuestionCount').value = String(settings.duel_question_count);
  el('quizQuestionCount').value = String(settings.quiz_question_count);
  el('topReward').value = settings.top_reward || '';
  renderTopReward(settings.top_reward);
  el('duelFormat').textContent = `Вопросов: ${settings.duel_question_count}, по 15 секунд.`;
  el('expressQuestionCount').textContent = `До ${settings.quiz_question_count}`;
}

function lockGameSettings(locked) {
  for (const id of ['duelQuestionCount', 'quizQuestionCount', 'topReward', 'gameSettingsSave']) el(id).disabled = locked;
}

async function loadGameSettings() {
  if (!currentChatId || gameSettingsSaving) return;
  const request = ++gameSettingsRequest;
  lockGameSettings(true);
  el('gameSettingsStatus').textContent = 'Загружаем настройки…';
  try {
    const settings = await api(`/api/game-settings?chat_id=${currentChatId}`);
    if (request !== gameSettingsRequest) return;
    applyGameSettings(settings);
    lockGameSettings(false);
    el('gameSettingsStatus').textContent = '';
  } catch (error) {
    if (request === gameSettingsRequest) el('gameSettingsStatus').textContent = error.message;
  }
}

async function saveGameSettings(event) {
  event.preventDefault();
  if (gameSettingsSaving || el('gameSettingsSave').disabled) return;
  const duelCount = Number(el('duelQuestionCount').value), quizCount = Number(el('quizQuestionCount').value);
  if (![duelCount, quizCount].every(count => Number.isInteger(count) && count >= 1 && count <= 20)) {
    el('gameSettingsStatus').textContent = 'Введите целые числа от 1 до 20.';
    return;
  }
  gameSettingsSaving = true;
  ++gameSettingsRequest;
  lockGameSettings(true);
  el('gameSettingsStatus').textContent = 'Сохраняем…';
  try {
    const settings = await api('/api/game-settings', {chat_id: currentChatId, duel_question_count: duelCount, quiz_question_count: quizCount, top_reward: el('topReward').value.trim()}, 'PUT');
    applyGameSettings(settings);
    el('gameSettingsStatus').textContent = 'Сохранено. Награда видна в рейтинге этой группы, количество вопросов действует для новых игр.';
  } catch (error) { el('gameSettingsStatus').textContent = error.message; }
  finally { gameSettingsSaving = false; lockGameSettings(false); }
}

function switchTeacherTab(name) {
  document.querySelectorAll('.teacher-tab-content').forEach(item => item.classList.remove('active'));
  el(`tab-teacher-${name}`).classList.add('active');
  if (name === 'analytics') loadTeacherAnalytics();
  if (name === 'subjects') loadCategories().catch(notifyError);
  if (name === 'constructor') void loadQuestions();
  if (name === 'settings') void loadGameSettings();
}

function appendSubjectArtwork(row, subject) {
  const artwork = document.createElement('span');
  artwork.slot = 'start';
  artwork.className = 'subject-artwork';
  artwork.innerHTML = materialIcon(getCategoryIcon(subject.id));
  if (/^data:image\/(jpeg|png);base64,/.test(subject.image_url || '')) {
    const image = document.createElement('img');
    image.src = subject.image_url;
    image.alt = '';
    image.loading = 'lazy';
    image.onerror = () => { image.remove(); artwork.innerHTML = materialIcon(getCategoryIcon(subject.id)); };
    artwork.replaceChildren(image);
  }
  row.prepend(artwork);
}

function subjectAvailability(subject) {
  const count = Number(subject.question_count) || 0, minimum = subject.min_question_count || 1;
  return `${subject.ready === false ? 'Черновик' : 'Готов'} · включённых вопросов: ${count}, минимум ${minimum}`;
}

function renderSubjects() {
  const list = el('subjectsList');
  list.replaceChildren();
  if (!currentCategories.length) {
    list.textContent = 'Предметов пока нет. Добавьте первый предмет и вопросы к нему.';
    return;
  }
  for (const subject of currentCategories) {
    const row = document.createElement('md-list-item');
    row.innerHTML = `<span slot="headline">${escapeHTML(subject.name)}</span><span slot="supporting-text">${escapeHTML(subjectAvailability(subject))}${subject.description ? ' · ' + escapeHTML(subject.description) : ''}</span>`;
    appendSubjectArtwork(row, subject);
    const actions = document.createElement('span');
    actions.className = 'subject-actions';
    actions.slot = 'end';
    for (const [label, icon, handler] of [
      ['Изменить', 'edit', () => editSubject(subject)],
      ['Удалить', 'delete', () => deleteSubject(subject)],
    ]) {
      const button = document.createElement('md-icon-button');
      button.type = 'button';
      button.innerHTML = materialIcon(icon);
      button.setAttribute('aria-label', `${label} предмет ${subject.name}`);
      button.onclick = handler;
      actions.append(button);
    }
    row.append(actions);
    list.append(row);
  }
}

function editSubject(subject = null) {
  if (subjectSaving) return;
  subjectImageTask++;
  subjectImageBusy = false;
  subjectImageURL = subject?.image_url || '';
  el('subjectImageFile').value = '';
  el('subjectImageStatus').classList.remove('status-error');
  el('subjectImageStatus').textContent = 'JPEG, PNG или WebP до 10 МБ. Изображение будет уменьшено автоматически.';
  updateSubjectImagePreview();
  editingSubjectId = subject?.id || '';
  el('subjectName').value = subject?.name || '';
  el('subjectDescription').value = subject?.description || '';
  el('subjectMinimum').value = String(subject?.min_question_count || 3);
  updateSubjectRequirementHint();
  el('subjectFormTitle').textContent = subject ? 'Изменить предмет' : 'Новый предмет';
  el('subjectSave').textContent = subject ? 'Сохранить изменения' : 'Добавить предмет';
  if (subject) {
    el('subjectForm').scrollIntoView({block: 'start'});
    el('subjectName').focus();
  }
}

function updateSubjectRequirementHint() {
  const subject = currentCategories.find(c => c.id === editingSubjectId);
  const count = Number(subject?.question_count) || 0;
  const minimum = Number(el('subjectMinimum').value);
  el('subjectRequirementHint').textContent = Number.isInteger(minimum) && minimum >= 1 && minimum <= 20
    ? `Включённых вопросов: ${count}. ${count < minimum ? `До запуска нужно добавить ещё ${minimum - count}. Предмет сохранится как черновик.` : 'Предмет готов к новым играм.'} Вопросы добавляются во вкладке «Вопросы».`
    : 'Укажите целое число от 1 до 20.';
}

function updateSubjectImagePreview() {
  const image = el('subjectImage');
  if (subjectImageURL) image.src = subjectImageURL;
  else image.removeAttribute('src');
  image.classList.toggle('hidden', !subjectImageURL);
  el('subjectImagePlaceholder').classList.toggle('hidden', !!subjectImageURL);
  el('subjectImageRemove').classList.toggle('hidden', !subjectImageURL);
  for (const id of ['subjectImagePick', 'subjectImageRemove', 'subjectName', 'subjectDescription', 'subjectMinimum']) el(id).disabled = subjectSaving;
  el('subjectSave').disabled = subjectImageBusy || subjectSaving;
}

function removeSubjectImage() {
  if (subjectSaving) return;
  subjectImageTask++;
  subjectImageBusy = false;
  subjectImageURL = '';
  el('subjectImageFile').value = '';
  el('subjectImageStatus').classList.remove('status-error');
  el('subjectImageStatus').textContent = 'Обложка убрана. Сохраните предмет, чтобы применить изменение.';
  updateSubjectImagePreview();
}

async function compressSubjectImage(file) {
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) throw new Error('Выберите файл JPEG, PNG или WebP.');
  if (!file.size || file.size > 10 * 1024 * 1024) throw new Error('Выберите изображение размером до 10 МБ.');
  const url = URL.createObjectURL(file);
  const image = new Image();
  try {
    await new Promise((resolve, reject) => {
      image.onload = resolve;
      image.onerror = () => reject(new Error('Не удалось прочитать картинку. Выберите другой файл.'));
      image.src = url;
    });
    let edge = 512;
    const canvas = document.createElement('canvas');
    const context = canvas.getContext('2d');
    if (!context || !image.naturalWidth || !image.naturalHeight) throw new Error('Не удалось обработать картинку.');
    while (edge >= 128) {
      const ratio = Math.min(1, edge / Math.max(image.naturalWidth, image.naturalHeight));
      canvas.width = Math.max(1, Math.round(image.naturalWidth * ratio));
      canvas.height = Math.max(1, Math.round(image.naturalHeight * ratio));
      context.fillStyle = '#ffffff';
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.drawImage(image, 0, 0, canvas.width, canvas.height);
      for (const quality of [0.86, 0.72, 0.58, 0.42]) {
        const result = canvas.toDataURL('image/jpeg', quality);
        // Keep room for the remaining JSON fields inside the API's 64 KiB limit.
        if (result.startsWith('data:image/jpeg;base64,') && result.length <= 44000) return result;
      }
      edge = Math.floor(edge * 0.75);
    }
    throw new Error('Не удалось уменьшить изображение. Выберите другую картинку.');
  } finally { URL.revokeObjectURL(url); }
}

async function selectSubjectImage(event) {
  const file = event.target.files?.[0];
  event.target.value = '';
  if (!file || subjectSaving) return;
  const task = ++subjectImageTask;
  subjectImageBusy = true;
  el('subjectImageStatus').classList.remove('status-error');
  el('subjectImageStatus').textContent = 'Подготавливаем обложку…';
  updateSubjectImagePreview();
  try {
    const image = await compressSubjectImage(file);
    if (task !== subjectImageTask) return;
    subjectImageURL = image;
    el('subjectImageStatus').textContent = 'Обложка готова. Сохраните предмет, чтобы её увидела группа.';
  } catch (error) {
    if (task !== subjectImageTask) return;
    el('subjectImageStatus').classList.add('status-error');
    el('subjectImageStatus').textContent = error.message;
  } finally {
    if (task === subjectImageTask) { subjectImageBusy = false; updateSubjectImagePreview(); }
  }
}

async function saveSubject(event) {
  event.preventDefault();
  const button = el('subjectSave');
  if (button.disabled || subjectImageBusy || subjectSaving) return;
  const minimum = Number(el('subjectMinimum').value);
  if (!Number.isInteger(minimum) || minimum < 1 || minimum > 20) { el('subjectMinimum').focus(); updateSubjectRequirementHint(); return; }
  subjectSaving = true;
  updateSubjectImagePreview();
  try {
    const saved = await api('/api/teacher/subjects', {chat_id: currentChatId, id: editingSubjectId, name: el('subjectName').value.trim(), description: el('subjectDescription').value.trim(), image_url: subjectImageURL, min_question_count: minimum}, editingSubjectId ? 'PATCH' : 'POST');
    subjectSaving = false;
    editSubject();
    await loadCategories();
    const subject = currentCategories.find(c => c.id === saved?.id);
    await showMessage(subject?.ready === false ? `Предмет сохранён как черновик. Включённых вопросов: ${subject.question_count} из ${subject.min_question_count}. Добавьте вопросы во вкладке «Вопросы», чтобы открыть игры по предмету.` : 'Предмет сохранён. Он доступен участникам этой группы.', 'Предметы');
  } catch (error) { notifyError(error); } finally { subjectSaving = false; updateSubjectImagePreview(); }
}

async function deleteSubject(subject) {
  if (!await showMessage(`Удалить «${subject.name}» из предметов этой группы? Новые игры по нему станут недоступны. Уже начатые игры и история ответов сохранятся.`, 'Удалить предмет?', true)) return;
  try {
    await api('/api/teacher/subjects', {chat_id: currentChatId, id: subject.id}, 'DELETE');
    if (editingSubjectId === subject.id) editSubject();
    await loadCategories();
  } catch (error) { notifyError(error); }
}

async function submitCustomQuestion(event) {
  event.preventDefault();
  const button = el('questionForm').querySelector('md-filled-button[type="submit"]');
  if (button.disabled) return;
  button.disabled = true;
  try {
    await api('/api/teacher/questions', { chat_id: currentChatId, id: editingQuestionId, category: el('formCategory').value, topic: el('formTopic').value, text: el('formText').value, options: [0, 1, 2, 3].map(i => el(`opt${i}`).value), correct_idx: Number(Array.from(document.querySelectorAll('md-radio[name="correctOpt"]')).find(radio => radio.checked)?.value), explanation: el('formExplanation').value }, editingQuestionId ? 'PATCH' : 'POST');
    editQuestion(null, false); await loadQuestions();
    await showMessage('Вопрос сохранён. Изменения появятся в новых играх этой группы.', 'Вопросы');
  } catch (error) { notifyError(error); } finally { button.disabled = false; }
}

async function loadTeacherAnalytics() {
  if (currentUser.role !== 'teacher') return;
  try {
    const data = await api(`/api/teacher/analytics?chat_id=${currentChatId}`);
    el('analyticsList').innerHTML = data.length ? data.map(item => `<md-list-item>${materialIcon(getCategoryIcon(item.subject), 'start')}<span slot="headline">${escapeHTML(item.topic)}</span><span slot="supporting-text">${Number(item.error_rate)}% ошибок (${Number(item.errors_count)}/${Number(item.total_attempts)}). ${escapeHTML(item.recommendation)}</span></md-list-item>`).join('') : '<p>Пока нет ответов учеников.</p>';
  } catch (error) { showError(error); }
}

async function launchExpressQuizFromTeacher() {
  if (actionPending) return;
  actionPending = true;
  try {
    const quiz = await api('/api/teacher/express-quiz', { chat_id: currentChatId, subject: el('expressSubject').value, topic: el('expressTopic').value.trim() || 'Проверка знаний' });
    await showMessage('Участники могут войти через «Экспресс-квиз» на Арене.', 'Квиз открыт');
    await openQuiz(quiz.id);
  } catch (error) { notifyError(error); } finally { actionPending = false; }
}

async function openExpressQuizScreen() {
  try { const quiz = await api(`/api/teacher/express-quiz?chat_id=${currentChatId}`); await openQuiz(quiz.id); } catch (error) { notifyError(error); }
}

async function openQuiz(id) {
  const refreshing = screen === 'quiz' && activeQuiz?.id === id;
  loadingQuizId = id;
  stopGameWork();
  if (!refreshing) {
    switchScreen('quiz');
    el('quizAnswersGrid').replaceChildren();
    el('quizQText').textContent = 'Загрузка вопроса…';
  }
  const token = generation;
  try {
    const quiz = await api(`/api/teacher/express-quiz?id=${encodeURIComponent(id)}`);
    if (token !== generation) return;
    activeQuiz = quiz;
    const participant = quiz.participants[currentUser.id];
    activeQuizQIndex = participant.total_answers;
    if (activeQuizQIndex >= quiz.questions.length) {
      renderQuizResult(quiz, participant);
      loadUserProfile(); switchScreen('quiz-result'); return;
    }
    el('quizTitle').textContent = quiz.topic;
    el('quizMeta').textContent = `Учитель: ${quiz.teacher_name} • 2x XP`;
    const question = quiz.questions[activeQuizQIndex];
    el('quizQTopic').textContent = `Вопрос ${activeQuizQIndex + 1}/${quiz.questions.length} • ${question.topic}`;
    el('quizQText').textContent = question.text;
    renderAnswers('quizAnswersGrid', question.options, submitQuizAnswer);
    void revealNewQuestion(`${quiz.id}:${activeQuizQIndex}`, 'quizQuestionStart', 'quizAnswersGrid');
    startTimer(participant.question_started_at, 'quizTimerBar', 'quizTimerDigits', () => submitQuizAnswer(-1));
  } catch (error) { if (token === generation) { if (error.network) el('quizQText').textContent = 'Восстанавливаем связь с квизом…'; else { notifyError(error); switchScreen('home'); } } }
}

async function submitQuizAnswer(optionIdx) {
  if (answerPending || !activeQuiz || screen !== 'quiz') return;
  answerPending = true;
  clearInterval(timerInterval);
  el('quizAnswersGrid').querySelectorAll('md-outlined-button').forEach(button => { button.disabled = true; });
  const token = generation, id = activeQuiz.id;
  try {
    await api('/api/teacher/express-quiz/answer', { quiz_id: id, q_index: activeQuizQIndex, opt_index: optionIdx });
    if (token === generation) await openQuiz(id);
  } catch (error) {
    if (token === generation) {
      showError(error);
      const button = document.createElement('md-filled-tonal-button'); button.textContent = 'Проверить ответ'; button.onclick = () => openQuiz(id);
      el('quizAnswersGrid').replaceChildren(button);
    }
  } finally { if (token === generation) answerPending = false; }
}

function preferenceKey() { return `quiz-hackathon-preferences-${currentUser.id || 'device'}`; }
function normalizePreferences(value) {
  const v = value && typeof value === 'object' ? value : {};
  return {theme: ['system','light','dark'].includes(v.theme) ? v.theme : 'dark',
    palette: ['hackathon','blue','purple','green','rose'].includes(v.palette) ? v.palette : 'hackathon',
    font: ['1','1.15','1.3'].includes(v.font) ? v.font : '1', haptics: v.haptics !== false};
}
function loadPreferences() {
  let stored;
  try {
    const saved = localStorage.getItem(preferenceKey());
    if (saved) stored = JSON.parse(saved);
    else {
      const legacy = JSON.parse(localStorage.getItem(`quiz-preferences-${currentUser.id || 'device'}`) || '{}');
      // Carry accessibility choices into this separate visual identity.
      stored = {font: legacy?.font, haptics: legacy?.haptics};
    }
  } catch (_) { /* device storage may be unavailable */ }
  preferences = normalizePreferences(stored);
  el('settingTheme').value = preferences.theme; el('settingPalette').value = preferences.palette; el('settingFont').value = preferences.font; el('settingHaptics').selected = preferences.haptics;
  applyPreferences();
}
function savePreferences() {
  preferences = normalizePreferences({theme: el('settingTheme').value, palette: el('settingPalette').value, font: el('settingFont').value, haptics: el('settingHaptics').selected});
  try { localStorage.setItem(preferenceKey(), JSON.stringify(preferences)); } catch (_) { /* still works for this session */ }
  applyPreferences();
}
function applyPreferences() {
  const dark = preferences.theme === 'dark' || preferences.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.dataset.palette = preferences.palette || 'hackathon';
  document.documentElement.style.setProperty('--font-scale', preferences.font);
  document.querySelector('meta[name="theme-color"]').content = getComputedStyle(document.documentElement).getPropertyValue('--md-sys-color-surface').trim();
}
function setConnection(connected) {
  const wasLost = connectionLost; connectionLost = !connected;
  el('connectionStatus').classList.toggle('disconnected', !connected);
  el('connectionText').textContent = connected ? 'На связи' : navigator.onLine ? 'Восстанавливаем связь…' : 'Нет подключения к сети';
  el('connectionRetry').classList.toggle('hidden', connected);
  if (connected) { reconnectDelay = 1000; }
  else if (!reconnectTimer && !reconnecting) reconnectTimer = setTimeout(reconnectNow, reconnectDelay);
  if (connected && wasLost && initialized && !reconnecting) {
    // A polling response can be the first evidence of recovery. Reconcile only
    // reads; never replay an answer, invitation or teacher edit automatically.
    clearTimeout(reconnectTimer); reconnectTimer = setTimeout(reconnectNow, 0);
  }
}
async function reconnectNow() {
  clearTimeout(reconnectTimer); reconnectTimer = null;
  if (reconnecting) return;
  reconnecting = true;
  const recoveryScreen = screen, recoveryDuel = currentDuel?.id || loadingDuelId, recoveryQuiz = activeQuiz?.id || loadingQuizId;
  try {
    await api('/api/about');
    if (!initialized) await initializeApp();
    else {
      if (screen === recoveryScreen && screen === 'duel' && recoveryDuel) await openDuel(recoveryDuel);
      else if (screen === recoveryScreen && screen === 'quiz' && recoveryQuiz) await openQuiz(recoveryQuiz);
      await loadUserProfile();
      if (currentChatId) await refreshLobby();
    }
  } catch (_) { /* scheduled below */ }
  finally {
    reconnecting = false;
    if (connectionLost) { reconnectDelay = Math.min(reconnectDelay * 2, 15000); reconnectTimer = setTimeout(reconnectNow, reconnectDelay); }
  }
}
async function resumeActiveDuel() {
  if (!currentChatId) return;
  const list = await api(`/api/duel/list?chat_id=${currentChatId}`);
  const own = list.find(d => ['preparing','countdown','in_progress'].includes(d.status) && (d.player1.id === currentUser.id || d.player2?.id === currentUser.id)) || list.find(d => d.status === 'waiting' && d.player1.id === currentUser.id);
  if (own) await openDuel(own.id);
}
function closureText(d) { return ({rejected: 'Соперник отклонил приглашение.', expired: 'Время приглашения истекло. Создайте новую дуэль.', cancelled: 'Дуэль отменена.', timeout: 'Дуэль закрыта из-за длительного бездействия.'})[d.close_reason] || 'Дуэль закрыта.'; }
function renderQuizResult(quiz, participant) {
  el('quizResultSummary').textContent = `Верных ответов: ${participant.correct_count}/${quiz.questions.length}. Очки: ${participant.score}.`;
  el('quizResultQuestions').replaceChildren();
  quiz.questions.forEach((q,index) => {
    const section = document.createElement('section'); section.className = 'review-round';
    section.innerHTML = `<h2>Вопрос ${index+1}</h2><p>${escapeHTML(q.text)}</p><p>Правильный ответ: ${escapeHTML(q.options[q.correct_index])}</p><p class="muted">${escapeHTML(q.explanation)}</p>`;
    const button = document.createElement('md-text-button'); button.type = 'button'; button.textContent = 'Ошибка в вопросе'; button.onclick = () => reportPlayedQuestion('', quiz.id, index); section.append(button); el('quizResultQuestions').append(section);
  });
}

function updateQuestionSubjectFilter() {
  const select = el('questionSubjectFilter'), previous = select.value || '';
  select.innerHTML = '<md-select-option value="" selected><span slot="headline">Все предметы</span></md-select-option>' + currentCategories.map(c => `<md-select-option value="${escapeHTML(c.id)}"><span slot="headline">${escapeHTML(c.name)}</span></md-select-option>`).join('');
  void select.updateComplete.then(() => { select.value = currentCategories.some(c => c.id === previous) ? previous : ''; });
}
async function loadQuestions() {
  if (currentUser.role !== 'teacher' || !currentChatId) return;
  el('questionCount').textContent = 'Загружаем вопросы…';
  try { questionCatalog = await api(`/api/teacher/questions?chat_id=${currentChatId}`); renderQuestionCatalog(); await loadCategories(); }
  catch (error) { el('questionCount').textContent = error.message; }
}
function renderQuestionCatalog(reset = true) {
  if (reset) catalogLimit = 6;
  const search = el('questionSearch').value.trim().toLocaleLowerCase('ru'), subject = el('questionSubjectFilter').value, status = el('questionStatusFilter').value;
  const list = questionCatalog.filter(q => (!subject || q.category === subject) && (status !== 'enabled' || !q.disabled) && (status !== 'disabled' || q.disabled) && (!search || [q.text,q.topic,...q.options].join(' ').toLocaleLowerCase('ru').includes(search)));
  el('questionCount').textContent = `Найдено: ${list.length} из ${questionCatalog.length}`;
  el('questionCatalog').replaceChildren();
  el('questionMore').classList.toggle('hidden', list.length <= catalogLimit);
  for (const q of list.slice(0, catalogLimit)) {
    const row = document.createElement('md-list-item');
    row.innerHTML = `<span slot="headline">${escapeHTML(q.text)}</span><span slot="supporting-text">${escapeHTML(categoryName(q.category))} · ${escapeHTML(q.topic)} · ${q.disabled ? 'Отключён' : 'В игре'}</span>`;
    const actions = document.createElement('span'); actions.slot = 'end'; actions.className = 'question-actions';
    for (const [label, icon, handler] of [ ['Изменить вопрос', 'edit', () => editQuestion(q)], [q.disabled ? 'Включить вопрос' : 'Отключить вопрос', q.disabled ? 'visibility_off' : 'visibility', () => changeQuestionStatus(q)], ['Удалить вопрос', 'delete', () => deleteQuestion(q)] ]) {
      const button = document.createElement('md-icon-button'); button.type = 'button'; button.setAttribute('aria-label', label); button.innerHTML = materialIcon(icon); button.onclick = handler; actions.append(button);
    }
    row.append(actions); el('questionCatalog').append(row);
  }
}
function editQuestion(q = null, scroll = true) {
  editingQuestionId = q?.id || '';
  el('questionFormTitle').textContent = q ? 'Изменить вопрос' : 'Новый вопрос'; el('questionSubmit').textContent = q ? 'Сохранить изменения' : 'Опубликовать вопрос';
  el('formCategory').value = q?.category || currentCategories[0]?.id || '';
  el('formTopic').value = q?.topic || ''; el('formText').value = q?.text || ''; el('formExplanation').value = q?.explanation || '';
  for (let i=0;i<4;i++) el(`opt${i}`).value = q?.options[i] || '';
  document.querySelectorAll('md-radio[name="correctOpt"]').forEach(radio => { radio.checked = Number(radio.value) === (q?.correct_index ?? 0); });
  if (scroll) { el('questionFormTitle').scrollIntoView({block: 'start'}); el('formText').focus(); }
}
async function changeQuestionStatus(q) {
  if (actionPending) return; actionPending = true;
  try { await api('/api/teacher/questions', {chat_id: currentChatId, id: q.id, disabled: !q.disabled}, 'PATCH'); await loadQuestions(); }
  catch (error) { notifyError(error); } finally { actionPending = false; }
}
async function deleteQuestion(q) {
  if (!await showMessage('Удалить вопрос из банка этой группы? В начатых играх и истории он сохранится.', 'Удалить вопрос?', true)) return;
  try { await api('/api/teacher/questions', {chat_id: currentChatId, id: q.id}, 'DELETE'); if (editingQuestionId === q.id) editQuestion(null, false); await loadQuestions(); }
  catch (error) { notifyError(error); }
}

function openHistory() { switchScreen('history'); void loadHistory(false); }
async function loadHistory(more = false) {
  if (historyPending) return; historyPending = true; el('historyMore').disabled = true;
  if (!more) { historyOffset = 0; el('historyList').replaceChildren(); }
  el('historyStatus').textContent = 'Загрузка…';
  try {
    const list = await api(`/api/duel/history?chat_id=${currentChatId}&offset=${historyOffset}`);
    for (const d of list) {
      const first = d.player1.id === currentUser.id, opponent = first ? d.player2 : d.player1;
      const row = document.createElement('md-list-item'); row.type = 'button';
      const result = d.status === 'declined' ? closureText(d) : d.winner_id === 0 ? 'Ничья' : d.winner_id === currentUser.id ? 'Победа' : 'Поражение';
      row.innerHTML = `<img slot="start" src="${escapeHTML(avatarURL(opponent?.photo_url, fullName(opponent)))}" class="leader-avatar" alt=""><span slot="headline">${escapeHTML(fullName(opponent))} · ${escapeHTML(result)}</span><span slot="supporting-text">${escapeHTML(new Date(d.finished_at && d.status === 'finished' ? d.finished_at : d.created_at).toLocaleString('ru-RU'))} · +${Number(first ? d.player1_xp_delta : d.player2_xp_delta) || 0} XP${d.status === 'finished' ? ' · Разбор ответов' : ''}</span>`;
      row.onclick = () => d.status === 'finished' ? reviewDuel(d.id) : showMessage(closureText(d), 'Дуэль');
      el('historyList').append(row);
    }
    historyOffset += list.length; el('historyStatus').textContent = historyOffset ? `Показано: ${historyOffset}` : 'Завершённых дуэлей пока нет.';
    el('historyMore').classList.toggle('hidden', list.length < 20);
  } catch (error) { el('historyStatus').textContent = error.message; }
  finally { historyPending = false; el('historyMore').disabled = false; }
}
async function reviewDuel(id) {
  try {
    const d = await api(`/api/duel/get?id=${encodeURIComponent(id)}`);
    if (d.status !== 'finished') throw new Error('Разбор появится после завершения дуэли.');
    const first = d.player1.id === currentUser.id, prefix = first ? 'player1' : 'player2';
    el('reviewSummary').textContent = `${fullName(first ? d.player2 : d.player1)} · +${first ? d.player1_xp_delta : d.player2_xp_delta} XP`;
    el('reviewRounds').replaceChildren();
    for (const [index,r] of d.rounds.entries()) {
      const section = document.createElement('section'); section.className = 'review-round';
      const answer = r[`${prefix}_answer`], correct = answer === r.question.correct_index && r[`${prefix}_time_sec`] < 15;
      section.innerHTML = `<h2>Раунд ${index+1} · ${correct ? 'Верно' : 'Нужно повторить'}</h2><p class="question-title">${escapeHTML(r.question.text)}</p><md-list>${r.question.options.map((v,i) => `<md-list-item>${i === r.question.correct_index ? materialIcon('check','start') : i === answer ? materialIcon('close','start') : ''}<span slot="headline">${escapeHTML(v)}</span><span slot="supporting-text">${i === r.question.correct_index ? 'Правильный ответ' : ''}${i === answer ? ' · Ваш ответ' : ''}</span></md-list-item>`).join('')}</md-list><p>${answer < 0 ? 'Вы не успели ответить.' : `Ваши очки: ${r[`${prefix}_score`]}.`}</p><p class="muted">${escapeHTML(r.question.explanation)}</p>`;
      const button = document.createElement('md-text-button'); button.type = 'button'; button.innerHTML = materialIcon('flag','icon') + 'Ошибка в вопросе'; button.onclick = () => reportPlayedQuestion(id,'',index); section.append(button); el('reviewRounds').append(section);
    }
    switchScreen('review');
  } catch (error) { notifyError(error); }
}
async function reportPlayedQuestion(duelId, quizId, index) {
  if (el('reportDialog').open) return;
  reportTarget = {duel_id: duelId, quiz_id: quizId, index}; el('reportReason').value = ''; el('reportError').textContent = ''; await el('reportDialog').show();
}
async function sendQuestionReport(event) {
  event.preventDefault(); if (!reportTarget || el('reportSend').disabled) return;
  el('reportSend').disabled = true;
  try { await api('/api/question-reports', {...reportTarget, reason: el('reportReason').value}); await el('reportDialog').close(); reportTarget = null; void showMessage('Замечание отправлено учителю этой группы.', 'Спасибо за помощь'); }
  catch (error) { el('reportError').textContent = error.message; }
  finally { el('reportSend').disabled = false; }
}
async function openReports() {
  switchScreen('reports'); el('reportsStatus').textContent = 'Загрузка…';
  try {
    const reports = await api(`/api/teacher/reports?chat_id=${currentChatId}`); el('reportsList').replaceChildren();
    el('reportsStatus').textContent = reports.length ? `Открыто: ${reports.filter(r => !r.resolved).length} · Всего: ${reports.length}` : 'Замечаний пока нет.';
    for (const r of reports) {
      const section = document.createElement('section'); section.className = 'review-round';
      section.innerHTML = `<h2>${r.resolved ? 'Решено' : 'Нужно проверить'}</h2><p>${escapeHTML(r.question.text)}</p><p class="report-reason">${escapeHTML(r.reason)}</p><p class="muted">${escapeHTML(r.user_name)} · ${escapeHTML(new Date(r.created_at).toLocaleString('ru-RU'))}</p><p class="muted">Ответ в игре: ${escapeHTML(r.question.options[r.question.correct_index])}</p>`;
      const edit = document.createElement('md-outlined-button'); edit.type = 'button'; edit.textContent = 'Открыть вопрос'; edit.onclick = async () => { await loadQuestions(); const q = questionCatalog.find(q => q.id === r.question.id); if (!q) return showMessage('Вопрос уже удалён. Замечание сохраняет версию из игры.'); switchScreen('teacher'); el('teacherTabs').activeTabIndex = 1; switchTeacherTab('constructor'); editQuestion(q); };
      const resolve = document.createElement('md-text-button'); resolve.type = 'button'; resolve.textContent = r.resolved ? 'Открыть повторно' : 'Отметить решённым'; resolve.onclick = async () => { resolve.disabled = true; try { await api('/api/teacher/reports', {chat_id: currentChatId, id: r.id, resolved: !r.resolved}, 'PATCH'); await openReports(); } catch (error) { notifyError(error); } finally { resolve.disabled = false; } };
      section.append(edit,resolve); el('reportsList').append(section);
    }
  } catch (error) { el('reportsStatus').textContent = error.message; }
}
