/** i18n 消息模块共享类型。 */
export type Locale = 'zh-CN' | 'en-US'

/** 单个命名空间模块的双语字典。 */
export interface MessageBundle {
  'zh-CN': Record<string, string>
  'en-US': Record<string, string>
}
