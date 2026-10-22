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

// 浏览器拒绝存储或配额用完时，游戏继续运行；调用方负责提示无法刷新恢复。
export function readGameStorage(key) {
  try {
    const raw = localStorage.getItem(key);
    return { ok: true, value: raw === null ? null : JSON.parse(raw) };
  } catch (error) {
    return { ok: false, value: null, error };
  }
}

export function writeGameStorage(key, value) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
    return { ok: true };
  } catch (error) {
    return { ok: false, error };
  }
}

export function removeGameStorage(key) {
  try {
    localStorage.removeItem(key);
    return { ok: true };
  } catch (error) {
    return { ok: false, error };
  }
}

export const REPLAY_TTL_MS = 2 * 60 * 60 * 1000;

// 每个账户、游戏、会话单独保存。storage 可注入，验证不会接触玩家数据。
export function createReplayStore(storage, userId, game, now = Date.now) {
  const prefix = `game-replay:v1:${userId}:${game}:`;
  const memory = new Map();
  const removed = new Set();
  const validOwner = Number.isSafeInteger(userId) && userId > 0;
  return {
    save(payload, startedAt) {
      if (
        !validOwner ||
        !Number.isSafeInteger(payload?.session_id) ||
        payload.session_id <= 0 ||
        !Number.isFinite(startedAt)
      ) {
        return { ok: false };
      }
      const record = {
        version: 1,
        userId,
        game,
        sessionId: payload.session_id,
        startedAt,
        expiresAt: startedAt + REPLAY_TTL_MS,
        savedAt: now(),
        payload,
      };
      try {
        const serialized = JSON.stringify(record);
        memory.set(record.sessionId, JSON.parse(serialized));
        removed.delete(record.sessionId);
        storage().setItem(prefix + record.sessionId, serialized);
        return { ok: true };
      } catch (error) {
        return { ok: false, error };
      }
    },
    list() {
      let ok = validOwner;
      if (!validOwner) return { ok: false, value: [] };
      try {
        const target = storage();
        const keys = Array.from({ length: target.length }, (_, index) =>
          target.key(index),
        );
        for (const key of keys) {
          if (!key?.startsWith(prefix)) continue;
          let record;
          try {
            record = JSON.parse(target.getItem(key));
          } catch {
            record = null;
          }
          if (
            !record ||
            record.version !== 1 ||
            record.userId !== userId ||
            record.game !== game ||
            !Number.isSafeInteger(record.sessionId) ||
            key !== prefix + record.sessionId ||
            record.payload?.session_id !== record.sessionId ||
            !Number.isFinite(record.startedAt) ||
            record.expiresAt !== record.startedAt + REPLAY_TTL_MS ||
            record.expiresAt <= now()
          ) {
            target.removeItem(key);
            continue;
          }
          if (
            !removed.has(record.sessionId) &&
            (!memory.has(record.sessionId) ||
              memory.get(record.sessionId).savedAt < record.savedAt)
          )
            memory.set(record.sessionId, record);
        }
      } catch {
        ok = false;
      }
      for (const [id, record] of memory) {
        if (record.expiresAt <= now()) memory.delete(id);
      }
      return {
        ok,
        value: [...memory.values()].sort((a, b) => a.savedAt - b.savedAt),
      };
    },
    remove(sessionId) {
      memory.delete(sessionId);
      removed.add(sessionId);
      try {
        storage().removeItem(prefix + sessionId);
        return { ok: true };
      } catch (error) {
        return { ok: false, error };
      }
    },
  };
}
