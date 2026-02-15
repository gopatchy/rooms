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
        const header = document.createElement('div');
        header.className = 'student-header';
        const h3 = document.createElement('h3');
        h3.textContent = student.name + ' (' + student.email + ')';
        const deleteBtn = document.createElement('wa-button');
        deleteBtn.variant = 'danger';
        deleteBtn.size = 'small';
        deleteBtn.textContent = 'Remove';
        deleteBtn.addEventListener('click', async () => {
            if (!confirm('Remove student "' + student.name + '"?')) return;
            await api('DELETE', '/api/trips/' + tripID + '/students/' + student.id);
            loadStudents();
        });
        header.appendChild(h3);
        header.appendChild(deleteBtn);
        card.appendChild(header);

        const parentLabel = document.createElement('strong');
        parentLabel.textContent = 'Parents:';
        card.appendChild(parentLabel);

        for (const parent of student.parents) {
            const row = document.createElement('div');
            row.className = 'parent-row';
            const span = document.createElement('span');
            span.textContent = parent.email;
            const removeBtn = document.createElement('wa-button');
            removeBtn.variant = 'danger';
            removeBtn.size = 'small';
            removeBtn.textContent = 'Remove';
            removeBtn.addEventListener('click', async () => {
                await api('DELETE', '/api/trips/' + tripID + '/students/' + student.id + '/parents/' + parent.id);
                loadStudents();
            });
            row.appendChild(span);
            row.appendChild(removeBtn);
            card.appendChild(row);
        }

        const addRow = document.createElement('div');
        addRow.className = 'add-parent-row';
        const input = document.createElement('wa-input');
        input.placeholder = 'Parent email';
        const addBtn = document.createElement('wa-button');
        addBtn.variant = 'neutral';
        addBtn.size = 'small';
        addBtn.textContent = 'Add Parent';
        addBtn.addEventListener('click', async () => {
            const email = input.value.trim();
            if (!email) return;
            await api('POST', '/api/trips/' + tripID + '/students/' + student.id + '/parents', { email });
            loadStudents();
        });
        addRow.appendChild(input);
        addRow.appendChild(addBtn);
        card.appendChild(addRow);

        container.appendChild(card);
    }
}

document.getElementById('add-student-btn').addEventListener('click', async () => {
    const nameInput = document.getElementById('new-student-name');
    const emailInput = document.getElementById('new-student-email');
    const name = nameInput.value.trim();
    const email = emailInput.value.trim();
    if (!name || !email) return;
    await api('POST', '/api/trips/' + tripID + '/students', { name, email });
    nameInput.value = '';
    emailInput.value = '';
    loadStudents();
});

await loadStudents();
await customElements.whenDefined('wa-button');
document.body.style.opacity = 1;
