/* Mock-DOM execution of the exact standalone report script; no browser/service. */
'use strict';
const fs=require('fs'),path=require('path'),vm=require('vm'),crypto=require('crypto');
const root=path.resolve(__dirname,'..'),folder=path.join(root,'reports','draw-sporttery-study');
const reportPath=path.join(folder,'report.html'),html=fs.readFileSync(reportPath,'utf8');
const dataText=html.match(/<script id="sporttery-data" type="application\/json">([\s\S]*?)<\/script>/)[1];
const code=html.match(/<script>\s*([\s\S]*?)<\/script>/)[1];
const elements=new Map();
class Element {
 constructor(id){this.id=id;this.value=id==='scope'?'marked':id==='outcome'?'all':'';this.textContent=id==='sporttery-data'?dataText:'';this.innerHTML='';this.disabled=false;this.listeners={};}
 addEventListener(event,fn){(this.listeners[event]??=[]).push(fn);}
 emit(event){for(const fn of this.listeners[event]||[])fn();}
}
const get=id=>{if(!elements.has(id))elements.set(id,new Element(id));return elements.get(id);};
const context=vm.createContext({document:{getElementById:get},Set,Map,JSON,String,Math});
const issues=[];let methodChecks=0,outcomeChecks=0,searchChecks=0,paginationChecks=0,formulaChecks=0;
function assert(ok,message){if(!ok)throw new Error(message);}
try{
 vm.runInContext(code,context,{timeout:10000});
 const api=context.sportteryReport,data=api.data;
 assert(data.rows.length===512,'union row count');
 for(const scope of Object.keys(data.scopes)){
  get('scope').value=scope;get('scope').emit('change');
  const scopeSet=new Set(data.scopes[scope]),rows=data.rows.filter(r=>scopeSet.has(r.id));
  assert(rows.length===data.scopes[scope].length,'scope rows mismatch '+scope);
  for(const method of data.methods.filter(m=>m.scope===scope)){
   get('method').value=method.id;get('method').emit('change');
   get('search').value='';get('outcome').value='all';get('outcome').emit('change');
   const selected=new Set(method.selected),s=method.stats;
   const selectedRows=rows.filter(r=>selected.has(r.id));
   const d=selectedRows.filter(r=>r.label===1).length;
   assert(selectedRows.length===s.selected&&d===s.draw&&selectedRows.length-d===s.falseSelections,'aggregate mismatch '+method.id);
   assert(Math.abs(d/selectedRows.length-s.precision)<1e-12,'precision mismatch '+method.id);
   const aggregate=get('aggregate').textContent;
   for(const outcome of ['all','hit','false','miss','rejected']){
    get('outcome').value=outcome;get('outcome').emit('change');
    const expected=rows.filter(r=>outcome==='all'||api.status(r,selected)===outcome).map(r=>r.id);
    const actual=Array.from(api.getFiltered(),r=>r.id);
    assert(JSON.stringify(actual)===JSON.stringify(expected),'filter mismatch '+method.id+' '+outcome);
    const pageRows=(get('matches').innerHTML.match(/<tr>/g)||[]).length;
    assert(pageRows===Math.min(100,expected.length),'page count mismatch '+method.id+' '+outcome);
    assert(get('aggregate').textContent===aggregate,'outcome filter changes overall metrics');
    outcomeChecks++;
   }
   get('outcome').value='all';get('outcome').emit('change');
   for(const query of [rows[0].id,rows[0].home,'this-team-id-does-not-exist']){
    get('search').value=query;get('search').emit('input');
    const q=query.toLowerCase(),expected=rows.filter(r=>[r.id,r.jingcaiId,r.home,r.away].some(v=>String(v).toLowerCase().includes(q))).map(r=>r.id);
    assert(JSON.stringify(Array.from(api.getFiltered(),r=>r.id))===JSON.stringify(expected),'search mismatch '+method.id);
    assert(get('aggregate').textContent===aggregate,'search changes overall metrics');searchChecks++;
   }
   get('search').value='';get('search').emit('input');
   const pages=Math.ceil(rows.length/100);
   for(let page=0;page<pages;page++){
    assert(api.getPage()===page,'page state mismatch');
    assert((get('matches').innerHTML.match(/<tr>/g)||[]).length===Math.min(100,rows.length-page*100),'pagination slice mismatch');
    assert(get('prev').disabled===(page===0),'previous disabled mismatch');
    assert(get('next').disabled===(page===pages-1),'next disabled mismatch');
    paginationChecks++;if(page<pages-1)get('next').emit('click');
   }
   get('prev').emit('click');assert(api.getPage()===pages-2,'previous click mismatch');
   const formula=JSON.parse(get('formula').textContent);
   if(method.type==='rule')assert(JSON.stringify(formula.rules)===JSON.stringify(method.rules),'guard/formula display mismatch');
   else assert(formula.threshold===method.threshold&&formula.kind===method.kind,'model formula display mismatch');
   formulaChecks++;methodChecks++;
  }
 }
 assert(html.includes('随机标签也达到47.74%—51.79%')&&html.includes('模型训练内91.34%')&&html.includes('训练内甚至100%'),'capacity warnings missing');
}catch(error){issues.push(error.stack||String(error));}
const sha=value=>crypto.createHash('sha256').update(value).digest('hex');
const result={purpose:'Exact report JS executed in a minimal mock DOM; no live browser',reportSHA256:sha(fs.readFileSync(reportPath)),reportJavaScriptSHA256:sha(code),qaScriptSHA256:sha(fs.readFileSync(__filename)),checks:{methodChecks,outcomeChecks,searchChecks,paginationChecks,formulaChecks},issues};
fs.writeFileSync(path.join(folder,'report-ui-qa.json'),JSON.stringify(result,null,2)+'\n');
process.stdout.write(JSON.stringify(result,null,2)+'\n');if(issues.length)process.exitCode=1;
