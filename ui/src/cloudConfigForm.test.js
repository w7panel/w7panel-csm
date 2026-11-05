import { test } from 'node:test'
import assert from 'node:assert/strict'
import { cloudConfigFromForm, formFromCloudConfig } from './cloudConfigForm.js'

test('form config keeps commands and file content when reopened', () => {
  const form = { packages: 'curl\ngit', commands: 'echo "a: b"\nsystemctl enable nginx', files: [{ path: '/etc/csm/test.txt', content: 'key: value\nline 2\n' }] }
  const raw = cloudConfigFromForm(form)
  assert.deepEqual(formFromCloudConfig(raw), form)
  assert.match(raw, /^#cloud-config\n/)
})

test('unsupported YAML stays in YAML mode', () => {
  assert.equal(formFromCloudConfig('#cloud-config\nusers:\n  - alice\n'), null)
  assert.equal(formFromCloudConfig('#cloud-config\n{\"packages\":null}'), null)
  assert.throws(() => cloudConfigFromForm({ packages: '', commands: '', files: [] }), /至少填写一项/)
  assert.throws(() => cloudConfigFromForm({ packages: '', commands: '', files: [{ path: 'relative', content: 'x' }] }), /路径必须/)
})
