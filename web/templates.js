// Shared native-WebView / browser template chooser. Opening and cancelling is
// read-only; the host owns its normal save/discard guard and document replacement.
globalThis.keynopeChooseTemplate = (() => {
  let pending = null;
  return () => {
    if (pending) return pending;
    pending = new Promise(resolve => {
      const previous=document.activeElement,dialog=document.createElement('dialog');
      dialog.className='keynope-template-dialog';dialog.setAttribute('aria-label','New presentation');
      const heading=document.createElement('h2');heading.textContent='Choose your starting point';
      const intro=document.createElement('p');intro.textContent='Editable slides and masters. Your presentation stays in one portable Markdown file.';
      const grid=document.createElement('div');grid.className='keynope-template-grid';
      const status=document.createElement('p');status.setAttribute('role','status');status.textContent='Preparing templates…';
      const cancel=document.createElement('button');cancel.type='button';cancel.textContent='Cancel';
      dialog.append(heading,intro,grid,status,cancel);document.body.append(dialog);
      const views=[],observers=[];let closed=false;
      const finish=id=>{if(closed)return;closed=true;views.forEach(v=>v.dispose());observers.forEach(o=>o.disconnect());dialog.close();dialog.remove();previous?.isConnected&&previous.focus({preventScroll:true});pending=null;resolve(id);};
      cancel.onclick=()=>finish(null);
      dialog.addEventListener('cancel',event=>{event.preventDefault();finish(null);});
      dialog.addEventListener('keydown',event=>event.stopPropagation());
      dialog.showModal();cancel.focus();
      (async()=>{
        try {
          const response=await fetch('/api/editor/templates');if(!response.ok)throw Error(await response.text());
          const templates=await response.json();await KeynopeScene.ensureFonts();if(closed)return;
          for(const template of templates){
            const card=document.createElement('button');card.type='button';card.className='keynope-template-card';card.setAttribute('aria-label','Use '+template.name+' template');
            const preview=document.createElement('div');preview.className='keynope-template-preview';preview.setAttribute('aria-hidden','true');
            const title=document.createElement('strong');title.textContent=template.name;
            const description=document.createElement('span');description.textContent=template.description;
            card.append(preview,title,description);grid.append(card);card.onclick=()=>finish(template.id);
            const view=KeynopeScene.mount(preview,template.scene);views.push(view);
            const fit=()=>{const scale=preview.clientWidth/template.scene.width;Object.assign(view.element.style,{transformOrigin:'0 0',transform:'scale('+scale+')',left:'0',top:'0'});};
            const observer=new ResizeObserver(fit);observer.observe(preview);observers.push(observer);fit();await view.ready;
            if(closed)return;
          }
          status.textContent='';grid.querySelector('button')?.focus();
        }catch(error){if(!closed){status.textContent='Could not prepare templates: '+error.message;status.setAttribute('role','alert');}}
      })();
    });
    return pending;
  };
})();
