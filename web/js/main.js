/**
 * 星记 — 主逻辑
 */

// ===== 类型配置 =====
const TYPE_COLORS = {
    family: { main: '#9acd32', light: '#b5e666', dark: '#6b8e23', glow: 'rgba(154,205,50,0.4)', glowSoft: 'rgba(154,205,50,0.12)', name: '家庭' },
    money:  { main: '#ff6347', light: '#ff8c7a', dark: '#cd4f39', glow: 'rgba(255,99,71,0.4)', glowSoft: 'rgba(255,99,71,0.12)', name: '赚钱' },
    self:   { main: '#9370db', light: '#b08cef', dark: '#7b5fb5', glow: 'rgba(147,112,219,0.4)', glowSoft: 'rgba(147,112,219,0.12)', name: '自我时间' },
    sport:  { main: '#ffd700', light: '#ffe44d', dark: '#ccb800', glow: 'rgba(255,215,0,0.4)', glowSoft: 'rgba(255,215,0,0.12)', name: '运动' },
    love:   { main: '#ff69b4', light: '#ff8cc8', dark: '#cd5c91', glow: 'rgba(255,105,180,0.4)', glowSoft: 'rgba(255,105,180,0.12)', name: '爱情' },
    study:  { main: '#4169e1', light: '#6b8ef5', dark: '#2f4fb3', glow: 'rgba(65,105,225,0.4)', glowSoft: 'rgba(65,105,225,0.12)', name: '学习' }
};

// ===== 状态 =====
let todos = [];
let historyData = {};
let currentView = 'today';
let selectedTodo = null;
let selectedType = 'self';
let starfield = null;
let calYear, calMonth; // 日历当前年月
let currentTheme = 'dark';

// ===== 时间轴调度 =====
// schedule: { hour: [todoId, ...], ... }
let schedule = {};
const SCHEDULE_START = 8;
const SCHEDULE_END = 21;

function applyTheme(theme) {
    currentTheme = theme;
    const root = document.documentElement;
    const body = document.body;
    root.className = 'theme-' + theme;
    body.className = 'theme-' + theme;
    localStorage.setItem('star-todo-theme', theme);

    // 更新按钮图标
    const btn = document.getElementById('themeToggle');
    if (btn) {
        btn.innerHTML = theme === 'dark'
            ? '<i class="bi bi-moon-stars-fill"></i>'
            : '<i class="bi bi-sun-fill"></i>';
    }

    // 通知星云背景切换
    if (starfield) {
        starfield.setTheme(theme);
    }
}

function toggleTheme() {
    applyTheme(currentTheme === 'dark' ? 'light' : 'dark');
}

// ===== 初始化 =====
document.addEventListener('DOMContentLoaded', init);

async function init() {
    // 从 localStorage 恢复 token 到 meta 标签（login 跳转后需要）
    const savedToken = localStorage.getItem('star-track-token');
    if (savedToken) {
        const meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && !meta.content) meta.content = savedToken;
    }

    starfield = new StarField(document.getElementById('starfield'));
    const now = new Date();
    calYear = now.getFullYear();
    calMonth = now.getMonth();

    // 清理过期的 schedule 缓存（只保留当天）
    cleanOldSchedules();

    // 主题初始化
    const savedTheme = localStorage.getItem('star-todo-theme') || 'dark';
    applyTheme(savedTheme);

    await loadTodos();
    await render();
    bindEvents();
}

// ===== 数据层 =====

// 统一 API 响应处理：401 时跳转登录
async function apiFetch(url, options = {}) {
    // 从 meta 取 token（优先），回退到 localStorage
    let csrfToken = document.querySelector('meta[name="csrf-token"]')?.content;
    if (!csrfToken) {
        csrfToken = localStorage.getItem('star-track-token');
        const meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && csrfToken) meta.content = csrfToken;
    }
    // 所有请求都带 X-CSRF-Token header（auth 需要）
    if (csrfToken) {
        options.headers = options.headers || {};
        if (options.headers instanceof Headers) {
            options.headers.set('X-CSRF-Token', csrfToken);
        } else {
            options.headers['X-CSRF-Token'] = csrfToken;
        }
    }
    const res = await fetch(url, options);
    if (res.status === 401) {
        localStorage.removeItem('star-track-token');
        window.location.href = 'login.php';
        throw new Error('未登录');
    }
    return res;
}

async function loadTodos() {
    try {
        const res = await apiFetch('api/get_todos.php');
        const data = await res.json();
        if (data.success) {
            todos = data.todos;
        }
    } catch (e) {
        console.error('加载任务失败:', e);
    }
}

async function loadHistory() {
    try {
        const res = await apiFetch('api/get_history.php');
        const data = await res.json();
        if (data.success) {
            historyData = data.history;
        }
    } catch (e) {
        console.error('加载历史失败:', e);
    }
}

async function apiAddTodo(data) {
    const formData = new FormData();
    Object.entries(data).forEach(([k, v]) => {
        if (v !== null && v !== undefined && v !== '') formData.append(k, v);
    });
    const res = await apiFetch('api/add_todo.php', { method: 'POST', body: formData });
    return res.json();
}

async function apiCompleteTodo(id, completedDate, completeChildren) {
    const formData = new FormData();
    formData.append('id', id);
    if (completedDate) formData.append('completed_date', completedDate);
    if (completeChildren) formData.append('complete_children', '1');
    const res = await apiFetch('api/complete_todo.php', { method: 'POST', body: formData });
    return res.json();
}

async function apiDeleteTodo(id) {
    const formData = new FormData();
    formData.append('id', id);
    const res = await apiFetch('api/delete_todo.php', { method: 'POST', body: formData });
    return res.json();
}

async function apiUpdateTodo(id, data) {
    const res = await apiFetch('api/update_todo.php', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, ...data })
    });
    return res.json();
}

// ===== 渲染 =====
async function render() {
    if (currentView === 'today') {
        await renderTodayView();
    } else {
        renderHistoryView();
    }
}

async function renderTodayView() {
    const today = getTodayStr();

    // 从服务器加载当天时间轴（跨设备同步）
    await loadSchedule();

    // 渲染时间轴
    renderTimeline();

    // 渲染统一星空（全部任务，视觉区分今日相关/休眠/活跃）
    renderConstellations(todos, today);
}

// ===== 时间轴渲染与交互 =====
function renderTimeline() {
    const body = document.getElementById('timelineBody');
    body.innerHTML = '';

    for (let h = SCHEDULE_START; h < SCHEDULE_END; h++) {
        const slot = document.createElement('div');
        slot.className = 'timeline-slot';
        slot.dataset.hour = h;

        // 时间刻度
        const time = document.createElement('div');
        time.className = 'timeline-time';
        const displayHour = h === 0 ? 12 : (h > 12 ? h - 12 : h);
        const suffix = h < 12 ? ' am' : ' pm';
        time.textContent = displayHour + ':00' + suffix;
        slot.appendChild(time);

        // 放置区
        const drop = document.createElement('div');
        drop.className = 'timeline-drop';

        // 渲染已安排的任务芯片
        const items = schedule[h] || [];
        items.forEach(id => {
            const todo = findTodoById(parseInt(id));
            if (todo) {
                const colors = TYPE_COLORS[todo.task_type] || TYPE_COLORS.self;
                const chip = document.createElement('span');
                chip.className = 'timeline-chip';
                chip.style.background = colors.main;
                chip.innerHTML = `<span class="chip-dot"></span><span class="chip-title">${escHtml(todo.title)}</span><span class="chip-remove"><i class="bi bi-x"></i></span>`;
                chip.onclick = (e) => {
                    if (e.target.closest('.chip-remove')) {
                        removeFromSchedule(h, String(todo.id));
                    } else {
                        showDetail(todo);
                    }
                };
                drop.appendChild(chip);
            }
        });

        slot.appendChild(drop);

        // 拖放事件
        slot.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'copy';
            slot.classList.add('drag-over');
        });
        slot.addEventListener('dragleave', () => {
            slot.classList.remove('drag-over');
        });
        slot.addEventListener('drop', (e) => {
            e.preventDefault();
            slot.classList.remove('drag-over');
            const todoId = e.dataTransfer.getData('text/plain');
            if (todoId) {
                addToSchedule(h, todoId);
            }
        });

        body.appendChild(slot);
    }
}

