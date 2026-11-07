'use strict';
Object.assign(messages,{
 overviewTitle:['额度总览','Quota overview'],overviewHint:['先看余量，再决定使用速度。','See what remains, then choose your pace.'],accountSelect:['账号','Account'],windowSelect:['额度窗口','Quota window'],curveTitle:['余量走势与预测','Remaining quota and forecast'],actualLine:['实测余量','Observed remaining'],forecastLine:['按近期速度预测','Recent-pace projection'],budgetLine:['均匀用到重置','Budget through reset'],timeToReset:['距离重置','Until reset'],estimatedExhaustion:['预计耗尽','Estimated exhaustion'],unusedAtReset:['预计重置时剩余','Projected unused at reset'],currentRemaining:['当前余量','Remaining quota'],noConsumption:['暂未观察到消耗','No recent consumption'],needSamples:['等待更多样本','Waiting for more samples'],noQuotaData:['启用账号监控后，这里会显示实测曲线。','Enable account monitoring to start the observed curve.'],chartLoading:['正在读取历史采样…','Loading quota history…'],chartUnavailable:['历史数据暂不可用，请稍后刷新。','History is unavailable. Refresh to try again.'],historyNote:['仅显示实际采样；重置或采样中断处断线。虚线是按近一小时内有效样本推算的场景，不是保证值。','Only real observations are plotted. Lines break at resets or collection gaps. Dashed lines project the valid samples from up to the last hour; they are not guarantees.'],historyWarmup:['更新前只保存了最近两个读数，较早历史无法补造；新的采样会持续积累。','Earlier versions kept only two readings. Older history cannot be reconstructed; new observations accumulate from now on.'],chartInspect:['悬停、触摸或用左右方向键查看采样点。','Hover, touch, or use the arrow keys to inspect samples.'],chartObserved:['实测','Observed'],chartProjected:['预测','Projected'],overdue:['已到预计时间','Estimated time reached'],timeDay:['天','d'],timeHour:['小时','h'],timeMinute:['分钟','min'],sampleCount:['采样点','samples'],rateBasis:['按近期实测速度估计','Based on recent observed pace'],budgetBasis:['按当前余量均匀消耗','Evenly spending the observed balance'],remoteTitle:['服务端通知','Server-side notifications'],remoteHint:['关闭浏览器也能收到提醒。勾选启用并保存后，会发送符合条件的正式提醒；不会发送测试邮件。','Receive alerts with the browser closed. Enabling a channel and saving starts real alerts when conditions match; no test messages are sent.'],deliveryLanguage:['通知语言','Notification language'],cooldownLabel:['同类提醒间隔（分钟）','Same-alert cooldown (minutes)'],emailEnabled:['启用邮件通知','Enable email notifications'],smtpHost:['SMTP 服务器','SMTP host'],smtpPort:['端口','Port'],smtpTLS:['加密方式','Encryption'],smtpUser:['登录账号','SMTP username'],smtpPassword:['SMTP 密码或授权码','SMTP password / app password'],smtpFrom:['发件邮箱','Sender email'],recipientsLabel:['收件邮箱（每行一个或逗号分隔）','Recipients (one per line or comma-separated)'],recipientsHint:['最多 50 位收件人，分别投递，不公开其他收件人的地址。仅重试失败的收件人。','Up to 50 recipients, delivered individually without exposing other addresses. Only failed recipients are retried.'],clearPassword:['清除已保存的 SMTP 密码','Clear saved SMTP password'],webhookEnabled:['启用通用 Webhook','Enable generic webhook'],webhookURL:['HTTPS Webhook 地址','HTTPS webhook URL'],webhookHint:['向接收端发送包含 title、text、notice、id 的 JSON，可接入自己的通知自动化。','Sends JSON with title, text, notice and id to your receiver or notification automation.'],clearWebhook:['清除已保存的 Webhook 地址','Clear saved webhook URL'],saveDelivery:['保存通知设置','Save notification settings'],deliveryHistory:['发送记录','Delivery history'],secretSaved:['已保存，留空可保留；不会回显。','Saved. Leave blank to keep it; never returned to the browser.'],secretEmpty:['尚未保存','Not configured'],deliverySaved:['通知设置已保存','Notification settings saved'],deliveryEmpty:['暂无发送记录。未启用的渠道不会发送任何消息。','No deliveries yet. Disabled channels do not send messages.'],sent:['已发送','Sent'],failed:['发送失败','Failed'],sending:['发送中或结果待确认','Sending or awaiting confirmation'],unknown:['结果待确认','Outcome unknown'],smtp:['邮件','Email'],webhook:['Webhook','Webhook'],reset_soon:['额度即将重置','Quota resets soon'],credit_expiring:['重置券即将到期','Credit expires soon'],alertSlow:['用量过快','Usage too fast'],alertFast:['额度可能用不完','Quota may remain unused'],alertStale:['额度数据过期','Stale quota data']
});
function kindLabel(kind){return t({slow_down:'alertSlow',speed_up:'alertFast',stale:'alertStale'}[kind]||kind);}
let chartAccount=readPreference('ccrm-chart-account',''),chartWindow='primary',chartHours=Number(readPreference('ccrm-chart-hours','24'));
if(![6,24,168,336].includes(chartHours))chartHours=24;
let historyData=null,historyKey='',historyLoaded=0,historySequence=0,historyPending='',historyError=false;
let deliveryState=null,deliveryDirty=false,deliveryLoading=false,deliveryStatus='',deliveryDetail='',deliveryLoaded=0;
function relativeTime(value){
 const ms=new Date(value).getTime()-Date.now();if(!Number.isFinite(ms))return '—';if(ms<=0)return t('overdue');
 const minutes=Math.ceil(ms/60000),days=Math.floor(minutes/1440),hours=Math.floor(minutes%1440/60),mins=minutes%60;
 return (days?days+' '+t('timeDay')+' ':'')+(hours?hours+' '+t('timeHour')+' ':'')+(!days?mins+' '+t('timeMinute'):'');
}
function currentForecast(){return (state?.forecasts?.[chartAccount]||historyData?.forecasts||[]).find(f=>f.window===chartWindow);}
function metric(label,value,note){const box=el('div',undefined,'metric');box.append(el('div',t(label),'metric-label'),el('div',value,'metric-value'),el('div',note,'metric-note'));return box;}
function renderWorkspace(){
 if(!state)return;
 const choices=new Map();for(const a of state.accounts||[])choices.set(a.key,labelAccount(a.key));for(const id of Object.keys(state.runtime.accounts))if(!choices.has(id))choices.set(id,labelAccount(id));
 const previous=chartAccount;
 if(!choices.has(chartAccount))chartAccount=Object.keys(state.runtime.accounts)[0]||choices.keys().next().value||'';
 const select=$('chart-account');select.replaceChildren();for(const [id,label] of choices){const o=el('option',label);o.value=id;select.append(o);}select.value=chartAccount;
 const r=state.runtime.accounts[chartAccount];const windows=r?.current?.windows||[];
 if(!windows.some(w=>w.name===chartWindow))chartWindow=windows[0]?.name||'primary';
 $('chart-window').replaceChildren();for(const w of windows){const o=el('option',windowLabel(w));o.value=w.name;$('chart-window').append(o);}$('chart-window').value=chartWindow;
 const f=currentForecast();$('quota-summary').replaceChildren();
 if(f){
  $('quota-summary').append(metric('currentRemaining',f.remaining.toFixed(1)+'%',t('sample')+': '+time(f.observed_at)),metric('estimatedExhaustion',f.known?(f.exhaust_at?relativeTime(f.exhaust_at):t('noConsumption')):t('needSamples'),f.exhaust_at?time(f.exhaust_at):t('rateBasis')),metric('timeToReset',relativeTime(f.deadline),time(f.deadline)));
  $('pace-verdict').className='pace-verdict '+f.kind;
  const notice=(state.notices||[]).find(n=>n.account===chartAccount&&n.window===chartWindow);
  let verdict=notice?noticeText(notice):t('stale');
  if(f.known&&f.remaining_at_reset!=null)verdict+=' '+t('unusedAtReset')+' '+f.remaining_at_reset.toFixed(1)+'%。'.replace('。',language==='en'?'.':'。');
  $('pace-verdict').textContent=verdict;
 }else{$('quota-summary').append(metric('currentRemaining','—',t('needSamples')),metric('estimatedExhaustion','—',t('needSamples')),metric('timeToReset','—',t('needSamples')));$('pace-verdict').textContent=t('noQuotaData');}
 document.querySelectorAll('[data-hours]').forEach(b=>b.setAttribute('aria-pressed',String(Number(b.dataset.hours)===chartHours)));
 if(previous!==chartAccount){historyData=null;historyKey='';historyPending='';historySequence++;}
 drawChart();if(chartAccount)loadHistory();if(!deliveryLoading&&(!deliveryState||($('delivery-panel').open&&!deliveryDirty&&Date.now()-deliveryLoaded>30000)))loadDelivery();else renderDeliveryLog();
}
async function loadHistory(){
 const wanted=chartAccount+'|'+chartHours;if(historyPending===wanted||(historyKey===wanted&&Date.now()-historyLoaded<25000))return;
 const sequence=++historySequence;historyPending=wanted;historyError=false;
 try{const result=await api('/history?account='+encodeURIComponent(chartAccount)+'&hours='+chartHours);if(sequence!==historySequence||!state)return;historyData=result;historyKey=wanted;historyLoaded=Date.now();drawChart();}
 catch(e){if(!e.stale&&sequence===historySequence){historyError=true;drawChart();}}
 finally{if(sequence===historySequence)historyPending='';}
}
function svgNode(name,attrs={},text){const n=document.createElementNS('http://www.w3.org/2000/svg',name);for(const [k,v] of Object.entries(attrs))n.setAttribute(k,String(v));if(text!==undefined)n.textContent=text;return n;}
function drawChart(){
 const host=$('quota-chart');if(!host)return;host.replaceChildren();$('chart-inspection').textContent=t('chartInspect');$('chart-note').textContent=t('historyNote');
 const f=currentForecast();const points=[];
 if(historyData?.account===chartAccount)for(const sample of historyData.points||[]){const w=sample.windows.find(w=>w.name===chartWindow);const at=new Date(sample.at).getTime();if(w&&(!f||w.seconds===f.seconds)&&Number.isFinite(at)&&Number.isFinite(w.used))points.push({at,remaining:100-w.used,reset:w.reset_at,seconds:w.seconds});}
 if(!points.length){host.append(el('div',t(historyError?'chartUnavailable':historyPending?'chartLoading':'noQuotaData'),'chart-empty'));return;}
 points.sort((a,b)=>a.at-b.at);
 const W=Math.max(240,host.clientWidth||800),H=W<600?280:340,L=46,R=18,T=32,B=48;
 const first=points[0].at,last=points[points.length-1].at,deadline=f?new Date(f.deadline).getTime():last;
 const start=first===last?first-3600000:first,end=Math.max(last+60000,Number.isFinite(deadline)?deadline:last),span=end-start;
 const x=at=>L+(at-start)/span*(W-L-R),y=v=>T+(100-Math.max(0,Math.min(100,v)))/100*(H-T-B);
 const svg=svgNode('svg',{viewBox:`0 0 ${W} ${H}`,role:'img',tabindex:0,'aria-label':t('curveTitle')});
 for(const v of [0,25,50,75,100]){svg.append(svgNode('line',{x1:L,x2:W-R,y1:y(v),y2:y(v),class:'grid-line'}),svgNode('text',{x:L-8,y:y(v)+4,'text-anchor':'end'},v+'%'));}
 const ticks=W<600?3:5;
 for(let i=0;i<ticks;i++){const at=start+span*i/(ticks-1);const label=new Date(at).toLocaleString(language==='en'?'en-US':'zh-CN',{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit',hour12:false});svg.append(svgNode('text',{x:x(at),y:H-18,'text-anchor':i===0?'start':i===ticks-1?'end':'middle'},label));}
 let segment=[];const flush=()=>{if(segment.length>1)svg.append(svgNode('polyline',{points:segment.map(p=>`${x(p.at)},${y(p.remaining)}`).join(' '),class:'actual-line'}));segment=[];};
 for(let i=0;i<points.length;i++){const p=points[i],prev=points[i-1];if(prev&&(p.reset!==prev.reset||p.seconds!==prev.seconds||p.remaining>prev.remaining||p.at-prev.at>state.settings.poll_minutes*3*60000))flush();segment.push(p);}flush();
 if(points.length<4)for(const p of points)svg.append(svgNode('circle',{cx:x(p.at),cy:y(p.remaining),r:4,class:'sample-dot'}));
 if(f&&f.kind!=='stale'&&deadline>last){const observed=new Date(f.observed_at).getTime();svg.append(svgNode('line',{x1:x(observed),y1:y(f.remaining),x2:x(deadline),y2:y(0),class:'budget-line'}));if(f.known){const stop=Math.min(deadline,f.exhaust_at?new Date(f.exhaust_at).getTime():deadline);svg.append(svgNode('line',{x1:x(observed),y1:y(f.remaining),x2:x(stop),y2:y(f.remaining-f.rate_per_hour*(stop-observed)/3600000),class:'forecast-line'}));}}
 if(f&&Number.isFinite(deadline)){svg.append(svgNode('line',{x1:x(deadline),x2:x(deadline),y1:T,y2:H-B,class:'reset-line'}),svgNode('text',{x:x(deadline)-4,y:18,'text-anchor':'end'},t('reset')));}
 const cross=svgNode('line',{x1:L,x2:L,y1:T,y2:H-B,class:'crosshair',visibility:'hidden'});svg.append(cross);let inspected=points.length-1;
 function inspect(index){const p=points[Math.max(0,Math.min(points.length-1,index))];cross.setAttribute('x1',x(p.at));cross.setAttribute('x2',x(p.at));cross.setAttribute('visibility','visible');$('chart-inspection').textContent=t('chartObserved')+' · '+time(new Date(p.at).toISOString())+' · '+t('remaining')+' '+p.remaining.toFixed(1)+'%';}
 svg.addEventListener('pointermove',event=>{const rect=svg.getBoundingClientRect(),at=start+(((event.clientX-rect.left)/rect.width*W-L)/(W-L-R))*span;let best=0;for(let i=1;i<points.length;i++)if(Math.abs(points[i].at-at)<Math.abs(points[best].at-at))best=i;inspected=best;if(f?.known&&at>new Date(f.observed_at).getTime()&&at<=deadline){cross.setAttribute('x1',x(at));cross.setAttribute('x2',x(at));cross.setAttribute('visibility','visible');const value=Math.max(0,f.remaining-f.rate_per_hour*(at-new Date(f.observed_at).getTime())/3600000);$('chart-inspection').textContent=t('chartProjected')+' · '+time(new Date(at).toISOString())+' · '+t('remaining')+' '+value.toFixed(1)+'%';}else inspect(best);});
 svg.addEventListener('keydown',event=>{if(!['ArrowLeft','ArrowRight'].includes(event.key))return;event.preventDefault();inspected=Math.max(0,Math.min(points.length-1,inspected+(event.key==='ArrowRight'?1:-1)));inspect(inspected);});
 host.append(svg);$('chart-note').textContent=points.length+' '+t('sampleCount')+' · '+t('historyNote')+(points.length<3?' '+t('historyWarmup'):'');
}
async function loadDelivery(){
 if(deliveryLoading||!state)return;deliveryLoading=true;
 try{const result=await api('/notifications');if(!state)return;deliveryState=result;deliveryLoaded=Date.now();if(!deliveryDirty)fillDelivery();renderDeliveryLog();}
 catch(e){if(!e.stale){deliveryStatus='requestFailed';deliveryDetail=e.message;paintDeliveryFeedback();}}
 finally{deliveryLoading=false;}
}
function fillDelivery(){
 const c=deliveryState?.config;if(!c)return;
 $('delivery-language').value=c.language;$('delivery-cooldown').value=c.cooldown_minutes;$('smtp-enabled').checked=c.smtp.enabled;
 for(const [id,field] of [['smtp-host','host'],['smtp-port','port'],['smtp-tls','tls'],['smtp-user','username'],['smtp-from','from']])$(id).value=c.smtp[field]??'';
 $('smtp-recipients').value=(c.smtp.recipients||[]).join('\n');$('webhook-enabled').checked=c.webhook.enabled;
 $('smtp-password').value='';$('webhook-url').value='';$('clear-password').checked=false;$('clear-webhook').checked=false;
 $('delivery-kinds').replaceChildren();for(const kind of ['slow_down','speed_up','reset_soon','credit_expiring','stale']){const label=el('label'),input=el('input');input.type='checkbox';input.dataset.kind=kind;input.checked=c.kinds.includes(kind);const labelKey={slow_down:'alertSlow',speed_up:'alertFast',stale:'alertStale'}[kind]||kind;const span=el('span',t(labelKey));span.dataset.i18n=labelKey;label.append(input,span);$('delivery-kinds').append(label);}
}
function paintDeliveryFeedback(){$('delivery-feedback').textContent=deliveryStatus?t(deliveryStatus)+(deliveryDetail?' · '+serverText(deliveryDetail):''):'';}
function renderDeliveryLog(){
 paintDeliveryFeedback();if(!deliveryState)return;$('password-state').textContent=t(deliveryState.password_set?'secretSaved':'secretEmpty');$('webhook-state').textContent=t(deliveryState.webhook_set?'secretSaved':'secretEmpty');
 $('delivery-log').replaceChildren();for(const r of deliveryState.deliveries||[]){const entry=el('div',undefined,'delivery-entry '+r.status);entry.append(el('strong',t(r.status)+' · '+t(r.channel)+' · '+r.recipient),el('small',time(r.at)+' · '+kindLabel(r.kind)+(r.error?' · '+serverText(r.error):'')));$('delivery-log').append(entry);}
 if(!deliveryState.deliveries?.length)$('delivery-log').append(el('p',t('deliveryEmpty'),'muted'));
 paintDeliveryFeedback();
 if(deliveryState.error)$('delivery-feedback').textContent=t('requestFailed')+' · '+serverText(deliveryState.error);
}
$('clear-password').onchange=()=>{if($('clear-password').checked)$('smtp-enabled').checked=false;};
$('clear-webhook').onchange=()=>{if($('clear-webhook').checked)$('webhook-enabled').checked=false;};
$('delivery-panel').addEventListener('toggle',()=>{if($('delivery-panel').open&&!deliveryDirty)loadDelivery();});
$('delivery-form').addEventListener('input',()=>deliveryDirty=true);
$('delivery-form').onsubmit=async event=>{
 event.preventDefault();if(!deliveryState||deliveryLoading)return;
 const c=deliveryState.config;
 const next={revision:c.revision,language:$('delivery-language').value,cooldown_minutes:Number($('delivery-cooldown').value),kinds:[...$('delivery-kinds').querySelectorAll('input:checked')].map(n=>n.dataset.kind),smtp:{enabled:$('smtp-enabled').checked,host:$('smtp-host').value.trim(),port:Number($('smtp-port').value),tls:$('smtp-tls').value,username:$('smtp-user').value.trim(),password:$('smtp-password').value,from:$('smtp-from').value.trim(),recipients:$('smtp-recipients').value.split(/[\n,;]+/).map(v=>v.trim()).filter(Boolean)},webhook:{enabled:$('webhook-enabled').checked,url:$('webhook-url').value.trim()},clear_password:$('clear-password').checked,clear_webhook:$('clear-webhook').checked};
 $('delivery-save').disabled=true;deliveryLoading=true;
 try{deliveryState=await api('/notifications','PUT',next);deliveryLoaded=Date.now();deliveryDirty=false;deliveryStatus='deliverySaved';deliveryDetail='';fillDelivery();renderDeliveryLog();}
 catch(e){if(!e.stale){deliveryStatus='requestFailed';deliveryDetail=e.message;paintDeliveryFeedback();}}
 finally{$('delivery-save').disabled=false;deliveryLoading=false;}
};
$('chart-account').onchange=()=>{chartAccount=$('chart-account').value;storePreference('ccrm-chart-account',chartAccount);historyData=null;historyKey='';historySequence++;historyPending='';renderWorkspace();};
$('chart-window').onchange=()=>{chartWindow=$('chart-window').value;renderWorkspace();};
document.querySelectorAll('[data-hours]').forEach(b=>b.onclick=()=>{chartHours=Number(b.dataset.hours);storePreference('ccrm-chart-hours',String(chartHours));historyKey='';loadHistory();document.querySelectorAll('[data-hours]').forEach(n=>n.setAttribute('aria-pressed',String(n===b)));});
let chartWidth=0;new ResizeObserver(entries=>{const width=entries[0].contentRect.width;if(width>0&&Math.abs(width-chartWidth)>2){chartWidth=width;drawChart();}}).observe($('quota-chart'));
window.workspaceDirty=()=>deliveryDirty;
window.resetWorkspace=()=>{historyData=null;historyKey='';historyPending='';historySequence++;deliveryState=null;deliveryDirty=false;deliveryStatus='';deliveryDetail='';deliveryLoaded=0;paintDeliveryFeedback();$('smtp-password').value='';$('webhook-url').value='';$('quota-chart').replaceChildren();$('quota-summary').replaceChildren();$('delivery-log').replaceChildren();};
applyLanguage();
