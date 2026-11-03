export const mediaCategoryTypes=[{value:'tv',label:'剧集',icon:'Tv'},{value:'movie',label:'电影',icon:'Film'}]
export const mediaCategoryOperators={equals:'等于',not_equals:'不等于',in:'属于以下任一值',not_in:'不属于以下值',contains:'包含',starts_with:'开头是',ends_with:'结尾是',gt:'大于',gte:'大于等于',lt:'小于',lte:'小于等于',between:'介于',contains_any:'包含任一项',contains_all:'包含全部项',contains_none:'不包含这些项',is_true:'是',is_false:'否',exists:'存在',not_exists:'不存在'}
let sequence=0
export const mediaCategoryId=()=>`mc-${Date.now().toString(36)}-${(++sequence).toString(36)}`
export const mediaCategoryLeaf=()=>({kind:'condition',field:'title',operator:'exists'})
export const mediaCategoryNoValue=op=>['exists','not_exists','is_true','is_false'].includes(op)
export const mediaCategoryListValue=(field,op)=>['list','integer_list'].includes(field?.type)||['between','in','not_in'].includes(op)