async function addToSchedule(hour, todoId) {
    if (!schedule[hour]) schedule[hour] = [];
    if (schedule[hour].includes(todoId)) return;

    // 先乐观更新 UI
    schedule[hour].push(todoId);
    saveSchedule();
    renderTimeline();
    updateConstellationScheduledState();

    // 异步同步到服务器
    try {
        const fd = new FormData();
        fd.append('action', 'add');
        fd.append('todo_id', todoId);
        fd.append('slot_date', getTodayStr());
        fd.append('slot_hour', hour);
        await apiFetch('api/save_timeline.php', { method: 'POST', body: fd });
    } catch (e) {
        console.error('同步时间轴失败:', e);
    }
}

async function removeFromSchedule(hour, todoId) {
    if (!schedule[hour]) return;

    // 先乐观更新 UI
    schedule[hour] = schedule[hour].filter(id => id !== todoId);
    if (schedule[hour].length === 0) delete schedule[hour];
    saveSchedule();
    renderTimeline();
    updateConstellationScheduledState();

    // 异步同步到服务器
    try {
        const fd = new FormData();
        fd.append('action', 'remove');
        fd.append('todo_id', todoId);
        fd.append('slot_date', getTodayStr());
        fd.append('slot_hour', hour);
        await apiFetch('api/save_timeline.php', { method: 'POST', body: fd });
    } catch (e) {
        console.error('同步时间轴失败:', e);
    }
}

function getScheduledIds() {
    const ids = new Set();
    Object.values(schedule).forEach(list => {
        list.forEach(id => ids.add(id));
    });
    return ids;
}

function updateConstellationScheduledState() {
    const scheduledIds = getScheduledIds();
    document.querySelectorAll('.constellation-node.today-relevant').forEach(node => {
        if (scheduledIds.has(node.dataset.id)) {
            node.classList.add('scheduled');
        } else {
            node.classList.remove('scheduled');
        }
    });
}

function getScheduleKey() {
    return 'star-todo-schedule-' + getTodayStr();
}

async function loadSchedule() {
    // 先从 localStorage 读取，快速渲染
    const key = getScheduleKey();
    try {
        const raw = localStorage.getItem(key);
        if (raw) schedule = JSON.parse(raw);
    } catch {
        schedule = {};
    }

    // 再从服务器拉取最新数据
    try {
        const res = await apiFetch('api/get_timeline.php?date=' + getTodayStr());
        const data = await res.json();
        if (data.success && data.schedule) {
            schedule = data.schedule;
            // 同步回 localStorage 作为缓存
            saveSchedule();
        }
    } catch (e) {
        console.error('加载时间轴失败，使用本地缓存:', e);
        // API 失败时保持 localStorage 数据
    }
}

function saveSchedule() {
    localStorage.setItem(getScheduleKey(), JSON.stringify(schedule));
}

function cleanOldSchedules() {
    const prefix = 'star-todo-schedule-';
    const todayKey = prefix + getTodayStr();
    for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i);
        if (key && key.startsWith(prefix) && key !== todayKey) {
            localStorage.removeItem(key);
        }
    }
}

function escHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function renderConstellations(allTodos, today) {
    const zone = document.getElementById('constellation-zone');
    zone.innerHTML = '';

    const activeTodos = allTodos.filter(t => !t.completed);

    if (activeTodos.length === 0) {
        zone.innerHTML = '<div class="empty-state"><i class="bi bi-moon"></i>星空很安静</div>';
        return;
    }

    // 单一融合画布
    const canvas = document.createElement('div');
    canvas.className = 'nebula-canvas nebula-canvas-fused';

    // 按类型分组
    const groups = {};
    activeTodos.forEach(todo => {
        const type = todo.task_type || 'self';
        if (!groups[type]) groups[type] = [];
        groups[type].push(todo);
    });
    const types = Object.keys(groups);
    const typeCount = types.length;

    // 为每种类型分配扇区角度，同类型节点倾向于聚集
    const typeSectors = {};
    types.forEach((type, i) => {
        typeSectors[type] = {
            start: (i / typeCount) * Math.PI * 2,
            end: ((i + 1) / typeCount) * Math.PI * 2
        };
    });

    // 生成所有节点的位置（同类型在扇区内散布）
    const allPositions = [];
    const todoToIdx = new Map();
    let globalIdx = 0;

    activeTodos.forEach(todo => {
        const type = todo.task_type || 'self';
        const sector = typeSectors[type];
        const angle = sector.start + Math.random() * (sector.end - sector.start);
        const r = 14 + Math.random() * 32;
        const x = Math.max(8, Math.min(92, 50 + r * Math.cos(angle)));
        const y = Math.max(8, Math.min(92, 50 + r * Math.sin(angle)));
        allPositions.push({ x, y });
        todoToIdx.set(todo.id, globalIdx);
        globalIdx++;
    });

    // 碰撞回避：最小间距 22%（考虑标题文字高度）
    for (let i = 0; i < allPositions.length; i++) {
        for (let j = i + 1; j < allPositions.length; j++) {
            const dx = allPositions[i].x - allPositions[j].x;
            const dy = allPositions[i].y - allPositions[j].y;
            const dist = Math.sqrt(dx * dx + dy * dy);
            if (dist < 22) {
                const push = (22 - dist) / 2;
                const nx = dx / Math.max(dist, 0.1);
                const ny = dy / Math.max(dist, 0.1);
                allPositions[i].x = Math.max(8, Math.min(92, allPositions[i].x + nx * push));
                allPositions[i].y = Math.max(8, Math.min(92, allPositions[i].y + ny * push));
                allPositions[j].x = Math.max(8, Math.min(92, allPositions[j].x - nx * push));
                allPositions[j].y = Math.max(8, Math.min(92, allPositions[j].y - ny * push));
            }
        }
    }

    // SVG 连线：同类型节点之间画星座连线
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.classList.add('constellation-svg');
    svg.setAttribute('preserveAspectRatio', 'none');

    const allLines = [];
    types.forEach(type => {
        const groupTodos = groups[type];
        const colors = TYPE_COLORS[type] || TYPE_COLORS.self;
        const groupPositions = groupTodos.map(t => allPositions[todoToIdx.get(t.id)]);
        const lines = generateConstellationLines(groupPositions);
        lines.forEach(([a, b]) => {
            const gA = todoToIdx.get(groupTodos[a].id);
            const gB = todoToIdx.get(groupTodos[b].id);
            const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
            line.setAttribute('x1', allPositions[gA].x + '%');
            line.setAttribute('y1', allPositions[gA].y + '%');
            line.setAttribute('x2', allPositions[gB].x + '%');
            line.setAttribute('y2', allPositions[gB].y + '%');
            line.setAttribute('stroke', colors.main);
            line.setAttribute('stroke-opacity', '0.12');
            line.setAttribute('stroke-width', '1');
            line.classList.add('constellation-line');
            line.dataset.type = type;
            svg.appendChild(line);
            allLines.push({ a: gA, b: gB, type, lineEl: line });
        });
    });

    canvas.appendChild(svg);

    // 渲染节点
    activeTodos.forEach((todo, idx) => {
        const isDorm = isDormant(todo, today);
        const isToday = isTodayRelevant(todo, today);
        const colors = TYPE_COLORS[todo.task_type || 'self'] || TYPE_COLORS.self;

        const node = document.createElement('div');
        node.className = 'constellation-node';
        if (isDorm) node.classList.add('dormant');
        if (isToday) node.classList.add('today-relevant');
        node.dataset.type = todo.task_type || 'self';
        node.dataset.id = todo.id;
        node.style.left = allPositions[idx].x + '%';
        node.style.top = allPositions[idx].y + '%';

        // 行星图标
        const planet = document.createElement('div');
        planet.className = 'constellation-planet';

        // 今日脉冲光环（放在 planet 内，绝对定位居中）
        if (isToday && !isDorm) {
            const pulse = document.createElement('div');
            pulse.className = 'today-pulse';
            planet.appendChild(pulse);
        }

        // 子任务卫星（用 left/top 替代 transform）
        if (todo.children && todo.children.length > 0) {
            const childCount = Math.min(todo.children.length, 4);
            const orbitRadius = 18 + childCount * 3;
            todo.children.forEach((child, ci) => {
                if (ci >= 4) return;
                const angle = (ci / childCount) * Math.PI * 2 - Math.PI / 2;
                const satellite = document.createElement('div');
                satellite.className = 'constellation-satellite';
                satellite.style.left = (16 + Math.cos(angle) * orbitRadius) + 'px';
                satellite.style.top = (16 + Math.sin(angle) * orbitRadius) + 'px';
                if (child.completed) satellite.classList.add('completed');
                // 卫星仅展示，不可点击
                satellite.title = child.title;
                planet.appendChild(satellite);
            });
        }

        node.appendChild(planet);

        // 进度环（挂在 planet 上，居中显示）
        if (todo.progress > 0 && todo.progress < 100) {
            const ring = createMiniProgressRing(todo.progress, colors.main);
            planet.appendChild(ring);
        }

        // 标题
        const title = document.createElement('span');
        title.className = 'constellation-node-title';
        title.textContent = todo.title;
        node.appendChild(title);

        // 日期标签
        const dateTag = document.createElement('span');
        dateTag.className = 'constellation-node-due';
        if (isDorm && todo.start_date) {
            dateTag.textContent = formatDateShort(todo.start_date);
        } else if (todo.due_date) {
            dateTag.textContent = formatDateShort(todo.due_date);
        }
        if (dateTag.textContent) node.appendChild(dateTag);

        // 点击
        node.onclick = () => showDetail(todo);

        // 拖拽
        node.draggable = true;
        node.addEventListener('dragstart', (e) => {
            e.dataTransfer.setData('text/plain', String(todo.id));
            e.dataTransfer.effectAllowed = 'copy';
            node.classList.add('dragging');
        });
        node.addEventListener('dragend', () => { node.classList.remove('dragging'); });

        // hover 高亮同类型连线
        node.addEventListener('mouseenter', () => {
            node.classList.add('highlight');
            allLines.forEach(l => {
                if (l.a === idx || l.b === idx) {
                    l.lineEl.setAttribute('stroke-opacity', '0.45');
                    l.lineEl.setAttribute('stroke-width', '1.5');
                }
            });
        });
        node.addEventListener('mouseleave', () => {
            node.classList.remove('highlight');
            allLines.forEach(l => {
                if (l.a === idx || l.b === idx) {
                    l.lineEl.setAttribute('stroke-opacity', '0.12');
                    l.lineEl.setAttribute('stroke-width', '1');
                }
            });
        });

        canvas.appendChild(node);
    });

    // 类型图例（固定在右下角）
    const legendEl = document.getElementById('constellationLegend');
    legendEl.innerHTML = '';
    types.forEach(type => {
        const colors = TYPE_COLORS[type] || TYPE_COLORS.self;
        const item = document.createElement('span');
        item.className = 'legend-item';
        item.innerHTML = '<span class="legend-dot" style="background:' + colors.main + '"></span>' + colors.name;
        legendEl.appendChild(item);
    });

    zone.appendChild(canvas);
}

// ---- 以下为旧的分组渲染逻辑（已废弃） ----
// 判断任务是否今日相关
function isTodayRelevant(todo, today) {
    return todo.due_date === today ||
        (todo.start_date && todo.start_date <= today &&
         (!todo.due_date || todo.due_date >= today));
}

// 判断任务是否休眠（开始日期在未来）
function isDormant(todo, today) {
    return todo.start_date && todo.start_date > today;
}

// 生成星座布局位置（伪随机，保证最小间距）
function generateConstellationPositions(count, container) {
    const positions = [];
    const margin = 12; // 百分比边距
    const minDist = 18; // 最小间距百分比

    for (let i = 0; i < count; i++) {
        let attempts = 0;
        let x, y;
        do {
            x = margin + Math.random() * (100 - margin * 2);
            y = margin + Math.random() * (100 - margin * 2);
            attempts++;
        } while (
            attempts < 50 &&
            positions.some(p => Math.hypot(p.x - x, p.y - y) < minDist)
        );
        positions.push({ x, y });
    }
    return positions;
}

// 生成星座连线（每个点连接最近的 1~2 个点，形成链状星座）
function generateConstellationLines(positions) {
    if (positions.length < 2) return [];
    const lines = [];
    const connected = new Set();

    // 最小生成树式连线：每个未连接点找最近的已连接点
    connected.add(0);
    while (connected.size < positions.length) {
        let bestDist = Infinity;
        let bestA = -1, bestB = -1;
        for (const a of connected) {
            for (let b = 0; b < positions.length; b++) {
                if (connected.has(b)) continue;
                const dist = Math.hypot(positions[a].x - positions[b].x, positions[a].y - positions[b].y);
                if (dist < bestDist) {
                    bestDist = dist;
                    bestA = a;
                    bestB = b;
                }
            }
        }
        if (bestB >= 0) {
            lines.push([bestA, bestB]);
            connected.add(bestB);
        } else break;
    }

    // 额外加一条最短的跨边，避免纯链状
    if (positions.length >= 4) {
        let extraDist = Infinity;
        let extraA = -1, extraB = -1;
        for (let a = 0; a < positions.length; a++) {
            for (let b = a + 2; b < positions.length; b++) {
                const exists = lines.some(([x, y]) => (x===a&&y===b)||(x===b&&y===a));
                if (exists) continue;
                const dist = Math.hypot(positions[a].x - positions[b].x, positions[a].y - positions[b].y);
                if (dist < extraDist && dist < 40) {
                    extraDist = dist;
                    extraA = a;
                    extraB = b;
                }
            }
        }
        if (extraA >= 0) lines.push([extraA, extraB]);
    }

    return lines;
}

