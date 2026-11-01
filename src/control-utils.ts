// LAN HTTP is not a secure context: randomUUID() is unavailable there.
// getRandomValues remains available, so task retry keys work on the router URL.
export function makeTaskID():string {
 return Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
}
