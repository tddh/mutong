window.sharedComponents = {
    NavBar: {
        template: `
            <nav class="navbar">
                <div class="nav-brand" @click="home">牧童 · 重明</div>
                <div class="nav-links">
                    <a href="/view/index.html" :class="{ active: isActive('/view/index.html') }">仪表盘</a>
                    <a href="/view/topology/index.html" :class="{ active: isActive('/view/topology/index.html') }">拓扑</a>
                    <a href="/view/alerts/index.html" :class="{ active: isActive('/view/alerts/index.html') }">告警</a>
                    <a href="/view/inspection/index.html" :class="{ active: isActive('/view/inspection/index.html') }">巡检</a>
                    <a href="/view/resource_table.html" :class="{ active: isActive('/view/resource_table.html') }">资源表格</a>
                </div>
            </nav>
        `,
        setup() {
            const currentPath = window.location.pathname;
            const home = () => { window.location.href = '/view/index.html'; };
            const isActive = (href) => currentPath === href;
            return { home, isActive };
        }
    },
    StatCard: {
        props: ['title', 'value', 'unit', 'color', 'icon'],
        template: `
            <div class="stat-card">
                <div class="stat-content">
                    <span class="stat-title">{{ title }}</span>
                    <span class="stat-value" :style="{ color: color }">{{ value }}<small class="unit">{{ unit }}</small></span>
                </div>
                <div class="stat-icon" v-if="icon">{{ icon }}</div>
            </div>
        `
    }
};
