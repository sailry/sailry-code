import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFile} from 'node:fs/promises';

// Adapted from sailry-plugins/tests/panel-entry.test.mjs at bd42d9c.
test('database page entry refreshes catalogs without SQL execution or losing selection and drafts',async()=>{
  const source=(await readFile(new URL('../view/main.js',import.meta.url),'utf8'))
    .replace(/^import.*;\n/gm,'').replace(/^export /gm,'').replace('default class','class');
  const jobs=[],loads=[],profile={id:'saved',revision:'4'}, nextChange=()=>{};
  const Controller=vm.runInNewContext(`${source}\nDatabases`,{
    View:class{},context:()=>JSON.stringify({locale:'en'}),messages:()=>({}),nextChange,
    listDatabases:async()=>[profile],listSsh:async()=>[],readProjectCatalog:async()=>({projects:[]}),
    createText:()=> 'sql-draft',nextTableEvent(){},nextTreeEvent(){},nextContextMenuEvent(){},
    header_action(){},nextNavigationTabEvent(){},nextPickerEvent(){},nextControlEvent(){},nextTextEvent(){},modal_closed(){},nextAssistantTools(){},
    Request:class{constructor(){assert.fail('entry must not execute SQL or replay a request');}},
  });
  const owner=new Controller(),cx={notify(){},spawn:job=>jobs.push(job)};
  let changed;
  owner.watch=(read,handle)=>{if(read===nextChange)changed=handle;};owner.init(null,cx);
  assert.equal(jobs.length,0);
  changed({cursor:'0/7:true',entry:'0',connected:true},cx);await jobs.shift()(cx);
  const catalog={profile,loading:new Set(),expanded:new Set(['main']),current:'table:0:0',load:async(database,_cx,reset)=>loads.push([database,reset])};
  const result={rows:[['Retained result']]},editor={draft:'Unsaved connection'};
  Object.assign(owner,{opened:profile.id,catalog,result,editor,selectedLog:'selected',page:3});
  changed({cursor:'1/7:true',entry:'1',connected:true},cx);await jobs.shift()(cx);
  assert.deepEqual(loads,[[null,undefined],['main',undefined]]);
  assert.equal(owner.catalog,catalog);assert.equal(catalog.current,'table:0:0');assert.ok(catalog.expanded.has('main'));
  assert.equal(owner.result,result);assert.equal(owner.editor,editor);assert.equal(owner.sql,'sql-draft');
  assert.equal(owner.selectedLog,'selected');assert.equal(owner.page,3);
  const pending={request:{id:'original-request',unknown:true}};owner.pending=pending;
  changed({cursor:'2/7:true',entry:'2',connected:true},cx);await jobs.shift()(cx);
  assert.equal(loads.length,2);assert.equal(owner.pending,pending);
});
