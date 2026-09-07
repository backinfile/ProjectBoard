// Reproducible local acceptance: exact argv avoids PowerShell's dotted-option parsing.
const fs=require('node:fs'),path=require('node:path'),{spawn}=require('node:child_process');
const out=path.resolve(process.env.PB_TEST_OUTPUT||'test-output/full');
fs.mkdirSync(out,{recursive:true});
// Clear only generated result files so an interrupted run cannot report older passes.
for(const name of ['blackbox.json','browser.json','frontend.json','frozen-preview.json','standalone.json','run.json']) {
 const file=path.join(out,name);
 if(fs.existsSync(file)){fs.copyFileSync(file,file+'.previous');fs.unlinkSync(file);}
}
const runtime=path.join(out,'runtime-coverage-'+Date.now());fs.mkdirSync(runtime);
const binary=path.resolve('dist/projectboard-test.exe'),steps=[];
async function run(id,command,args,stdoutName,env={}){
 const start=Date.now(),stdout=fs.createWriteStream(path.join(out,stdoutName||id+'.log')),stderr=fs.createWriteStream(path.join(out,id+'-stderr.log'));
 const p=spawn(command,args,{windowsHide:true,env:{...process.env,PB_TEST_OUTPUT:out,...env}});
 p.stdout.pipe(stdout);p.stderr.pipe(stderr);
 const code=await new Promise(resolve=>{p.on('error',e=>{stderr.write(String(e));resolve(-1)});p.on('close',resolve)});
 stdout.end();stderr.end();steps.push({id,command,args,exitCode:code,ms:Date.now()-start});console.log(id+': '+(code===0?'PASS':'FAIL'));
 return code===0;
}
(async()=>{
 await run('build-web',process.execPath,['scripts/build-web.cjs']);
 await run('vet','go',['vet','./...']);
 await run('whitebox','go',['test','./...','-count=1','-json','-coverprofile='+path.join(out,'coverage.out')],'go-tests.jsonl');
 await run('coverage','go',['tool','cover','-func='+path.join(out,'coverage.out')],'coverage.txt');
 await run('coverage-html','go',['tool','cover','-html='+path.join(out,'coverage.out'),'-o',path.join(out,'coverage.html')]);
 await run('frontend',process.execPath,['web/check.cjs']);
 await run('frozen-preview',process.execPath,['preview/check.cjs']);
 if(await run('build','go',['build','-cover','-coverpkg=./...','-o',binary,'./cmd/projectboard'])){
  const env={GOCOVERDIR:runtime};
  await run('blackbox',process.execPath,['scripts/blackbox.cjs',binary],undefined,env);
  await run('browser',process.execPath,['scripts/browser-test.cjs',binary],undefined,env);
  await run('standalone',process.execPath,['scripts/smoke.cjs',binary],undefined,env);
  await run('runtime-covdata','go',['tool','covdata','textfmt','-i='+runtime,'-o='+path.join(out,'runtime-coverage.out')]);
  await run('runtime-coverage','go',['tool','cover','-func='+path.join(out,'runtime-coverage.out')],'runtime-coverage.txt');
 }
 for(const [source,name] of [['test-output/web-tests.json','frontend.json'],['preview/test-results.json','frozen-preview.json'],['test-output/smoke.json','standalone.json']])if(fs.existsSync(source))fs.copyFileSync(source,path.join(out,name));
 fs.writeFileSync(path.join(out,'run.json'),JSON.stringify({at:new Date().toISOString(),platform:process.platform,arch:process.arch,node:process.version,steps},null,2));
 await run('report',process.execPath,['scripts/test-report.cjs']);
 if(steps.some(s=>s.exitCode!==0))process.exitCode=1;
})().catch(e=>{console.error(e);process.exitCode=1});
