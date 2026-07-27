(() => {
  'use strict';

  const surfaceURL = new URL(location.href);
  if (surfaceURL.searchParams.get('keynopeSurface') !== 'app') {
    surfaceURL.searchParams.set('keynopeSurface', 'app');
    history.replaceState(null, '', surfaceURL);
  }
  const nativeFetch = window.fetch.bind(window);
  const baseURL = new URL('./', document.currentScript.src);
  const databaseName = 'keynope-web-editor';
  const draftKey = 'current';
  let dirty = false;
  let draftTimer = 0;
  let documentFileHandle = null;
  let currentName = 'Untitled.md';
  let runtimeReady;

  function openDatabase() {
    return new Promise((resolve, reject) => {
      const request = indexedDB.open(databaseName, 1);
      request.onupgradeneeded = () => request.result.createObjectStore('documents');
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
  }

  async function readDraft() {
    try {
      const db = await openDatabase();
      return await new Promise((resolve, reject) => {
        const request = db.transaction('documents').objectStore('documents').get(draftKey);
        request.onsuccess = () => resolve(request.result || null);
        request.onerror = () => reject(request.error);
      });
    } catch (_) {
      return null;
    }
  }

  async function writeDraft(value) {
    try {
      const db = await openDatabase();
      await new Promise((resolve, reject) => {
        const transaction = db.transaction('documents', 'readwrite');
        transaction.objectStore('documents').put(value, draftKey);
        transaction.oncomplete = resolve;
        transaction.onerror = () => reject(transaction.error);
      });
    } catch (_) {}
  }

  async function clearDraft() {
    try {
      const db = await openDatabase();
      await new Promise((resolve, reject) => {
        const transaction = db.transaction('documents', 'readwrite');
        transaction.objectStore('documents').delete(draftKey);
        transaction.oncomplete = resolve;
        transaction.onerror = () => reject(transaction.error);
      });
    } catch (_) {}
  }

  function responseFromEnvelope(raw) {
    const envelope = typeof raw === 'string' ? JSON.parse(raw) : raw;
    return new Response(envelope.body || '', {
      status: envelope.status || 500,
      headers: {'Content-Type': envelope.contentType || 'text/plain; charset=utf-8'}
    });
  }

  async function loadScript(source) {
    await new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = source;
      script.onload = resolve;
      script.onerror = () => reject(new Error('Could not load the Keynope runtime'));
      document.head.appendChild(script);
    });
  }

  async function waitForRuntime() {
    for (let attempt = 0; attempt < 500 && !window.keynopeWasmReady; attempt++) {
      await new Promise(resolve => setTimeout(resolve, 10));
    }
    if (!window.keynopeWasmReady) throw new Error('Keynope runtime did not start');
  }

  async function initialDocument() {
    const draft = await readDraft();
    if (draft && typeof draft.markdown === 'string') return draft;
    const response = await nativeFetch(new URL('Welcome.md', baseURL), {cache: 'no-store'});
    if (!response.ok) throw new Error('Could not load the starter presentation');
    return {markdown: await response.text(), name: 'Untitled.md', untitled: true};
  }

  async function bootRuntime() {
    await loadScript(new URL('wasm_exec.js', baseURL));
    const go = new Go();
    const wasmURL = new URL('keynope-editor.wasm', baseURL);
    let result;
    try {
      result = await WebAssembly.instantiateStreaming(nativeFetch(wasmURL), go.importObject);
    } catch (_) {
      const response = await nativeFetch(wasmURL);
      result = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
    }
    go.run(result.instance);
    await waitForRuntime();
    const initial = await initialDocument();
    currentName = initial.name || 'Untitled.md';
    const envelope = JSON.parse(window.keynopeWasmInit(initial.markdown, currentName, initial.untitled !== false));
    if (envelope.status !== 200) throw new Error(envelope.body || 'Could not initialize Keynope');
  }

  async function refreshWorkspace(state) {
    const envelope = JSON.parse(window.keynopeWasmWorkspace());
    if (envelope.status !== 200) return;
    const workspace = JSON.parse(envelope.body);
    if (state && Number.isInteger(state.current)) workspace.current = state.current;
    if (window.keynopeLoadWebWorkspace) await window.keynopeLoadWebWorkspace(workspace);
  }

  runtimeReady = bootRuntime().catch(error => {
    document.body.innerHTML = '<main class="keynope-web-fatal"><h1>KEYNOPE</h1><p>' +
      String(error && error.message || error).replace(/[&<>]/g, value => ({'&':'&amp;','<':'&lt;','>':'&gt;'}[value])) +
      '</p><button onclick="location.reload()">RETRY</button></main>';
    throw error;
  });

  window.fetch = async (input, init = {}) => {
    const rawURL = typeof input === 'string' ? input : input.url;
    const target = new URL(rawURL, location.href);
    if (!target.pathname.startsWith('/api/editor/')) return nativeFetch(input, init);
    await runtimeReady;
    const method = String(init.method || (input && input.method) || 'GET').toUpperCase();
    let raw;
    if (target.pathname === '/api/editor/upload' && init.body instanceof FormData) {
      const file = init.body.get('image');
      if (!(file instanceof Blob)) return new Response('missing image', {status: 400});
      raw = window.keynopeWasmUpload(new Uint8Array(await file.arrayBuffer()));
    } else {
      const body = typeof init.body === 'string' ? init.body : '';
      raw = window.keynopeWasmRequest(method, target.pathname + target.search, body);
    }
    const envelope = JSON.parse(raw);
    if (envelope.status >= 200 && envelope.status < 300 &&
        (target.pathname === '/api/editor/action' || target.pathname === '/api/editor/upload' || target.pathname === '/api/editor/state')) {
      let state = null;
      try { state = JSON.parse(envelope.body); } catch (_) {}
      await refreshWorkspace(state);
    }
    return responseFromEnvelope(envelope);
  };

  async function documentPayload(name = currentName) {
    const response = await window.fetch('/api/editor/document', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({path: name || 'Untitled.md'})
    });
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not create document');
    return response.json();
  }

  async function saveBlob(blob, suggestedName, types, rememberHandle = false) {
    if ('showSaveFilePicker' in window) {
      const handle = rememberHandle && documentFileHandle
        ? documentFileHandle
        : await window.showSaveFilePicker({suggestedName, types});
      if (rememberHandle) documentFileHandle = handle;
      const stream = await handle.createWritable();
      await stream.write(blob);
      await stream.close();
      return handle.name || suggestedName;
    }
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = suggestedName;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    return suggestedName;
  }

  async function savePresentation() {
    await runtimeReady;
    const payload = await documentPayload(currentName);
    const name = currentName && currentName !== 'Untitled.md' ? currentName : 'Untitled.md';
    const savedName = await saveBlob(new Blob([payload.content], {type:'text/markdown;charset=utf-8'}), name, [{
      description: 'Keynope Markdown deck', accept: {'text/markdown':['.md']}
    }], true);
    currentName = savedName;
    const response = await window.fetch('/api/editor/action', {
      method:'POST', headers:{'Content-Type':'application/json'},
      body:JSON.stringify({action:'confirm-save', path:savedName, value:payload.version})
    });
    if (!response.ok) throw new Error((await response.text()).trim() || 'The presentation changed while saving');
    dirty = false;
    await clearDraft();
    document.title = 'Keynope — ' + currentName;
    if (window.keynopeDidSave) window.keynopeDidSave();
  }

  async function exportHTML(openAfterExport = false, presentationWindow = null) {
    await runtimeReady;
    const baseName = (currentName || 'Untitled.md').replace(/\.[^.]+$/, '') || 'Keynope';
    const response = await window.fetch('/api/editor/export-document', {
      method:'POST', headers:{'Content-Type':'application/json'},
      body:JSON.stringify({path:baseName + '.html'})
    });
    if (!response.ok) throw new Error((await response.text()).trim() || 'Could not export presentation');
    const payload = await response.json();
    const blob = new Blob([payload.content], {type:'text/html;charset=utf-8'});
    if (openAfterExport) {
      const url = URL.createObjectURL(blob);
      if (presentationWindow) presentationWindow.location.replace(url);
      else window.open(url, '_blank', 'noopener');
      setTimeout(() => URL.revokeObjectURL(url), 60000);
      return;
    }
    await saveBlob(blob, baseName + '.html', [{
      description:'Keynope HTML presentation', accept:{'text/html':['.html']}
    }]);
    if (window.keynopeDidExport) window.keynopeDidExport();
  }

  async function autosaveDraft() {
    if (!dirty) return;
    try {
      const payload = await documentPayload(currentName);
      await writeDraft({markdown:payload.content, name:currentName, untitled:true, updated:Date.now()});
    } catch (_) {}
  }

  async function loadDocument(markdown, name, untitled) {
    await runtimeReady;
    const envelope = JSON.parse(window.keynopeWasmInit(markdown, name, untitled));
    if (envelope.status !== 200) throw new Error(envelope.body || 'Could not open presentation');
    currentName = name;
    documentFileHandle = null;
    const workspaceEnvelope = JSON.parse(window.keynopeWasmWorkspace());
    const workspace = JSON.parse(workspaceEnvelope.body);
    workspace.current = 0;
    if (window.keynopeReloadWebDocument) await window.keynopeReloadWebDocument(workspace);
    document.title = 'Keynope — ' + currentName + (untitled ? ' *' : '');
  }

  async function newPresentation() {
    if (dirty && !confirm('Discard the unsaved changes and create a new presentation?')) return;
    const response = await nativeFetch(new URL('Welcome.md', baseURL), {cache:'no-store'});
    await loadDocument(await response.text(), 'Untitled.md', true);
    dirty = true;
    await autosaveDraft();
  }

  function openPresentation() {
    if (dirty && !confirm('Discard the unsaved changes and open another presentation?')) return;
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.md,text/markdown,text/plain';
    input.onchange = async () => {
      const file = input.files && input.files[0];
      if (!file) return;
      try {
        await loadDocument(await file.text(), file.name, false);
        dirty = false;
        await clearDraft();
      } catch (error) {
        alert('Could not open presentation\n\n' + (error.message || error));
      }
    };
    input.click();
  }

  function addWebControls() {
    const topbar = document.querySelector('.keynope-editor-topbar');
    if (!topbar || topbar.querySelector('.keynope-web-controls')) return false;
    const controls = document.createElement('div');
    controls.className = 'keynope-web-controls';
    const newButton = document.createElement('button');
    newButton.type = 'button';
    newButton.textContent = 'NEW';
    newButton.title = 'New presentation';
    newButton.onclick = () => newPresentation().catch(error => alert(error.message || error));
    const openButton = document.createElement('button');
    openButton.type = 'button';
    openButton.textContent = 'OPEN';
    openButton.title = 'Open a Markdown presentation';
    openButton.onclick = openPresentation;
    controls.append(newButton, openButton);
    const save = topbar.querySelector('.keynope-save-button');
    topbar.insertBefore(controls, save ? save.nextSibling : topbar.firstChild);
    return true;
  }

  const presenter = {
    postMessage(message) {
      const action = message && message.action;
      if (action === 'editor-dirty-state') {
        dirty = !!message.dirty;
        document.title = 'Keynope — ' + currentName + (dirty ? ' *' : '');
        clearTimeout(draftTimer);
        if (dirty) draftTimer = setTimeout(autosaveDraft, 750);
      } else if (action === 'save-presentation') {
        savePresentation().catch(error => alert('Could not save presentation\n\n' + (error.message || error)));
      } else if (action === 'export-html') {
        exportHTML(false).catch(error => alert('Could not export presentation\n\n' + (error.message || error)));
      } else if (action === 'show-main') {
        const presentationWindow = window.open('', '_blank');
        if (presentationWindow) {
          presentationWindow.document.write('<title>Keynope is preparing your presentation…</title><body style="margin:0;display:grid;min-height:100vh;place-items:center;color:#f3efe0;background:#000;font:16px monospace">RENDERING…</body>');
        }
        exportHTML(true, presentationWindow).catch(error => {
          if (presentationWindow) presentationWindow.close();
          alert('Could not present\n\n' + (error.message || error));
        });
      } else if (action === 'show-about') {
        window.open('https://keynope.sh/', '_blank', 'noopener');
      } else if (action === 'query-display-state') {
        if (window.keynopeSetExternalDisplayAvailable) window.keynopeSetExternalDisplayAvailable(false);
      }
    }
  };
  window.webkit = window.webkit || {messageHandlers:{}};
  window.webkit.messageHandlers = window.webkit.messageHandlers || {};
  window.webkit.messageHandlers.keynopePresenter = presenter;

  addEventListener('beforeunload', event => {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
  });
  addEventListener('DOMContentLoaded', () => {
    addWebControls();
    const observer = new MutationObserver(() => {
      if (addWebControls()) observer.disconnect();
    });
    observer.observe(document.body, {childList:true, subtree:true});
    if ('serviceWorker' in navigator) navigator.serviceWorker.register(new URL('service-worker.js', baseURL)).catch(() => {});
  });
})();
