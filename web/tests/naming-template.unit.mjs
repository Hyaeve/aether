import {test} from 'node:test'
import assert from 'node:assert/strict'
import {parseNamingTemplate,serializeNamingTemplate,reorderRuleItems} from '../src/naming-template.js'
const variables=[{id:'title'},{id:'year'},{id:'season_episode',tvOnly:true}]
test('template source roundtrips paths spaces and literal connectors without evaluation',()=>{const s='{title} ({year})/{title}.{season_episode}';const p=parseNamingTemplate(s,variables,'tv');assert.equal(p.error,'');assert.equal(serializeNamingTemplate(p.tokens),s);assert.equal(serializeNamingTemplate([{kind:'text',value:''}]),' ')})
test('template diagnostics distinguish scoped duplicate variables and incomplete paths',()=>{for(const s of ['{title}/{title}.{year}','{title}'])assert.equal(parseNamingTemplate(s,variables,'movie').error,'');for(const s of ['{title}.{title}','{season_episode}','{ext}','{title}/','/{title}','{title}\\{year}','{title}\n','{title}:{year}'])assert.notEqual(parseNamingTemplate(s,variables,'movie').error,'',s)})
test('reordering supports first last movement and preserves the draft input',()=>{const a=['title','text','year'];assert.deepEqual(reorderRuleItems(a,2,0),['year','title','text']);assert.deepEqual(reorderRuleItems(a,0,2),['text','year','title']);assert.deepEqual(reorderRuleItems(a,0,-1),a);assert.deepEqual(a,['title','text','year'])})
