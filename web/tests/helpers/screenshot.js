export async function captureScreenshot(page, options) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    // Let layout settle, but still capture if WebKit pauses its frame clock.
    await page.evaluate(() => new Promise(resolve => {
      let first, second
      const finish = () => {
        clearTimeout(timer)
        cancelAnimationFrame(first)
        cancelAnimationFrame(second)
        resolve()
      }
      const timer = setTimeout(finish, 100)
      first = requestAnimationFrame(() => { second = requestAnimationFrame(finish) })
    }))
    try {
      return await page.screenshot({ ...options, animations: 'disabled', timeout: 5000 })
    } catch (error) {
      const transient = error.message?.includes('Protocol error (Page.captureScreenshot): Unable to capture screenshot')
      if (!transient || attempt === 3 || page.isClosed()) throw error
      console.warn(`Screenshot capture failed; retrying (${attempt}/2): ${error.message}`)
    }
  }
}
