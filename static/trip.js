import { init, logout, api } from '/app.js';

const DOMAIN = '{{.env.DOMAIN}}';
const tripID = location.pathname.split('/').pop();

const profile = await init();

let trip;
try {
    trip = await api('GET', '/api/trips/' + tripID);
} catch (e) {
    document.body.style.opacity = 1;
    document.body.textContent = 'Access denied.';
    throw e;
}

document.getElementById('trip-name').textContent = trip.name;
document.getElementById('room-size').value = trip.room_size;
document.getElementById('pn-multiple').value = trip.prefer_not_multiple;
document.getElementById('main').style.display = 'block';
document.getElementById('logout-btn').addEventListener('click', logout);
document.getElementById('room-size').addEventListener('change', async () => {
    const size = parseInt(document.getElementById('room-size').value);
    if (size >= 1) await api('PATCH', '/api/trips/' + tripID, { room_size: size });
});
document.getElementById('pn-multiple').addEventListener('change', async () => {
    const val = parseInt(document.getElementById('pn-multiple').value);
    if (val >= 1) await api('PATCH', '/api/trips/' + tripID, { prefer_not_multiple: val });
});

async function loadStudents() {
    const [students, constraints] = await Promise.all([
        api('GET', '/api/trips/' + tripID + '/students'),
        api('GET', '/api/trips/' + tripID + '/constraints')
    ]);
    const kindLabels = { must: 'Must', prefer: 'Prefer', prefer_not: 'Prefer Not', must_not: 'Must Not' };
    const kindVariant = { must: 'success', prefer: 'brand', prefer_not: 'warning', must_not: 'danger' };
    const kindColor = { must: 'var(--wa-color-success-50)', prefer: 'var(--wa-color-brand-50)', prefer_not: 'var(--wa-color-warning-50)', must_not: 'var(--wa-color-danger-50)' };
    const kindOrder = { must: 0, prefer: 1, prefer_not: 2, must_not: 3 };
    const isPositive = kind => kind === 'must' || kind === 'prefer';
    const capitalize = s => s.charAt(0).toUpperCase() + s.slice(1);

    const pairs = {};
    for (const c of constraints) {
        const k = c.student_a_id + '-' + c.student_b_id;
        if (!pairs[k]) pairs[k] = [];
        pairs[k].push(c);
    }
    const conflictList = [];
    const conflictMap = {};
    for (const group of Object.values(pairs)) {
        const pos = group.filter(c => isPositive(c.kind));
        const neg = group.filter(c => !isPositive(c.kind));
        if (pos.length === 0 || neg.length === 0) continue;
        conflictList.push({
            names: group[0].student_a_name + ' \u2192 ' + group[0].student_b_name,
            positives: pos.map(c => ({ level: c.level, kind: c.kind })),
            negatives: neg.map(c => ({ level: c.level, kind: c.kind }))
        });
        for (const c of group) {
            const opposing = isPositive(c.kind) ? neg : pos;
            conflictMap[c.id] = opposing.map(o => capitalize(o.level) + ' says ' + kindLabels[o.kind]).join(', ');
        }
    }

    const kindSpan = (kind) => {
        const span = document.createElement('span');
        span.textContent = kindLabels[kind];
        span.style.color = kindColor[kind];
        span.style.fontWeight = 'bold';
        return span;
    };

    const allOveralls = {};
    for (const s of students) {
        const myC = constraints.filter(c => c.student_a_id === s.id);
        const byPeer = {};
        for (const c of myC) {
            if (!byPeer[c.student_b_id]) byPeer[c.student_b_id] = {};
            byPeer[c.student_b_id][c.level] = c;
        }
        allOveralls[s.id] = {};
        for (const [peerId, levels] of Object.entries(byPeer)) {
            const eff = levels.admin || levels.parent || levels.student;
            if (eff) allOveralls[s.id][peerId] = eff;
        }
    }

    const mismatchList = [];
    for (const s of students) {
        for (const [bId, effA] of Object.entries(allOveralls[s.id])) {
            if (!allOveralls[bId] || !allOveralls[bId][s.id]) continue;
            const effB = allOveralls[bId][s.id];
            if (isPositive(effA.kind) && !isPositive(effB.kind)) {
                mismatchList.push({
                    nameA: s.name,
                    nameB: students.find(x => x.id === parseInt(bId)).name,
                    kindA: effA.kind,
                    kindB: effB.kind,
                });
            }
        }
    }

    const studentName = {};
    for (const s of students) studentName[s.id] = s.name;

    const mustAdj = {};
    for (const s of students) {
        for (const [bId, eff] of Object.entries(allOveralls[s.id])) {
            if (eff.kind === 'must') {
                const a = s.id, b = parseInt(bId);
                if (!mustAdj[a]) mustAdj[a] = [];
                mustAdj[a].push(b);
                if (!mustAdj[b]) mustAdj[b] = [];
                mustAdj[b].push(a);
            }
        }
    }

    const ufParent = {};
    for (const s of students) ufParent[s.id] = s.id;
    const ufFind = (x) => {
        if (ufParent[x] !== x) ufParent[x] = ufFind(ufParent[x]);
        return ufParent[x];
    };
    for (const s of students) {
        for (const [bId, eff] of Object.entries(allOveralls[s.id])) {
            if (eff.kind === 'must') {
                const ra = ufFind(s.id), rb = ufFind(parseInt(bId));
                if (ra !== rb) ufParent[ra] = rb;
            }
        }
    }

    const findMustPath = (from, to) => {
        if (from === to) return [from];
        const visited = new Set([from]);
        const queue = [[from]];
        while (queue.length > 0) {
            const path = queue.shift();
            const curr = path[path.length - 1];
            for (const next of (mustAdj[curr] || [])) {
                if (next === to) return [...path, next];
                if (!visited.has(next)) {
                    visited.add(next);
                    queue.push([...path, next]);
                }
            }
        }
        return null;
    };

    const hardConflictList = [];
    for (const s of students) {
        for (const [bId, eff] of Object.entries(allOveralls[s.id])) {
            if (eff.kind !== 'must_not') continue;
            const b = parseInt(bId);
            if (ufFind(s.id) !== ufFind(b)) continue;
            const path = findMustPath(b, s.id);
            if (!path) continue;
            const chain = [];
            for (let i = 0; i < path.length - 1; i++) {
                const x = path[i], y = path[i + 1];
                if (allOveralls[x]?.[y]?.kind === 'must') {
                    chain.push({ from: studentName[x], to: studentName[y], kind: 'must' });
                } else {
                    chain.push({ from: studentName[y], to: studentName[x], kind: 'must' });
                }
            }
            chain.push({ from: s.name, to: studentName[b], kind: 'must_not' });
            hardConflictList.push(chain);
        }
    }

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
            div.appendChild(document.createTextNode(m.nameA + ' \u2192 ' + m.nameB + ': '));
            div.appendChild(kindSpan(m.kindA));
            div.appendChild(document.createTextNode(' but ' + m.nameB + ' \u2192 ' + m.nameA + ': '));
            div.appendChild(kindSpan(m.kindB));
            det.appendChild(div);
        }
        mismatchesEl.appendChild(det);
    }

    const hardConflictsEl = document.getElementById('hard-conflicts');
    const hardConflictsWasOpen = hardConflictsEl.querySelector('wa-details')?.open;
    hardConflictsEl.innerHTML = '';
    if (hardConflictList.length > 0) {
        const det = document.createElement('wa-details');
        det.summary = '\u26a0 Conflicts (' + hardConflictList.length + ')';
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
            const email = input.value.trim();
            if (!email) return;
            await api('POST', '/api/trips/' + tripID + '/students/' + student.id + '/parents', { email });
            loadStudents();
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
        const levelKinds = {
            student: ['prefer', 'prefer_not'],
            parent: ['must_not'],
            admin: ['must', 'prefer', 'prefer_not', 'must_not']
        };
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
        const updateKinds = () => {
            kindSelect.querySelectorAll('wa-option').forEach(o => o.remove());
            for (const kind of levelKinds[levelSelect.value]) {
                const opt = document.createElement('wa-option');
                opt.value = kind;
                opt.textContent = kindLabels[kind];
                kindSelect.appendChild(opt);
            }
            kindSelect.value = levelKinds[levelSelect.value][0];
        };
        updateKinds();
        levelSelect.addEventListener('wa-change', updateKinds);
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
                if (selects[0]) selects[0].value = savedLevel;
                if (selects[1]) {
                    const kinds = levelKinds[savedLevel];
                    selects[1].querySelectorAll('wa-option').forEach(o => o.remove());
                    for (const kind of kinds) {
                        const opt = document.createElement('wa-option');
                        opt.value = kind;
                        opt.textContent = kindLabels[kind];
                        selects[1].appendChild(opt);
                    }
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
        for (let i = 0; i < result.rooms.length; i++) {
            const card = document.createElement('wa-card');
            card.className = 'room-card';
            const label = document.createElement('div');
            label.className = 'room-label';
            label.textContent = 'Room ' + (i + 1);
            card.appendChild(label);
            const tags = document.createElement('div');
            tags.className = 'tags';
            for (const member of result.rooms[i]) {
                const tag = document.createElement('wa-tag');
                tag.size = 'small';
                tag.textContent = member.name;
                tags.appendChild(tag);
            }
            card.appendChild(tags);
            container.appendChild(card);
        }
        const scoreDiv = document.createElement('div');
        scoreDiv.className = 'solver-score';
        scoreDiv.textContent = 'Score: ' + result.score;
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
