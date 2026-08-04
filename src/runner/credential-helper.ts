import { readFileSync } from 'node:fs';

// Git invokes this process only as a credential helper. The token is read from a
// 0600/ACL-restricted ephemeral file owned by Runner and is never exported to Codex.
const file=process.env.PB_GIT_CREDENTIAL_FILE;if(!file)process.exit(1);
const credential=JSON.parse(readFileSync(file,'utf8')) as {username:string;token:string};
const input=await new Promise<string>(resolve=>{let value='';process.stdin.setEncoding('utf8');process.stdin.on('data',c=>value+=c);process.stdin.on('end',()=>resolve(value))});
if(process.argv[2]==='get'&&input.includes('protocol=https'))process.stdout.write(`username=${credential.username}\npassword=${credential.token}\n`);
