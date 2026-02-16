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
document.getElementById('main').style.display = 'block';
document.getElementById('logout-btn').addEventListener('click', logout);

async function loadStudents() {
    const students = await api('GET', '/api/trips/' + tripID + '/students');
    const container = document.getElementById('students');
    container.innerHTML = '';
    for (const student of students) {
        const card = document.createElement('wa-card');

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
