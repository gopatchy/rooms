import { init, logout, api } from '/app.js';

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
document.getElementById('main').style.display = 'block';
document.getElementById('logout-btn').addEventListener('click', logout);
document.getElementById('room-size').addEventListener('change', async () => {
    const size = parseInt(document.getElementById('room-size').value);
    if (size >= 1) await api('PATCH', '/api/trips/' + tripID, { room_size: size });
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

    const conflictsEl = document.getElementById('conflicts');
    const conflictsWasOpen = conflictsEl.querySelector('wa-details')?.open;
    conflictsEl.innerHTML = '';
    if (conflictList.length > 0) {
        const det = document.createElement('wa-details');
        det.summary = '\u26a0 Overrides (' + conflictList.length + ')';
        if (conflictsWasOpen) det.open = true;
        const kindSpan = (kind) => {
            const span = document.createElement('span');
            span.textContent = kindLabels[kind];
            span.style.color = kindColor[kind];
            span.style.fontWeight = 'bold';
            return span;
        };
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

        const myConstraints = constraints.filter(c => c.student_a_id === student.id || c.student_b_id === student.id);

        for (const level of ['admin', 'parent', 'student']) {
            const lc = myConstraints.filter(c => c.level === level);
            if (lc.length === 0) continue;
            lc.sort((a, b) => {
                const kd = kindOrder[a.kind] - kindOrder[b.kind];
                if (kd !== 0) return kd;
                const na = a.student_a_id === student.id ? a.student_b_name : a.student_a_name;
                const nb = b.student_a_id === student.id ? b.student_b_name : b.student_a_name;
                return na.localeCompare(nb);
            });
            const group = document.createElement('div');
            group.className = 'constraint-group';
            const levelLabel = document.createElement('span');
            levelLabel.className = 'constraint-level';
            levelLabel.textContent = level.charAt(0).toUpperCase() + level.slice(1);
            group.appendChild(levelLabel);
            for (const c of lc) {
                const otherName = c.student_a_id === student.id ? c.student_b_name : c.student_a_name;
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
        const levelSelect = document.createElement('select');
        for (const level of ['student', 'parent', 'admin']) {
            const opt = document.createElement('option');
            opt.value = level;
            opt.textContent = level.charAt(0).toUpperCase() + level.slice(1);
            levelSelect.appendChild(opt);
        }
        const kindSelect = document.createElement('select');
        const updateKinds = () => {
            kindSelect.innerHTML = '';
            for (const kind of levelKinds[levelSelect.value]) {
                const opt = document.createElement('option');
                opt.value = kind;
                opt.textContent = kindLabels[kind];
                kindSelect.appendChild(opt);
            }
        };
        updateKinds();
        levelSelect.addEventListener('change', updateKinds);
        const studentSelect = document.createElement('select');
        const defaultOpt = document.createElement('option');
        defaultOpt.value = '';
        defaultOpt.textContent = 'Student\u2026';
        studentSelect.appendChild(defaultOpt);
        for (const other of students) {
            if (other.id === student.id) continue;
            const opt = document.createElement('option');
            opt.value = other.id;
            opt.textContent = other.name;
            studentSelect.appendChild(opt);
        }
        const cAddBtn = document.createElement('button');
        cAddBtn.className = 'input-action';
        cAddBtn.textContent = '+';
        cAddBtn.addEventListener('click', async () => {
            const otherID = parseInt(studentSelect.value);
            if (!otherID) return;
            await api('POST', '/api/trips/' + tripID + '/constraints', {
                student_a_id: student.id,
                student_b_id: otherID,
                kind: kindSelect.value,
                level: levelSelect.value
            });
            loadStudents();
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
document.getElementById('new-student-name').addEventListener('keydown', (e) => { if (e.key === 'Enter') addStudent(); });
document.getElementById('new-student-email').addEventListener('keydown', (e) => { if (e.key === 'Enter') addStudent(); });

await loadStudents();
await customElements.whenDefined('wa-button');
document.body.style.opacity = 1;
