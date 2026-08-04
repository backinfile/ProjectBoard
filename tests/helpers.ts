import { Db } from '../src/server/db.js';
import type { Actor } from '../src/shared/types.js';
import { rmSync } from 'node:fs';
import { resolve } from 'node:path';

export function fixture(name:string){const path=resolve(`data/test-${name}-${crypto.randomUUID()}.db`);const db=new Db(path);const now=db.now();const admin='u-admin',developer='u-dev',viewer='u-view',project='p-main',agent='a-one',agent2='a-two';
 db.run("INSERT INTO users VALUES(?,?,?,?, 'administrator','active',0,NULL,?,?,NULL)",admin,'admin','Admin','hash',now,now);
 db.run("INSERT INTO users VALUES(?,?,?,?, 'user','active',0,NULL,?,?,NULL)",developer,'dev','Developer','hash',now,now);
 db.run("INSERT INTO users VALUES(?,?,?,?, 'user','active',0,NULL,?,?,NULL)",viewer,'viewer','Viewer','hash',now,now);
 db.run("INSERT INTO projects(id,project_key,name,repository_url,allowed_target_branches_json,validation_commands_json,forbidden_paths_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)",project,'pb','ProjectBoard','https://github.com/acme/projectboard.git',JSON.stringify(['main','develop','release/*']),JSON.stringify([{name:'test',command:'pnpm test',required:true,timeoutMs:60_000}]),JSON.stringify(['infra/production/**']),now,now);
 db.run("INSERT INTO project_memberships VALUES(?,?, 'developer',?)",project,developer,now);db.run("INSERT INTO project_memberships VALUES(?,?, 'viewer',?)",project,viewer,now);
 db.run("INSERT INTO agents VALUES(?,?,?,'active',?,NULL)",agent,'runner-one','tests',now);db.run("INSERT INTO agents VALUES(?,?,?,'active',?,NULL)",agent2,'runner-two','acceptance',now);db.run('INSERT INTO agent_project_grants VALUES(?,?,?)',agent,project,now);db.run('INSERT INTO agent_project_grants VALUES(?,?,?)',agent2,project,now);
 return{db,path,ids:{admin,developer,viewer,project,agent,agent2},actors:{admin:{type:'human',id:admin} as Actor,developer:{type:'human',id:developer} as Actor,viewer:{type:'human',id:viewer} as Actor,agent:{type:'agent',id:agent} as Actor,agent2:{type:'agent',id:agent2} as Actor},cleanup(){db.close();for(const suffix of ['','-wal','-shm'])rmSync(path+suffix,{force:true})}}}
