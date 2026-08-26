import { describe, expect, it } from 'vitest'
import { parseAgentMentions } from './mentions'
import type { AgentProfile } from './types'

const agents=[
  {id:'a',name:'Codex · 自动实现'},
  {id:'b',name:'Codex · 安全分析'},
] as AgentProfile[]

describe('parseAgentMentions',()=>{
  it('routes one instruction to every uniquely mentioned agent',()=>{
    const result=parseAgentMentions('@Codex · 自动实现 @Codex · 安全分析 检查登录流程',agents)
    expect(result.agents.map(a=>a.id)).toEqual(['a','b'])
    expect(result.prompt).toBe('检查登录流程')
  })
  it('leaves ordinary messages for the current agent',()=>{
    expect(parseAgentMentions('继续并运行测试',agents)).toEqual({agents:[],prompt:'继续并运行测试'})
  })
})
