import { t } from './i18n'

/** 可切换的对象存储类型（S3 标准枚举的常用子集）。 */
export const STORAGE_CLASS_VALUES = [
  'STANDARD',
  'STANDARD_IA',
  'ONEZONE_IA',
  'INTELLIGENT_TIERING',
  'GLACIER_IR',
  'GLACIER',
  'DEEP_ARCHIVE',
  'REDUCED_REDUNDANCY',
  'EXPRESS_ONEZONE',
] as const

/** 带本地化标签的选项列表（依赖当前 locale）。 */
export function storageClassOptions(): { value: string; label: string }[] {
  return STORAGE_CLASS_VALUES.map((value) => ({
    value,
    label: storageClassLabel(value),
  }))
}

/** 存储类型简要展示名；未识别时原样返回。 */
export function storageClassLabel(v: string): string {
  const key = `storage.class.${v}` as const
  const translated = t(key)
  return translated === key ? v : translated
}
