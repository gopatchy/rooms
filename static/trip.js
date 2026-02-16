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

        const kindVariant = { must: 'success', prefer: 'brand', prefer_not: 'warning', must_not: 'danger' };
        const kindLabels = { must: 'Must', prefer: 'Prefer', prefer_not: 'Prefer Not', must_not: 'Must Not' };
        const kindOrder = { must: 0, prefer: 1, prefer_not: 2, must_not: 3 };
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
                tag.textContent = kindLabels[c.kind] + ': ' + otherName;
                tag.addEventListener('wa-remove', async () => {
                    await api('DELETE', '/api/trips/' + tripID + '/constraints/' + c.id);
                    loadStudents();
                });
                group.appendChild(tag);
            }
            cDetails.appendChild(group);
        }

        const addRow = document.createElement('div');
        addRow.className = 'constraint-add';
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
        const kindSelect = document.createElement('select');
        for (const kind of ['must', 'prefer', 'prefer_not', 'must_not']) {
            const opt = document.createElement('option');
            opt.value = kind;
            opt.textContent = kindLabels[kind];
            kindSelect.appendChild(opt);
        }
        const levelSelect = document.createElement('select');
        for (const level of ['student', 'parent', 'admin']) {
            const opt = document.createElement('option');
            opt.value = level;
            opt.textContent = level.charAt(0).toUpperCase() + level.slice(1);
            levelSelect.appendChild(opt);
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
        addRow.appendChild(studentSelect);
        addRow.appendChild(kindSelect);
        addRow.appendChild(levelSelect);
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
await customElements.whenDefined('wa-card');
document.body.style.opacity = 1;