// 迷你进度环（星座节点用）
function createMiniProgressRing(progress, color) {
    const size = 38;
    const r = 16;
    const c = 2 * Math.PI * r;
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.classList.add('mini-progress-ring');
    svg.setAttribute('width', size);
    svg.setAttribute('height', size);
    svg.setAttribute('viewBox', `0 0 ${size} ${size}`);

    const bg = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    bg.setAttribute('cx', size/2); bg.setAttribute('cy', size/2); bg.setAttribute('r', r);
    bg.setAttribute('fill', 'none'); bg.setAttribute('stroke', 'var(--ring-bg)'); bg.setAttribute('stroke-width', '1.5');

    const fill = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    fill.setAttribute('cx', size/2); fill.setAttribute('cy', size/2); fill.setAttribute('r', r);
    fill.setAttribute('fill', 'none'); fill.setAttribute('stroke', color); fill.setAttribute('stroke-width', '2');
    fill.setAttribute('stroke-dasharray', c); fill.setAttribute('stroke-dashoffset', c - (progress/100)*c);
    fill.setAttribute('stroke-linecap', 'round');
    fill.setAttribute('transform', `rotate(-90 ${size/2} ${size/2})`);
    fill.style.filter = `drop-shadow(0 0 3px ${color}66)`;

    svg.appendChild(bg);
    svg.appendChild(fill);
    return svg;
}

