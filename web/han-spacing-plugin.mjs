import { MagicString, parse } from 'vue/compiler-sfc'

const excluded = new Set(['input', 'textarea', 'select', 'option', 'script', 'style'])
const escapeAttribute = value => value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

export function transformHanSpacing(source, filename = 'Component.vue') {
  const { descriptor, errors } = parse(source, { filename })
  const template = descriptor.template
  if (errors.length || !template?.ast || template.lang || template.src) return null
  const output = new MagicString(source)
  let changed = false
  function visit(node) {
    if (node.ns || excluded.has(node.tag) || node.props?.some(prop =>
      prop.name === 'contenteditable' || prop.name === 'pre' && prop.type === 7)) return
    const children = node.children || []
    for (let index = 0; index < children.length;) {
      const child = children[index]
      if (child.type !== 2 && child.type !== 5) { visit(child); index++; continue }
      const group = []
      while (index < children.length && [2, 5].includes(children[index].type)) group.push(children[index++])
      if (!group.some(item => item.type === 5 || /\p{Unified_Ideograph}{2}/u.test(item.content))) continue
      const values = group.map(item => item.type === 2 ? JSON.stringify(item.content) : `(${item.content.content})`)
      // Vue owns both the original values and the visual glyph nodes, including virtual rows.
      output.overwrite(group[0].loc.start.offset, group.at(-1).loc.end.offset,
        `<AetherHanText :parts="${escapeAttribute(`[${values.join(',')}]`)}" />`)
      changed = true
    }
  }
  visit(template.ast)
  return changed ? { code: output.toString(), map: output.generateMap({ hires: true, source: filename, includeContent: true }) } : null
}

export function hanSpacingPlugin() {
  return {
    name: 'aether-han-spacing',
    enforce: 'pre',
    transform(source, id) {
      return id.endsWith('.vue') ? transformHanSpacing(source, id) : null
    }
  }
}
