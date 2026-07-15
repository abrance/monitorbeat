import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
// base: './' 让 Go 从子路径托管 dist 也正常（HashRouter 下服务器只收到 '/'）。
export default defineConfig({
    base: './',
    plugins: [react()],
    build: {
        outDir: 'dist',
    },
    server: {
        proxy: {
            '/api': 'http://127.0.0.1:8080',
        },
    },
});
