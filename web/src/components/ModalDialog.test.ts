import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ModalDialog from './ModalDialog.vue'

describe('ModalDialog', () => {
  it('shows title when open and emits close', async () => {
    const w = mount(ModalDialog, {
      props: { open: true, title: 'Test Modal' },
      slots: { default: '<p>body content</p>' },
      attachTo: document.body,
    })
    await nextTick()
    // Teleport 渲染到 body，从 document 断言
    expect(document.body.textContent).toContain('Test Modal')
    expect(document.body.textContent).toContain('body content')
    const closeBtn = document.body.querySelector('button.dlg-x') as HTMLButtonElement | null
    expect(closeBtn).toBeTruthy()
    closeBtn!.click()
    await nextTick()
    expect(w.emitted('close')).toBeTruthy()
    w.unmount()
  })

  it('hides content when closed', async () => {
    const w = mount(ModalDialog, {
      props: { open: false, title: 'Hidden' },
      attachTo: document.body,
    })
    await nextTick()
    expect(document.body.textContent ?? '').not.toContain('Hidden')
    w.unmount()
  })
})
