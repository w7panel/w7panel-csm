import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'

export default defineConfig({
  base: './',
  plugins: [vue(), VitePWA({
    injectRegister: null,
    registerType: 'autoUpdate',
    manifest: {
      name: 'CSM',
      short_name: 'CSM',
      lang: 'zh-CN',
      start_url: '/',
      scope: '/',
      display: 'standalone',
      background_color: '#f5f7fa',
      theme_color: '#165dff',
      icons: [
        { src: '/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any maskable' },
        { src: '/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any maskable' },
      ],
    },
    workbox: {
      globPatterns: ['**/*.{html,js,css,png}'],
      navigateFallbackDenylist: [/^\/csm-api\//, /^\/healthz$/],
    },
  })],
})
