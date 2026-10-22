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

// 可独立验证的结算边界：请求超时、登录过期、限流和服务端错误都保留录像。
export async function submitPendingReplay(store, game, payload, post) {
  const { session_id: sessionId, ...body } = payload;
  let response;
  try {
    const result = await post(
      `/api/game/${game}/finish`,
      game === 'tetris' ? body : payload,
      {
        ...(game === 'tetris' ? { params: { session_id: sessionId } } : {}),
        skipErrorHandler: true,
      },
    );
    response = result.data.success
      ? { data: result.data.data }
      : { error: result.data.message };
  } catch (error) {
    const status = error?.response?.status;
    response = {
      error: error?.response?.data?.message || '',
      network: !status || status >= 500 || status === 401,
      limited: status === 429,
    };
  }
  if (!response.network && !response.limited)
    response.storageFailure = !store.remove(sessionId).ok;
  return response;
}
