let csrf=sessionStorage.getItem('pb_csrf')??'';
const csrfCookie=()=>document.cookie.split('; ').find(value=>value.startsWith('pb_csrf='))?.slice('pb_csrf='.length)??'';
export function setCsrf(value:string){csrf=value;sessionStorage.setItem('pb_csrf',value)}
export async function api<T=any>(path:string,init:RequestInit={}):Promise<T>{const token=decodeURIComponent(csrfCookie())||csrf;const response=await fetch(path,{...init,credentials:'same-origin',headers:{...(init.body instanceof Blob?{}:{'content-type':'application/json'}),...(token?{'x-csrf-token':token}:{}),...(init.headers??{})}});const data=await response.json().catch(()=>({}));if(!response.ok)throw new Error(data.error?.message??response.statusText);return data}
export const requestId=()=>crypto.randomUUID();
