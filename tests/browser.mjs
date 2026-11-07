import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import {spawn} from 'node:child_process';
const html=await fs.readFile(new URL('../web/index.html',import.meta.url));
const logo=await fs.readFile(new URL('../logo.png',import.meta.url));
const js=await fs.readFile(new URL('../web/app.js',import.meta.url));
const workspaceJS=await fs.readFile(new URL('../web/workspace.js',import.meta.url));
const css=await fs.readFile(new URL('../web/style.css',import.meta.url));
let settings={revision:0,poll_minutes:5,lead_minutes:30,reminder_hours:24,accounts:{'account:demo':{monitor:true,expiring:false,exhausted:false}}};
let failSave=false;
let deliveryConfig={revision:0,language:'zh-CN',cooldown_minutes:360,kinds:['slow_down','reset_soon'],smtp:{enabled:false,host:'',port:465,tls:'implicit',username:'',from:'',recipients:[]},webhook:{enabled:false}};
let mockPassword='';
const observedAt=Date.now(),resetAt=new Date(observedAt+86400000).toISOString();
const mockForecast={window:'primary',seconds:604800,remaining:65,observed_at:new Date(observedAt).toISOString(),reset_at:resetAt,deadline:resetAt,rate_per_hour:15,target_per_hour:65/24,known:true,kind:'slow_down',exhaust_at:new Date(observedAt+65/15*3600000).toISOString(),remaining_at_reset:0};
const mockHistory=Array.from({length:13},(_,i)=>({at:new Date(observedAt+(i-12)*5*60000).toISOString(),windows:[{name:'primary',seconds:604800,used:20+i*1.25,reset_at:resetAt}]}));

