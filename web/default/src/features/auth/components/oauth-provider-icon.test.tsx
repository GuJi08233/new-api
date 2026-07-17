import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { renderToStaticMarkup } from 'react-dom/server'

import { OAuthProviderIcon } from './oauth-provider-icon'

describe('OAuthProviderIcon', () => {
  test('renders the Slack brand icon without relying on a global symbol', () => {
    const markup = renderToStaticMarkup(<OAuthProviderIcon icon='slack' />)

    assert.match(markup, /<title>Slack<\/title>/)
  })

  test('renders URL and emoji provider icons', () => {
    const urlMarkup = renderToStaticMarkup(
      <OAuthProviderIcon icon='https://example.com/icon.png' />
    )
    const emojiMarkup = renderToStaticMarkup(<OAuthProviderIcon icon='🐱' />)

    assert.match(urlMarkup, /src="https:\/\/example.com\/icon.png"/)
    assert.match(emojiMarkup, /🐱/)
  })
})
