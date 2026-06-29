<!-- 星记 · 登录页（桌面版） -->
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="csrf-token" content="" id="csrfMeta">
    <title>星记 · 登录</title>
    <link rel="stylesheet" href="bootstrap-icons.min.css">
    <style>
        :root {
            --bg-deep: #060a14;
            --bg-mid: #0c1220;
            --accent: #5B9BD5;
            --accent-glow: rgba(91, 155, 213, 0.3);
            --text: rgba(255, 255, 255, 0.88);
            --text-dim: rgba(255, 255, 255, 0.45);
            --border: rgba(255, 255, 255, 0.08);
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            min-height: 100vh;
            background: linear-gradient(165deg, var(--bg-deep) 0%, var(--bg-mid) 50%, #0a0f1c 100%);
            display: flex;
            align-items: center;
            justify-content: center;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            color: var(--text);
            overflow: hidden;
        }
        body::before {
            content: '';
            position: fixed;
            top: 0; left: 0; right: 0; bottom: 0;
            background:
                radial-gradient(1px 1px at 20% 30%, rgba(255,255,255,0.4), transparent),
                radial-gradient(1px 1px at 50% 60%, rgba(255,255,255,0.3), transparent),
                radial-gradient(1px 1px at 80% 20%, rgba(255,255,255,0.35), transparent),
                radial-gradient(1.5px 1.5px at 35% 75%, rgba(91,155,213,0.4), transparent),
                radial-gradient(1px 1px at 65% 45%, rgba(255,255,255,0.25), transparent),
                radial-gradient(1.5px 1.5px at 90% 80%, rgba(255,255,255,0.3), transparent),
                radial-gradient(1px 1px at 10% 90%, rgba(255,255,255,0.2), transparent),
                radial-gradient(1px 1px at 70% 10%, rgba(91,155,213,0.3), transparent);
            pointer-events: none;
            animation: twinkle 8s ease-in-out infinite alternate;
        }
        @keyframes twinkle {
            0% { opacity: 0.6; }
            100% { opacity: 1; }
        }
        .login-card {
            position: relative;
            width: 340px;
            padding: 40px 32px;
            background: rgba(12, 18, 32, 0.85);
            border: 1px solid var(--border);
            border-radius: 20px;
            backdrop-filter: blur(20px);
            box-shadow: 0 24px 64px rgba(0, 0, 0, 0.5);
        }
        .login-card::before {
            content: '';
            position: absolute;
            top: -1px; left: 30%; right: 30%;
            height: 1px;
            background: linear-gradient(90deg, transparent, var(--accent), transparent);
        }
        .logo {
            text-align: center;
            margin-bottom: 32px;
        }
        .logo-icon {
            font-size: 2rem;
            margin-bottom: 8px;
            display: block;
            filter: drop-shadow(0 0 8px var(--accent-glow));
        }
        .logo h1 {
            font-size: 1.1rem;
            font-weight: 600;
            letter-spacing: 0.5px;
        }
        .logo p {
            font-size: 0.72rem;
            color: var(--text-dim);
            margin-top: 4px;
        }
        .field {
            margin-bottom: 16px;
        }
        .field label {
            display: block;
            font-size: 0.72rem;
            color: var(--text-dim);
            margin-bottom: 6px;
            letter-spacing: 0.3px;
        }
        .field input {
            width: 100%;
            padding: 10px 14px;
            background: rgba(255, 255, 255, 0.04);
            border: 1px solid var(--border);
            border-radius: 10px;
            color: var(--text);
            font-size: 0.88rem;
            outline: none;
            transition: border-color 0.2s, box-shadow 0.2s;
        }
        .field input:focus {
            border-color: var(--accent);
            box-shadow: 0 0 0 3px var(--accent-glow);
        }
        .field input::placeholder {
            color: rgba(255, 255, 255, 0.2);
        }
        .btn-login {
            width: 100%;
            padding: 11px;
            margin-top: 8px;
            background: linear-gradient(135deg, var(--accent), #4a8bc2);
            border: none;
            border-radius: 10px;
            color: #fff;
            font-size: 0.88rem;
            font-weight: 500;
            cursor: pointer;
            letter-spacing: 0.5px;
            transition: transform 0.15s, box-shadow 0.15s;
        }
        .btn-login:hover {
            transform: translateY(-1px);
            box-shadow: 0 4px 16px var(--accent-glow);
        }
        .btn-login:active {
            transform: translateY(0);
        }
        .error-msg {
            text-align: center;
            font-size: 0.78rem;
            color: #e57373;
            margin-top: 12px;
            min-height: 1.2em;
        }
    </style>
</head>
<body>
    <div class="login-card">
        <div class="logo">
            <span class="logo-icon"><i class="bi bi-stars"></i></span>
            <h1>星记</h1>
            <p>私人领地</p>
        </div>
        <form id="loginForm" autocomplete="off">
            <div class="field">
                <label>领航员</label>
                <input type="text" id="loginUser" placeholder="身份标识" autocomplete="username" required autofocus>
            </div>
            <div class="field">
                <label>密钥</label>
                <input type="password" id="loginPass" placeholder="通行密码" autocomplete="current-password" required>
            </div>
            <button type="submit" class="btn-login" id="btnLogin">进入星空</button>
            <div class="error-msg" id="loginError"></div>
        </form>
    </div>
    <script>
    (function() {
        // SHA-256 哈希（Web Crypto API）
        async function sha256(str) {
            const buf = new TextEncoder().encode(str);
            const hash = await crypto.subtle.digest('SHA-256', buf);
            return Array.from(new Uint8Array(hash)).map(b => b.toString(16).padStart(2, '0')).join('');
        }

        // 如果已有 token，直接进入主界面
        if (localStorage.getItem('star-track-token')) {
            window.location.href = 'index.php';
            return;
        }

        document.getElementById('loginForm').addEventListener('submit', async (e) => {
            e.preventDefault();
            const btn = document.getElementById('btnLogin');
            const errEl = document.getElementById('loginError');
            const user = document.getElementById('loginUser').value.trim();
            const pass = document.getElementById('loginPass').value;

            errEl.textContent = '';
            btn.disabled = true;
            btn.textContent = '验证中...';

            try {
                // 1. 取 nonce
                const nonceRes = await fetch('api/auth.php?action=challenge');
                const nonceData = await nonceRes.json();
                if (!nonceData.nonce) throw new Error('无法获取验证令牌');

                // 2. 客户端哈希：SHA256(nonce + password)
                const hash = await sha256(nonceData.nonce + pass);

                // 3. 发送哈希（带 nonce）
                const loginRes = await fetch('api/auth.php', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/x-www-form-urlencoded'},
                    body: 'action=login&username=' + encodeURIComponent(user) + '&hash=' + hash + '&nonce=' + nonceData.nonce
                });
                const loginData = await loginRes.json();

                if (loginData.success) {
                    // 存储 token 到 localStorage
                    localStorage.setItem('star-track-token', loginData.csrf_token);
                    // 同时写入 meta tag，供 main.js 读取
                    const meta = document.querySelector('meta[name="csrf-token"]');
                    if (meta) meta.content = loginData.csrf_token;
                    window.location.href = 'index.php';
                } else {
                    errEl.textContent = loginData.error || '登录失败';
                    btn.disabled = false;
                    btn.textContent = '进入星空';
                }
            } catch (err) {
                errEl.textContent = '连接异常，请重试';
                btn.disabled = false;
                btn.textContent = '进入星空';
            }
        });
    })();
    </script>
</body>
</html>