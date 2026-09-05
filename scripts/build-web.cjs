const fs=require('node:fs');
const crypto=require('node:crypto');
const baseline=fs.readFileSync('preview/frozen-20260905.html');
if(crypto.createHash('sha256').update(baseline).digest('hex').toUpperCase()!==fs.readFileSync('preview/frozen-20260905.sha256','utf8').trim())throw Error('Frozen preview checksum mismatch');
fs.mkdirSync('web/assets',{recursive:true});
for(const name of ['index.html','app.js','app.css'])fs.copyFileSync('web/src/'+name,'web/assets/'+name);
console.log('Web assets ready');
