/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

let verificationHandler;

export function registerSecurityVerification(handler) {
  verificationHandler = handler;
  return () => {
    if (verificationHandler === handler) verificationHandler = undefined;
  };
}

export function requestSecurityVerification(challenge) {
  if (!verificationHandler) return Promise.reject(new Error('需要安全验证'));
  return verificationHandler(challenge);
}

export function securityOAuthUrl(slug, state, status) {
  const redirect = `${window.location.origin}/oauth/${slug}`;
  let endpoint;
  let clientId;
  let scope = 'openid profile email';
  switch (slug) {
    case 'github':
      endpoint = 'https://github.com/login/oauth/authorize';
      clientId = status.github_client_id;
      scope = 'user:email';
      break;
    case 'discord':
      endpoint = 'https://discord.com/oauth2/authorize';
      clientId = status.discord_client_id;
      scope = 'identify openid';
      break;
    case 'oidc':
      endpoint = status.oidc_authorization_endpoint;
      clientId = status.oidc_client_id;
      break;
    case 'linuxdo':
      endpoint = 'https://connect.linux.do/oauth2/authorize';
      clientId = status.linuxdo_client_id;
      break;
    case 'steam': {
      const url = new URL('https://steamcommunity.com/openid/login');
      const params = {
        'openid.claimed_id':
          'http://specs.openid.net/auth/2.0/identifier_select',
        'openid.identity': 'http://specs.openid.net/auth/2.0/identifier_select',
        'openid.mode': 'checkid_setup',
        'openid.ns': 'http://specs.openid.net/auth/2.0',
        'openid.realm': window.location.origin,
        'openid.return_to': `${redirect}?state=${encodeURIComponent(state)}`,
      };
      Object.entries(params).forEach(([key, value]) =>
        url.searchParams.set(key, value),
      );
      return url.toString();
    }
    default: {
      const provider = status.custom_oauth_providers?.find(
        (item) => item.slug === slug,
      );
      if (!provider) throw new Error('无效的 OAuth 提供商');
      endpoint = provider.authorization_endpoint;
      clientId = provider.client_id;
      scope = provider.scopes || scope;
    }
  }
  const url = new URL(endpoint);
  if (!['https:', 'http:'].includes(url.protocol))
    throw new Error('无效的 OAuth 提供商');
  url.searchParams.set('client_id', clientId);
  url.searchParams.set('redirect_uri', redirect);
  url.searchParams.set('response_type', 'code');
  url.searchParams.set('scope', scope);
  url.searchParams.set('state', state);
  url.searchParams.set('prompt', 'login');
  return url.toString();
}
