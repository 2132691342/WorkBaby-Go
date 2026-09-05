import type { Component } from 'vue'
import { FileText, FolderOpen, Globe } from '@/components/common/icons'

/**
 * 内置工作流模板：面向小白用户的「一键起步」。
 *
 * graph 为持久化 Graph JSON（与服务端 /api/v1/workflows 的 graph 字段同构）。
 * llm 节点的 providerID/model 留空 —— 创建后在画布上点节点，从下拉选择即可
 * （属性面板的 provider/model 均为动态下拉）。
 */
export interface WorkflowTemplate {
  id: string
  nameKey: string
  descKey: string
  icon: Component
  graph: string
}

const json = (v: unknown): string => JSON.stringify(v, null, 2)

export const WORKFLOW_TEMPLATES: WorkflowTemplate[] = [
  {
    id: 'writer',
    nameKey: 'workflow.tpl.writer',
    descKey: 'workflow.tpl.writerDesc',
    icon: FileText,
    graph: json({
      name: '',
      inputs: {},
      nodes: [
        {
          id: 'writer',
          type: 'llm',
          deps: [],
          config: {
            providerID: '',
            model: '',
            systemPrompt: '你是一位专业的中文写手，输出结构清晰、语言自然。',
            userPromptTemplate: '请围绕下面的主题写一段 300 字左右的内容：',
            temperature: 0.7
          },
          inputs: {}
        }
      ],
      outputs: { text: 'writer.text' }
    })
  },
  {
    id: 'web-summary',
    nameKey: 'workflow.tpl.webSummary',
    descKey: 'workflow.tpl.webSummaryDesc',
    icon: Globe,
    graph: json({
      name: '',
      inputs: {},
      nodes: [
        {
          id: 'fetch',
          type: 'http',
          deps: [],
          config: { method: 'GET', url: 'https://example.com', headers: {}, body: '' },
          inputs: {}
        },
        {
          id: 'summarize',
          type: 'llm',
          deps: ['fetch'],
          config: {
            providerID: '',
            model: '',
            systemPrompt: '你是摘要助手，用中文输出不超过 3 句话的摘要。',
            userPromptTemplate: '请总结以下网页内容：\n{{#fetch.body#}}'
          },
          inputs: {}
        }
      ],
      outputs: { summary: 'summarize.text' }
    })
  },
  {
    id: 'folder-scan',
    nameKey: 'workflow.tpl.folderScan',
    descKey: 'workflow.tpl.folderScanDesc',
    icon: FolderOpen,
    graph: json({
      name: '',
      inputs: {},
      nodes: [
        {
          id: 'scan',
          type: 'tool',
          deps: [],
          config: { toolName: 'file_list', args: {} },
          inputs: {}
        },
        {
          id: 'read',
          type: 'llm',
          deps: ['scan'],
          config: {
            providerID: '',
            model: '',
            systemPrompt: '你是文件管理员，用中文简要解读目录内容。',
            userPromptTemplate: '这是当前工作区的文件列表，请说明里面有什么：\n{{#scan.content#}}'
          },
          inputs: {}
        }
      ],
      outputs: { summary: 'read.text' }
    })
  }
]
