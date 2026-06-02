import { reactive } from 'vue';

const authState = reactive({
    status: sessionStorage.getItem('access_token') ? 'loggedIn' : 'unknown',
    accessToken: sessionStorage.getItem('access_token') || null,
    user: JSON.parse(sessionStorage.getItem('user') || 'null'),

    get isAuthenticated() {
        return this.status === 'loggedIn';
    },

    async init() {
        // Try session cookie first (set by backend on /api/auth/login)
        try {
            const res = await fetch('/api/auth/me', { credentials: 'include' });
            if (res.ok) {
                this.user = await res.json();
                sessionStorage.setItem('user', JSON.stringify(this.user));
                this.status = 'loggedIn';
                return;
            }
        } catch {}

        if (this.accessToken) {
            try {
                const res = await fetch('/api/auth/me', {
                    headers: { Authorization: `Bearer ${this.accessToken}` },
                });
                if (res.ok) {
                    this.user = await res.json();
                    sessionStorage.setItem('user', JSON.stringify(this.user));
                    this.status = 'loggedIn';
                    return;
                }
            } catch {}
        }

        this.status = 'loggedOut';
        this.accessToken = null;
        this.user = null;
        sessionStorage.removeItem('access_token');
        sessionStorage.removeItem('user');
    },

    async logout() {
        await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' });
        this.accessToken = null;
        this.user = null;
        this.status = 'loggedOut';
        sessionStorage.removeItem('access_token');
        sessionStorage.removeItem('user');
        window.location.href = '/view/login.html';
    }
});

export function useAuth() { return authState; }