function renderHistoryView() {
    loadHistory().then(() => {
        const grid = document.getElementById('calendarGrid');
        grid.innerHTML = '';

        // 更新月份标签
        const monthNames = ['1月','2月','3月','4月','5月','6月','7月','8月','9月','10月','11月','12月'];
        document.getElementById('calMonthLabel').textContent = `${calYear}年 ${monthNames[calMonth]}`;

        // 计算本月第一天和最后一天
        const firstDay = new Date(calYear, calMonth, 1);
        const lastDay = new Date(calYear, calMonth + 1, 0);
        const daysInMonth = lastDay.getDate();

        // 本月第一天是周几（调整为周一=0）
        let startWeekday = firstDay.getDay() - 1; // JS的0=周日
        if (startWeekday < 0) startWeekday = 6;

        // 上月补位天数
        const prevMonthDays = new Date(calYear, calMonth, 0).getDate();

        const today = getTodayStr();

        // 渲染上月尾部
        for (let i = startWeekday - 1; i >= 0; i--) {
            const cell = createCalCell(prevMonthDays - i, true, null, false);
            grid.appendChild(cell);
        }

        // 渲染本月每天
        for (let d = 1; d <= daysInMonth; d++) {
            const dateStr = `${calYear}-${String(calMonth + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
            const isToday = dateStr === today;
            const items = historyData[dateStr] || [];
            const cell = createCalCell(d, false, items, isToday, dateStr);
            grid.appendChild(cell);
        }

        // 渲染下月开头（补满6行）
        const totalCells = startWeekday + daysInMonth;
        const remainder = totalCells % 7;
        if (remainder > 0) {
            for (let i = 1; i <= 7 - remainder; i++) {
                const cell = createCalCell(i, true, null, false);
                grid.appendChild(cell);
            }
        }
    });
}

function createCalCell(dayNum, isOtherMonth, items, isToday, dateStr) {
    const cell = document.createElement('div');
    cell.className = 'cal-cell';
    if (isOtherMonth) cell.classList.add('other-month');
    if (isToday) cell.classList.add('today');
    if (items && items.length > 0) cell.classList.add('has-tasks');

    const num = document.createElement('div');
    num.className = 'cal-day-num';
    num.textContent = dayNum;
    cell.appendChild(num);

    if (items && items.length > 0) {
        const tasksDiv = document.createElement('div');
        tasksDiv.className = 'cal-tasks';

        // 展开态显示全部，收起态最多显示3个
        const maxShow = 3;

        items.forEach((item, idx) => {
            const colors = TYPE_COLORS[item.task_type] || TYPE_COLORS.self;
            const task = document.createElement('div');
            task.className = 'cal-task';
            if (idx >= maxShow) task.style.display = 'none';

            const dot = document.createElement('div');
            dot.className = 'cal-task-dot';
            if (item.type === 'completed') {
                dot.style.background = colors.main;
                dot.style.boxShadow = `0 0 4px ${colors.glow}`;
            } else {
                dot.style.background = 'transparent';
                dot.style.border = `1.5px solid ${colors.main}`;
                dot.style.boxShadow = `0 0 4px ${colors.glowSoft}`;
            }

            const title = document.createElement('span');
            title.className = 'cal-task-title';
            title.textContent = item.title;

            task.appendChild(dot);
            task.appendChild(title);
            tasksDiv.appendChild(task);
        });

        // 超出3个时显示 +N
        if (items.length > maxShow) {
            const more = document.createElement('div');
            more.className = 'cal-task';
            more.innerHTML = `<span style="color:var(--text-faint)">+${items.length - maxShow}</span>`;
            tasksDiv.appendChild(more);
        }

        cell.appendChild(tasksDiv);
    }

    // 点击弹出浮层（所有格子都可点击，非其他月份的）
    if (!isOtherMonth && dateStr) {
        cell.style.cursor = 'pointer';
        cell.addEventListener('click', () => openCalPopup(dateStr, items || []));
    }

    return cell;
}

// 确认弹窗（返回 Promise<boolean>）
function showConfirmDialog(message, okText, cancelText) {
    return new Promise(resolve => {
        const dialog = document.getElementById('confirmDialog');
        const msg = document.getElementById('confirmMessage');
        const okBtn = document.getElementById('confirmOk');
        const cancelBtn = document.getElementById('confirmCancel');
        const backdrop = dialog.querySelector('.cal-popup-backdrop');

        msg.textContent = message;
        okBtn.textContent = okText || '确定';
        cancelBtn.textContent = cancelText || '取消';
        dialog.classList.remove('hidden');

        const cleanup = (result) => {
            dialog.classList.add('hidden');
            okBtn.onclick = null;
            cancelBtn.onclick = null;
            backdrop.onclick = null;
            resolve(result);
        };

        okBtn.onclick = () => cleanup(true);
        cancelBtn.onclick = () => cleanup(false);
        backdrop.onclick = () => cleanup(false);
    });
}

function openCalPopup(dateStr, items) {
    const popup = document.getElementById('calPopup');
    const title = document.getElementById('calPopupTitle');
    const body = document.getElementById('calPopupBody');

    // 格式化标题
    const parts = dateStr.split('-');
    const weekdays = ['日','一','二','三','四','五','六'];
    const d = new Date(parseInt(parts[0]), parseInt(parts[1]) - 1, parseInt(parts[2]));
    title.textContent = `${parseInt(parts[1])}月${parseInt(parts[2])}日 周${weekdays[d.getDay()]}`;

    body.innerHTML = '';

    if (items.length === 0) {
        body.innerHTML = '<div class="popup-empty">这一天很安静</div>';
    } else {
        items.forEach(item => {
            const colors = TYPE_COLORS[item.task_type] || TYPE_COLORS.self;
            const row = document.createElement('div');
            row.className = 'popup-task';

            const dot = document.createElement('div');
            dot.className = 'popup-task-dot';
            dot.style.background = colors.main;
            dot.style.boxShadow = `0 0 6px ${colors.glow}`;

            const info = document.createElement('div');
            info.className = 'popup-task-info';

            const nameRow = document.createElement('div');
            nameRow.style.display = 'flex';
            nameRow.style.alignItems = 'center';
            nameRow.style.gap = '8px';

            const name = document.createElement('span');
            name.className = 'popup-task-title';
            name.textContent = item.title;

            // 状态标签
            const badge = document.createElement('span');
            badge.className = 'popup-badge';
            if (item.type === 'completed') {
                badge.textContent = '已完成';
                badge.dataset.status = 'completed';
            } else {
                badge.textContent = item.progress + '%';
                badge.dataset.status = 'progress';
            }

            nameRow.appendChild(name);
            nameRow.appendChild(badge);

            const meta = document.createElement('div');
            meta.className = 'popup-task-meta';
            let metaText = '';
            if (item.completed_at) metaText += item.completed_at.substring(11, 16) + ' 完成';
            if (item.start_date && item.due_date && item.start_date !== item.due_date) {
                metaText += (metaText ? ' · ' : '') + formatDateShort(item.start_date) + ' → ' + formatDateShort(item.due_date);
            } else if (item.due_date) {
                metaText += (metaText ? ' · ' : '') + '截止 ' + formatDateShort(item.due_date);
            }
            meta.textContent = metaText;

            info.appendChild(nameRow);
            if (metaText) info.appendChild(meta);
            row.appendChild(dot);
            row.appendChild(info);
            body.appendChild(row);
        });
    }

    popup.classList.remove('hidden');
}

function closeCalPopup() {
    document.getElementById('calPopup').classList.add('hidden');
}

// ===== 星球元素创建 =====
function createStarElement(todo, size, isDormant) {
    const colors = TYPE_COLORS[todo.task_type] || TYPE_COLORS.self;

    const star = document.createElement('div');
    star.className = 'star' + (isDormant ? ' dormant' : '');
    star.dataset.id = todo.id;
    star.dataset.type = todo.task_type || 'self';
    star.style.setProperty('--size', size + 'px');

    const body = document.createElement('div');
    body.className = 'star-body';

    // 进度光环
    if (todo.progress > 0) {
        const ringSvg = createProgressRing(todo.progress, colors);
        ringSvg.classList.add('star-ring');
        body.appendChild(ringSvg);
    }

    star.appendChild(body);

    // 标题
    const title = document.createElement('div');
    title.className = 'star-title';
    title.textContent = todo.title;
    star.appendChild(title);

    // 进度文字
    if (todo.progress > 0 && todo.progress < 100) {
        const progress = document.createElement('div');
        progress.className = 'star-progress';
        progress.textContent = todo.progress + '%';
        star.appendChild(progress);
    }

    // 子任务数量标记
    if (todo.children && todo.children.length > 0) {
        const badge = document.createElement('div');
        badge.style.cssText = `
            position: absolute; top: -6px; right: -6px;
            background: rgba(0,0,0,0.6); border: 1px solid ${colors.main};
            border-radius: 10px; padding: 1px 6px;
            font-size: 0.65rem; color: ${colors.main};
        `;
        badge.textContent = todo.children.length;
        body.appendChild(badge);
    }

    // 点击打开详情
    star.onclick = () => showDetail(todo);

    return star;
}

function createProgressRing(progress, colors) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 100 100');

    const circumference = 2 * Math.PI * 45; // r=45
    const offset = circumference * (1 - progress / 100);

    const bgCircle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    bgCircle.setAttribute('cx', '50');
    bgCircle.setAttribute('cy', '50');
    bgCircle.setAttribute('r', '45');
    bgCircle.classList.add('ring-bg');

    const fillCircle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    fillCircle.setAttribute('cx', '50');
    fillCircle.setAttribute('cy', '50');
    fillCircle.setAttribute('r', '45');
    fillCircle.classList.add('ring-fill');
    fillCircle.style.strokeDasharray = circumference;
    fillCircle.style.strokeDashoffset = offset;
    fillCircle.style.setProperty('--color', colors.main);
    fillCircle.style.setProperty('--color-glow', colors.glow);

    svg.appendChild(bgCircle);
    svg.appendChild(fillCircle);

    return svg;
}

// ===== 详情面板 =====
function showDetail(todo) {
    selectedTodo = todo;
    const panel = document.getElementById('detail-panel');
    const overlay = document.getElementById('overlay');
    const colors = TYPE_COLORS[todo.task_type] || TYPE_COLORS.self;

    // 类型色点
    document.getElementById('detailTypeDot').style.background = colors.main;
    document.getElementById('detailTypeDot').style.boxShadow = `0 0 8px ${colors.glow}`;

    // 标题 — 点击可编辑
    const titleEl = document.getElementById('detailTitle');
    const titleInput = document.getElementById('detailTitleInput');
    titleEl.textContent = todo.title;
    titleEl.style.display = '';
    titleInput.style.display = 'none';

    // 类型切换点
    renderDetailTypeDots(todo);

    // 进度
    const progressFill = document.getElementById('detailProgressFill');
    progressFill.style.width = todo.progress + '%';
    progressFill.style.background = colors.main;
    progressFill.style.boxShadow = `0 0 8px ${colors.glow}`;
    document.getElementById('detailProgressText').textContent = todo.progress + '%';
    document.getElementById('progressSlider').value = todo.progress;

    // 元信息
    const meta = document.getElementById('detailMeta');
    meta.innerHTML = '';
    const tagSpan = document.createElement('span');
    tagSpan.innerHTML = `<i class="bi bi-tag"></i> `;
    tagSpan.appendChild(document.createTextNode(colors.name));
    meta.appendChild(tagSpan);

    // 日期编辑
    document.getElementById('detailDueDate').value = todo.due_date || '';
    document.getElementById('detailStartDate').value = todo.start_date || '';

    // 子任务列表
    renderDetailChildren(todo);

    // 按钮状态
    const btnComplete = document.getElementById('btnComplete');

    // 完成日期默认今天（本地时间）
    const completedDateInput = document.getElementById('completedDate');
    const completeDateLabel = document.getElementById('completeDateLabel');
    const btnCompleteDate = document.getElementById('btnCompleteDate');
    const now = new Date();
    const localDate = now.getFullYear() + '-' + String(now.getMonth()+1).padStart(2,'0') + '-' + String(now.getDate()).padStart(2,'0');
    completedDateInput.value = localDate;
    completedDateInput.max = localDate;
    completeDateLabel.textContent = '今天';
    const showCompleted = !todo.completed;
    btnComplete.style.display = showCompleted ? 'flex' : 'none';
    btnCompleteDate.style.display = showCompleted ? 'flex' : 'none';
    completedDateInput.style.display = showCompleted ? 'inline-block' : 'none';

    panel.classList.add('open');
    overlay.classList.remove('hidden');
}

function renderDetailTypeDots(todo) {
    const container = document.getElementById('detailTypeDots');
    container.innerHTML = '';
    const currentType = todo.task_type || 'self';

    Object.keys(TYPE_COLORS).forEach(type => {
        const dot = document.createElement('button');
        dot.type = 'button';
        dot.className = 'type-dot' + (type === currentType ? ' active' : '');
        dot.style.setProperty('--dot-color', TYPE_COLORS[type].main);
        dot.style.background = TYPE_COLORS[type].main;
        dot.title = TYPE_COLORS[type].name;
        dot.dataset.type = type;
        dot.addEventListener('click', () => onDetailTypeChange(type));
        container.appendChild(dot);
    });
}

async function onDetailTypeChange(newType) {
    if (!selectedTodo || selectedTodo.task_type === newType) return;
    try {
        await apiUpdateTodo(selectedTodo.id, { task_type: newType });
        await refresh();
        const updated = findTodoById(selectedTodo.id);
        if (updated) showDetail(updated);
    } catch (err) {
        console.error('切换类型失败:', err);
    }
}

function onDetailTitleClick() {
    if (!selectedTodo) return;
    const titleEl = document.getElementById('detailTitle');
    const titleInput = document.getElementById('detailTitleInput');
    titleInput.value = selectedTodo.title;
    titleEl.style.display = 'none';
    titleInput.style.display = '';
    titleInput.focus();
    titleInput.select();
}

async function onDetailTitleConfirm() {
    if (!selectedTodo) return;
    const titleInput = document.getElementById('detailTitleInput');
    const newTitle = titleInput.value.trim();
    if (!newTitle || newTitle === selectedTodo.title) {
        // 取消编辑
        document.getElementById('detailTitle').style.display = '';
        titleInput.style.display = 'none';
        return;
    }
    try {
        await apiUpdateTodo(selectedTodo.id, { title: newTitle });
        await refresh();
        const updated = findTodoById(selectedTodo.id);
        if (updated) showDetail(updated);
    } catch (err) {
        console.error('更新标题失败:', err);
    }
}

function renderDetailChildren(todo) {
    const container = document.getElementById('detailChildren');
    container.innerHTML = '';
    const colors = TYPE_COLORS[todo.task_type] || TYPE_COLORS.self;

    if (!todo.children || todo.children.length === 0) return;

    todo.children.forEach((child, idx) => {
        const childDiv = document.createElement('div');
        childDiv.className = 'child-task';
        childDiv.dataset.id = child.id;

        // 只有按住拖拽手柄时才允许拖拽
        childDiv.addEventListener('mousedown', (e) => {
            if (e.target.closest('.child-drag-handle')) {
                childDiv.draggable = true;
            } else {
                childDiv.draggable = false;
            }
        });

        // 拖拽手柄
        const handle = document.createElement('span');
        handle.className = 'child-drag-handle';
        handle.innerHTML = '<i class="bi bi-grip-vertical"></i>';
        childDiv.appendChild(handle);

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'form-check-input';
        checkbox.checked = child.completed;
        checkbox.style.accentColor = colors.main;
        checkbox.onchange = () => handleCompleteChild(child.id, checkbox.checked);

        const title = document.createElement('span');
        title.className = 'child-task-title' + (child.completed ? ' completed' : '');
        title.textContent = child.title;

        childDiv.appendChild(checkbox);
        childDiv.appendChild(title);

        // 添加子步骤按钮
        const addStepBtn = document.createElement('button');
        addStepBtn.type = 'button';
        addStepBtn.className = 'child-add-step-btn';
        addStepBtn.title = '添加子步骤';
        addStepBtn.innerHTML = '<i class="bi bi-plus"></i>';
        addStepBtn.onclick = () => onAddStep(child.id);
        childDiv.appendChild(addStepBtn);

        // 进度
        if (child.progress > 0 && child.progress < 100) {
            const prog = document.createElement('span');
            prog.className = 'child-progress';
            prog.textContent = child.progress + '%';
            childDiv.appendChild(prog);
        }

        // 拖拽排序事件
        childDiv.addEventListener('dragstart', (e) => {
            childDiv.classList.add('child-dragging');
            e.dataTransfer.setData('text/plain', String(child.id));
            e.dataTransfer.effectAllowed = 'move';
        });
        childDiv.addEventListener('dragend', () => {
            childDiv.classList.remove('child-dragging');
            childDiv.draggable = false;
            container.querySelectorAll('.child-task').forEach(el => {
                el.classList.remove('child-drag-over-top', 'child-drag-over-bottom');
            });
        });
        childDiv.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
            container.querySelectorAll('.child-task').forEach(el => {
                el.classList.remove('child-drag-over-top', 'child-drag-over-bottom');
            });
            const rect = childDiv.getBoundingClientRect();
            const midY = rect.top + rect.height / 2;
            if (e.clientY < midY) {
                childDiv.classList.add('child-drag-over-top');
            } else {
                childDiv.classList.add('child-drag-over-bottom');
            }
        });
        childDiv.addEventListener('dragleave', () => {
            childDiv.classList.remove('child-drag-over-top', 'child-drag-over-bottom');
        });
        childDiv.addEventListener('drop', (e) => {
            e.preventDefault();
            childDiv.classList.remove('child-drag-over-top', 'child-drag-over-bottom');
            const draggedId = e.dataTransfer.getData('text/plain');
            if (!draggedId || String(child.id) === draggedId) return;
            onChildReorder(todo.id, parseInt(draggedId), child.id, e.clientY < childDiv.getBoundingClientRect().top + childDiv.getBoundingClientRect().height / 2 ? 'before' : 'after');
        });

        container.appendChild(childDiv);

        // 步骤
        if (child.children && child.children.length > 0) {
            const stepsDiv = document.createElement('div');
            stepsDiv.className = 'child-steps';

            // 折叠按钮
            const toggleBtn = document.createElement('button');
            toggleBtn.type = 'button';
            toggleBtn.className = 'child-toggle';
            toggleBtn.innerHTML = '<i class="bi bi-chevron-down"></i>';
            toggleBtn.onclick = () => {
                stepsDiv.classList.toggle('collapsed');
                const icon = toggleBtn.querySelector('i');
                icon.classList.toggle('bi-chevron-down');
                icon.classList.toggle('bi-chevron-right');
            };
            childDiv.appendChild(toggleBtn);

            child.children.forEach(step => {
                const stepDiv = document.createElement('div');
                stepDiv.className = 'step-item';

                const stepCheck = document.createElement('input');
                stepCheck.type = 'checkbox';
                stepCheck.className = 'form-check-input';
                stepCheck.checked = step.completed;
                stepCheck.style.accentColor = colors.main;
                stepCheck.onchange = () => handleCompleteChild(step.id, stepCheck.checked);

                const stepTitle = document.createElement('span');
                stepTitle.className = 'step-title' + (step.completed ? ' completed' : '');
                stepTitle.textContent = step.title;

                stepDiv.appendChild(stepCheck);
                stepDiv.appendChild(stepTitle);
                stepsDiv.appendChild(stepDiv);
            });

            container.appendChild(stepsDiv);
        }
    });
}

async function onChildReorder(parentId, draggedId, targetId, position) {
    const parent = findTodoById(parentId);
    if (!parent || !parent.children) return;

    const currentOrder = parent.children.map(c => c.id);
    const fromIdx = currentOrder.indexOf(draggedId);
    if (fromIdx === -1) return;
    currentOrder.splice(fromIdx, 1);
    const toIdx = currentOrder.indexOf(targetId);
    if (toIdx === -1) return;
    const insertIdx = position === 'before' ? toIdx : toIdx + 1;
    currentOrder.splice(insertIdx, 0, draggedId);

    try {
        await apiFetch('api/reorder_todos.php', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ parent_id: parentId, order: currentOrder })
        });
        await refresh();
        const updated = findTodoById(parentId);
        if (updated) showDetail(updated);
    } catch (err) {
        console.error('排序失败:', err);
    }
}

async function onAddStep(parentChildId) {
    const title = prompt('子步骤名称：');
    if (!title || !title.trim()) return;

    try {
        await apiAddTodo({
            title: title.trim(),
            task_type: selectedTodo ? selectedTodo.task_type : 'self',
            parent_id: parentChildId
        });
        await refresh();
        if (selectedTodo) {
            const updated = findTodoById(selectedTodo.id);
            if (updated) showDetail(updated);
        }
    } catch (err) {
        console.error('添加子步骤失败:', err);
    }
}

function hideDetail() {
    selectedTodo = null;
    document.getElementById('detail-panel').classList.remove('open');
    document.getElementById('overlay').classList.add('hidden');
}

// ===== 交互处理 =====
function bindEvents() {
    // 快速添加
    document.getElementById('quickAdd').addEventListener('submit', onQuickAdd);

    // 类型选择
    document.querySelectorAll('.type-dot').forEach(dot => {
        dot.addEventListener('click', () => {
            document.querySelectorAll('.type-dot').forEach(d => d.classList.remove('active'));
            dot.classList.add('active');
            selectedType = dot.dataset.type;
        });
    });

    // 日期快捷键
    document.querySelectorAll('.date-chip').forEach(chip => {
        chip.addEventListener('click', () => {
            document.querySelectorAll('.date-chip').forEach(c => c.classList.remove('active'));
            chip.classList.add('active');
            const val = chip.dataset.date;
            const startInput = document.getElementById('inputStartDate');
            const dueInput = document.getElementById('inputDueDate');
            if (val === 'today') {
                const today = getTodayStr();
                startInput.value = today;
                dueInput.value = today;
            }
        });
    });

    // date input 变化时，取消今天快捷键的 active
    document.getElementById('inputDueDate').addEventListener('change', () => {
        document.querySelectorAll('.date-chip').forEach(c => c.classList.remove('active'));
    });
    document.getElementById('inputStartDate').addEventListener('change', () => {
        document.querySelectorAll('.date-chip').forEach(c => c.classList.remove('active'));
    });

    // 详情面板日期编辑
    document.getElementById('detailDueDate').addEventListener('change', onDetailDueDateChange);
    document.getElementById('detailStartDate').addEventListener('change', onDetailStartDateChange);

    // 详情面板标题点击编辑
    document.getElementById('detailTitle').addEventListener('click', onDetailTitleClick);
    document.getElementById('detailTitleInput').addEventListener('blur', onDetailTitleConfirm);
    document.getElementById('detailTitleInput').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            document.getElementById('detailTitleInput').blur();
        } else if (e.key === 'Escape') {
            document.getElementById('detailTitleInput').value = selectedTodo ? selectedTodo.title : '';
            document.getElementById('detailTitle').style.display = '';
            document.getElementById('detailTitleInput').style.display = 'none';
        }
    });

    // 进度条点击显示滑块
    document.getElementById('progressBarClick').addEventListener('click', () => {
        document.getElementById('progressSlider').classList.remove('hidden');
    });
    document.getElementById('progressSlider').addEventListener('input', (e) => {
        const val = parseInt(e.target.value);
        document.getElementById('detailProgressFill').style.width = val + '%';
        document.getElementById('detailProgressText').textContent = val + '%';
    });
    document.getElementById('progressSlider').addEventListener('change', async (e) => {
        const val = parseInt(e.target.value);
        if (selectedTodo) {
            await apiUpdateTodo(selectedTodo.id, { progress: val });
            selectedTodo.progress = val;
            render();
        }
    });

    // 换肤
    document.getElementById('themeToggle').addEventListener('click', toggleTheme);

    // 登出
    document.getElementById('btnLogout').addEventListener('click', async () => {
        const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content;
        await fetch('api/auth.php', {
            method: 'POST',
            headers: {'Content-Type': 'application/x-www-form-urlencoded', 'X-CSRF-Token': csrfToken || ''},
            body: 'action=logout'
        });
        // 清除 token
        localStorage.removeItem('star-track-token');
        document.querySelector('meta[name="csrf-token"]').content = '';
        window.location.href = 'login.php';
    });

    // 详情面板关闭
    document.getElementById('detailClose').addEventListener('click', hideDetail);
    document.getElementById('overlay').addEventListener('click', hideDetail);

    // 完成任务
    document.getElementById('btnComplete').addEventListener('click', () => {
        if (selectedTodo) {
            const dateInput = document.getElementById('completedDate');
            handleCompleteTodo(selectedTodo.id, dateInput.value || null);
        }
    });

    // 完成日期选择
    document.getElementById('btnCompleteDate').addEventListener('click', () => {
        document.getElementById('completedDate').showPicker();
    });
    document.getElementById('completedDate').addEventListener('change', (e) => {
        const label = document.getElementById('completeDateLabel');
        const today = new Date();
        const todayStr = today.getFullYear() + '-' + String(today.getMonth()+1).padStart(2,'0') + '-' + String(today.getDate()).padStart(2,'0');
        if (e.target.value === todayStr) {
            label.textContent = '今天';
        } else {
            const d = new Date(e.target.value + 'T00:00:00');
            label.textContent = (d.getMonth()+1) + '/' + d.getDate();
        }
    });

    // 删除任务
    document.getElementById('btnDelete').addEventListener('click', () => {
        if (selectedTodo) handleDeleteTodo(selectedTodo.id);
    });

    // 添加子任务
    document.getElementById('addChildForm').addEventListener('submit', onAddChild);

    // 视图切换
    document.querySelectorAll('.nav-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            document.querySelectorAll('.nav-tab').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            currentView = tab.dataset.view;
            document.getElementById('today-view').classList.toggle('hidden', currentView !== 'today');
            document.getElementById('history-view').classList.toggle('hidden', currentView !== 'history');
            document.getElementById('input-nebula').classList.toggle('hidden', currentView !== 'today');
            render();
        });
    });

    // 移动端 Tab 切换（时间轴 / 星座）
    document.querySelectorAll('.mobile-tab').forEach(tab => {
        tab.addEventListener('click', () => {
            const target = tab.dataset.mobileTab;
            document.querySelectorAll('.mobile-tab').forEach(t => t.classList.remove('active'));
            tab.classList.add('active');
            document.getElementById('today-zone').classList.toggle('mobile-hidden', target !== 'timeline');
            document.getElementById('constellation-zone').classList.toggle('mobile-hidden', target !== 'constellation');
        });
    });

    // 日历月份导航
    document.getElementById('calPrev').addEventListener('click', () => {
        calMonth--;
        if (calMonth < 0) { calMonth = 11; calYear--; }
        render();
    });
    document.getElementById('calNext').addEventListener('click', () => {
        calMonth++;
        if (calMonth > 11) { calMonth = 0; calYear++; }
        render();
    });
    document.getElementById('calToday').addEventListener('click', () => {
        const now = new Date();
        calYear = now.getFullYear();
        calMonth = now.getMonth();
        render();
    });

    // 导出 CSV（仅电脑端）
    const btnExport = document.getElementById('btnExportCSV');
    if (btnExport) {
        btnExport.addEventListener('click', () => {
            const dialog = document.getElementById('exportDialog');
            const now = new Date();
            const thisMonth = now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0');
            // 默认范围：本月
            document.getElementById('exportFrom').value = thisMonth;
            document.getElementById('exportTo').value = thisMonth;
            dialog.classList.remove('hidden');
        });

        document.getElementById('exportCancel').addEventListener('click', () => {
            document.getElementById('exportDialog').classList.add('hidden');
        });

        document.getElementById('exportConfirm').addEventListener('click', () => {
            const from = document.getElementById('exportFrom').value;
            const to = document.getElementById('exportTo').value;
            if (!from || !to) { return; }
            if (from > to) { alert('起始月不能晚于结束月'); return; }

            // 直接用浏览器下载，带 cookie
            const a = document.createElement('a');
            a.href = `api/export_csv.php?from=${from}&to=${to}`;
            a.download = `star-track-${from}-to-${to}.csv`;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            document.getElementById('exportDialog').classList.add('hidden');
        });
    }

    // 日历弹窗关闭
    document.getElementById('calPopupClose').addEventListener('click', closeCalPopup);
    document.querySelector('.cal-popup-backdrop').addEventListener('click', closeCalPopup);
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') closeCalPopup();
    });
}

async function onQuickAdd(e) {
    e.preventDefault();
    const input = document.getElementById('quickInput');
    const title = input.value.trim();
    if (!title) return;

    const dueDate = document.getElementById('inputDueDate').value || null;
    const startDate = document.getElementById('inputStartDate').value || null;

    try {
        await apiAddTodo({
            title,
            task_type: selectedType,
            due_date: dueDate,
            start_date: startDate
        });

        input.value = '';
        document.getElementById('inputDueDate').value = '';
        document.getElementById('inputStartDate').value = '';
        await refresh();
    } catch (err) {
        console.error('添加失败:', err);
    }
}

async function onDetailDueDateChange(e) {
    if (!selectedTodo) return;
    const newDate = e.target.value || null;
    try {
        await apiUpdateTodo(selectedTodo.id, { due_date: newDate });
        await refresh();
        const updated = findTodoById(selectedTodo.id);
        if (updated) {
            selectedTodo = updated;
        }
    } catch (err) {
        console.error('更新截止日期失败:', err);
    }
}

async function onDetailStartDateChange(e) {
    if (!selectedTodo) return;
    const newDate = e.target.value || null;
    try {
        await apiUpdateTodo(selectedTodo.id, { start_date: newDate });
        await refresh();
        const updated = findTodoById(selectedTodo.id);
        if (updated) {
            selectedTodo = updated;
        }
    } catch (err) {
        console.error('更新开始日期失败:', err);
    }
}

async function onAddChild(e) {
    e.preventDefault();
    const input = document.getElementById('addChildInput');
    const title = input.value.trim();
    if (!title || !selectedTodo) return;

    try {
        await apiAddTodo({
            title,
            task_type: selectedTodo.task_type,
            parent_id: selectedTodo.id
        });

        input.value = '';
        await refresh();

        // 重新打开详情
        const updated = findTodoById(selectedTodo.id);
        if (updated) showDetail(updated);
    } catch (err) {
        console.error('添加子任务失败:', err);
    }
}

async function handleCompleteTodo(id, completedDate) {
    // 检查是否有未完成的子任务
    const todo = findTodoById(id);
    if (todo && todo.children && todo.children.length > 0) {
        const unfinished = todo.children.filter(c => !c.completed);
        if (unfinished.length > 0) {
            // 弹窗确认
            const confirmed = await showConfirmDialog(
                `「${todo.title}」还有 ${unfinished.length} 个未完成的子任务，\n确定要一起完成吗？`,
                '全部完成',
                '取消'
            );
            if (!confirmed) return;

            // 批量完成
            const nodeEl = document.querySelector(`.constellation-node[data-id="${id}"]`);
            if (nodeEl) {
                nodeEl.classList.add('completing');
            }
            try {
                await apiCompleteTodo(id, completedDate, true);
                await new Promise(r => setTimeout(r, 600));
                hideDetail();
                await refresh();
            } catch (err) {
                console.error('完成失败:', err);
            }
            return;
        }
    }

    // 播放完成动画
    const nodeEl = document.querySelector(`.constellation-node[data-id="${id}"]`);
    if (nodeEl) {
        nodeEl.classList.add('completing');
        await new Promise(r => setTimeout(r, 600));
    }

    try {
        await apiCompleteTodo(id, completedDate);
        hideDetail();
        await refresh();
    } catch (err) {
        console.error('完成失败:', err);
    }
}

async function handleCompleteChild(id, checked) {
    if (checked) {
        try {
            await apiCompleteTodo(id);
            await refresh();
            // 刷新详情面板
            if (selectedTodo) {
                const updated = findTodoById(selectedTodo.id);
                if (updated) showDetail(updated);
            }
        } catch (err) {
            console.error('完成子任务失败:', err);
        }
    } else {
        // 取消完成 — 通过 update_todo 设置 completed=0
        try {
            // 目前 complete_todo 是 toggle，所以再调一次就是取消
            await apiCompleteTodo(id);
            await refresh();
            if (selectedTodo) {
                const updated = findTodoById(selectedTodo.id);
                if (updated) showDetail(updated);
            }
        } catch (err) {
            console.error('取消完成失败:', err);
        }
    }
}

async function handleDeleteTodo(id) {
    const confirmed = await showConfirmDialog('确定删除？子任务也会一起删除。', '删除', '取消');
    if (!confirmed) return;

    try {
        await apiDeleteTodo(id);
        hideDetail();
        await refresh();
    } catch (err) {
        console.error('删除失败:', err);
    }
}

async function refresh() {
    await loadTodos();
    render();
}

// ===== 布局计算 =====
function calcStarSize(count) {
    if (count <= 0) return 100;
    if (count === 1) return 110;
    const base = 105;
    return Math.max(50, Math.round(base / Math.pow(count, 0.45)));
}

// ===== 工具函数 =====
function getTodayStr() {
    return fmtDate(new Date());
}

function fmtDate(d) {
    return d.getFullYear() + '-' +
        String(d.getMonth() + 1).padStart(2, '0') + '-' +
        String(d.getDate()).padStart(2, '0');
}

function formatDateShort(dateStr) {
    if (!dateStr) return '';
    const parts = dateStr.split('-');
    return parts[1] + '/' + parts[2];
}

function formatDateChinese(dateStr) {
    if (!dateStr) return '';
    const d = new Date(dateStr + 'T00:00:00');
    const weekDays = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
    return `${d.getMonth() + 1}月${d.getDate()}日 ${weekDays[d.getDay()]}`;
}

function findTodoById(id, list) {
    list = list || todos;
    for (const todo of list) {
        if (todo.id == id) return todo;
        if (todo.children) {
            const found = findTodoById(id, todo.children);
            if (found) return found;
        }
    }
    return null;
}

// ===== 左右分区拖拽 =====
(function initZoneResizer() {
    const resizer = document.getElementById('zone-resizer');
    const root = document.documentElement;
    if (!resizer) return;

    // 恢复记忆比例
    const saved = localStorage.getItem('star-todo-zone-ratio');
    if (saved) {
        const pct = Math.max(20, Math.min(75, parseFloat(saved)));
        root.style.setProperty('--today-zone-width', pct + '%');
        root.style.setProperty('--constellation-zone-width', (100 - pct) + '%');
        resizer.style.left = pct + '%';
    }

    let dragging = false;

    function onMove(e) {
        if (!dragging) return;
        const rect = document.getElementById('today-view').getBoundingClientRect();
        let pct = ((e.clientX - rect.left) / rect.width) * 100;
        pct = Math.max(20, Math.min(75, pct));
        root.style.setProperty('--today-zone-width', pct + '%');
        root.style.setProperty('--constellation-zone-width', (100 - pct) + '%');
        resizer.style.left = pct + '%';
    }

    function onUp() {
        if (!dragging) return;
        dragging = false;
        resizer.classList.remove('active');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
        // 记忆比例
        const pct = parseFloat(resizer.style.left);
        localStorage.setItem('star-todo-zone-ratio', pct);
    }

    resizer.addEventListener('mousedown', (e) => {
        e.preventDefault();
        dragging = true;
        resizer.classList.add('active');
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
    });
})();
