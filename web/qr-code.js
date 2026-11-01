/* Local QR authoring. Preview and insertion share the Go encoder. */
globalThis.KeynopeQRCode=(()=>{
  function insert(commit){
    const previous=document.activeElement,dialog=document.createElement('dialog');
    dialog.className='keynope-qr-dialog';dialog.setAttribute('aria-labelledby','keynope-qr-title');
    dialog.innerHTML=`<form><header><span class="keynope-qr-eyebrow">SHARE SOMETHING</span><h2 id="keynope-qr-title">QR Code</h2><p>Turn a link or text into a scannable slide object.</p></header>
      <div class="keynope-qr-body"><section class="keynope-qr-fields"><label>URL or text<textarea name="content" rows="4" placeholder="https://example.com" required spellcheck="false" autocomplete="off"></textarea></label>
      <div class="keynope-qr-colours"><label>Code colour<input name="foreground" type="color" value="#000000"></label><label>Background<input name="background" type="color" value="#ffffff"></label></div>
      <label>Error correction<select name="correction" aria-label="Error correction"><option value="L">Low · 7%</option><option value="M" selected>Medium · 15% (recommended)</option><option value="Q">Quartile · 25%</option><option value="H">High · 30%</option></select></label>
      <p class="keynope-qr-hint">Higher correction tolerates more damage but makes the code denser. Keep its clear border and test scanning at presentation size.</p></section>
      <figure><div class="keynope-qr-preview" aria-label="QR code preview"><span>Your code appears here</span></div><figcaption>Generated on your device.<br>No tracking or external service.</figcaption></figure></div>
      <p class="keynope-qr-status" role="status" aria-live="polite"></p><footer><button type="button" name="cancel">Cancel</button><button type="submit" name="insert" disabled>Insert QR Code</button></footer></form>`;
    const form=dialog.querySelector('form'),fields=form.elements,preview=dialog.querySelector('.keynope-qr-preview'),status=dialog.querySelector('[role=status]');
    let timer,request,sequence=0,valid='',busy=false,closed=false;
    const values=()=>({content:fields.content.value,foreground:fields.foreground.value,background:fields.background.value,correction:fields.correction.value});
    const update=()=>{
      clearTimeout(timer);request?.abort();const stamp=++sequence;valid='';fields.insert.disabled=true;preview.classList.add('pending');status.textContent='';
      if(!fields.content.value.trim()){preview.replaceChildren(Object.assign(document.createElement('span'),{textContent:'Your code appears here'}));preview.classList.remove('pending');return;}
      timer=setTimeout(async()=>{
        request=new AbortController();const data=values();
        try{
          const response=await fetch('/api/editor/qr-code',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data),signal:request.signal});
          if(!response.ok)throw Error((await response.text()).trim());
          const result=await response.json();if(closed||stamp!==sequence)return;
          const image=new Image();image.alt='Preview of your QR code';image.src='data:image/svg+xml;charset=utf-8,'+encodeURIComponent(result.svg);preview.replaceChildren(image);
          valid=JSON.stringify(data);fields.insert.disabled=busy;status.textContent=result.modules+' × '+result.modules+' modules · Ready to insert';
        }catch(error){if(error.name!=='AbortError'&&!closed&&stamp===sequence){preview.replaceChildren(Object.assign(document.createElement('span'),{textContent:'Adjust your code settings'}));status.textContent=error.message;}}
        finally{if(stamp===sequence)preview.classList.remove('pending');}
      },180);
    };
    form.addEventListener('input',update);
    form.onsubmit=async event=>{event.preventDefault();if(busy||!valid||valid!==JSON.stringify(values()))return;busy=true;fields.insert.disabled=true;fields.cancel.disabled=true;dialog.setAttribute('aria-busy','true');
      try{await commit(values());dialog.close();}catch(error){status.textContent=error.message||'Could not insert QR code';}
      finally{busy=false;dialog.removeAttribute('aria-busy');fields.cancel.disabled=false;fields.insert.disabled=valid!==JSON.stringify(values());}
    };
    fields.cancel.onclick=()=>dialog.close();
    dialog.addEventListener('cancel',event=>{if(busy)event.preventDefault();});
    dialog.addEventListener('close',()=>{closed=true;clearTimeout(timer);request?.abort();dialog.remove();if(previous?.isConnected)previous.focus();},{once:true});
    dialog.addEventListener('keydown',event=>event.stopPropagation());
    document.body.append(dialog);dialog.showModal();fields.content.focus();
    return dialog;
  }
  return {insert};
})();
