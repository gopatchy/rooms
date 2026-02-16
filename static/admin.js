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

        const nameRow = document.createElement('div');
        nameRow.style.display = 'flex';
        nameRow.style.alignItems = 'center';
        const tripLink = document.createElement('a');
        tripLink.href = '/trip/' + trip.id;
        tripLink.textContent = trip.name;
        tripLink.className = 'trip-name';
        tripLink.style.flex = '1';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'close-btn';
        deleteBtn.textContent = '\u00d7';
        deleteBtn.addEventListener('click', async () => {
            if (!confirm('Delete trip "' + trip.name + '"?')) return;
            await api('DELETE', '/api/trips/' + trip.id);
            loadTrips();
        });
        nameRow.appendChild(tripLink);
        nameRow.appendChild(deleteBtn);
        card.appendChild(nameRow);

        const details = document.createElement('wa-details');
        details.summary = 'Admins';

        const tags = document.createElement('div');
        tags.className = 'tags';
        for (const admin of trip.admins) {
            const tag = document.createElement('wa-tag');
            tag.size = 'small';
            tag.variant = 'brand';
            tag.setAttribute('with-remove', '');
            tag.textContent = admin.email;
            tag.addEventListener('wa-remove', async () => {
                await api('DELETE', '/api/trips/' + trip.id + '/admins/' + admin.id);
                loadTrips();
            });
            tags.appendChild(tag);
        }
        details.appendChild(tags);

        const input = document.createElement('wa-input');
        input.placeholder = 'Add admin email';
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
            await api('POST', '/api/trips/' + trip.id + '/admins', { email });
            loadTrips();
        };
        addBtn.addEventListener('click', doAdd);
        input.addEventListener('keydown', (e) => { if (e.key === 'Enter') doAdd(); });
        input.appendChild(addBtn);
        details.appendChild(input);

        card.appendChild(details);
        container.appendChild(card);
    }
}

async function createTrip() {
    const input = document.getElementById('new-trip-name');
    const name = input.value.trim();
    if (!name) return;
    await api('POST', '/api/trips', { name });
    input.value = '';
    loadTrips();
}

document.getElementById('create-trip-btn').addEventListener('click', createTrip);
document.getElementById('new-trip-name').addEventListener('keydown', (e) => { if (e.key === 'Enter') createTrip(); });

await loadTrips();
await customElements.whenDefined('wa-button');
document.body.style.opacity = 1;
