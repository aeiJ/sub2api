import { beforeEach, describe, expect, it } from 'vitest'

import { applyThemeClass, shouldUseDarkTheme } from '../theme'

describe('theme defaults', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('keeps explicit light preference', () => {
    localStorage.setItem('theme', 'light')

    expect(shouldUseDarkTheme()).toBe(false)
    expect(applyThemeClass()).toBe(false)
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('uses dark by default without saved preference', () => {
    expect(shouldUseDarkTheme()).toBe(true)
    expect(applyThemeClass()).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('uses dark when dark preference is saved', () => {
    localStorage.setItem('theme', 'dark')

    expect(shouldUseDarkTheme()).toBe(true)
    expect(applyThemeClass()).toBe(true)
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
