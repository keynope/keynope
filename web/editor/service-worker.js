// Content fingerprint injected by build_web_editor.sh; no manual version bumps.
const BUILD = '__KEYNOPE_BUILD__';
const CACHE = 'keynope-editor-' + BUILD;
const ASSETS = [
  '/editor/',
  '/editor/editor.js',
  '/editor/editor.css',
  '/editor/Welcome.md',
  '/editor/licenses.txt',
  '/editor/wasm_exec.js',
  '/editor/keynope-editor.wasm'
].map(path => path === '/editor/' ? path : path + '?build=' + BUILD);

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.addAll(ASSETS.map(url => new Request(url, {cache:'reload'})))));
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key.startsWith('keynope-editor-') && key !== CACHE).map(key => caches.delete(key)))));
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  const request = event.request, url = new URL(request.url);
  // Never cache API responses, participant state, or unrelated origins.
  if (request.method !== 'GET' || url.origin !== self.location.origin || !url.pathname.startsWith('/editor/')) return;
  event.respondWith((async () => {
    const cache = await caches.open(CACHE);
    const cached = await cache.match(request);
    // Versioned assets belong to the same build as the HTML that loaded them.
    // Unversioned requests (especially HTML) must check for updates first.
    if (cached && url.searchParams.has('build') && request.cache !== 'no-store' && request.cache !== 'reload') return cached;
    try {
      const response = await fetch(request, {cache:'no-cache'});
      if (response.ok && request.cache !== 'no-store') await cache.put(request, response.clone());
      return response;
    } catch (error) {
      if (cached) return cached; // Keep the editor usable offline.
      throw error;
    }
  })());
});
