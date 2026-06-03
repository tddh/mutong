import { createApp, ref } from 'vue';

export function buildSSOLoginURL() {
    return '/api/auth/oidc/login';
}

export function safeRedirect(raw) {
    if (typeof raw !== 'string' || raw === '') return '/view/index.html';
    if (raw.includes('..')) return '/view/index.html';
    if (raw.startsWith('/view/')) return raw;
    if (raw.startsWith('/device')) return raw;
    if (raw.startsWith('/oauth2/')) return raw;
    return '/view/index.html';
}

createApp({
    setup() {
        const username = ref('');
        const password = ref('');
        const error = ref('');
        const loading = ref(false);

        function loginWithZitadel() {
            window.location.href = buildSSOLoginURL();
        }

        async function handleLogin() {
            error.value = '';
            if (!username.value || !password.value) {
                error.value = '请输入用户名和密码';
                return;
            }
            loading.value = true;

            try {
                const loginRes = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: username.value, password: password.value }),
                    credentials: 'include',
                });

                if (!loginRes.ok) {
                    const data = await loginRes.json().catch(() => ({}));
                    throw new Error(data.error || '用户名或密码错误');
                }

                const data = await loginRes.json();
                sessionStorage.setItem('access_token', data.access_token);
                sessionStorage.setItem('user', JSON.stringify(data.user));
                const redirect = new URLSearchParams(location.search).get('redirect');
                window.location.href = safeRedirect(redirect);
            } catch (e) {
                error.value = e.message;
            } finally {
                loading.value = false;
            }
        }

        return { username, password, error, loading, loginWithZitadel, handleLogin };
    }
}).mount('#app');
