/**
 * medrouter-skills — medical / pharmaceutical authoring skills for the
 * DeepSeek Harness profile MedRouter runs.
 *
 * The harness lists every skill by its frontmatter description and loads the
 * full instructions only when the model picks it. This package is a plain
 * Cordis plugin with no dependencies: it is added with
 * `dsh plugin add /opt/dsh-plugins/medrouter-skills` and mounted by
 * overlay.yml. Keeping it import-free matters because the package lives
 * outside the profile's node_modules, where a bare import would not resolve.
 */

import { readFileSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import { join, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const SKILL_NAMES = [
  'slides-design',
  'medical-slides',
  'medical-writing',
  'pharma-trials',
]

// Same value as @deepseek-ai/dsh-skill's BUNDLED_SKILL_RANK. Duplicated to
// avoid importing a peer package that this out-of-tree plugin cannot resolve.
const BUNDLED_SKILL_RANK = 600

const name = 'medrouter-skills'
const inject = ['skills']

/** Minimal frontmatter reader: these assets are authored here, not user-supplied. */
function parseSkill(raw, path) {
  const frontmatter = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/u.exec(raw)
  if (frontmatter?.[1] === undefined) {
    throw new Error(`medrouter-skills: ${path} has no YAML frontmatter`)
  }
  const description = /^description:\s*(.+)$/mu.exec(frontmatter[1])?.[1]
  const trimmed = description?.trim().replace(/^["']|["']$/gu, '') ?? ''
  if (!trimmed) {
    throw new Error(`medrouter-skills: ${path} has no description`)
  }
  return {
    description: trimmed,
    content: raw.slice(frontmatter[0].length).trim(),
  }
}

function apply(ctx, config = {}) {
  const assetRoot = config.assetRoot ?? fileURLToPath(new URL('../assets/', import.meta.url))

  const candidates = SKILL_NAMES.map((skillName) => {
    const directory = join(assetRoot, skillName)
    const path = join(directory, 'SKILL.md')
    const { description } = parseSkill(readFileSync(path, 'utf8'), path)
    return {
      name: skillName,
      description,
      invocation: {
        modelInvocable: true,
        userInvocable: true,
      },
      provider: 'medrouter',
      source: 'bundled',
      rank: BUNDLED_SKILL_RANK,
      resourceBase: {
        kind: 'directory',
        path: directory.split(sep).join('/'),
      },
      locator: path,
    }
  })

  const provider = {
    name: 'medrouter',
    list: () => Promise.resolve(candidates),
    async get(candidate, options) {
      const { rank: _rank, locator, ...summary } = candidate
      const content = parseSkill(
        await readFile(locator, { encoding: 'utf8', signal: options.signal }),
        locator
      ).content
      return { ...summary, content }
    },
  }

  ctx.skills.registerProvider(() => provider)
}

export { apply, inject, name }
