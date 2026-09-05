const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const root = __dirname;
const port = Number(process.env.PORT || 4173);
http.createServer((req,res) => {
  const route = new URL(req.url, 'http://localhost').pathname;
  if (route === '/health') { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({status:'ok',mode:'preview'})); return; }
  if (route !== '/' && route !== '/index.html') {res.writeHead(404, {'Content-Type':'text/plain'}); res.end('Not found'); return;}
  res.writeHead(200, {'Content-Type':'text/html; charset=utf-8','Cache-Control':'no-store'});
  fs.createReadStream(path.join(root,'index.html')).pipe(res);
}).listen(port,'127.0.0.1',()=>process.stdout.write(`ProjectBoard preview: http://127.0.0.1:${port}\n`));
