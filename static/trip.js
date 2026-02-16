import { init, logout, api } from '/app.js';

const DOMAIN = '{{.env.DOMAIN}}';
const tripID = location.pathname.split('/').pop();

const profile = await init();

let trip, me;
try {
    [trip, me] = await Promise.all([
        api('GET', '/api/trips/' + tripID),
        api('GET', '/api/trips/' + tripID + '/me')
    ]);
} catch (e) {
    document.body.style.opacity = 1;
    document.body.textContent = 'Access denied.';
    throw e;
}

document.getElementById('trip-name').textContent = trip.name;
document.getElementById('main').style.display = 'block';
document.getElementById('logout-btn').addEventListener('click', logout);

if (me.role !== 'admin') {
    document.getElementById('member-view').style.display = 'block';
    await renderMemberView(me);
    await customElements.whenDefined('wa-button');
    document.body.style.opacity = 1;
} else {
await (async () => {

document.getElementById('admin-view').style.display = 'block';
document.getElementById('pn-multiple').value = trip.prefer_not_multiple;
document.getElementById('np-cost').value = trip.no_prefer_cost;

let roomGroups = [];

async function loadRoomGroups() {
    roomGroups = await api('GET', '/api/trips/' + tripID + '/room-groups');
    const tags = document.getElementById('room-group-tags');
    tags.innerHTML = '';
    for (const rg of roomGroups) {
        const tag = document.createElement('wa-tag');
        tag.size = 'small';
        tag.setAttribute('with-remove', '');
        tag.textContent = rg.count + ' \u00d7 ' + rg.size + '-person';
        tag.addEventListener('wa-remove', async () => {
            await api('DELETE', '/api/trips/' + tripID + '/room-groups/' + rg.id);
            loadRoomGroups();
        });
        tags.appendChild(tag);
    }
}
await loadRoomGroups();

document.getElementById('add-rg-btn').addEventListener('click', async () => {
    const sizeInput = document.getElementById('new-rg-size');
    const countInput = document.getElementById('new-rg-count');
    const size = parseInt((sizeInput.value || '').trim());
    const count = parseInt((countInput.value || '').trim());
    if (!size || size < 1 || !count || count < 1) return;
    await api('POST', '/api/trips/' + tripID + '/room-groups', { size, count });
    sizeInput.value = '';
    countInput.value = '';
    loadRoomGroups();
});

document.getElementById('pn-multiple').addEventListener('change', async () => {
    const val = parseInt(document.getElementById('pn-multiple').value);
    if (val >= 1) await api('PATCH', '/api/trips/' + tripID, { prefer_not_multiple: val });
});
document.getElementById('np-cost').addEventListener('change', async () => {
    const val = parseInt(document.getElementById('np-cost').value);
    if (val >= 0) await api('PATCH', '/api/trips/' + tripID, { no_prefer_cost: val });
});

let lastOveralls = {};

async function loadStudents() {
    const [students, constraintData] = await Promise.all([
        api('GET', '/api/trips/' + tripID + '/students'),
        api('GET', '/api/trips/' + tripID + '/constraints')
    ]);
    const constraints = constraintData.constraints;
    const conflictList = constraintData.overrides;
    const kindLabels = { must: 'Must', prefer: 'Prefer', prefer_not: 'Prefer Not', must_not: 'Must Not' };
    const kindVariant = { must: 'success', prefer: 'brand', prefer_not: 'warning', must_not: 'danger' };
    const kindColor = { must: 'var(--wa-color-success-50)', prefer: 'var(--wa-color-brand-50)', prefer_not: 'var(--wa-color-warning-50)', must_not: 'var(--wa-color-danger-50)' };
    const kindOrder = { must: 0, prefer: 1, prefer_not: 2, must_not: 3 };
    const capitalize = s => s.charAt(0).toUpperCase() + s.slice(1);

    const conflictMap = {};
    for (const c of constraints) {
        if (c.override) conflictMap[c.id] = c.override;
    }

    const kindSpan = (kind) => {
        const span = document.createElement('span');
        span.textContent = kindLabels[kind];
        span.style.color = kindColor[kind];
        span.style.fontWeight = 'bold';
        return span;
    };

    const allOveralls = {};
    for (const s of students) allOveralls[s.id] = {};
    for (const o of constraintData.overalls) {
        allOveralls[o.student_a_id][o.student_b_id] = o;
    }
    lastOveralls = allOveralls;

    const mismatchList = constraintData.mismatches;
    const hardConflictList = constraintData.hard_conflicts;
    const oversizedGroups = constraintData.oversized_groups;

    const conflictsEl = document.getElementById('conflicts');
    const conflictsWasOpen = conflictsEl.querySelector('wa-details')?.open;
    conflictsEl.innerHTML = '';
    if (conflictList.length > 0) {
        const det = document.createElement('wa-details');
        det.summary = '\u26a0 Overrides (' + conflictList.length + ')';
        if (conflictsWasOpen) det.open = true;
        for (const conflict of conflictList) {
            const div = document.createElement('div');
            div.className = 'conflict-row';
            div.appendChild(document.createTextNode(conflict.names + ': '));
            conflict.positives.forEach((p, i) => {
                if (i > 0) div.appendChild(document.createTextNode(', '));
                div.appendChild(document.createTextNode(capitalize(p.level) + ' '));
                div.appendChild(kindSpan(p.kind));
            });
            div.appendChild(document.createTextNode(' vs '));
            conflict.negatives.forEach((n, i) => {
                if (i > 0) div.appendChild(document.createTextNode(', '));
                div.appendChild(document.createTextNode(capitalize(n.level) + ' '));
                div.appendChild(kindSpan(n.kind));
            });
            det.appendChild(div);
        }
        conflictsEl.appendChild(det);
    }

    const mismatchesEl = document.getElementById('mismatches');
    const mismatchesWasOpen = mismatchesEl.querySelector('wa-details')?.open;
    mismatchesEl.innerHTML = '';
    if (mismatchList.length > 0) {
        const det = document.createElement('wa-details');
        det.summary = '\u26a0 Mismatches (' + mismatchList.length + ')';
        if (mismatchesWasOpen) det.open = true;
        for (const m of mismatchList) {
            const div = document.createElement('div');
            div.className = 'conflict-row';
            div.appendChild(document.createTextNode(m.name_a + ' \u2192 ' + m.name_b + ': '));
            div.appendChild(kindSpan(m.kind_a));
            div.appendChild(document.createTextNode(' but ' + m.name_b + ' \u2192 ' + m.name_a + ': '));
            div.appendChild(kindSpan(m.kind_b));
            det.appendChild(div);
        }
        mismatchesEl.appendChild(det);
    }

    const hardConflictsEl = document.getElementById('hard-conflicts');
    const hardConflictsWasOpen = hardConflictsEl.querySelector('wa-details')?.open;
    hardConflictsEl.innerHTML = '';
    const totalConflicts = hardConflictList.length + oversizedGroups.length;
    if (totalConflicts > 0) {
        const det = document.createElement('wa-details');
        det.summary = '\u26a0 Conflicts (' + totalConflicts + ')';
        if (hardConflictsWasOpen) det.open = true;
        for (const chain of hardConflictList) {
            const div = document.createElement('div');
            div.className = 'conflict-row';
            chain.forEach((link, i) => {
                if (i === chain.length - 1 && chain.length > 1) {
                    div.appendChild(document.createTextNode(', but '));
                } else if (i > 0) {
                    div.appendChild(document.createTextNode(', '));
                }
                div.appendChild(document.createTextNode(link.from + ' '));
                div.appendChild(kindSpan(link.kind));
                div.appendChild(document.createTextNode(' ' + link.to));
            });
            det.appendChild(div);
        }
        for (const members of oversizedGroups) {
            const div = document.createElement('div');
            div.className = 'conflict-row';
            div.appendChild(kindSpan('must'));
            const maxSize = roomGroups.length > 0 ? Math.max(...roomGroups.map(g => g.size)) : 0;
            div.appendChild(document.createTextNode(' group too large (' + members.length + ' for max room size ' + maxSize + '): ' + members.join(', ')));
            det.appendChild(div);
        }
        hardConflictsEl.appendChild(det);
    }

    const container = document.getElementById('students');
    const openStates = {};
    for (const card of container.children) {
        const sid = card.dataset.studentId;
        if (!sid) continue;
        openStates[sid] = {};
        for (const det of card.querySelectorAll('wa-details')) openStates[sid][det.summary] = det.open;
    }
    container.innerHTML = '';
    for (const student of students) {
        const card = document.createElement('wa-card');
        card.dataset.studentId = student.id;

        const nameRow = document.createElement('div');
        nameRow.style.display = 'flex';
        nameRow.style.alignItems = 'center';
        const label = document.createElement('span');
        label.className = 'student-name';
        label.style.flex = '1';
        label.textContent = student.name + ' (' + student.email + ')';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'close-btn';
        deleteBtn.textContent = '\u00d7';
        deleteBtn.addEventListener('click', async () => {
            if (!confirm('Remove student "' + student.name + '"?')) return;
            await api('DELETE', '/api/trips/' + tripID + '/students/' + student.id);
            loadStudents();
        });
        nameRow.appendChild(label);
        nameRow.appendChild(deleteBtn);
        card.appendChild(nameRow);

        const details = document.createElement('wa-details');
        details.summary = 'Parents';

        const tags = document.createElement('div');
        tags.className = 'tags';
        for (const parent of student.parents) {
            const tag = document.createElement('wa-tag');
            tag.size = 'small';
            tag.variant = 'success';
            tag.setAttribute('with-remove', '');
            tag.textContent = parent.email;
            tag.addEventListener('wa-remove', async () => {
                await api('DELETE', '/api/trips/' + tripID + '/students/' + student.id + '/parents/' + parent.id);
                loadStudents();
            });
            tags.appendChild(tag);
        }
        details.appendChild(tags);

        const input = document.createElement('wa-input');
        input.placeholder = 'Add parent email';
        input.size = 'small';
        input.className = 'email';
        input.style.marginTop = '0.3rem';
        const addBtn = document.createElement('button');
        addBtn.slot = 'end';
        addBtn.className = 'input-action';
        addBtn.textContent = '+';
        const doAdd = async () => {
            const email = (input.value || '').trim();
            if (!email) return;
            await api('POST', '/api/trips/' + tripID + '/students/' + student.id + '/parents', { email });
            await loadStudents();
            const reCard = container.querySelector('[data-student-id="' + student.id + '"]');
            if (reCard) {
                const det = reCard.querySelector('wa-details');
                if (det) det.open = true;
                const inp = reCard.querySelector('wa-input');
                if (inp) inp.focus();
            }
        };
        addBtn.addEventListener('click', doAdd);
        input.addEventListener('keydown', (e) => { if (e.key === 'Enter') doAdd(); });
        input.appendChild(addBtn);
        details.appendChild(input);

        card.appendChild(details);

        const cDetails = document.createElement('wa-details');
        cDetails.summary = 'Constraints';

        const myConstraints = constraints.filter(c => c.student_a_id === student.id);

        const overall = Object.values(allOveralls[student.id] || {});
        if (overall.length > 0) {
            overall.sort((a, b) => {
                const kd = kindOrder[a.kind] - kindOrder[b.kind];
                if (kd !== 0) return kd;
                return a.student_b_name.localeCompare(b.student_b_name);
            });
            const group = document.createElement('div');
            group.className = 'constraint-group';
            const levelLabel = document.createElement('span');
            levelLabel.className = 'constraint-level';
            levelLabel.textContent = 'Overall';
            group.appendChild(levelLabel);
            for (const c of overall) {
                const tag = document.createElement('wa-tag');
                tag.size = 'small';
                tag.variant = kindVariant[c.kind];
                tag.textContent = kindLabels[c.kind] + ': ' + c.student_b_name;
                tag.title = 'From ' + capitalize(c.level);
                group.appendChild(tag);
            }
            cDetails.appendChild(group);
        }

        for (const level of ['admin', 'parent', 'student']) {
            const lc = myConstraints.filter(c => c.level === level);
            if (lc.length === 0) continue;
            lc.sort((a, b) => {
                const kd = kindOrder[a.kind] - kindOrder[b.kind];
                if (kd !== 0) return kd;
                return a.student_b_name.localeCompare(b.student_b_name);
            });
            const group = document.createElement('div');
            group.className = 'constraint-group';
            const levelLabel = document.createElement('span');
            levelLabel.className = 'constraint-level';
            levelLabel.textContent = level.charAt(0).toUpperCase() + level.slice(1);
            group.appendChild(levelLabel);
            for (const c of lc) {
                const otherName = c.student_b_name;
                const tag = document.createElement('wa-tag');
                tag.size = 'small';
                tag.variant = kindVariant[c.kind];
                tag.setAttribute('with-remove', '');
                tag.addEventListener('wa-remove', async () => {
                    await api('DELETE', '/api/trips/' + tripID + '/constraints/' + c.id);
                    loadStudents();
                });
                if (conflictMap[c.id]) {
                    const icon = document.createElement('span');
                    icon.className = 'conflict-icon';
                    icon.textContent = '\u26a0 ';
                    tag.appendChild(icon);
                    tag.appendChild(document.createTextNode(kindLabels[c.kind] + ': ' + otherName));
                    tag.title = 'Overrides: ' + conflictMap[c.id];
                } else {
                    tag.textContent = kindLabels[c.kind] + ': ' + otherName;
                }
                group.appendChild(tag);
            }
            cDetails.appendChild(group);
        }

        const addRow = document.createElement('div');
        addRow.className = 'constraint-add';
        const levelKinds = me.level_kinds;
        const levelSelect = document.createElement('wa-select');
        levelSelect.size = 'small';
        for (const level of ['student', 'parent', 'admin']) {
            const opt = document.createElement('wa-option');
            opt.value = level;
            opt.textContent = capitalize(level);
            levelSelect.appendChild(opt);
        }
        levelSelect.value = 'student';
        const kindSelect = document.createElement('wa-select');
        kindSelect.size = 'small';
        const updateKinds = async (level) => {
            kindSelect.innerHTML = '';
            for (const kind of levelKinds[level]) {
                const opt = document.createElement('wa-option');
                opt.value = kind;
                opt.textContent = kindLabels[kind];
                kindSelect.appendChild(opt);
            }
            await kindSelect.updateComplete;
            kindSelect.value = levelKinds[level][0];
        };
        updateKinds('student');
        levelSelect.addEventListener('change', (e) => {
            updateKinds(e.target.value);
        });
        const studentSelect = document.createElement('wa-select');
        studentSelect.size = 'small';
        studentSelect.placeholder = 'Student\u2026';
        for (const other of students) {
            if (other.id === student.id) continue;
            const opt = document.createElement('wa-option');
            opt.value = other.id;
            opt.textContent = other.name;
            studentSelect.appendChild(opt);
        }
        const cAddBtn = document.createElement('wa-button');
        cAddBtn.size = 'small';
        cAddBtn.textContent = '+';
        cAddBtn.addEventListener('click', async () => {
            const otherID = parseInt(studentSelect.value);
            if (!otherID) return;
            const savedLevel = levelSelect.value;
            const savedKind = kindSelect.value;
            await api('POST', '/api/trips/' + tripID + '/constraints', {
                student_a_id: student.id,
                student_b_id: otherID,
                kind: savedKind,
                level: savedLevel
            });
            await loadStudents();
            const card = document.querySelector('[data-student-id="' + student.id + '"]');
            if (card) {
                const selects = card.querySelectorAll('.constraint-add wa-select');
                if (selects[0]) {
                    await selects[0].updateComplete;
                    selects[0].value = savedLevel;
                }
                if (selects[1]) {
                    selects[1].innerHTML = '';
                    const kinds = levelKinds[savedLevel];
                    for (const kind of kinds) {
                        const opt = document.createElement('wa-option');
                        opt.value = kind;
                        opt.textContent = kindLabels[kind];
                        selects[1].appendChild(opt);
                    }
                    await selects[1].updateComplete;
                    selects[1].value = savedKind;
                }
            }
        });
        addRow.appendChild(levelSelect);
        addRow.appendChild(kindSelect);
        addRow.appendChild(studentSelect);
        addRow.appendChild(cAddBtn);
        cDetails.appendChild(addRow);

        card.appendChild(cDetails);

        const saved = openStates[student.id];
        if (saved) for (const det of card.querySelectorAll('wa-details')) if (saved[det.summary]) det.open = true;

        container.appendChild(card);
    }
}

async function addStudent() {
    const nameInput = document.getElementById('new-student-name');
    const emailInput = document.getElementById('new-student-email');
    const name = nameInput.value.trim();
    const email = emailInput.value.trim();
    if (!name || !email) return;
    await api('POST', '/api/trips/' + tripID + '/students', { name, email });
    nameInput.value = '';
    emailInput.value = '';
    loadStudents();
}

document.getElementById('add-student-btn').addEventListener('click', addStudent);
document.getElementById('solve-btn').addEventListener('click', async () => {
    const btn = document.getElementById('solve-btn');
    btn.loading = true;
    try {
        const result = await api('POST', '/api/trips/' + tripID + '/solve');
        const container = document.getElementById('solver-results');
        container.innerHTML = '';

        const renderRoomCard = (room, parent, roomNum, locked) => {
            const card = document.createElement('wa-card');
            card.className = 'room-card' + (locked ? ' room-locked' : '');
            if (locked) card.setAttribute('appearance', 'outlined');
            const label = document.createElement('div');
            label.className = 'room-label';
            label.textContent = 'Room ' + roomNum;
            card.appendChild(label);
            const tags = document.createElement('div');
            tags.className = 'tags';
            const roomIDs = room.map(m => m.id);
            const violations = [];
            for (const a of room) {
                for (const b of room) {
                    if (a.id === b.id) continue;
                    const eff = lastOveralls[a.id]?.[b.id];
                    if (eff && eff.kind === 'prefer_not') {
                        violations.push({ from: a.name, to: b.name });
                    }
                }
            }
            for (const member of room) {
                const tag = document.createElement('wa-tag');
                tag.size = 'small';
                tag.style.cursor = 'pointer';
                const hasViolation = violations.some(v => v.from === member.name || v.to === member.name);
                const hasPrefers = Object.values(lastOveralls[member.id] || {}).some(e => e.kind === 'prefer');
                const gotPrefer = hasPrefers && roomIDs.some(rid => rid !== member.id && lastOveralls[member.id]?.[rid]?.kind === 'prefer');
                if (hasViolation) tag.variant = 'danger';
                else if (hasPrefers && !gotPrefer) tag.variant = 'warning';
                else tag.variant = 'brand';
                tag.textContent = member.name;
                tag.addEventListener('click', () => {
                    const studentCard = document.querySelector('[data-student-id="' + member.id + '"]');
                    if (!studentCard) return;
                    const cDet = [...studentCard.querySelectorAll('wa-details')].find(d => d.summary === 'Constraints');
                    if (cDet) cDet.open = true;
                    studentCard.scrollIntoView({ behavior: 'smooth', block: 'center' });
                });
                tags.appendChild(tag);
            }
            if (violations.length > 0) {
                const warn = document.createElement('div');
                warn.style.fontSize = '0.75rem';
                warn.style.color = 'var(--wa-color-warning-50)';
                warn.textContent = violations.map(v => v.from + ' \u2192 ' + v.to).join(', ');
                card.appendChild(tags);
                card.appendChild(warn);
            } else {
                card.appendChild(tags);
            }
            parent.appendChild(card);
        };

        const solutions = result.solutions;
        const roomKey = (room) => room.map(m => m.id).sort((a, b) => a - b).join(',');
        let swapGroups = [];

        if (solutions.length === 1) {
            let roomNum = 1;
            for (const room of solutions[0].rooms) {
                renderRoomCard(room, container, roomNum++, false);
            }
        } else if (solutions.length > 1) {
            const sets = solutions.map(sol => new Set(sol.rooms.map(roomKey)));
            const lockedKeys = new Set([...sets[0]].filter(k => sets.every(s => s.has(k))));

            const lockedRoomsList = solutions[0].rooms.filter(r => lockedKeys.has(roomKey(r)));

            const uf = {};
            const ufFind = (x) => {
                if (uf[x] === undefined) uf[x] = x;
                if (uf[x] !== x) uf[x] = ufFind(uf[x]);
                return uf[x];
            };
            const ufUnion = (a, b) => {
                const ra = ufFind(a), rb = ufFind(b);
                if (ra !== rb) uf[ra] = rb;
            };

            for (const sol of solutions) {
                for (const room of sol.rooms) {
                    if (lockedKeys.has(roomKey(room))) continue;
                    const ids = room.map(m => m.id);
                    for (let i = 1; i < ids.length; i++) {
                        ufUnion(ids[0], ids[i]);
                    }
                }
            }

            const components = {};
            for (const id of Object.keys(uf)) {
                const root = ufFind(parseInt(id));
                if (!components[root]) components[root] = new Set();
                components[root].add(parseInt(id));
            }

            for (const studentIDs of Object.values(components)) {
                const configs = [];
                const configKeySet = new Set();
                for (const sol of solutions) {
                    const groupRooms = sol.rooms.filter(r => r.some(m => studentIDs.has(m.id)));
                    groupRooms.sort((a, b) => roomKey(a).localeCompare(roomKey(b)));
                    const ck = groupRooms.map(r => roomKey(r)).join('|');
                    if (!configKeySet.has(ck)) {
                        configKeySet.add(ck);
                        configs.push(groupRooms);
                    }
                }
                swapGroups.push({ studentIDs, configs });
            }
            swapGroups.sort((a, b) => Math.min(...a.studentIDs) - Math.min(...b.studentIDs));

            let roomNum = 1;
            for (const room of lockedRoomsList) {
                renderRoomCard(room, container, roomNum++, true);
            }

            for (let gi = 0; gi < swapGroups.length; gi++) {
                const group = swapGroups[gi];
                const prefix = String.fromCharCode('A'.charCodeAt(0) + gi);
                const section = document.createElement('div');
                section.className = 'swap-group';
                const baseRoomNum = roomNum;

                const tabGroup = document.createElement('wa-tab-group');
                for (let ci = 0; ci < group.configs.length; ci++) {
                    const tab = document.createElement('wa-tab');
                    tab.slot = 'nav';
                    tab.panel = 'sg-' + gi + '-' + ci;
                    tab.textContent = prefix + (ci + 1);
                    tabGroup.appendChild(tab);
                }
                for (let ci = 0; ci < group.configs.length; ci++) {
                    const panel = document.createElement('wa-tab-panel');
                    panel.name = 'sg-' + gi + '-' + ci;
                    let rn = baseRoomNum;
                    for (const room of group.configs[ci]) {
                        renderRoomCard(room, panel, rn++, false);
                    }
                    tabGroup.appendChild(panel);
                }

                section.appendChild(tabGroup);
                container.appendChild(section);
                roomNum += group.configs[0].length;
            }
        }

        const scoreDiv = document.createElement('div');
        scoreDiv.className = 'solver-score';
        let scoreText = 'Score: ' + (solutions[0]?.score ?? 0);
        if (swapGroups.length > 0) {
            const counts = swapGroups.map(g => g.configs.length);
            const total = counts.reduce((a, b) => a * b, 1);
            if (swapGroups.length === 1) {
                scoreText += ' (' + total + ' options)';
            } else {
                scoreText += ' (' + counts.join(' \u00d7 ') + ' = ' + total + ' combinations)';
            }
        }
        scoreDiv.textContent = scoreText;
        container.appendChild(scoreDiv);
    } catch (e) {
        const container = document.getElementById('solver-results');
        container.textContent = e.message || 'Solver failed';
    } finally {
        btn.loading = false;
    }
});
document.getElementById('new-student-name').addEventListener('keydown', (e) => { if (e.key === 'Enter') addStudent(); });
document.getElementById('new-student-email').addEventListener('keydown', (e) => { if (e.key === 'Enter') addStudent(); });
await loadStudents();
await customElements.whenDefined('wa-button');
document.body.style.opacity = 1;

if (DOMAIN) {
    document.getElementById('new-student-name').addEventListener('blur', () => {
        const emailInput = document.getElementById('new-student-email');
        if ((emailInput.value || '').trim()) return;
        const name = (document.getElementById('new-student-name').value || '').trim();
        const parts = name.toLowerCase().split(/\s+/);
        if (parts.length >= 2) emailInput.value = parts.join('.') + '@' + DOMAIN;
    });
}

})();
}

