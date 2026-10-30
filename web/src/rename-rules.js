export const renameRuleTypes = [
  { value: 'replace', label: '查找替换', icon: 'Search' },
  { value: 'number', label: '修改名称/添加序号', icon: 'ListOrdered' },
  { value: 'insert', label: '添加字符', icon: 'Plus' },
  { value: 'delete', label: '删除字符', icon: 'Trash2' },
  { value: 'move', label: '移动字符', icon: 'ArrowLeftRight' },
  { value: 'convert', label: '字符转换', icon: 'Repeat' }
]
export const renameDeleteTypes = [
  { value: 'english', label: '删除英文字母' }, { value: 'digits', label: '删除数字' },
  { value: 'special', label: '删除特殊字符' }, { value: 'serial', label: '删除序号编号' },
  { value: 'punctuation', label: '删除标点符号' }, { value: 'units', label: '删除单位符号' },
  { value: 'arrows', label: '删除箭头符号' }, { value: 'box', label: '删除绘表符号' }
]
const fields = {
  replace: { find: '', replace: '', findType: 'literal', caseSensitive: false, firstOnly: false },
  number: { name: '', start: 1, digits: 1, numberPosition: 'before', separator: '、' },
  insert: { position: 1, text: '' },
  delete: { deleteTypes: [], deleteRange: false, from: 1, to: 1, fromEnd: false, toEnd: false, text: '', firstOnly: false },
  move: { from: 1, to: 1 },
  convert: { caseMode: 'none', chineseMode: 'none', widthMode: 'none' }
}
export function newRenameRule(kind = 'replace', saved = {}) {
  return { kind, ...structuredClone(fields[kind] || fields.replace), ...saved }
}
export function serializedRenameRule(rule) {
  const defaults = fields[rule.kind]
  if (!defaults) return null
  if (rule.kind === 'replace' && !rule.find?.trim()) return null
  if (rule.kind === 'insert' && !rule.text) return null
  if (rule.kind === 'delete' && !rule.deleteTypes?.length && !rule.deleteRange && !rule.text) return null
  const result = { kind: rule.kind }
  for (const key of Object.keys(defaults)) result[key] = rule[key] ?? defaults[key]
  return result
}
