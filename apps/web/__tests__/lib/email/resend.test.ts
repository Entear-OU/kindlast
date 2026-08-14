import { afterEach, describe, expect, it, vi } from 'vitest'

import { createResendProvider } from '@/lib/email/resend'
import { EmailProviderError } from '@/lib/email/types'

/**
 * ENT-76 — Resend transport (AC: "email transport wired and tested"). Mocks
 * global fetch to assert the request shape and the success/failure handling
 * without a real network call.
 */

const provider = createResendProvider({
  apiKey: 're_test',
  from: 'Kindlast <noreply@kindlast.com>',
})
const message = {
  to: 'founder@example.com',
  subject: 'Hi',
  html: '<p>Hi</p>',
  text: 'Hi',
}

afterEach(() => vi.restoreAllMocks())

describe('createResendProvider (ENT-76)', () => {
  it('POSTs to the Resend API with auth + payload and returns the message id', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: 'msg_123' }), { status: 200 }),
      )
    vi.stubGlobal('fetch', fetchMock)

    const result = await provider.send(message)

    expect(result).toEqual({ id: 'msg_123' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('https://api.resend.com/emails')
    expect(init.method).toBe('POST')
    expect(init.headers.authorization).toBe('Bearer re_test')
    const body = JSON.parse(init.body)
    expect(body).toMatchObject({
      from: 'Kindlast <noreply@kindlast.com>',
      to: 'founder@example.com',
      subject: 'Hi',
      html: '<p>Hi</p>',
      text: 'Hi',
    })
  })

  it('omits text when not provided', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await provider.send({ to: 'a@b.com', subject: 's', html: '<p>h</p>' })
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).not.toHaveProperty(
      'text',
    )
  })

  it('throws EmailProviderError on a non-2xx response', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response('rate limited', { status: 429 }))
    vi.stubGlobal('fetch', fetchMock)
    await expect(provider.send(message)).rejects.toBeInstanceOf(
      EmailProviderError,
    )
  })
})