async function renderMemberView(me) {
    const [students, constraintData] = await Promise.all([
        api('GET', '/api/trips/' + tripID + '/students'),
        api('GET', '/api/trips/' + tripID + '/constraints')
    ]);
    const constraints = constraintData.constraints;

    const myStudentIDs = new Set(me.students.map(s => s.id));
    const container = document.getElementById('member-students');

    const kindLabels = me.role === 'student'
        ? { '': 'OK', prefer: 'Prefer', prefer_not: 'Prefer Not' }
        : { '': 'OK to room with', must_not: 'Not OK to room with' };

    const kindOptions = ['', ...me.level_kinds[me.role]];

    const pendingRadios = [];

    for (const myStudent of me.students) {
        const card = document.createElement('wa-card');
        const label = document.createElement('span');
        label.className = 'student-name';
        label.textContent = myStudent.name;
        card.appendChild(label);

        const myConstraints = {};
        for (const c of constraints) {
            if (c.student_a_id === myStudent.id) {
                myConstraints[c.student_b_id] = c;
            }
        }

        const rows = document.createElement('div');
        rows.className = 'pref-rows';
        for (const other of students) {
            if (myStudentIDs.has(other.id)) continue;
            const row = document.createElement('div');
            row.className = 'pref-row';
            const name = document.createElement('span');
            name.className = 'pref-name';
            name.textContent = other.name;
            row.appendChild(name);

            const group = document.createElement('wa-radio-group');
            group.orientation = 'horizontal';
            group.size = 'small';
            for (const kind of kindOptions) {
                const radio = document.createElement('wa-radio');
                radio.value = kind;
                radio.textContent = kindLabels[kind];
                radio.setAttribute('appearance', 'button');
                group.appendChild(radio);
            }
            const existing = myConstraints[other.id];
            pendingRadios.push({ group, value: existing ? existing.kind : '' });

            group.addEventListener('change', async (e) => {
                const val = e.target.value;
                if (val === '') {
                    const c = myConstraints[other.id];
                    if (c) {
                        await api('DELETE', '/api/trips/' + tripID + '/constraints/' + c.id);
                        delete myConstraints[other.id];
                    }
                } else {
                    const result = await api('POST', '/api/trips/' + tripID + '/constraints', {
                        student_a_id: myStudent.id,
                        student_b_id: other.id,
                        kind: val,
                        level: me.role
                    });
                    myConstraints[other.id] = { id: result.id, kind: val, student_a_id: myStudent.id, student_b_id: other.id };
                }
            });
            row.appendChild(group);
            rows.appendChild(row);
        }
        card.appendChild(rows);
        container.appendChild(card);
    }

    await customElements.whenDefined('wa-radio-group');
    for (const { group, value } of pendingRadios) {
        await group.updateComplete;
        group.value = value;
    }
}
