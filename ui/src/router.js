import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/hosts' },
  { path: '/hosts', name: 'hosts' },
  { path: '/hosts/:name', name: 'host', meta: { section: 'hosts' } },
  { path: '/hosts/:name/terminal', name: 'terminal', meta: { section: 'hosts' } },
  ...['templates', 'preinstall', 'upgrade', 'orders', 'disks', 'versions', 'pricing', 'coupons', 'purchase'].map(name => ({ path: `/${name}`, name })),
  { path: '/apps', name: 'apps', meta: { section: 'templates' } },
  { path: '/:pathMatch(.*)*', redirect: '/hosts' },
]

export function createAppRouter(component, history = createWebHashHistory()) {
  return createRouter({ history, routes: routes.map(route => route.redirect ? route : { ...route, component }) })
}
