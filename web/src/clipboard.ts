/** 剪贴板工具：优先 Clipboard API，降级 textarea + execCommand。 */

export function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    return navigator.clipboard.writeText(text).catch(() => fallbackCopy(text))
  }
  return Promise.resolve(fallbackCopy(text))
}

function fallbackCopy(text: string): void {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try {
    document.execCommand('copy')
  } finally {
    document.body.removeChild(ta)
  }
}
