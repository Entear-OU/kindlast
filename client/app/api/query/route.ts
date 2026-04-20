import { NextRequest } from 'next/server'
import { cookies } from 'next/headers'
import { getApiConfig, buildApiUrl } from '@/lib/api/config'

export async function POST(request: NextRequest) {
  try {
    const config = getApiConfig()
    const cookieStore = await cookies()
    const accessToken = cookieStore.get(config.accessTokenCookie)?.value

    if (!accessToken) {
      return new Response(
        JSON.stringify({ error: 'Unauthorized', message: 'Missing authorization', code: 'UNAUTHORIZED' }),
        { status: 401, headers: { 'Content-Type': 'application/json' } }
      )
    }

    const body = await request.json()
    const gatewayUrl = buildApiUrl('/api/v1/query', config)

    // Forward the request to the gateway with auth
    const response = await fetch(gatewayUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${accessToken}`,
        'Accept': body.stream ? 'text/event-stream' : 'application/json',
      },
      body: JSON.stringify(body),
    })

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({ error: 'Request failed' }))
      return new Response(
        JSON.stringify(errorData),
        { status: response.status, headers: { 'Content-Type': 'application/json' } }
      )
    }

    // For streaming responses, pipe through the SSE stream
    if (body.stream && response.body) {
      return new Response(response.body, {
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
          'Connection': 'keep-alive',
        },
      })
    }

    // For non-streaming responses, return JSON
    const data = await response.json()
    return new Response(JSON.stringify(data), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  } catch (error) {
    console.error('Query proxy error:', error)
    return new Response(
      JSON.stringify({ error: 'Internal server error', code: 'INTERNAL_ERROR' }),
      { status: 500, headers: { 'Content-Type': 'application/json' } }
    )
  }
}
