import { createApp } from 'vue'
import ArcoVue from '@arco-design/web-vue'
import '@arco-design/web-vue/dist/arco.css'
import { RouterView } from 'vue-router'
import App from './App.vue'
import { createAppRouter } from './router'

createApp(RouterView).use(ArcoVue).use(createAppRouter(App)).mount('#app')

if (window.top === window && 'serviceWorker' in navigator) {
  import('virtual:pwa-register').then(({ registerSW }) => registerSW({ immediate: true }))
}
