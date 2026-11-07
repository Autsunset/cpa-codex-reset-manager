'use strict';
const base='/v0/management/cpa-codex-reset-manager';
const $=id=>document.getElementById(id);
const messages={
 product:['Codex 额度管家','Codex Quota Steward'],subtitle:['看清余量，把握重置时机','Know your quota. Plan your next reset.'],
 languageLabel:['界面语言','Language'],themeLabel:['外观主题','Appearance'],themeAuto:['自动','Auto'],themeLight:['浅色','Light'],themeDark:['深色','Dark'],
 connectTitle:['连接额度管家','Connect to Quota Steward'],connectDescription:['输入 CPA 管理密钥，查看额度与重置设置。','Enter your CPA Management Key to view quota and reset settings.'],managementKey:['管理密钥','Management Key'],rememberKey:['在此浏览器记住密钥','Remember key in this browser'],rememberHint:['共享设备请勿勾选；未勾选时只保留在当前标签页。','Use only on a trusted device. Otherwise the key stays in this tab’s session.'],connect:['连接','Connect'],forget:['忘记密钥','Forget key'],
 settingsTitle:['持久设置','Persistent settings'],policyHint:['新账号默认关闭。周额度耗尽时可兑换一张，每七天最多一次；到期兑换独立启用。兑换会重置额度窗口。','New accounts default to off. Weekly exhaustion allows one redemption per seven days; expiring credits are a separate policy. Redemption resets quota windows.'],pollLabel:['巡检间隔（分钟）','Poll interval (minutes)'],leadLabel:['到期前兑换（分钟）','Redeem before expiry (minutes)'],reminderLabel:['提前提醒（小时）','Reminder horizon (hours)'],scheduleHint:['兑换提前量至少为巡检间隔的两倍。提醒依赖实测额度变化，不会生成模型请求。','The expiry lead must be at least twice the poll interval. Reminders use observed quota changes and do not generate model requests.'],save:['保存设置','Save settings'],refresh:['刷新','Refresh'],check:['立即巡检','Check now'],notify:['启用浏览器提醒','Enable notifications'],
 noticesTitle:['当前提醒','Pace reminders'],notificationHint:['浏览器通知需要此页面保持打开；后台兑换不依赖页面。','Keep this page open for browser notifications. Server-side resets run independently.'],accountsTitle:['账号状态','Accounts'],activityTitle:['操作记录','Activity'],monitor:['监控与提醒','Monitor and remind'],expiring:['到期前兑换','Redeem before expiry'],exhausted:['周额度耗尽兑换','Redeem when weekly quota is exhausted'],accountMissing:['账号暂不可见，设置仍然保留。','Account currently unavailable; its saved policy is retained.'],
 savedRevision:['已保存版本','Saved revision'],remaining:['剩余','Remaining'],currentRate:['当前速度','Current pace'],targetRate:['可用速度','Budget pace'],perHour:['%/小时','%/h'],sample:['采样时间','Last sample'],credits:['重置券','Reset credits'],used:['已用','used'],reset:['重置时间','Resets at'],creditExpiry:['重置券到期','Credit expires'],hours:['小时','hours'],weekly:['周额度','Weekly quota'],fiveHour:['五小时额度','Five-hour quota'],primary:['主额度','Primary quota'],secondary:['次级额度','Secondary quota'],
 noticeEmpty:['启用账号监控后开始采样。','Enable monitoring to start collecting samples.'],stale:['额度数据过期或尚未采样，暂不判断用量速度。','Quota data is stale or missing; pace estimates are paused.'],slowNear:['额度接近耗尽，请减缓速度。','Quota is nearly exhausted. Slow down.'],slowPace:['当前速度可能在重置前耗尽额度，请适当减速。','Your current pace may exhaust quota before reset. Consider slowing down.'],speed_up:['重置临近，按当前速度将留下未用额度，可以加快有价值任务。','Reset is approaching. Your current pace may leave quota unused; there is room to do more useful work.'],constrained:['其他额度窗口受限，请优先减速。','Another quota window is constrained. Prioritize slowing down.'],learning:['等待同周期的第二个有效样本。','Waiting for a second valid sample in this quota window.'],balanced:['当前速度基本合适。','Your current pace is within budget.'],
 redemptionState:['兑换状态','Redemption status'],pending:['等待确认','Pending'],accepted:['请求已接受','Accepted'],blocked:['已暂停，待核验','Paused for review'],rejected:['请求被拒绝','Rejected'],verified:['已验证','Verified'],resolve:['已人工核对，解除挂起','Resolve after manual review'],resolveConfirm:['已核对上游兑换记录？此操作将关闭该账号自动兑换。','Have you reviewed the upstream redemption history? This disables automatic redemption for this account.'],eventRequest:['兑换请求：','Redemption request: '],eventVerified:['重置已验证：','Reset verified: '],eventResolved:['已人工核对待确认请求，自动兑换已关闭。','Unresolved request reviewed by operator; automatic redemption disabled.'],weekly_exhausted:['周额度耗尽','weekly exhaustion'],
 persistentUnavailable:['无法长期保存密钥，已改为仅当前标签页。','Persistent storage unavailable; using this tab only.'],storagePermissions:['浏览器禁止访问长期存储，请检查网站存储权限。','Check this site’s storage permissions.'],memoryOnly:['浏览器禁止存储，密钥仅保留在当前页面。','Storage unavailable; key kept in memory only.'],clearStorageHint:['浏览器禁止访问存储，请在浏览器设置中清除此站点数据。','Storage access is blocked. Clear this site’s data in browser settings.'],keyRejected:['密钥无效或已失效，请重新连接。','Key rejected or expired. Please reconnect.'],keyRemoved:['已清除此插件保存的密钥。','This plugin’s saved key has been removed.'],forgetConfirm:['忘记密钥并放弃未保存修改？','Forget the key and discard unsaved changes?'],refreshConfirm:['放弃未保存修改并重新读取？','Discard unsaved edits and refresh?'],refreshed:['已刷新','Refreshed'],saved:['设置已持久保存','Settings saved to disk'],queued:['巡检已排队','Check queued'],notificationsUnavailable:['浏览器不支持通知。','Notifications are unavailable in this browser.'],notificationsEnabled:['提醒已启用，请保持页面打开。','Notifications enabled. Keep this page open.'],notificationsDenied:['通知未获授权。','Notification permission denied.'],requestFailed:['请求失败','Request failed'],saveConflict:['设置已被其他页面修改，请刷新后重试。','Settings changed in another page. Refresh before saving.']
};
function readPreference(name,fallback){try{return localStorage.getItem(name)||fallback;}catch{return fallback;}}
let language=readPreference('ccrm-language','zh-CN');if(!['zh-CN','en'].includes(language))language='zh-CN';
let themeMode=readPreference('ccrm-theme','auto');if(!['auto','light','dark'].includes(themeMode))themeMode='auto';
const colorPreference=matchMedia('(prefers-color-scheme: dark)');
let lastFeedback={code:'',detail:''};
function t(name){return messages[name]?.[language==='en'?1:0]??name;}
function storePreference(name,value){try{localStorage.setItem(name,value);}catch{}}
function panelTheme(){
 try{if(parent!==window){const root=parent.document.documentElement;const value=root.dataset.theme||root.getAttribute('data-bs-theme');if(['light','white'].includes(value)||root.classList.contains('theme-light')||root.classList.contains('light'))return 'light';if(['dark','black'].includes(value)||root.classList.contains('theme-dark')||root.classList.contains('dark'))return 'dark';}}catch{}
 return colorPreference.matches?'dark':'light';
}
function applyTheme(){document.documentElement.dataset.theme=themeMode==='auto'?panelTheme():themeMode;document.querySelectorAll('[data-theme-mode]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.themeMode===themeMode)));}
function applyLanguage(){
 document.documentElement.lang=language;document.title=t('product');
 document.querySelectorAll('[data-i18n]').forEach(n=>n.textContent=t(n.dataset.i18n));
 document.querySelectorAll('[data-i18n-aria]').forEach(n=>n.setAttribute('aria-label',t(n.dataset.i18nAria)));
 document.querySelectorAll('[data-language]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.language===language)));
 if(state)render(false);paintFeedback();
}
function serverText(value){
 const text=String(value||'');
 if(text.startsWith('Reset request: '))return t('eventRequest')+t(text.slice(15));
 if(text.includes('Reset verified: ')){const detail=text.split('Reset verified: ')[1];const split=detail.indexOf(';');return t('eventVerified')+t(split<0?detail:detail.slice(0,split))+(split<0?'':detail.slice(split));}
 if(text.startsWith('Operator acknowledged unresolved attempt;'))return t('eventResolved');
 if(text==='settings changed elsewhere; refresh before saving')return t('saveConflict');
 const split=text.indexOf(' / ');return split<0?text:(language==='en'?text.slice(split+3):text.slice(0,split));
}
function noticeText(n){return t(n.kind==='slow_down'?(n.remaining<=1?'slowNear':'slowPace'):n.kind);}
function windowLabel(w){if(typeof w==='string')return t(w);if(w.seconds>=600000&&w.seconds<=610000)return t('weekly');if(w.seconds===18000)return t('fiveHour');return t(w.name);}
function paintFeedback(){$('feedback').dataset.code=lastFeedback.code;$('feedback').textContent=lastFeedback.code?t(lastFeedback.code)+(lastFeedback.detail?' · '+serverText(lastFeedback.detail):''):'';}

const sessionKeyStorageKey='ccrm-key';
const persistentKeyStorageKey='ccrm-persistent-key';
function readStoredAuth(){
 try{const saved=localStorage.getItem(persistentKeyStorageKey);if(saved)return {key:saved,persistent:true};}catch{}
 try{return {key:sessionStorage.getItem(sessionKeyStorageKey)||'',persistent:false};}catch{return {key:'',persistent:false};}
}
const storedAuth=readStoredAuth();
let key=storedAuth.key,state=null,dirty=false,busy=false,notifications=false,formRevision=0,authGeneration=0;
$('rememberKey').checked=storedAuth.persistent;
const sent=new Map();
function el(tag,text,cls){const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n;}
function clearStoredAuth(){
 let cleared=true;
 try{localStorage.removeItem(persistentKeyStorageKey);}catch{cleared=false;}
 try{sessionStorage.removeItem(sessionKeyStorageKey);}catch{cleared=false;}
 return cleared;
}
function rememberAuth(){
 let warning='';
 if($('rememberKey').checked){
  try{localStorage.setItem(persistentKeyStorageKey,key);try{sessionStorage.removeItem(sessionKeyStorageKey);}catch{}return '';}
  catch{$('rememberKey').checked=false;warning='persistentUnavailable';}
 }
 try{localStorage.removeItem(persistentKeyStorageKey);}catch{warning='storagePermissions';}
 try{sessionStorage.setItem(sessionKeyStorageKey,key);return warning;}
 catch{return 'memoryOnly';}
}
function disconnect(message){
 authGeneration++;const cleared=clearStoredAuth();key='';state=null;dirty=false;busy=false;notifications=false;sent.clear();
 $('app').hidden=true;$('login').hidden=false;$('forget').hidden=true;$('key').value='';$('rememberKey').checked=false;$('version').textContent='';
 for(const id of ['policies','notices','accounts','events'])$(id).replaceChildren();
 feedback(cleared?message:'clearStorageHint');$('key').focus();
}
async function api(path,method='GET',body){
 const generation=authGeneration;
 const r=await fetch(base+path,{method,headers:{Authorization:'Bearer '+key,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),cache:'no-store'});
 if(generation!==authGeneration)throw Object.assign(Error('Connection changed'),{stale:true});
 if(r.status===401||r.status===403){disconnect('keyRejected');throw Object.assign(Error('Key rejected'),{stale:true});}
 const data=await r.json();
 if(generation!==authGeneration)throw Object.assign(Error('Connection changed'),{stale:true});
 if(!r.ok)throw Error(data.error||String(r.status));return data;
}
function feedback(code,detail=''){lastFeedback={code,detail};paintFeedback();}
function labelAccount(id){const a=(state.accounts||[]).find(a=>a.key===id);return a?(a.email||a.name||a.id):id;}
function time(v){if(!v||v.startsWith('0001'))return '—';return new Date(v).toLocaleString(language==='en'?'en-US':'zh-CN');}
function policies(){
 formRevision=state.settings.revision;
 $('poll').value=state.settings.poll_minutes;$('lead').value=state.settings.lead_minutes;$('remind').value=state.settings.reminder_hours;
 $('policies').replaceChildren();
 const ids=new Set([...Object.keys(state.settings.accounts),...(state.accounts||[]).map(a=>a.key)]);
 for(const id of ids){
  const row=el('div',undefined,'card account');row.dataset.account=id;
  row.append(el('strong',labelAccount(id)));
  if(!(state.accounts||[]).some(a=>a.key===id)){const note=el('p',t('accountMissing'),'muted');note.dataset.i18n='accountMissing';row.append(note);}
  const p=state.settings.accounts[id]||{};
  for(const [field,text] of [['monitor','monitor'],['expiring','expiring'],['exhausted','exhausted']]){
   const l=el('label'),input=el('input');input.type='checkbox';input.dataset.field=field;input.checked=!!p[field];const span=el('span',t(text));span.dataset.i18n=text;l.append(input,span);row.append(l);
   input.addEventListener('change',()=>{dirty=true;if(field!=='monitor'&&input.checked)row.querySelector('[data-field=monitor]').checked=true;if(field==='monitor'&&!input.checked)row.querySelectorAll('input').forEach(i=>i.checked=false);});
  }
  $('policies').append(row);
 }
}
function render(sendNotifications=true){
 $('app').hidden=false;$('login').hidden=true;$('forget').hidden=false;$('version').textContent='v'+state.version;
 $('storage').textContent=t('savedRevision')+' '+state.settings.revision+' · '+state.data_dir;
 if(!dirty)policies();
 $('notices').replaceChildren();
 for(const n of state.notices){
  const box=el('div',undefined,'notice '+n.kind);
  box.append(el('strong',labelAccount(n.account)+' · '+(n.window?windowLabel(n.window):'')),el('p',noticeText(n)));
  if(n.deadline&&!n.deadline.startsWith('0001'))box.append(el('small',t('remaining')+' '+n.remaining.toFixed(1)+'% · '+t('currentRate')+' '+n.rate_per_hour.toFixed(2)+t('perHour')+' · '+t('targetRate')+' '+n.target_per_hour.toFixed(2)+t('perHour')+' · '+time(n.deadline)));
  $('notices').append(box);
  const stamp=n.account+'|'+n.window+'|'+n.kind+'|'+n.deadline;
  if(sendNotifications&&notifications&&['slow_down','speed_up','stale'].includes(n.kind)&&(!sent.has(stamp)||Date.now()-sent.get(stamp)>3600000)){new Notification('Codex · '+labelAccount(n.account),{body:noticeText(n),tag:n.account+'|'+n.window});sent.set(stamp,Date.now());}
 }
 if(!state.notices.length)$('notices').append(el('p',t('noticeEmpty'),'muted'));
 $('accounts').replaceChildren();
 for(const [id,r] of Object.entries(state.runtime.accounts)){
  const card=el('div',undefined,'card account');card.append(el('strong',labelAccount(id)),el('p',t('sample')+': '+time(r.current.at)+' · '+t('credits')+': '+r.current.count));
  for(const w of r.current.windows||[]){const box=el('div',undefined,'window');box.append(el('p',windowLabel(w)+' · '+(w.seconds/3600)+' '+t('hours')+' · '+w.used.toFixed(1)+'% '+t('used')));const bar=el('progress');bar.max=100;bar.value=w.used;box.append(bar,el('small',t('reset')+': '+time(w.reset_at)));card.append(box);}
  for(const c of r.current.credits||[])card.append(el('p',t('creditExpiry')+': '+time(c.expires_at)));
  if(r.error)card.append(el('p',serverText(r.error),'status'));
  if(r.attempt){card.append(el('p',t('redemptionState')+': '+t(r.attempt.status)));const button=el('button',t('resolve'));button.type='button';button.onclick=async()=>{if(!confirm(t('resolveConfirm')))return;try{await api('/resolve','POST',{account:id,request_id:r.attempt.request_id});await load(false);}catch(e){feedback('requestFailed',e.message);}};card.append(button);}
  $('accounts').append(card);
 }
 $('events').replaceChildren();for(const e of [...(state.runtime.events||[])].reverse().slice(0,40))$('events').append(el('p',time(e.at)+' · '+labelAccount(e.account)+' · '+serverText(e.message)));
 if(state.health)feedback('requestFailed',state.health);
}
async function load(manual=true){
 if(busy)return false;busy=true;const generation=authGeneration;
 try{const data=await api('/status');state=data;render();if(manual&&!state.health)feedback('refreshed');return true;}
 catch(e){if(!e.stale&&generation===authGeneration)feedback('requestFailed',e.message);return false;}finally{if(generation===authGeneration)busy=false;}
}
$('connect').onsubmit=async e=>{e.preventDefault();if(busy)return;authGeneration++;key=$('key').value.trim();$('key').value='';if(await load()){const warning=rememberAuth();if(warning)feedback(warning);}};
$('forget').onclick=()=>{if(dirty&&!confirm(t('forgetConfirm')))return;disconnect('keyRemoved');};
$('settings').addEventListener('input',()=>dirty=true);
$('settings').onsubmit=async e=>{
 e.preventDefault();if(busy||!state)return;
 const generation=authGeneration;
 const next={...state.settings,revision:formRevision,poll_minutes:Number($('poll').value),lead_minutes:Number($('lead').value),reminder_hours:Number($('remind').value),accounts:{}};
 for(const row of $('policies').children){const p={};for(const i of row.querySelectorAll('input'))p[i.dataset.field]=i.checked;next.accounts[row.dataset.account]=p;}
 busy=true;$('save').disabled=true;
 try{state.settings=await api('/settings','PUT',next);dirty=false;feedback('saved');}
 catch(e){if(!e.stale)feedback('requestFailed',e.message);}finally{if(generation===authGeneration)busy=false;$('save').disabled=false;}if(key&&generation===authGeneration)await load(false);
};
$('refresh').onclick=()=>{if(dirty&&!confirm(t('refreshConfirm')))return;dirty=false;load();};
$('check').onclick=async()=>{try{await api('/check','POST',{});feedback('queued');}catch(e){feedback('requestFailed',e.message);}};
$('notify').onclick=async()=>{if(!('Notification'in window)){feedback('notificationsUnavailable');return;}notifications=(await Notification.requestPermission())==='granted';feedback(notifications?'notificationsEnabled':'notificationsDenied');};
setInterval(()=>{if(key&&state)load(false);},30000);
document.querySelectorAll('[data-language]').forEach(b=>b.onclick=()=>{language=b.dataset.language;storePreference('ccrm-language',language);applyLanguage();});
document.querySelectorAll('[data-theme-mode]').forEach(b=>b.onclick=()=>{themeMode=b.dataset.themeMode;storePreference('ccrm-theme',themeMode);applyTheme();});
colorPreference.addEventListener('change',applyTheme);
try{if(parent!==window)new MutationObserver(()=>{if(themeMode==='auto')applyTheme();}).observe(parent.document.documentElement,{attributes:true,attributeFilter:['class','data-theme','data-bs-theme']});}catch{}
window.addEventListener('storage',e=>{if(e.key==='ccrm-theme'){themeMode=['light','dark'].includes(e.newValue)?e.newValue:'auto';applyTheme();}if(e.key==='ccrm-language'){language=e.newValue==='en'?'en':'zh-CN';applyLanguage();}});
applyTheme();applyLanguage();
if(key)load(false);
