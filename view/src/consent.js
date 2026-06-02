import { createApp, ref, onMounted } from 'vue';

createApp({
    setup() {
        const clientName = ref('');
        const scopes = ref('');

        onMounted(async () => {
            const params = new URLSearchParams(location.search);
            const challenge = params.get('consent_challenge');
            if (challenge) {
                try {
                    const res = await fetch(`/api/auth/consent?challenge=${challenge}`).then(r => r.json());
                    clientName.value = res.client_name || '未知应用';
                    scopes.value = (res.scopes || []).join(', ');
                } catch {}
            }
        });

        function approve() {
            const params = new URLSearchParams(location.search);
            fetch('/api/auth/consent', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'approve', challenge: params.get('consent_challenge') }),
            }).then(r => r.json()).then(data => {
                window.location.href = data.redirect_to || '/view/index.html';
            });
        }

        function deny() {
            const params = new URLSearchParams(location.search);
            fetch('/api/auth/consent', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'deny', challenge: params.get('consent_challenge') }),
            }).then(r => r.json()).then(data => {
                window.location.href = data.redirect_to || '/view/index.html';
            });
        }

        return { clientName, scopes, approve, deny };
    }
}).mount('#app');
