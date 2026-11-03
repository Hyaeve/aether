export function parseNamingTemplate(source,variables,kind){
 const tokens=[],allowed=new Set(variables.filter(v=>!v.tvOnly||kind==='tv').map(v=>v.id));let remaining=source,seen=new Set(),count=0,segments=1,error=''
 if(!source||source.length>8192||/[\\\x00-\x1f\x7f]/.test(source))error='命名模板为空、过长或含非法字符'
 while(remaining){
  if(remaining[0]==='/'){tokens.push({kind:'path',value:'/'});remaining=remaining.slice(1);if(!count||segments>=8)error||='路径前后须有命名变量，且最多8层';seen=new Set();count=0;segments++}
  else if(remaining[0]==='{'){const end=remaining.indexOf('}');if(end<0){error||='命名变量缺少右花括号';break}const value=remaining.slice(1,end);if(!allowed.has(value))error||=`不支持的命名变量：${value}`;if(seen.has(value))error||=`同一路径区段不能重复使用变量：${value}`;seen.add(value);count++;tokens.push({kind:'variable',value});remaining=remaining.slice(end+1)}
  else{let end=remaining.search(/[/{]/);if(end<0)end=remaining.length;const value=remaining.slice(0,end);if(value.includes('}'))error||='文本标签不能含花括号';if(/[<>:"|?*]/.test(value))error||='文本标签含文件名称不支持的字符';tokens.push({kind:'text',value});remaining=remaining.slice(end)}
  if(tokens.length>256){error||='模板最多256个标签';break}
 }
 if(!count)error||='每个路径区段须有命名变量'
 return {tokens,error}
}
export const serializeNamingTemplate=tokens=>tokens.map(t=>t.kind==='variable'?`{${t.value}}`:t.kind==='path'?'/':t.value||' ').join('')
export function reorderRuleItems(list,from,to){const copy=[...list];if(from<0||to<0||from>=copy.length||to>=copy.length)return copy;const [item]=copy.splice(from,1);copy.splice(to,0,item);return copy}
