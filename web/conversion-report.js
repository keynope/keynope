// Shared editor-only report UI. Authored deck data never contains this report.
window.KeynopeConversionReport=(()=>{
  function show(report,verb){
    const warnings=Array.isArray(report?.warnings)?report.warnings:[];
    window.showEngagementToast?.(verb+(warnings.length?' · '+warnings.length+' conversion note'+(warnings.length===1?'':'s'):''));
    if(!warnings.length)return;
    document.querySelector('.keynope-conversion-report')?.close();
    const dialog=document.createElement('dialog');dialog.className='keynope-conversion-report';dialog.dataset.keynopeSceneDialog='conversion';
    const title=verb+' — conversion notes';dialog.setAttribute('aria-label',title);
    Object.assign(dialog.style,{width:'min(680px,90vw)',maxHeight:'80vh',boxSizing:'border-box',background:'#191c20',color:'#f3f4f6',border:'1px solid #65717a',borderRadius:'8px',padding:'20px',font:'inherit'});
    const heading=document.createElement('h2');heading.textContent=title;
    const intro=document.createElement('p');intro.textContent='The conversion completed. Review these notes before sharing; keep your original deck for editing.';
    const list=document.createElement('ol');Object.assign(list.style,{maxHeight:'50vh',overflowY:'auto',overflowWrap:'anywhere',paddingInlineStart:'26px'});
    const button=label=>{const node=document.createElement('button');node.type='button';node.textContent=label;Object.assign(node.style,{font:'inherit',color:'inherit',background:'#292e33',border:'1px solid #65717a',borderRadius:'4px',padding:'6px 14px',cursor:'pointer'});return node;};
    const previous=button('Previous'),next=button('Next'),range=document.createElement('span'),navigation=document.createElement('nav');navigation.setAttribute('aria-label','Conversion notes pages');navigation.style.cssText='display:flex;align-items:center;gap:10px;margin:12px 0';navigation.hidden=warnings.length<=50;if(navigation.hidden)navigation.style.display='none';navigation.append(previous,range,next);
    let page=0;
    function render(){
      list.replaceChildren();list.start=page*50+1;
      for(const warning of warnings.slice(page*50,page*50+50)){
        const item=document.createElement('li');item.style.marginBottom='18px';
        const location=document.createElement('strong');location.textContent=[Number.isInteger(warning.slide)&&warning.slide>=0?'Slide '+(warning.slide+1):'Document',warning.object].filter(Boolean).join(' · ');
        const detail=document.createElement('p');detail.textContent=warning.message||warning.code||'Conversion note';
        const policy=document.createElement('p');policy.textContent=[warning.feature,warning.severity,warning.fallback?'Fallback: '+warning.fallback.replaceAll('-',' '):null].filter(Boolean).join(' · ');
        const help=document.createElement('p');help.textContent=warning.remediation||'Check this content in the intended viewer.';
        for(const paragraph of [detail,policy,help])paragraph.style.margin='6px 0';
        const code=document.createElement('small');code.textContent=warning.code||'';item.append(location,detail,policy,help,code);list.append(item);
      }
      previous.disabled=page===0;next.disabled=(page+1)*50>=warnings.length;range.textContent=(page*50+1)+'–'+Math.min((page+1)*50,warnings.length)+' of '+warnings.length;list.scrollTop=0;
    }
    previous.onclick=()=>{if(page>0){page--;render();}};next.onclick=()=>{if((page+1)*50<warnings.length){page++;render();}};
    const close=button('Close');close.onclick=()=>dialog.close();
    dialog.append(heading,intro,list,navigation,close);dialog.addEventListener('keydown',event=>event.stopPropagation());dialog.addEventListener('close',()=>dialog.remove(),{once:true});document.body.append(dialog);render();dialog.showModal();close.focus();
  }
  return Object.freeze({show});
})();
