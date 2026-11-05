import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/hosts' },
  { path: '/hosts', name: 'hosts' },
  { path: '/hosts/:name', name: 'host', meta: { section: 'hosts' } },
  { path: '/hosts/:name/terminal', name: 'terminal', meta: { section: 'hosts' } },
  ...['templates', 'preinstall', 'upgrade', 'orders', 'disks', 'versions', 'pricing', 'coupons', 'purchase'].map(name => ({ path: `/${name}`, name })),
  { path: '/versions/:name/policies', name: 'version-policies', meta: { section: 'versions', versionPage: true } },
  { path: '/versions/:name/executions', name: 'version-executions', meta: { section: 'versions', versionPage: true } },
  { path: '/apps', name: 'apps', meta: { section: 'templates' } },
  { path: '/:pathMatch(.*)*', redirect: '/hosts' },
]

export function createAppRouter(component, history = createWebHashHistory()) {
  return createRouter({ history, routes: routes.map(route => route.redirect ? route : { ...route, component }) })
}
