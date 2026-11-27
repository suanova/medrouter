import assert from 'node:assert/strict'
import { test } from 'node:test'

import { apply } from '../lib/index.js'

function fakeContext() {
  let factory
  return {
    ctx: {
      skills: {
        registerProvider(fn) {
          factory = fn
        },
      },
    },
    makeProvider() {
      assert.ok(factory, 'apply must register a provider')
      return factory()
    },
  }
}

test('registers every bundled skill with a usable description', async () => {
  const { ctx, makeProvider } = fakeContext()
  apply(ctx, {})
  const skills = await makeProvider().list()

  assert.deepEqual(
    skills.map((skill) => skill.name).sort(),
    ['medical-slides', 'medical-writing', 'pharma-trials', 'slides-design']
  )
  for (const skill of skills) {
    assert.ok(skill.description.length > 30, `${skill.name} description is too short`)
    assert.equal(skill.source, 'bundled')
    assert.equal(skill.rank, 600)
    assert.equal(skill.provider, 'medrouter')
    assert.ok(skill.invocation.modelInvocable)
    assert.ok(skill.invocation.userInvocable)
  }
})

test('get loads the instruction body without leaking the locator', async () => {
  const { ctx, makeProvider } = fakeContext()
  apply(ctx, {})
  const provider = makeProvider()
  const [candidate] = await provider.list()

  const loaded = await provider.get(candidate, { signal: undefined })
  assert.equal(loaded.name, candidate.name)
  assert.equal(loaded.locator, undefined)
  assert.ok(loaded.content.length > 200)
  assert.ok(!loaded.content.startsWith('---'), 'frontmatter must be stripped')
  assert.ok(loaded.content.includes('#'), 'instruction body expected')
})

test('fails loudly when an asset has no description', () => {
  const { ctx } = fakeContext()
  assert.throws(() => apply(ctx, { assetRoot: '/nonexistent/assets' }), /SKILL\.md|ENOENT/u)
})
