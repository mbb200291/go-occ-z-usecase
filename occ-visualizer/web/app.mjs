const $=id=>document.getElementById(id), scale=100, offset=65;
const labels={reading:'私有運算',validating:'驗證中',writing:'提交中',done:'已完成',conflict:'衝突終止',error:'執行失敗'};
let session="",events=[],origin=null,time=0,end=1,live=true,selected=null,current=-2,active=0,now=0,pollAt=0;
let playing=false,lastFrame=0;
const position=t=>offset+t*scale;
const name=t=>`#${t.request}·${t.attempt}`;
const escape=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
let state={txns:[],ongoing:new Set(),prev:new Set(),message:'設定參數後啟動交易，或直接加入 1 個。'};
function reduce(index){
 const map=new Map(),ongoing=new Set(),prev=new Set();let message='等待交易開始';
 for(let i=0;i<=index;i++){
  const e=events[i],at=e.time-origin;
  if(e.kind==='read')map.set(e.id,{id:e.id,request:e.request,attempt:e.attempt,read:at,valid:null,commit:null,write:null,end:null,status:'reading',target:e.target,readOnly:e.readOnly});
  const t=map.get(e.id);if(!t)continue;
  if(e.kind!=='request_done'){t.stamps={read:e.read,validate:e.validate,write:e.write};t.rs=e.readSet??[];t.ws=e.writeSet??[];}
  const n=name(t);
  switch(e.kind){
   case 'read':ongoing.add(e.id);message=`${n} 開始讀取 target ${t.target}，加入 ongoing${t.attempt>1?'；這是重試建立的新交易':''}`;break;
   case 'validate':t.valid=at;t.status='validating';prev.add(e.id);message=`${n} 設定 ValidateTime，加入 prevTxns`;break;
   case 'blocked':t.blocker=e.otherID;message=`${n} 驗證遭 ${map.has(e.otherID)?name(map.get(e.otherID)):e.otherID} 阻擋`;break;
   case 'purge':prev.delete(e.otherID);message=`${n} 驗證時清除 ${map.has(e.otherID)?name(map.get(e.otherID)):e.otherID}：WriteTime 早於最早的 ongoing ReadTime`;break;
   case 'conflict':t.status='conflict';t.end=at;prev.delete(e.id);message=`${n} 發生衝突，移出 prevTxns；OCC 決定是否重試`;break;
   case 'commit':t.commit=at;t.status='writing';message=`${n} 通過驗證，開始真正的備份／journal／Write`;break;
   case 'end':ongoing.delete(e.id);t.end=at;t.error=e.error;if(t.status!=='conflict'){t.status=e.error?'error':'done';if(e.write)t.write=at;}message=`${n} ${labels[t.status]}，移出 ongoing${e.error?`：${e.error}`:''}`;break;
   case 'request_done':message=`Request #${t.request} ${e.error?`失敗：${e.error}`:`成功，共 ${t.attempt} 次嘗試`}`;break;
  }
 }
 return {txns:[...map.values()],ongoing,prev,message};
}
function details(){
 const t=state.txns.find(t=>t.id===selected);$('selected').textContent=t?name(t):'';
 if(!t){$('detail').textContent='點選時間軸上的交易。';return;}
 const blocker=state.txns.find(x=>x.id===t.blocker);
 $('detail').innerHTML=`<code>${escape(t.id)}</code><br>${t.readOnly?'只讀取':'讀取後 +1'} target ${escape(t.target)}<br>Read set {${t.rs.join(', ')}}<br>Write set {${t.ws.join(', ')}}<br>R ${t.stamps.read} / V ${t.stamps.validate||'—'} / W ${t.stamps.write||'∞'}${blocker?`<br>衝突來源：${name(blocker)}<br>來源 Write set {${blocker.ws.join(', ')}}`:''}${t.error?`<br>${escape(t.error)}`:''}`;
}
function marker(text,at,kind){return `<span class="marker ${kind}" style="left:${position(at)}px" title="${at.toFixed(4)}s"><i></i><b>${text}</b></span>`;}
function draw(){
 const index=events.findLastIndex(e=>e.time-origin<=time);
 if(index!==current){
  current=index;state=reduce(index);
  $('rows').innerHTML=state.txns.map(t=>`<button class="row${selected===t.id?' selected':''}" data-id="${t.id}" aria-label="檢視交易 ${name(t)}"><span class="row-name">${name(t)}${t.attempt>1?' ↳ retry':''}</span><span class="status" style="left:${position(t.read)+60}px">${labels[t.status]} · target ${t.target}</span><span class="bar" data-phase="read"></span><span class="bar checking" data-phase="validate"></span><span class="bar commit" data-phase="commit"></span>${marker('R '+t.stamps.read,t.read,'reading')}${t.valid!==null?marker('V '+t.stamps.validate,t.valid,'validating'):''}${t.write!==null?marker('W '+t.stamps.write,t.write,'done'):''}${t.status==='conflict'||t.status==='error'?`<span class="marker abort" style="left:${position(t.end)}px">×</span>`:''}</button>`).join('');
  for(const key of ['ongoing','prev']){
   $(`${key}-count`).textContent=state[key].size;
   $(key).innerHTML=[...state[key]].map(id=>{const t=state.txns.find(t=>t.id===id);return `<button class="chip ${t.status}" data-id="${id}">${name(t)} · ${labels[t.status]}</button>`;}).join('')||'<span class="empty">∅ 空集合</span>';
  }
  $('event').textContent=state.message;details();
  if((live||playing)&&$('follow').checked)$('scroll').scrollTop=$('scroll').scrollHeight;
 }
 state.txns.forEach((t,i)=>{
  const until=Math.min(time,t.end??time),v=t.valid??until,c=t.commit??until;
  for(const [phase,start,finish] of [['read',t.read,v],['validate',v,c],['commit',c,until]]){
   const bar=$('rows').children[i].querySelector(`[data-phase="${phase}"]`);bar.style.left=`${position(start)}px`;bar.style.width='1px';bar.style.transform=`scaleX(${Math.max(0,(finish-start)*scale)})`;
  }
 });
 $('clock').textContent=`${time.toFixed(1)}s`;$('seek').max=end;$('seek').value=time;
 $('cursor').style.transform=`translateX(${position(time)}px)`;
 $('live').textContent=live?'● 即時追蹤':'回到即時';$('pause').disabled=!live&&!playing;
 $('play').textContent=playing?'▶ 播放中':!active&&time>=latestTime()?'↺ 重播':'▶ 播放';$('play').disabled=playing;
 $('speed').title=live?'選擇速度即可切換為動畫播放；後端執行速度不變':'';
 $('timeline').style.width=`${position(Math.ceil(end)+1)}px`;
 const ticks=Math.ceil(end/2)+1;
 if($('ruler').childElementCount!==ticks)$('ruler').innerHTML=Array.from({length:ticks},(_,i)=>`<span style="left:${position(i*2)}px">${i*2}s</span>`).join('');
 if($('follow').checked){const el=$('scroll'),x=position(time);if(x>el.scrollLeft+el.clientWidth*.8||x<el.scrollLeft)el.scrollLeft=Math.max(0,x-el.clientWidth*.65);}
}
async function poll(){
 try{
  const res=await fetch(`/api/events?after=${events.length}&session=${encodeURIComponent(session)}`,{cache:'no-store'});if(res.status===409){session='';events=[];origin=null;time=0;end=1;active=0;current=-2;selected=null;live=true;playing=false;$('error').hidden=false;$('error').textContent='伺服器已重新啟動，已清空舊時間軸。';draw();return;}if(!res.ok)throw new Error(await res.text());
  const data=await res.json();session=data.session;events.push(...(data.events??[]));origin??=events[0]?.time??null;
  active=data.active;now=data.now;pollAt=performance.now();
  $('connection').textContent='● 已連線 · Go OCC';
  $('summary').textContent=`${data.active} 執行中 / ${data.completed} 成功 / ${data.failed} 失敗 / ${data.total} requests`;
  $('data').textContent=data.values?data.values.map((v,i)=>`${i}: ${v}`).join(' · '):'交易執行中…';
  if(origin!==null){const latest=Math.max(0,(active?now:events.at(-1).time)-origin);end=Math.max(1,latest);if(live)time=latest;draw();}
 }catch(err){$('connection').textContent='連線中斷，正在重試';$('error').hidden=false;$('error').textContent=`無法更新：${err.message}`;}
 finally{setTimeout(poll,200);}
}
async function launch(count){
 if(!$('launch').reportValidity())return;
 $('batch').disabled=true;$('add').disabled=true;$('error').hidden=true;
 try{
  const res=await fetch('/api/transactions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({count,workMS:Number($('work').value),distribution:$('distribution').value,target:Number($('target').value),readOnly:$('operation').value==='read'})});
  if(!res.ok)throw new Error(await res.text());if(Number($('speed').value)!==1){live=false;playing=true;lastFrame=0;}else if(!playing){live=true;}
 }catch(err){$('error').hidden=false;$('error').textContent=err.message;}
 finally{$('batch').disabled=false;$('add').disabled=false;}
}
$('launch').onsubmit=e=>{e.preventDefault();launch(Number($('count').value));};$('add').onclick=()=>launch(1);
function latestTime(){return origin===null?0:Math.max(0,(active?now:events.at(-1).time)-origin);}
function play(){live=false;playing=true;lastFrame=0;if(!active&&time>=latestTime())time=0;draw();}
$('play').onclick=play;$('speed').onchange=play;
$('pause').onclick=()=>{live=false;playing=false;draw();};$('live').onclick=()=>{live=true;playing=false;time=latestTime();draw();};
$('step').onclick=()=>{live=false;playing=false;time=events.find(e=>e.time-origin>time)?.time-origin||end;draw();};
$('seek').oninput=()=>{live=false;playing=false;time=Number($('seek').value);draw();};
document.addEventListener('click',e=>{const el=e.target.closest('[data-id]');if(el){selected=el.dataset.id;current=-1;draw();}});
window.addEventListener('resize',draw);
const reduced=matchMedia('(prefers-reduced-motion: reduce)');
function tick(frame){
 const dt=lastFrame?Math.min((frame-lastFrame)/1000,.1):0;lastFrame=frame;
 if(origin!==null&&!document.hidden){
  if(live&&active&&!reduced.matches){time=Math.max(0,now-origin+Math.min((performance.now()-pollAt)/1000,.2));end=Math.max(end,time);draw();}
  else if(playing){time=Math.min(latestTime(),time+dt*Number($('speed').value));if(!active&&time>=latestTime())playing=false;draw();}
 }
 requestAnimationFrame(tick);
}
draw();poll();requestAnimationFrame(tick);
