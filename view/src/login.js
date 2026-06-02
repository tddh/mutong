import { createApp, ref, onMounted } from 'vue';

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
        const locked = ref(false);

        const showCaptcha = ref(false);
        const captchaId = ref('');
        const captchaImage = ref('');
        const captchaAnswer = ref('');
        let failCount = 0;

        onMounted(async () => {
            try {
                const res = await fetch('/api/auth/login-status', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({}),
                }).then(r => r.json());
                failCount = res.fail_count || 0;
                showCaptcha.value = res.require_captcha || false;
                locked.value = res.locked || false;
                if (showCaptcha.value) await refreshCaptcha();
            } catch {}
        });

        async function refreshCaptcha() {
            try {
                const res = await fetch('/api/auth/captcha').then(r => r.json());
                captchaId.value = res.captcha_id;
                captchaImage.value = res.image || res.thumb;
                captchaAnswer.value = '';
            } catch { error.value = '无法加载验证码'; }
        }

        function loginWithZitadel() {
            window.location.href = buildSSOLoginURL();
        }

        async function handleLogin() {
            error.value = '';
            if (!username.value || !password.value) {
                error.value = '请输入用户名和密码';
                return;
            }
            if (showCaptcha.value && !captchaAnswer.value) {
                error.value = '请输入验证码';
                return;
            }
            loading.value = true;

            try {
                if (showCaptcha.value) {
                    const captchaRes = await fetch('/api/auth/captcha/verify', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ captcha_id: captchaId.value, dots: captchaAnswer.value }),
                    }).then(r => r.json());
                    if (!captchaRes.valid) {
                        error.value = '验证码错误';
                        loading.value = false;
                        await refreshCaptcha();
                        return;
                    }
                }

                const loginRes = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: username.value, password: password.value }),
                    credentials: 'include',
                });

                if (!loginRes.ok) {
                    const data = await loginRes.json().catch(() => ({}));
                    failCount++;
                    if (failCount >= 3) {
                        showCaptcha.value = true;
                        await refreshCaptcha();
                    }
                    if (failCount >= 5) locked.value = true;
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

        return { username, password, error, loading, locked, showCaptcha, captchaId, captchaImage, captchaAnswer, refreshCaptcha, loginWithZitadel, handleLogin };
    }
}).mount('#app');
