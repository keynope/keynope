// Current-slide documents travel as bounded gzip chunks, never canvas images.
globalThis.KeynopePresentationTransfer = (() => {
  // Signed public MESSAGE bodies are limited to 32768 characters. Leave room
  // for the protocol/session envelope as well as the part metadata.
  const chunkSize=30000, maxParts=64, maxBytes=16*1024*1024;
  const hex=bytes=>Array.from(new Uint8Array(bytes),b=>b.toString(16).padStart(2,'0')).join('');
  async function pack(markdown) {
    const bytes=new TextEncoder().encode(markdown);
    if(bytes.length>maxBytes)throw new Error('This slide is too large to share (16 MB limit).');
    const zipped=new Uint8Array(await new Response(new Blob([bytes]).stream().pipeThrough(new CompressionStream('gzip'))).arrayBuffer());
    let binary='';for(let i=0;i<zipped.length;i+=8192)binary+=String.fromCharCode(...zipped.subarray(i,i+8192));
    const encoded=btoa(binary),parts=[];
    for(let i=0;i<encoded.length;i+=chunkSize)parts.push(encoded.slice(i,i+chunkSize));
    if(parts.length>maxParts)throw new Error('This slide has too much embedded media to share.');
    return {id:hex(await crypto.subtle.digest('SHA-256',bytes)),parts};
  }
  async function unpack(parts,id) {
    const encoded=parts.join('');
    if(parts.length>maxParts||encoded.length>maxParts*chunkSize)throw new Error('Invalid slide transfer');
    const bytes=Uint8Array.from(atob(encoded),c=>c.charCodeAt(0));
    const reader=new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip')).getReader();
    const chunks=[];let length=0;
    for(;;){const {value,done}=await reader.read();if(done)break;length+=value.length;if(length>maxBytes){await reader.cancel();throw new Error('Slide exceeds size limit');}chunks.push(value);}
    const data=new Uint8Array(length);let offset=0;for(const chunk of chunks){data.set(chunk,offset);offset+=chunk.length;}
    if(hex(await crypto.subtle.digest('SHA-256',data))!==id)throw new Error('Incomplete slide transfer');
    return new TextDecoder().decode(data);
  }
  return {pack,unpack,chunkSize,maxParts};
})();
