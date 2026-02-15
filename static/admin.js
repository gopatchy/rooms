import { init, logout, api } from '/app.js';

const profile = await init();

try {
    const check = await api('GET', '/api/admin/check');
    if (!check.admin) {
        document.body.style.opacity = 1;
        document.body.textContent = 'Access denied.';
        throw new Error('not admin');
    }
} catch (e) {
    document.body.style.opacity = 1;
    if (!document.body.textContent) document.body.textContent = 'Access denied.';
    throw e;
}

document.getElementById('main').style.display = 'block';
document.getElementById('logout-btn').addEventListener('click', logout);

async function loadTrips() {
    const trips = await api('GET', '/api/trips');
    const container = document.getElementById('trips');
    container.innerHTML = '';
    for (const trip of trips) {
        const card = document.createElement('wa-card');
        const header = document.createElement('div');
        header.className = 'trip-header';
        const h3 = document.createElement('h3');
        h3.textContent = trip.name;
        const deleteBtn = document.createElement('wa-button');
        deleteBtn.variant = 'danger';
        deleteBtn.size = 'small';
        deleteBtn.textContent = 'Delete Trip';
        deleteBtn.addEventListener('click', async () => {
            if (!confirm('Delete trip "' + trip.name + '"?')) return;
            await api('DELETE', '/api/trips/' + trip.id);
            loadTrips();
        });
        header.appendChild(h3);
        header.appendChild(deleteBtn);
        card.appendChild(header);

        const adminLabel = document.createElement('strong');
        adminLabel.textContent = 'Trip Admins:';
        card.appendChild(adminLabel);

        for (const admin of trip.admins) {
            const row = document.createElement('div');
            row.className = 'admin-row';
            const span = document.createElement('span');
            span.textContent = admin.email;
            const removeBtn = document.createElement('wa-button');
            removeBtn.variant = 'danger';
            removeBtn.size = 'small';
            removeBtn.textContent = 'Remove';
            removeBtn.addEventListener('click', async () => {
                await api('DELETE', '/api/trips/' + trip.id + '/admins/' + admin.id);
                loadTrips();
            });
            row.appendChild(span);
            row.appendChild(removeBtn);
            card.appendChild(row);
        }

        const addRow = document.createElement('div');
        addRow.className = 'add-admin-row';
        const input = document.createElement('wa-input');
        input.placeholder = 'Admin email';
        const addBtn = document.createElement('wa-button');
        addBtn.variant = 'neutral';
        addBtn.size = 'small';
        addBtn.textContent = 'Add Admin';
        addBtn.addEventListener('click', async () => {
            const email = input.value.trim();
            if (!email) return;
            await api('POST', '/api/trips/' + trip.id + '/admins', { email });
            loadTrips();
        });
        addRow.appendChild(input);
        addRow.appendChild(addBtn);
        card.appendChild(addRow);

        container.appendChild(card);
    }
}

document.getElementById('create-trip-btn').addEventListener('click', async () => {
    const input = document.getElementById('new-trip-name');
    const name = input.value.trim();
    if (!name) return;
    await api('POST', '/api/trips', { name });
    input.value = '';
    loadTrips();
});

await loadTrips();
await customElements.whenDefined('wa-button');
document.body.style.opacity = 1;
