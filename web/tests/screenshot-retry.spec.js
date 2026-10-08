import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const captureError = new Error('page.screenshot: Protocol error (Page.captureScreenshot): Unable to capture screenshot')

function fakePage(failures, error = captureError, closed = false) {
  let calls = 0
  return {
    evaluate: async () => {},
    isClosed: () => closed,
    screenshot: async options => {
      calls++
      expect(options).toMatchObject({ path: 'capture.png', animations: 'disabled', timeout: 5000 })
      if (calls <= failures) throw error
      return Buffer.from('captured')
    },
    calls: () => calls
  }
}

test('screenshot retries only transient capture errors and returns the image', async () => {
  const page = fakePage(2)
  expect(await captureScreenshot(page, { path: 'capture.png' })).toEqual(Buffer.from('captured'))
  expect(page.calls()).toBe(3)
})

test('screenshot propagates persistent capture failure after three attempts', async () => {
  const page = fakePage(10)
  await expect(captureScreenshot(page, { path: 'capture.png' })).rejects.toBe(captureError)
  expect(page.calls()).toBe(3)
})

test('screenshot does not retry unrelated errors or a closed page', async () => {
  for (const [error, closed] of [[new Error('Timeout 5000ms exceeded'), false], [captureError, true]]) {
    const page = fakePage(10, error, closed)
    await expect(captureScreenshot(page, { path: 'capture.png' })).rejects.toBe(error)
    expect(page.calls()).toBe(1)
  }
})
