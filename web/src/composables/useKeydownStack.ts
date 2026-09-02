import { onBeforeUnmount, onMounted, watch, type Ref, type WatchSource } from 'vue'

type KeydownHandler = (e: KeyboardEvent) => void

/** LIFO stack: only the topmost registered handler receives keydown. */
const stack: KeydownHandler[] = []
let listening = false

function dispatch(e: KeyboardEvent) {
  const top = stack[stack.length - 1]
  top?.(e)
}

function ensureListen() {
  if (listening) return
  window.addEventListener('keydown', dispatch)
  listening = true
}

function maybeUnlisten() {
  if (stack.length || !listening) return
  window.removeEventListener('keydown', dispatch)
  listening = false
}

/** Push a handler onto the stack; returns a pop function. */
export function pushKeydown(handler: KeydownHandler): () => void {
  stack.push(handler)
  ensureListen()
  return () => {
    const i = stack.lastIndexOf(handler)
    if (i >= 0) stack.splice(i, 1)
    maybeUnlisten()
  }
}

/** Whether this handler is currently the topmost (for Enter etc.). */
export function isTopKeydown(handler: KeydownHandler): boolean {
  return stack.length > 0 && stack[stack.length - 1] === handler
}

/**
 * Register a window keydown handler in a LIFO stack so only the topmost
 * dialog receives Escape/Enter. Push while active (or on mount), pop on deactivate/unmount.
 */
export function useKeydownStack(handler: KeydownHandler, active?: WatchSource<boolean> | Ref<boolean>) {
  let pop: (() => void) | undefined

  function activate() {
    if (pop) return
    pop = pushKeydown(handler)
  }

  function deactivate() {
    pop?.()
    pop = undefined
  }

  if (active !== undefined) {
    watch(
      active,
      (on) => {
        if (on) activate()
        else deactivate()
      },
      { immediate: true },
    )
    onBeforeUnmount(deactivate)
  } else {
    onMounted(activate)
    onBeforeUnmount(deactivate)
  }
}
