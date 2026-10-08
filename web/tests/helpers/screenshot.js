export async function captureScreenshot(page, options) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    // Let theme and viewport changes reach the compositor before capture.
    await page.evaluate(() => new Promise(resolve => {
      requestAnimationFrame(() => requestAnimationFrame(resolve))
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
