export function registerPWA() {
  const viewport = window.visualViewport
  if (viewport) {
    let frame
    const sync = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        document.documentElement.style.setProperty('--aether-visible-height', `${viewport.height}px`)
        document.documentElement.style.setProperty('--aether-visible-top', `${viewport.offsetTop}px`)
      })
    }
    sync()
    viewport.addEventListener('resize', sync)
    viewport.addEventListener('scroll', sync)
  }
  if (!import.meta.env.PROD || !window.isSecureContext || !('serviceWorker' in navigator)) return
  const register = () => navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' }).catch(error => {
    console.warn('Aether PWA registration unavailable:', error.name)
  })
  if (document.readyState === 'complete') register()
  else window.addEventListener('load', register, { once: true })
}
