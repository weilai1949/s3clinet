/** 预览类型判断（按扩展名）。安全原则：可执行内容（HTML/SVG/JS）绝不渲染，仅文本化或沙箱展示。 */

export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'none'

const IMG_RE = /\.(png|jpe?g|gif|webp|svg|bmp|avif|heic|ico)$/i
const VIDEO_RE = /\.(mp4|webm|ogv|mov|m4v|mkv|avi)$/i
const AUDIO_RE = /\.(mp3|wav|ogg|oga|m4a|flac|aac|opus)$/i
// 文本/代码类。注意：HTML/XML/SVG 源码也按纯文本展示（转义），绝不渲染。
const TEXT_RE = /\.(txt|md|markdown|json|jsonl|csv|tsv|log|ini|conf|cfg|toml|yaml|yml|xml|html?|css|sh|bash|zsh|fish|py|js|mjs|cjs|ts|tsx|jsx|go|java|c|cpp|cc|h|hpp|rs|rb|php|swift|kt|kts|sql|r|pl|lua|scala|vue|svelte|env|gitignore|dockerfile|makefile|properties|sqlite|diff|patch|nfo|license)$/i

export function previewKind(key: string): PreviewKind {
  if (IMG_RE.test(key)) return 'image'
  if (VIDEO_RE.test(key)) return 'video'
  if (AUDIO_RE.test(key)) return 'audio'
  if (/\.pdf$/i.test(key)) return 'pdf'
  if (TEXT_RE.test(key)) return 'text'
  return 'none'
}
