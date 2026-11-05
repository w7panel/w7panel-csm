import { test } from 'node:test'
import assert from 'node:assert/strict'
import { deploymentToTemplateApp, templateToDeployment } from './templateApp.js'

const editor = { name: 'ubuntu', image: 'csm:old', app: { env: [{ name: 'MODE', value: 'safe' }], containers: [], initContainers: [] } }

test('host editor round trip saves only supported fields', () => {
  const unchanged = templateToDeployment(editor)
  assert.deepEqual(deploymentToTemplateApp(editor, unchanged), { image: 'csm:old', app: editor.app })
  delete unchanged.spec.template.spec.initContainers
  assert.deepEqual(deploymentToTemplateApp(editor, unchanged), { image: 'csm:old', app: editor.app })
  const changed = templateToDeployment(editor)
  changed.spec.template.spec.containers[0].image = 'csm:new'
  changed.spec.template.spec.containers.push({ name: 'helper', image: 'helper:1', command: ['sleep'], args: ['60'], env: [{ name: 'TOKEN', value: 'x' }] })
  changed.spec.template.spec.initContainers.push({ name: 'prepare', image: 'busybox', command: ['true'] })
  const result = deploymentToTemplateApp(editor, changed)
  assert.equal(result.image, 'csm:new')
  assert.equal(result.app.containers[0].name, 'helper')
  assert.equal(result.app.initContainers[0].name, 'prepare')
})

test('host editor rejects protected and unpersisted fields', () => {
  for (const change of [
    dep => { dep.spec.template.spec.volumes = [{ name: 'secret', secret: { secretName: 'other' } }] },
    dep => { dep.spec.template.spec.nodeName = 'node-1' },
    dep => { dep.spec.template.spec.containers[0].command = ['sh'] },
    dep => { dep.spec.template.spec.containers[0].volumeMounts = [{ name: 'system', mountPath: '/' }] },
    dep => { dep.spec.template.spec.containers[0].env.push({ name: 'X', valueFrom: { secretKeyRef: { name: 's', key: 'k' } } }) },
  ]) {
    const dep = templateToDeployment(editor)
    change(dep)
    assert.throws(() => deploymentToTemplateApp(editor, dep))
  }
})
