(() => {
  'use strict';

  window.KEYNOPE_APP_SURFACE = true;
  window.KEYNOPE_WEB_EDITOR = true;
  const loading = document.createElement('div');
  loading.className = 'keynope-web-loading';
  const loadingLogo = document.createElement('pre');
  loadingLogo.className = 'keynope-web-loading-logo';
  loadingLogo.textContent = [
    ' ▐███▌  ██▌ █████████ ▐██  ▐██  ▐██▌   ███  ▐██████  ▐███████▌  █████████',
    ' ▝▀██▌  ██▌ ▀███▀▀▀▜█ ▐██  ▐██  ▐██▙▖  ███ ▄▟▛▀▀▀▜█▄ ▝▀██▛▀▀█▙▖ ▀███▀▀▀▜█',
    '   ██▌  ██▌  ███   ▐█ ▐██  ▐██  ▐███▌  ███ ██▌   ▐██   ██▌  ██▌  ███   ▐█',
    '   ██▌▐██▌   ███ █▌   ▐██  ▐██  ▐█████ ███ ██▌   ▐██   ██▌  ██▌  ███ █▌',
    '   █████     █████▌    ▐█████   ▐██▌▐█████ ██▌   ▐██   ██████▌   █████▌',
    '   ██▛▜█▄▖   ███▀█▌    ▝▀██▛▀   ▐██▌▝▀████ ██▌   ▐██   ██▛▀▀▀▘   ███▀█▌',
    '   ██▌▝▀█▙▖  ███ ▀▘▗▄    ██▌    ▐██▌  ▀███ ██▌   ▐██   ██▌       ███ ▀▘▗▄',
    '   ██▌  ██▌  ███   ▐█    ██▌    ▐██▌   ███ ██▌   ▐██   ██▌       ███   ▐█',
    ' ▐███▌  ██▌ █████████  ▐█████   ▐██▌   ███  ▐██████  ▐████▌     █████████',
    ' ▝▀▀▀▘  ▀▀▘ ▀▀▀▀▀▀▀▀▀  ▝▀▀▀▀▀   ▝▀▀▘   ▀▀▀  ▝▀▀▀▀▀▀  ▝▀▀▀▀▘     ▀▀▀▀▀▀▀▀▀'
  ].join('\n');
  loading.innerHTML = '<pre class="keynope-web-loading-bar" aria-hidden="true"></pre><div class="keynope-web-loading-label"></div>';
  loading.prepend(loadingLogo);
  loading.setAttribute('role', 'status');
  loading.setAttribute('aria-live', 'polite');
  document.body.appendChild(loading);
  const loadingBar = loading.querySelector('.keynope-web-loading-bar');
  const loadingLabel = loading.querySelector('.keynope-web-loading-label');
  function setLoadingProgress(value, label) {
    const progress = Math.max(0, Math.min(20, Math.round(value)));
    loadingBar.textContent = '█'.repeat(progress) + '░'.repeat(20 - progress);
    loadingLabel.textContent = label;
  }
  setLoadingProgress(1, 'STARTING');
  const nativeFetch = window.fetch.bind(window);
  const baseURL = new URL('./', document.currentScript.src);
  const databaseName = 'keynope-web-editor';
  const draftKey = 'current';
  let dirty = false;
  let draftTimer = 0;
  let draftRevision = 0;
  let documentFileHandle = null;
  let currentName = 'Untitled.md';
  let runtimeReady;
  let workspaceLoaded = false;
  function finishLoading() {
    if (!loading.isConnected) return;
    setLoadingProgress(20, 'READY');
    loading.classList.add('ready');
    setTimeout(() => loading.remove(), 220);
  }

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

  function cancelled(error) {
    return !!error && (error.name === 'AbortError' || /cancelled|canceled|user aborted/i.test(error.message || ''));
  }

  function reportFailure(prefix, error) {
    if (cancelled(error)) return;
    alert(prefix + '\n\n' + (error && error.message || error));
  }

  function responseFromEnvelope(raw) {
    const envelope = typeof raw === 'string' ? JSON.parse(raw) : raw;
    return new Response(envelope.body || '', {
      status: envelope.status || 500,
      headers: {'Content-Type': envelope.contentType || 'text/plain; charset=utf-8'}
    });
  }

  async function loadScript(source) {
    setLoadingProgress(2, 'LOADING RUNTIME');
    await new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = source;
      script.onload = resolve;
      script.onerror = () => reject(new Error('Could not load the Keynope runtime'));
      document.head.appendChild(script);
    });
  }

  async function waitForRuntime() {
    setLoadingProgress(16, 'STARTING EDITOR');
    for (let attempt = 0; attempt < 500 && !window.keynopeWasmReady; attempt++) {
      await new Promise(resolve => setTimeout(resolve, 10));
    }
    if (!window.keynopeWasmReady) throw new Error('Keynope runtime did not start');
  }

  async function initialDocument() {
    setLoadingProgress(18, 'LOADING PRESENTATION');
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
    setLoadingProgress(4, 'LOADING EDITOR');
    const response = await nativeFetch(wasmURL);
    if (!response.ok) throw new Error('Could not load the Keynope editor');
    const total = Number(response.headers.get('Content-Length')) || 0;
    let bytes;
    if (response.body && total > 0) {
      const reader = response.body.getReader();
      const chunks = [];
      let loaded = 0;
      while (true) {
        const part = await reader.read();
        if (part.done) break;
        chunks.push(part.value);
        loaded += part.value.byteLength;
        const percent = Math.min(100, Math.round(loaded * 100 / total));
        setLoadingProgress(4 + Math.floor(percent * 10 / 100), 'LOADING EDITOR · ' + percent + '%');
      }
      bytes = new Uint8Array(loaded);
      let offset = 0;
      for (const chunk of chunks) {
        bytes.set(chunk, offset);
        offset += chunk.byteLength;
      }
    } else {
      bytes = new Uint8Array(await response.arrayBuffer());
    }
    setLoadingProgress(14, 'COMPILING EDITOR');
    const result = await WebAssembly.instantiate(bytes, go.importObject);
    go.run(result.instance);
    await waitForRuntime();
    const initial = await initialDocument();
    currentName = initial.name || 'Untitled.md';
    const envelope = JSON.parse(window.keynopeWasmInit(initial.markdown, currentName, initial.untitled !== false));
    if (envelope.status !== 200) throw new Error(envelope.body || 'Could not initialize Keynope');
    setLoadingProgress(19, 'DRAWING WORKSPACE');
  }

  async function refreshWorkspace(state) {
    const envelope = JSON.parse(window.keynopeWasmWorkspace());
    if (envelope.status !== 200) return;
    const workspace = JSON.parse(envelope.body);
    if (state && Number.isInteger(state.current)) workspace.current = state.current;
    if (window.keynopeLoadWebWorkspace) await window.keynopeLoadWebWorkspace(workspace);
    workspaceLoaded = true;
    finishLoading();
  }

  const stateOnlyEditorActions = new Set([
    'select-element',
    'update-slide-notes',
    'confirm-save',
    'start-timer',
    'stop-timer'
  ]);

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
    let refresh = target.pathname === '/api/editor/upload' ||
      (target.pathname === '/api/editor/state' && !workspaceLoaded);
    if (target.pathname === '/api/editor/action') {
      try {
        const action = JSON.parse(typeof init.body === 'string' ? init.body : '{}').action || '';
        refresh = !stateOnlyEditorActions.has(action);
      } catch (_) {
        refresh = true;
      }
    }
    if (envelope.status >= 200 && envelope.status < 300 && refresh) {
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
    draftRevision++;
    clearTimeout(draftTimer);
    await clearDraft();
    document.title = 'Keynope — ' + currentName;
    const savedState = await response.json();
    if (window.keynopeDidSave) window.keynopeDidSave(savedState);
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

  async function autosaveDraft(revision = draftRevision) {
    if (!dirty || revision !== draftRevision) return;
    try {
      const payload = await documentPayload(currentName);
      if (!dirty || revision !== draftRevision) return;
      await writeDraft({markdown:payload.content, name:currentName, untitled:true, updated:Date.now()});
    } catch (_) {}
  }

  async function loadDocument(markdown, name, untitled) {
    await runtimeReady;
    draftRevision++;
    clearTimeout(draftTimer);
    const envelope = JSON.parse(window.keynopeWasmInit(markdown, name, untitled));
    if (envelope.status !== 200) throw new Error(envelope.body || 'Could not open presentation');
    currentName = name;
    documentFileHandle = null;
    workspaceLoaded = false;
    const workspaceEnvelope = JSON.parse(window.keynopeWasmWorkspace());
    const workspace = JSON.parse(workspaceEnvelope.body);
    workspace.current = 0;
    if (window.keynopeReloadWebDocument) await window.keynopeReloadWebDocument(workspace);
    workspaceLoaded = true;
    document.title = 'Keynope — ' + currentName + (untitled ? ' *' : '');
  }

  async function newPresentation() {
    if (dirty && !confirm('Discard the unsaved changes and create a new presentation?')) return;
    const response = await nativeFetch(new URL('Welcome.md', baseURL), {cache:'no-store'});
    await loadDocument(await response.text(), 'Untitled.md', true);
    dirty = true;
    const revision = ++draftRevision;
    await autosaveDraft(revision);
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
        draftRevision++;
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
    newButton.textContent = 'New';
    newButton.title = 'New presentation';
    newButton.setAttribute('aria-label', newButton.title);
    newButton.onclick = () => newPresentation().catch(error => alert(error.message || error));
    const openButton = document.createElement('button');
    openButton.type = 'button';
    openButton.textContent = 'Open';
    openButton.title = 'Open a Markdown presentation';
    openButton.setAttribute('aria-label', openButton.title);
    openButton.onclick = openPresentation;
    const mainMode = topbar.querySelector('.keynope-topbar-mode');
    const addSlide = mainMode && mainMode.querySelector('button');
    const save = topbar.querySelector('.keynope-save-button');
    const importImage = topbar.querySelector('button[aria-label="Import image"]');
    if (addSlide) controls.appendChild(addSlide);
    controls.append(newButton, openButton);
    if (save) controls.appendChild(save);
    if (importImage) controls.appendChild(importImage);
    topbar.insertBefore(controls, topbar.firstChild);
    return true;
  }

  async function showAbout() {
    const existing = document.querySelector('.keynope-web-about');
    if (existing) {
      existing.remove();
      return;
    }
    const blocker = document.createElement('div');
    blocker.className = 'keynope-web-about';
    const dialog = document.createElement('section');
    dialog.className = 'keynope-web-about-dialog';
    dialog.setAttribute('role', 'dialog');
    dialog.setAttribute('aria-modal', 'true');
    dialog.setAttribute('aria-labelledby', 'keynope-web-about-title');
    dialog.innerHTML = [
      '<button class="keynope-web-about-close" type="button" aria-label="Close">×</button>',
      '<img src="/keynope-logo.png" alt="">',
      '<div class="keynope-web-about-heading"><h2 id="keynope-web-about-title">KEYNOPE</h2><p>Version __KEYNOPE_VERSION__ · Web Editor Beta</p></div>',
      '<nav><a href="https://keynope.sh/" target="_blank" rel="noopener">🌐 keynope.sh</a><a href="https://github.com/keynope/" target="_blank" rel="noopener">GitHub</a></nav>',
      '<p class="keynope-web-about-credit">© 2026 Dennis Vink · <a href="https://drvink.com" target="_blank" rel="noopener">drvink.com</a> · <a href="https://linkedin.com/in/drvink/" target="_blank" rel="noopener">LinkedIn</a></p>',
      '<details><summary>Open-source licenses</summary><pre>Loading licenses…</pre></details>'
    ].join('');
    blocker.appendChild(dialog);
    document.body.appendChild(blocker);
    let escape;
    const close = () => {
      if (escape) removeEventListener('keydown', escape, true);
      blocker.remove();
    };
    dialog.querySelector('.keynope-web-about-close').onclick = close;
    blocker.addEventListener('pointerdown', event => {
      if (event.target === blocker) close();
    });
    escape = event => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      close();
    };
    addEventListener('keydown', escape, true);
    try {
      const response = await nativeFetch(new URL('licenses.txt', baseURL));
      dialog.querySelector('pre').textContent = response.ok ? await response.text() : 'License information is unavailable.';
    } catch (_) {
      dialog.querySelector('pre').textContent = 'License information is unavailable.';
    }
  }

  const presenter = {
    postMessage(message) {
      const action = message && message.action;
      if (action === 'editor-dirty-state') {
        dirty = !!message.dirty;
        document.title = 'Keynope — ' + currentName + (dirty ? ' *' : '');
        clearTimeout(draftTimer);
        const revision = ++draftRevision;
        if (dirty) draftTimer = setTimeout(() => autosaveDraft(revision), 250);
      } else if (action === 'save-presentation') {
        savePresentation().catch(error => reportFailure('Could not save presentation', error));
      } else if (action === 'export-html') {
        exportHTML(false).catch(error => reportFailure('Could not export presentation', error));
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
        showAbout();
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
    autosaveDraft(draftRevision);
    event.preventDefault();
    event.returnValue = '';
  });
  addEventListener('pagehide', () => {
    if (dirty) autosaveDraft(draftRevision);
  });
  addEventListener('visibilitychange', () => {
    if (dirty && document.visibilityState === 'hidden') autosaveDraft(draftRevision);
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