const server=http.createServer(async(req,res)=>{
 if(req.url==='/'){res.setHeader('Content-Type','text/html');res.end(html);return;}
 if(req.url.endsWith('/logo.png')){res.setHeader('Content-Type','image/png');res.end(logo);return;}
 if(req.url.endsWith('/app.js')){res.setHeader('Content-Type','text/javascript');res.end(js);return;}
 if(req.url.endsWith('/workspace.js')){res.setHeader('Content-Type','text/javascript');res.end(workspaceJS);return;}
 if(req.url.endsWith('/style.css')){res.setHeader('Content-Type','text/css');res.end(css);return;}
 res.setHeader('Content-Type','application/json');
 if(req.headers.authorization!=='Bearer demo'){res.writeHead(401);res.end('{"error":"unauthorized"}');return;}
 if(req.url.startsWith('/v0/management/cpa-codex-reset-manager/history')){res.end(JSON.stringify({account:'account:demo',points:mockHistory,forecasts:[mockForecast]}));return;}
 if(req.url.endsWith('/notifications')){
  if(req.method==='PUT'){let raw='';for await(const chunk of req)raw+=chunk;const next=JSON.parse(raw);mockPassword=next.smtp.password||mockPassword;delete next.smtp.password;deliveryConfig={...next,revision:deliveryConfig.revision+1};}
  res.end(JSON.stringify({config:deliveryConfig,password_set:!!mockPassword,webhook_set:false,deliveries:[]}));return;
 }
 if(req.url.endsWith('/status')){
  res.end(JSON.stringify({version:'0.4.0',settings,accounts:[{key:'account:demo',email:'demo@example.invalid'}],forecasts:{'account:demo':[mockForecast]},runtime:{accounts:{'account:demo':{current:{at:new Date().toISOString(),count:2,windows:[{name:'primary',seconds:604800,used:35,reset_at:new Date(Date.now()+86400000).toISOString()}],credits:[]}}},events:[{at:new Date().toISOString(),account:'account:demo',message:'Reset request: accepted'}]},notices:[{account:'account:demo',window:'primary',kind:'slow_down',message:'当前用量过快 / Slow down',remaining:65,rate_per_hour:2,target_per_hour:3,deadline:new Date(Date.now()+86400000).toISOString()}],data_dir:'/persistent/test',health:''}));return;
 }
 if(req.url.endsWith('/settings')){
  let raw='';for await(const chunk of req)raw+=chunk;
  const next=JSON.parse(raw);
  if(failSave||next.revision!==settings.revision){res.writeHead(409);res.end('{"error":"save rejected"}');return;}
  settings={...next,revision:settings.revision+1};res.end(JSON.stringify(settings));return;
 }
 res.end('{}');
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const profile=await fs.mkdtemp(path.join(os.tmpdir(),'reset-browser-'));
const chrome=spawn(process.env.CHROME||'/opt/google/chrome/chrome',['--headless=new','--no-sandbox','--disable-gpu','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{stdio:'ignore'});
let socket;
const pending=new Map();let sequence=0;
const wait=ms=>new Promise(r=>setTimeout(r,ms));
async function until(f){
 for(let n=0;n<100;n++){try{if(await f())return;}catch{}await wait(50);}
 throw Error('timed out waiting for browser');
}
function cdp(method,params={}){
 const id=++sequence;
 return new Promise((resolve,reject)=>{pending.set(id,{resolve,reject});socket.send(JSON.stringify({id,method,params}));});
}
async function evaluate(expression){
 const out=await cdp('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});
 if(out.exceptionDetails)throw Error(JSON.stringify(out.exceptionDetails));
 return out.result.value;
}
try{
 let port;
 await until(async()=>{port=Number((await fs.readFile(path.join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]);return port;});
 const pages=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json();
 socket=new WebSocket(pages.find(p=>p.type==='page'&&!p.url.startsWith('chrome-extension:')).webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
 socket.onmessage=e=>{const m=JSON.parse(e.data);if(m.id&&pending.has(m.id)){const p=pending.get(m.id);pending.delete(m.id);if(m.error)p.reject(m.error);else p.resolve(m.result);}};
 await cdp('Page.enable');
 await cdp('Page.navigate',{url:`http://127.0.0.1:${server.address().port}/`});
 await until(()=>evaluate("typeof renderWorkspace==='function'&&typeof load==='function'&&!!document.getElementById('connect').onsubmit"));
 await evaluate("document.getElementById('key').value='demo';document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("!document.getElementById('app').hidden"));
 assert.equal(await evaluate("document.getElementById('key').value"),'');
 assert.equal(await evaluate("sessionStorage.getItem('ccrm-key')"),'demo');
 assert.equal(await evaluate("localStorage.getItem('ccrm-persistent-key')"),null);
 assert.equal(await evaluate("document.querySelector('h1').textContent"),'Codex 额度管家');
 await until(()=>evaluate("document.querySelector('.brand img').naturalWidth>0"));
 await evaluate("document.getElementById('reset-settings').open=true;document.querySelector('[data-field=expiring]').click();document.getElementById('settings').requestSubmit()");
 await until(()=>settings.revision===1);
 assert.equal(settings.accounts['account:demo'].expiring,true);
 // Default tab-scoped memory survives a reload without remembering long-term.
 await cdp('Page.reload',{ignoreCache:true});
 await until(()=>evaluate("!document.getElementById('app').hidden&&!busy"));
 assert.equal(await evaluate("localStorage.getItem('ccrm-persistent-key')"),null);
 await evaluate("document.getElementById('forget').click()");
 assert.equal(await evaluate("sessionStorage.getItem('ccrm-key')"),null);
 assert.equal(await evaluate("document.getElementById('login').hidden"),false);
 // Only a successfully authenticated key may become persistent.
 await evaluate("document.getElementById('key').value='wrong';document.getElementById('rememberKey').checked=true;document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("!busy&&document.getElementById('feedback').dataset.code==='keyRejected'"));
 assert.equal(await evaluate("localStorage.getItem('ccrm-persistent-key')"),null);
 await evaluate("document.getElementById('key').value='demo';document.getElementById('rememberKey').checked=true;document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("localStorage.getItem('ccrm-persistent-key')==='demo'&&!busy"));
 assert.equal(await evaluate("sessionStorage.getItem('ccrm-key')"),null);
 // A genuinely new tab has no sessionStorage but reuses the opted-in key.
 const target=await cdp('Target.createTarget',{url:`http://127.0.0.1:${server.address().port}/`});
 const newPages=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json();
 const handler=socket.onmessage;
 socket.close();
 socket=new WebSocket(newPages.find(p=>p.id===target.targetId).webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject;});
 socket.onmessage=handler;
 await cdp('Page.enable');
 await until(()=>evaluate("!document.getElementById('app').hidden&&!busy"));
 assert.equal(await evaluate("sessionStorage.getItem('ccrm-key')"),null);
 assert.equal(await evaluate("document.getElementById('rememberKey').checked"),true);
 // A revoked saved key clears storage and displays the connection form.
 await evaluate("localStorage.setItem('ccrm-persistent-key','revoked')");
 await cdp('Page.reload',{ignoreCache:true});
 await until(()=>evaluate("!document.getElementById('login').hidden&&document.getElementById('feedback').dataset.code==='keyRejected'"));
 assert.equal(await evaluate("localStorage.getItem('ccrm-persistent-key')"),null);
 // Remembering can be disabled again without leaving a persistent copy.
 await evaluate("document.getElementById('key').value='demo';document.getElementById('rememberKey').checked=false;document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("!document.getElementById('app').hidden&&!busy"));
 assert.equal(await evaluate("sessionStorage.getItem('ccrm-key')"),'demo');
 assert.equal(await evaluate("localStorage.getItem('ccrm-persistent-key')"),null);
 await until(()=>evaluate("!busy"));
 // Blocking storage never prevents a memory-only authenticated session.
 await evaluate("document.getElementById('forget').click();window.originalStorageSetter=Storage.prototype.setItem;Storage.prototype.setItem=function(){throw new DOMException('blocked','SecurityError')};document.getElementById('key').value='demo';document.getElementById('rememberKey').checked=true;document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("!document.getElementById('app').hidden&&!busy&&document.getElementById('feedback').dataset.code==='memoryOnly'"));
 assert.equal(await evaluate("document.getElementById('rememberKey').checked"),false);
 await evaluate("Storage.prototype.setItem=window.originalStorageSetter");
 // An edit must retain its old revision even while status is refreshed.
 await evaluate("document.getElementById('poll').value='4';document.getElementById('poll').dispatchEvent(new Event('input',{bubbles:true}))");
 settings.revision=2;
 await evaluate('load(false)');
 await evaluate("document.getElementById('settings').requestSubmit()");
 await until(()=>evaluate("document.getElementById('feedback').textContent.includes('save rejected')"));
 assert.equal(settings.poll_minutes,5);
 await evaluate('dirty=false;load(false)');
 failSave=true;
 await evaluate("document.getElementById('reset-settings').open=true;document.querySelector('[data-field=expiring]').click();document.getElementById('settings').requestSubmit()");
 await until(()=>evaluate("document.getElementById('feedback').textContent.includes('save rejected')"));
 assert.equal(settings.accounts['account:demo'].expiring,true);
 // Real curves and remote settings are exercised against this local mock only.
 await until(()=>evaluate("!!document.querySelector('#quota-chart svg .actual-line')&&!!deliveryState?.config"));
 assert.equal(await evaluate("document.querySelectorAll('#quota-summary .metric').length"),3);
 await evaluate("window.originalHistory=historyData;historyData=JSON.parse(JSON.stringify(historyData));historyData.points.slice(0,6).forEach(p=>p.windows[0].reset_at='2026-01-01T00:00:00Z');drawChart()");
 assert.equal(await evaluate("document.querySelectorAll('#quota-chart .actual-line').length"),2);
 await evaluate("historyData=window.originalHistory;drawChart()");
 await evaluate("document.querySelector('#quota-chart svg').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true}))");
 assert.equal(await evaluate("document.getElementById('chart-inspection').textContent.includes('实测')"),true);
 await evaluate("document.getElementById('delivery-panel').open=true;document.getElementById('smtp-host').value='smtp.example.invalid';document.getElementById('smtp-user').value='sender';document.getElementById('smtp-from').value='sender@example.invalid';document.getElementById('smtp-recipients').value='one@example.invalid,two@example.invalid';document.getElementById('smtp-password').value='mock-smtp-secret';document.getElementById('delivery-form').requestSubmit()");
 await until(()=>deliveryConfig.revision===1);
 assert.deepEqual(deliveryConfig.smtp.recipients,['one@example.invalid','two@example.invalid']);
 assert.equal(mockPassword,'mock-smtp-secret');
 await until(()=>evaluate("!deliveryLoading"));
 assert.equal(await evaluate("document.getElementById('smtp-password').value"),'');
 assert.equal(await evaluate("JSON.stringify(deliveryState).includes('mock-smtp-secret')"),false);
 await evaluate("document.getElementById('delivery-panel').open=false");
 // Theme and language changes preserve an unsaved form draft.
 await evaluate("document.getElementById('poll').value='6';document.getElementById('poll').dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('[data-language=en]').click()");
 assert.equal(await evaluate("document.documentElement.lang"),'en');
 assert.equal(await evaluate("document.getElementById('poll').value"),'6');
 assert.equal(await evaluate("/[\\u3400-\\u9fff]/.test(document.body.innerText.replace('中文',''))"),false);
 assert.equal(await evaluate("document.querySelector('h1').textContent"),'Codex Quota Steward');
 await evaluate("document.querySelector('[data-theme-mode=light]').click()");
 assert.equal(await evaluate("getComputedStyle(document.body).backgroundColor"),'rgb(243, 246, 250)');
 await cdp('Emulation.setEmulatedMedia',{features:[{name:'prefers-color-scheme',value:'dark'}]});
 assert.equal(await evaluate("document.documentElement.dataset.theme"),'light');
 await evaluate("document.querySelector('[data-theme-mode=auto]').click()");
 assert.equal(await evaluate("document.documentElement.dataset.theme"),'dark');
 await cdp('Emulation.setEmulatedMedia',{features:[{name:'prefers-color-scheme',value:'light'}]});
 await until(()=>evaluate("document.documentElement.dataset.theme==='light'"));
 await evaluate("document.querySelector('[data-theme-mode=dark]').click();dirty=false");
 // Restore the valid session after the intentionally blocked-storage case.
 await evaluate("sessionStorage.setItem('ccrm-key','demo')");
 await cdp('Page.reload',{ignoreCache:true});
 await until(()=>evaluate("!document.getElementById('app').hidden&&!busy"));
 assert.equal(await evaluate("document.documentElement.lang"),'en');
 assert.equal(await evaluate("document.documentElement.dataset.theme"),'dark');
 assert.equal(await evaluate("getComputedStyle(document.body).backgroundColor"),'rgb(16, 21, 27)');
 await evaluate("feedback('')");
 await evaluate(`window.auditContrast=()=>{
 const rgb=s=>{const m=s.match(/[\\d.]+/g)?.map(Number);return m&&m.length>=3?m:null;};
 const luminance=c=>c.slice(0,3).map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4;}).reduce((v,x,i)=>v+x*[.2126,.7152,.0722][i],0);
 const bad=[];for(const e of document.querySelectorAll('h1,h2,h3,p,small,label,span,button,summary,legend,select,input,textarea,svg text')){
  if(!e.getClientRects().length||!e.textContent.trim()&&!['INPUT','SELECT','TEXTAREA'].includes(e.tagName))continue;
  const st=getComputedStyle(e);let fg=rgb(e.tagName.toLowerCase()==='text'?st.fill:st.color);if(!fg)continue;let node=e,bg=null;
  while(node){const c=rgb(getComputedStyle(node).backgroundColor);if(c&&(c.length<4||c[3]>=.99)){bg=c;break;}node=node.parentElement;}
  if(!bg)bg=rgb(getComputedStyle(document.body).backgroundColor);const a=luminance(fg),b=luminance(bg),ratio=(Math.max(a,b)+.05)/(Math.min(a,b)+.05);
  if(ratio<4.5)bad.push({text:e.textContent.slice(0,40),ratio,color:st.color});
 }return bad;
};`);
 await evaluate("document.querySelectorAll('details').forEach(n=>n.open=true)");
 assert.deepEqual(await evaluate('auditContrast()'),[]);
 await evaluate("document.querySelectorAll('details').forEach(n=>n.open=false)");

 await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
 assert.equal(await evaluate('document.documentElement.scrollWidth<=window.innerWidth'),true);
 await fs.mkdir(new URL('../output/',import.meta.url),{recursive:true});
 const screenshot=await cdp('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});
 await fs.writeFile(new URL('../output/dashboard-mobile.png',import.meta.url),Buffer.from(screenshot.data,'base64'));
 await evaluate("document.querySelector('[data-language=\"zh-CN\"]').click();document.querySelector('[data-theme-mode=light]').click()");
 assert.equal(await evaluate('document.documentElement.scrollWidth<=window.innerWidth'),true);
 await evaluate("document.querySelectorAll('details').forEach(n=>n.open=true)");
 assert.deepEqual(await evaluate('auditContrast()'),[]);
 await evaluate("document.querySelectorAll('details').forEach(n=>n.open=false)");
 const lightShot=await cdp('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});
 await fs.writeFile(new URL('../output/dashboard-light-zh.png',import.meta.url),Buffer.from(lightShot.data,'base64'));
 console.log('Browser passed: quota curves/reset breaks, multi-recipient settings, write-only passwords, contrast, light/dark/auto themes, single-language UI, preference persistence, draft preservation, icon, authentication, session/persistent memory, new tab, forget/revoked keys, save conflicts, failed save, mobile overflow');
}catch(error){
 if(socket?.readyState===1)console.error(await evaluate("({url:location.href,text:document.body?.innerText?.slice(0,500)})"));
 throw error;
}finally{
 socket?.close();chrome.kill('SIGTERM');server.close();
 await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const t=setTimeout(()=>{chrome.kill('SIGKILL');resolve();},3000);chrome.once('exit',()=>{clearTimeout(t);resolve();});});
 await fs.rm(profile,{recursive:true,force:true,maxRetries:10,retryDelay:100});
}
