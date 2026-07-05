export function shouldUseDarkTheme(): boolean {
  const savedTheme = localStorage.getItem('theme')
  return savedTheme !== 'light'
}

export function applyThemeClass(): boolean {
  const useDark = shouldUseDarkTheme()
  document.documentElement.classList.toggle('dark', useDark)
  return useDark
}
