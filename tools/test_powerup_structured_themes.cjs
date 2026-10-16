// Owned fixture, actual WASM commands/page deltas, mounted production painter.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const playwright=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const {PNG}=require(path.join(process.env.PLAYWRIGHT_MODULE||path.dirname(require.resolve('playwright/package.json')),'../playwright-core/lib/utilsBundle.js'));
const root=path.resolve(process.env.TEST_SITE||'/tmp/keynope-structured-theme-site'),output='/tmp/keynope-structured-theme-proof';
const server=http.createServer((req,res)=>{
  let name=new URL(req.url,'http://localhost').pathname;if(name.endsWith('/'))name+='index.html';
  const file=path.resolve(root,'.'+name);
  if(!file.startsWith(root+'/')||!fs.existsSync(file)){res.writeHead(404);return res.end();}
  res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.wasm':'application/wasm','.json':'application/json'})[path.extname(file)]||'application/octet-stream');res.end(fs.readFileSync(file));
});
(async()=>{
  fs.mkdirSync(output,{recursive:true});await new Promise(r=>server.listen(0,'127.0.0.1',r));
  const browser=await playwright.chromium.launch({headless:true,executablePath:process.env.CHROME_PATH});
  try{
    const context=await browser.newContext({viewport:{width:1440,height:1000}});
    await context.route(/https?:\/\/(?!127\.0\.0\.1).*/,r=>r.abort());
    const page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
    page.on('dialog',async d=>{assert.equal(d.type(),'confirm');await d.accept();});
    await page.goto('http://127.0.0.1:'+server.address().port+'/editor/');
    await page.locator('.keynope-web-loading').waitFor({state:'detached',timeout:90000});
    const chooser=page.waitForEvent('filechooser');
    await page.getByRole('button',{name:'Open a Keynope or PowerPoint presentation',exact:true}).click();
    await(await chooser).setFiles({name:'Structured.md',mimeType:'text/markdown',buffer:fs.readFileSync(output+'/structured.md')});
    await page.waitForFunction(()=>deck.pages[0]?.scene?.objects.some(o=>o.table)&&deck.pages[0]?.scene?.objects.some(o=>o.chart));
    const result=await page.evaluate(async()=>{
      const state=await fetch('/api/editor/state').then(r=>r.json());let theme=structuredClone(state.theme);
      const read=async(cold=false)=>{const r=await fetch('/api/editor/scene?slide=0'+(cold?'&timing=1':''));if(!r.ok)throw Error(await r.text());return {scene:await r.json(),cache:r.headers.get('X-Keynope-Scene-Cache')};};
      const host=document.createElement('div');Object.assign(host.style,{position:'fixed',inset:0,zIndex:99999,background:'#eee'});document.body.append(host);
      let current=(await read()).scene;const view=KeynopeScene.mount(host,current);await view.ready;
      const scale=Math.min(innerWidth/current.width,innerHeight/current.height);Object.assign(view.element.style,{transformOrigin:'top left',transform:`translate(${(innerWidth-current.width*scale)/2}px,${(innerHeight-current.height*scale)/2}px) scale(${scale})`});
      const nodes=new Map(current.objects.map(o=>[o.id,view.element.querySelector('[data-object-id="'+o.id+'"]')]));
      const originalWorkspace=window.keynopeWasmWorkspace,workspaces=[],records=[];
      window.keynopeWasmWorkspace=(...args)=>{const raw=originalWorkspace(...args),e=JSON.parse(raw);if(e.status===200)workspaces.push(JSON.parse(e.body));return raw;};
      async function change(name,edit,cache,ids,slides){
        await read();const start=workspaces.length,next=structuredClone(theme);edit(next);
        const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-deck-theme',themeData:next})});if(!r.ok)throw Error(await r.text());theme=next;
        const warm=await read(),cold=await read(true);
        if(warm.cache!==cache||JSON.stringify(warm.scene)!==JSON.stringify(cold.scene))throw Error(name+' cache/parity '+warm.cache);
        const delta=workspaces[start];if(!delta?.baseRevision||JSON.stringify(delta.replaceSlides||[])!==JSON.stringify(slides))throw Error(name+' page delta '+JSON.stringify(delta?.replaceSlides));
        const before=view.renderStats();if(!view.update(warm.scene))throw Error(name+' update rejected');await view.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
        const after=view.renderStats();if(JSON.stringify([...after.lastChangedObjectIDs].sort())!==JSON.stringify([...ids].sort()))throw Error(name+' changed '+JSON.stringify(after.lastChangedObjectIDs));
        const remounted=[];
        for(const [id,node]of nodes){const next=view.element.querySelector('[data-object-id="'+id+'"]');if(next!==node)throw Error(name+' remounted '+id);}
        if(after.objectMounts!==before.objectMounts)throw Error(name+' mounted structured shell');
        if(['unused accent','unused mono size','table band','series and cell seam'].includes(name)&&after.textMounts!==before.textMounts)throw Error(name+' reshaped paint-only text');
        records.push({name,cache:warm.cache,slides:delta.replaceSlides||[],objects:after.lastChangedObjectIDs,remounted,objectMounts:after.objectMounts-before.objectMounts,textMounts:after.textMounts-before.textMounts,tableCells:after.tableCellPaintUpdates-before.tableCellPaintUpdates,chartMarks:after.chartMarkUpdates-before.chartMarkUpdates});current=warm.scene;
      }
      await change('unused accent',t=>t.colors.accent[5]='#ff00aa','hit',[],[1]);
      await change('unused mono size',t=>t.fonts.mono.size+=10,'hit',[],[]);
      await change('table band',t=>t.colors.surface='#55ffaa','miss',['table'],[0]);
      await change('series and cell seam',t=>t.colors.accent[1]='#ffaa55','miss',['table','chart'],[0]);
      await change('table default size',t=>t.fonts.body.size+=10,'miss',['table'],[0,1]);
      await change('shared label width',t=>t.fonts.body.width=80,'miss',['table','chart'],[0,1]);
      const table=current.objects.find(o=>o.table).table,chart=current.objects.find(o=>o.chart).chart;
      if(table.cells[0].text.runs[0].fontId!=='mono'||table.cells[0].text.runs[0].color!=='#aabbcc')throw Error('Inline overrides changed');
      if(!chart.seriesColors.includes('#ffaa55')||!table.borders.some(b=>b.color==='#ffaa55'))throw Error('Resolved consumers stale');
      window.keynopeWasmWorkspace=originalWorkspace;
      view.element.id='structured-retained-proof';
      window.structuredRendererProof={host,view,current,nodes,scale};
      return {records,tableCells:table.cells.length,chartSeries:chart.seriesColors};
    });
    // Compare at the authored scene resolution so compositor resampling of
    // two independently mounted CSS-scaled layers cannot alter edge pixels.
    await page.setViewportSize({width:1920,height:1080});
    await page.evaluate(async()=>{window.structuredRendererProof.view.element.style.transform='';await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));});
    const parity=async name=>{
      const retained=await page.locator('#structured-retained-proof').screenshot();
      await page.evaluate(async()=>{
        const proof=window.structuredRendererProof,host=document.createElement('div');Object.assign(host.style,{position:'fixed',inset:0,zIndex:100000,background:'#eee'});document.body.append(host);
        const fresh=KeynopeScene.mount(host,structuredClone(proof.current));fresh.element.id='structured-fresh-proof';fresh.element.style.cssText=proof.view.element.style.cssText;await fresh.ready;
        await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));proof.fresh={host,view:fresh};
      });
      const fresh=await page.locator('#structured-fresh-proof').screenshot();
      const semantics=await page.evaluate(()=>{
        const snapshot=root=>[...root.querySelectorAll('[data-table-cell],.keynope-shaped-text,svg path,svg line')].map(n=>{
          const style=getComputedStyle(n),rect=n.getBoundingClientRect();
          return {tag:n.tagName,text:n.textContent,attributes:Object.fromEntries([...n.attributes].filter(a=>a.name!=='style'&&a.name!=='id').map(a=>[a.name,a.value]).sort()),rect:{x:rect.x,y:rect.y,width:rect.width,height:rect.height},style:Object.fromEntries(['position','left','top','width','height','fontFamily','fontSize','fontWeight','lineHeight','color','backgroundColor','borderStyle','borderColor','borderWidth','textAlign','transform','filter'].map(key=>[key,style[key]]))};
        });
        const p=window.structuredRendererProof;return {retained:snapshot(p.view.element),fresh:snapshot(p.fresh.view.element)};
      });
      fs.writeFileSync(output+'/'+name+'-retained.png',retained);fs.writeFileSync(output+'/'+name+'-fresh.png',fresh);
      await page.evaluate(()=>{const proof=window.structuredRendererProof;proof.fresh.view.dispose();proof.fresh.host.remove();delete proof.fresh;});
      fs.writeFileSync(output+'/'+name+'-semantics.json',JSON.stringify(semantics,null,2));
      assert.deepEqual(semantics.retained,semantics.fresh,name+' retained text/geometry/paint differs');
      const a=PNG.sync.read(retained),b=PNG.sync.read(fresh);assert.equal(a.width,b.width);assert.equal(a.height,b.height);
      const edge=(image,i)=>[-image.width*4,image.width*4,-4,4].some(d=>i+d>=0&&i+d<image.data.length&&[0,1,2].some(c=>Math.abs(image.data[i+c]-image.data[i+d+c])>8));
      let changed=0,max=0,offEdge=0;for(let i=0;i<a.data.length;i+=4){let d=0;for(let c=0;c<4;c++)d=Math.max(d,Math.abs(a.data[i+c]-b.data[i+c]));if(d)changed++;if(d>3&&(!edge(a,i)||!edge(b,i)))offEdge++;max=Math.max(max,d);}
      // Chromium independently composites retained/new SVG edge coverage.
      // Exact resolved DOM geometry/style above is required as well; only
      // <=3 flat-region quantization or <=16 at an edge in BOTH renders, on
      // <1% of native-resolution pixels. This cannot excuse shifted geometry,
      // missing text or stale paint: those exact DOM checks are mandatory.
      assert(max<=16&&offEdge===0&&changed<a.width*a.height*.01,name+' raster differs: '+JSON.stringify({changed,max,offEdge}));
      (result.parity??=[]).push({name,changed,max,offEdge,pixels:a.width*a.height});
    };
    await parity('theme');
    result.patches=await page.evaluate(async()=>{
      const proof=window.structuredRendererProof,view=proof.view,records=[];
      const update=async(name,edit)=>{
        const next=structuredClone(proof.current),before=view.renderStats(),oldCells=[...view.element.querySelectorAll('[data-table-cell]')],oldPaths=[...view.element.querySelectorAll('.keynope-scene-chart path')];
        edit(next);if(!view.update(next))throw Error(name+' rejected');await view.ready;await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
        const after=view.renderStats();if(after.objectMounts!==before.objectMounts)throw Error(name+' replaced owner');
        if(oldCells.some((n,i)=>n!==view.element.querySelectorAll('[data-table-cell]')[i])||oldPaths.some((n,i)=>n!==view.element.querySelectorAll('.keynope-scene-chart path')[i]))throw Error(name+' replaced cell/mark');
        records.push({name,textMounts:after.textMounts-before.textMounts,textUpdates:after.textContentUpdates-before.textContentUpdates,cells:after.tableCellPaintUpdates-before.tableCellPaintUpdates,marks:after.chartMarkUpdates-before.chartMarkUpdates});proof.current=next;
      };
      await update('one cell fill',s=>s.objects.find(o=>o.table).table.cells[3].fill='#aa55ff');
      await update('one cell text',s=>{const text=s.objects.find(o=>o.table).table.cells[3].text,runs=[{text:'Changed cell'}];text.runs=runs;if(text.paragraphs)text.paragraphs=text.paragraphs.map((p,i)=>({...p,runs:i===0?runs:[]}));});
      const cellText=(s,index,value)=>{const t=s.objects.find(o=>o.table).table.cells[index].text,runs=[{text:value}];t.runs=runs;if(t.paragraphs)t.paragraphs=t.paragraphs.map((p,i)=>({...p,runs:i===0?runs:[]}));};
      await update('cell overflow',s=>cellText(s,3,'Overflow '.repeat(200)));
      const findings=()=>view.diagnostics.filter(d=>d.objectId==='table'&&d.message.startsWith('Cell B2:'));
      if(findings().length!==1)throw Error('Cell overflow not recorded once');
      await update('other cell edit',s=>cellText(s,5,'Another cell'));
      if(findings().length!==1)throw Error('Unrelated cell edit lost/duplicated overflow');
      await update('clear overflow',s=>cellText(s,3,'Changed cell'));
      if(findings().length)throw Error('Cleared cell retained stale overflow');
      await update('track resize',s=>{
        const o=s.objects.find(o=>o.table),d=o.table;d.columns=d.columns.map(v=>v*1.1);d.rows=d.rows.map(v=>v*.9);o.bounds.width=d.columns.reduce((a,b)=>a+b,0);o.bounds.height=d.rows.reduce((a,b)=>a+b,0);
        for(const c of d.cells){c.bounds.x*=1.1;c.bounds.y*=.9;c.bounds.width*=1.1;c.bounds.height*=.9;c.contentBounds.width=Math.max(1,c.bounds.width-2*c.contentBounds.x);c.contentBounds.height=Math.max(1,c.bounds.height-2*c.contentBounds.y);}
      });
      await update('chart caption and data',s=>{const c=s.objects.find(o=>o.chart).chart;c.data.title='Updated dataset';c.data.series[0].values[0]=42;c.labels[0].text.runs=[{text:'Updated dataset'}];if(c.labels[0].text.paragraphs)c.labels[0].text.paragraphs=c.labels[0].text.paragraphs.map((p,i)=>({...p,runs:i===0?[{text:'Updated dataset'}]:[]}));c.labels[0].fullText='Updated dataset';});
      // In-place caller mutation must be observed, not hidden by reference caches.
      const before=view.renderStats(),mark=proof.current.objects.find(o=>o.chart).chart.paths[0];mark.fill='#ff55aa';
      if(!view.update(proof.current))throw Error('In-place mark rejected');await view.ready;
      if(view.renderStats().chartMarkUpdates-before.chartMarkUpdates!==1)throw Error('In-place mark not patched once');
      const html=view.element.innerHTML,bad=structuredClone(proof.current);bad.objects.find(o=>o.table).table.cells[0].bounds.width=NaN;
      if(view.update(bad)!==false||view.element.innerHTML!==html)throw Error('Invalid geometry partially mutated DOM');
      const invalid=structuredClone(proof.current),cell=invalid.objects.find(o=>o.table).table.cells[0];cell.text.runs=[{text:42}];if(cell.text.paragraphs)cell.text.paragraphs=cell.text.paragraphs.map(p=>({...p,runs:[{text:42}]}));
      if(view.update(invalid)!==false||view.element.innerHTML!==html)throw Error('Invalid cell text partially mutated DOM');
      return records;
    });
    await parity('data-tracks');
    await page.locator('#structured-retained-proof .keynope-chart-data-access').focus();
    await page.locator('#structured-retained-proof .keynope-chart-data-access').click();
    const dataDialog=page.getByRole('dialog',{name:'Updated dataset data',exact:true});assert.equal(await dataDialog.getByRole('cell',{name:'42',exact:true}).count(),1);
    await dataDialog.getByRole('button',{name:'Close',exact:true}).click();await dataDialog.waitFor({state:'detached'});
    result.cleanup=await page.evaluate(async()=>{
      const proof=window.structuredRendererProof,view=proof.view,original=view.resourceCounts(),chart=view.element.querySelector('[data-object-id="chart"]');
      for(let i=0;i<4;i++){const next=structuredClone(proof.current),table=next.objects.find(o=>o.table);table.table.cells[0].header=!table.table.cells[0].header;const before=view.renderStats();if(!view.update(next))throw Error('Topology replacement rejected');await view.ready;if(view.renderStats().objectMounts-before.objectMounts!==1||chart!==view.element.querySelector('[data-object-id="chart"]'))throw Error('Topology replacement leaked to chart');proof.current=next;}
      if(JSON.stringify(view.resourceCounts())!==JSON.stringify(original))throw Error('Topology replacement leaked resources');
      const next=structuredClone(proof.current);next.objects=next.objects.filter(o=>o.chart);if(!view.update(next))throw Error('Deletion rejected');await view.ready;
      if(view.tableContentMetrics('table').length||view.element.querySelector('.keynope-scene-table'))throw Error('Removed table resources retained');
      const afterDelete=view.resourceCounts();view.dispose();if(view.resourceCounts().objects||view.resourceCounts().textSurfaces)throw Error('Dispose leaked structured resources');proof.host.remove();return {original,afterDelete};
    });
    await page.screenshot({path:output+'/structured-themes.png'});fs.writeFileSync(output+'/browser-report.json',JSON.stringify(result,null,2)+'\n');
    assert.deepEqual(errors,[]);console.log('PASS: WASM structured theme consumers, cache hits/cold parity, exact page deltas, unrelated retained nodes, inline overrides, live chart/table paints. '+output);console.log(JSON.stringify(result));
  }finally{await browser.close();server.close();}
})().catch(e=>{console.error(e);process.exitCode=1;server.close();});
