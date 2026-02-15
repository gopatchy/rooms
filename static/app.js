const CLIENT_ID = '{{.env.GOOGLE_CLIENT_ID}}';

function getProfile() {
    const data = localStorage.getItem('profile');
    return data ? JSON.parse(data) : null;
}

function setProfile(profile) {
    localStorage.setItem('profile', JSON.stringify(profile));
}

export function logout() {
    localStorage.removeItem('profile');
    location.reload();
}

export async function api(method, path, body) {
    const profile = getProfile();
    const opts = {
        method,
        headers: {
            'Content-Type': 'application/json'
        }
    };
    if (profile?.token) {
        opts.headers['Authorization'] = 'Bearer ' + profile.token;
    }
    if (body !== undefined) {
        opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    if (!res.ok) {
        throw new Error(await res.text());
    }
    return res.json();
}

function bind(data) {
    document.querySelectorAll('[data-bind]').forEach(el => {
        const key = el.dataset.bind;
        const value = key.split('.').reduce((o, k) => o?.[k], data);
        if (el.tagName === 'IMG') {
            el.src = value;
        } else {
            el.textContent = value;
        }
    });
}

const googleReady = new Promise((resolve) => {
    const script = document.createElement('script');
    script.src = 'https://accounts.google.com/gsi/client';
    script.onload = resolve;
    document.head.appendChild(script);
});

export async function init() {
    let profile = getProfile();
    if (profile) {
        bind(profile);
        return profile;
    }

    await googleReady;

    const signin = document.getElementById('signin');
    signin.style.display = 'flex';

    profile = await new Promise((resolve) => {
        google.accounts.id.initialize({
            client_id: CLIENT_ID,
            callback: async (response) => {
                try {
                    const res = await fetch('/auth/google/callback', {
                        method: 'POST',
                        headers: {'Content-Type': 'application/x-www-form-urlencoded'},
                        body: 'credential=' + encodeURIComponent(response.credential)
                    });
                    if (!res.ok) {
                        throw new Error(`server returned ${res.status}: ${await res.text()}`);
                    }
                    const profile = await res.json();
                    setProfile(profile);
                    signin.style.display = 'none';
                    resolve(profile);
                } catch (err) {
                    console.error('sign-in callback error:', err);
                    alert('Sign-in failed: ' + err.message);
                }
            },
            error_callback: (err) => {
                console.error('google sign-in error:', err);
                alert('Google sign-in error: ' + (err.message || err.type || JSON.stringify(err)));
            }
        });

        const buttonContainer = document.createElement('div');
        signin.appendChild(buttonContainer);

        google.accounts.id.renderButton(buttonContainer, {
            type: 'standard',
            theme: 'filled_black',
            size: 'large',
            text: 'sign_in_with',
            shape: 'pill',
            logo_alignment: 'left'
        });
    });

    bind(profile);
    return profile;
}
