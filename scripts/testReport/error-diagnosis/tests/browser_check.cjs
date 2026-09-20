const assert=require('node:assert/strict'),path=require('node:path'),fs=require('node:fs'),{pathToFileURL}=require('node:url');
const args={};for(let i=2;i<process.argv.length;i+=2)args[process.argv[i].replace(/^--/,'')]=process.argv[i+1];
const {chromium}=require(path.join(args.modules,'playwright'));
(async()=>{
const browser=await chromium.launch({headless:true,...(args.browser?{executablePath:args.browser}:{})});
try{
 const page=await browser.newPage({viewport:{width:1536,height:1100}}),errors=[],network=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(/^https?:/.test(r.url()))network.push(r.url());});
 const started=Date.now();
 await page.goto(pathToFileURL(path.resolve(args.html)).href);
 const settle=()=>page.waitForFunction(()=>window.__reportState&&window.__reportState.id===sequence,{},{timeout:60000});
 await settle();
 const total=await page.evaluate(()=>D.total);assert.equal(await page.evaluate(()=>state.total),total);
 const loadMs=Date.now()-started;
 const initialCompute=await page.evaluate(()=>state.milliseconds);
 assert.equal(await page.locator('script[src],link[rel="stylesheet"]').count(),0);
 async function check(){
  assert(await page.evaluate(()=>{
   const match=(r,skip)=>selected.every((v,i)=>i===skip||v<0||r[i]===v);
   return state.total===D.records.filter(r=>match(r,-1)).reduce((n,r)=>n+r[7],0)&&
    facets.every((list,i)=>{const expected=new Map();for(const r of D.records)if(match(r,i))expected.set(r[i],(expected.get(r[i])||0)+r[7]);return list.length===expected.size&&list.every(([id,n])=>expected.get(id)===n);});
  }));
 }
 await check();
 for(let i=0;i<5;i++){
  await page.locator('#reset').click();await settle();
  const value=await page.evaluate(i=>D.labels[i][facets[i][0]?.[0]],i);
  if(value){await page.locator('#f'+i).fill(value);await page.locator('#f'+i).press('Tab');await settle();await check();}
 }
 await page.locator('#reset').click();await settle();
 if(total){
  await page.locator('#ranks button[data-dim="pair"]').first().click();await settle();await check();
  await page.locator('#reset').click();await settle();
  for(let i=0;i<5;i++){const value=await page.evaluate(i=>D.labels[i][facets[i][0][0]],i);await page.locator('#f'+i).fill(value);await page.locator('#f'+i).press('Tab');await settle();await check();}
  await page.locator('#f1').fill('');await page.locator('#f1').press('Tab');await settle();await check();
 }
 await page.locator('#reset').click();await settle();
 const day=await page.evaluate(()=>D.start);
 if(day){
  await page.locator('#from').fill(day);await page.locator('#to').fill(day);await page.locator('#unknown').uncheck();await settle();
  assert.equal(await page.evaluate(()=>state.total),await page.evaluate(day=>D.records.filter(r=>r[5]===day).reduce((s,r)=>s+r[7],0),day));
 }
 await page.locator('#reset').click();await settle();
 await page.locator('#hourFrom').selectOption('23');await page.locator('#hourTo').selectOption('2');await settle();
 assert.equal(await page.locator('#hourFrom').inputValue(),'1');
 await page.locator('#reset').click();await settle();
 await page.locator('#search').fill('nothing-matches-86fdcef');await page.waitForFunction(()=>state.total===0);
 assert((await page.locator('#rows').innerText()).includes('没有错误'));
 await page.locator('#reset').click();await settle();await check();
 if(await page.evaluate(()=>state.rows.length>10)){await page.locator('#next').click();assert((await page.locator('#pageNum').innerText()).startsWith('2'));await page.locator('#prev').click();}
 if(args.screenshots){fs.mkdirSync(args.screenshots,{recursive:true});await page.screenshot({path:path.join(args.screenshots,'desktop.png'),fullPage:true});}
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>scrollTo(0,0));
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 if(args.screenshots)await page.screenshot({path:path.join(args.screenshots,'mobile.png')});
 assert.deepEqual(errors,[]);assert.deepEqual(network,[]);
 console.log(JSON.stringify({passed:true,errors:total,load_ms:loadMs,initial_worker_ms:Math.round(initialCompute),buckets:await page.evaluate(()=>D.records.length),offline:true}));
}finally{await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
