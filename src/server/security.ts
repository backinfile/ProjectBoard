import argon2 from 'argon2';
import { createHash, randomBytes, timingSafeEqual } from 'node:crypto';
import sanitizeHtml from 'sanitize-html';
import { marked } from 'marked';

export const hashOpaque = (value: string) => createHash('sha256').update(value).digest('hex');
export const randomToken = (bytes = 32) => randomBytes(bytes).toString('base64url');
export const hashPassword = (value: string) => argon2.hash(value, { type: argon2.argon2id, memoryCost: 19456, timeCost: 2, parallelism: 1 });
export const verifyPassword = (hash: string, value: string) => argon2.verify(hash, value);
export function safeEqual(a: string, b: string) { const aa = Buffer.from(a); const bb = Buffer.from(b); return aa.length === bb.length && timingSafeEqual(aa, bb); }
export function renderMarkdown(markdown: string) {
  const protocolClean = markdown.replace(/<[^>]*>/g, '').replace(/\b(?:javascript|vbscript|data):/gi, '');
  return sanitizeHtml(marked.parse(protocolClean, { async: false }) as string, {
    allowedTags: ['p','br','strong','em','del','blockquote','code','pre','ul','ol','li','h1','h2','h3','h4','hr','a','table','thead','tbody','tr','th','td'],
    allowedAttributes: { a: ['href','title','rel'] }, allowedSchemes: ['http','https','mailto'],
    transformTags: { a: (_tag, attrs) => ({ tagName: 'a', attribs: { ...attrs, rel: 'noopener noreferrer' } }) },
  });
}
const sensitive = /token|secret|password|authorization|cookie|private.?key|credential/i;
export function redact(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redact);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k,v]) => [k, sensitive.test(k) ? '[REDACTED]' : redact(v)]));
  if (typeof value === 'string') return value.replace(/(Bearer\s+)[A-Za-z0-9._~-]+/gi, '$1[REDACTED]');
  return value;
}
export function validatePassword(value: string) {
  return value.length >= 12 && value.length <= 256 && /[A-Za-z]/.test(value) && /\d/.test(value);
}
