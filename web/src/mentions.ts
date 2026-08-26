import type { AgentProfile } from './types'

export interface MentionResult { agents:AgentProfile[]; prompt:string }

export function parseAgentMentions(input:string, profiles:AgentProfile[]):MentionResult{
  const found:AgentProfile[]=[]
  let prompt=input
  const sorted=[...profiles].sort((a,b)=>b.name.length-a.name.length)
  for(const profile of sorted){
    const token=`@${profile.name}`
    if(prompt.includes(token) && !found.some(a=>a.id===profile.id)){
      found.push(profile)
      prompt=prompt.split(token).join(' ')
    }
  }
  return { agents:found, prompt:prompt.replace(/\s+/g,' ').trim() }
}
