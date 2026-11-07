import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import {spawn} from 'node:child_process';
const html=await fs.readFile(new URL('../web/index.html',import.meta.url));
let settings={revision:0,poll_minutes:5,lead_minutes:30,reminder_hours:24,accounts:{'account:demo':{monitor:true,expiring:false,exhausted:false}}};
let failSave=false;
const server=http.createServer(async(req,res)=>{
 if(req.url==='/'){res.setHeader('Content-Type','text/html');res.end(html);return;}
 res.setHeader('Content-Type','application/json');
 if(req.headers.authorization!=='Bearer demo'){res.writeHead(401);res.end('{"error":"unauthorized"}');return;}
 if(req.url.endsWith('/status')){
  res.end(JSON.stringify({version:'0.1.1',settings,accounts:[{key:'account:demo',email:'demo@example.invalid'}],runtime:{accounts:{},events:[]},notices:[],data_dir:'/persistent/test',health:''}));return;
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
 await until(()=>evaluate("!!document.getElementById('connect')"));
 await evaluate("document.getElementById('key').value='demo';document.getElementById('connect').requestSubmit()");
 await until(()=>evaluate("!document.getElementById('app').hidden"));
 assert.equal(await evaluate("document.getElementById('key').value"),'');
 await evaluate("document.querySelector('[data-field=expiring]').click();document.getElementById('settings').requestSubmit()");
 await until(()=>settings.revision===1);
 assert.equal(settings.accounts['account:demo'].expiring,true);
 await until(()=>evaluate("!busy"));
 // An edit must retain its old revision even while status is refreshed.
 await evaluate("document.getElementById('poll').value='4';document.getElementById('poll').dispatchEvent(new Event('input',{bubbles:true}))");
 settings.revision=2;
 await evaluate('load(false)');
 await evaluate("document.getElementById('settings').requestSubmit()");
 await until(()=>evaluate("document.getElementById('feedback').textContent.includes('save rejected')"));
 assert.equal(settings.poll_minutes,5);
 await evaluate('dirty=false;load(false)');
 failSave=true;
 await evaluate("document.querySelector('[data-field=expiring]').click();document.getElementById('settings').requestSubmit()");
 await until(()=>evaluate("document.getElementById('feedback').textContent.includes('save rejected')"));
 assert.equal(settings.accounts['account:demo'].expiring,true);
 await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
 assert.equal(await evaluate('document.documentElement.scrollWidth<=window.innerWidth'),true);
 await fs.mkdir(new URL('../output/',import.meta.url),{recursive:true});
 const screenshot=await cdp('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});
 await fs.writeFile(new URL('../output/dashboard-mobile.png',import.meta.url),Buffer.from(screenshot.data,'base64'));
 console.log('Browser passed: authentication, save, stale revisions, failed save, mobile overflow');
}catch(error){
 if(socket?.readyState===1)console.error(await evaluate("({url:location.href,text:document.body?.innerText?.slice(0,500)})"));
 throw error;
}finally{
 socket?.close();chrome.kill('SIGTERM');server.close();
 await new Promise(resolve=>{if(chrome.exitCode!==null)return resolve();const t=setTimeout(()=>{chrome.kill('SIGKILL');resolve();},3000);chrome.once('exit',()=>{clearTimeout(t);resolve();});});
 await fs.rm(profile,{recursive:true,force:true,maxRetries:10,retryDelay:100});
}
