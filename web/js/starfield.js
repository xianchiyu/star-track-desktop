/**
 * 星空背景渲染引擎
 * Canvas 绘制深空渐变 + 漂移星尘 + 星云色块
 * 支持深色/浅色主题切换
 */
class StarField {
    constructor(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.stars = [];
        this.nebulae = [];
        this.dpr = window.devicePixelRatio || 1;
        this.running = true;
        this.theme = 'dark';

        this.resize();
        this.initStars();
        this.initNebulae();
        this.bindEvents();
        this.animate();
    }

    setTheme(theme) {
        this.theme = theme;
        this.initNebulae();
    }

    resize() {
        this.width = window.innerWidth;
        this.height = window.innerHeight;
        this.canvas.width = this.width * this.dpr;
        this.canvas.height = this.height * this.dpr;
        this.canvas.style.width = this.width + 'px';
        this.canvas.style.height = this.height + 'px';
        this.ctx.scale(this.dpr, this.dpr);
    }

    initStars() {
        const count = Math.floor(this.width * this.height / 4000);
        this.stars = [];
        for (let i = 0; i < count; i++) {
            this.stars.push({
                x: Math.random() * this.width,
                y: Math.random() * this.height,
                size: Math.random() * 1.8 + 0.3,
                baseOpacity: Math.random() * 0.6 + 0.15,
                opacity: 0,
                twinkleSpeed: Math.random() * 0.008 + 0.002,
                twinklePhase: Math.random() * Math.PI * 2,
                drift: (Math.random() - 0.5) * 0.05
            });
        }
    }

    initNebulae() {
        if (this.theme === 'light') {
            // 浅色：暖纸底上的淡蓝灰色云纹
            this.nebulae = [
                { x: this.width * 0.2, y: this.height * 0.3, r: 280, color: 'rgba(83,125,150,0.04)' },
                { x: this.width * 0.75, y: this.height * 0.6, r: 320, color: 'rgba(83,125,150,0.05)' },
                { x: this.width * 0.5, y: this.height * 0.8, r: 220, color: 'rgba(120,100,80,0.03)' }
            ];
        } else {
            // 深色：紫蓝星云
            this.nebulae = [
                { x: this.width * 0.2, y: this.height * 0.3, r: 250, color: 'rgba(60,20,120,0.04)' },
                { x: this.width * 0.75, y: this.height * 0.6, r: 300, color: 'rgba(20,40,100,0.05)' },
                { x: this.width * 0.5, y: this.height * 0.8, r: 200, color: 'rgba(80,30,80,0.03)' }
            ];
        }
    }

    bindEvents() {
        window.addEventListener('resize', () => {
            this.resize();
            this.initStars();
            this.initNebulae();
        });
    }

    animate() {
        if (!this.running) return;

        this.ctx.clearRect(0, 0, this.width, this.height);

        if (this.theme === 'light') {
            // 浅色：暖纸渐变
            const bg = this.ctx.createLinearGradient(0, 0, 0, this.height);
            bg.addColorStop(0, '#F0EBE1');
            bg.addColorStop(0.5, '#F5EFE4');
            bg.addColorStop(1, '#EDE7DA');
            this.ctx.fillStyle = bg;
            this.ctx.fillRect(0, 0, this.width, this.height);
        } else {
            // 深空渐变背景
            const bg = this.ctx.createLinearGradient(0, 0, 0, this.height);
            bg.addColorStop(0, '#06061a');
            bg.addColorStop(0.5, '#0a0a2e');
            bg.addColorStop(1, '#050510');
            this.ctx.fillStyle = bg;
            this.ctx.fillRect(0, 0, this.width, this.height);
        }

        // 星云色块
        this.nebulae.forEach(n => {
            const grad = this.ctx.createRadialGradient(n.x, n.y, 0, n.x, n.y, n.r);
            grad.addColorStop(0, n.color);
            grad.addColorStop(1, 'transparent');
            this.ctx.fillStyle = grad;
            this.ctx.fillRect(n.x - n.r, n.y - n.r, n.r * 2, n.r * 2);
        });

        // 星尘
        const starColor = this.theme === 'light' ? [83, 125, 150] : [220, 230, 255];
        const glowColor = this.theme === 'light' ? [83, 125, 150] : [200, 210, 255];

        this.stars.forEach(star => {
            star.twinklePhase += star.twinkleSpeed;
            star.opacity = star.baseOpacity + Math.sin(star.twinklePhase) * star.baseOpacity * 0.4;
            star.opacity = Math.max(0.05, Math.min(0.9, star.opacity));

            // 微弱漂移
            star.x += star.drift;
            if (star.x < -5) star.x = this.width + 5;
            if (star.x > this.width + 5) star.x = -5;

            this.ctx.beginPath();
            this.ctx.arc(star.x, star.y, star.size, 0, Math.PI * 2);
            this.ctx.fillStyle = `rgba(${starColor[0]},${starColor[1]},${starColor[2]},${star.opacity})`;
            this.ctx.fill();

            // 较亮的星加一点光晕
            if (star.size > 1.2 && star.opacity > 0.4) {
                this.ctx.beginPath();
                this.ctx.arc(star.x, star.y, star.size * 2.5, 0, Math.PI * 2);
                this.ctx.fillStyle = `rgba(${glowColor[0]},${glowColor[1]},${glowColor[2]},${star.opacity * 0.08})`;
                this.ctx.fill();
            }
        });

        requestAnimationFrame(() => this.animate());
    }

    destroy() {
        this.running = false;
    }
}
