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

import { afterEach, beforeEach, describe, expect, spyOn, test } from 'bun:test';
import {
  DEFAULT_CONFIG,
  STORAGE_KEYS,
} from '../../constants/playground.constants';
import {
  clearConfig,
  clearMessages,
  getConfigTimestamp,
  getPlaygroundUserId,
  hasStoredConfig,
  importConfig,
  loadConfig,
  loadMessages,
  saveConfig,
  saveMessages,
} from './configStorage';

const originalStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage',
);
const originalReader = Object.getOwnPropertyDescriptor(
  globalThis,
  'FileReader',
);
let records;
let errorLog;

beforeEach(() => {
  records = new Map();
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key) => records.get(key) ?? null,
      setItem: (key, value) => records.set(key, String(value)),
      removeItem: (key) => records.delete(key),
    },
  });
  errorLog = spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  errorLog.mockRestore();
  if (originalStorage) {
    Object.defineProperty(globalThis, 'localStorage', originalStorage);
  } else {
    delete globalThis.localStorage;
  }
  if (originalReader) {
    Object.defineProperty(globalThis, 'FileReader', originalReader);
  } else {
    delete globalThis.FileReader;
  }
});

function signIn(id) {
  localStorage.setItem('user', JSON.stringify({ id }));
}

describe('Playground 账号数据隔离', () => {
  test('切换账号分别恢复各自的配置和消息', () => {
    const messagesA = [{ role: 'user', content: 'account-a-message' }];
    const messagesB = [{ role: 'user', content: 'account-b-message' }];
    signIn(1);
    saveConfig({ ...DEFAULT_CONFIG, inputs: { model: 'account-a-model' } });
    saveMessages(messagesA);
    expect(hasStoredConfig()).toBe(true);
    expect(getConfigTimestamp()).toBeString();

    signIn(2);
    expect(loadConfig()).toEqual(DEFAULT_CONFIG);
    expect(loadMessages()).toBeNull();
    expect(hasStoredConfig()).toBe(false);
    expect(getConfigTimestamp()).toBeNull();
    saveMessages(messagesB);

    signIn(1);
    expect(loadConfig().inputs.model).toBe('account-a-model');
    expect(loadMessages()).toEqual(messagesA);
    signIn(2);
    expect(loadMessages()).toEqual(messagesB);
  });

  test('不将无法确认归属的旧共享记录分配给登录账号，也不删除旧数据', () => {
    const legacyMessages = JSON.stringify({
      messages: [{ content: 'legacy-secret' }],
    });
    const legacyConfig = JSON.stringify({ inputs: { model: 'legacy-model' } });
    localStorage.setItem(STORAGE_KEYS.MESSAGES, legacyMessages);
    localStorage.setItem(STORAGE_KEYS.CONFIG, legacyConfig);
    signIn(1);
    expect(loadMessages()).toBeNull();
    expect(loadConfig()).toEqual(DEFAULT_CONFIG);
    saveMessages([{ content: 'new-account-message' }]);
    expect(localStorage.getItem(STORAGE_KEYS.MESSAGES)).toBe(legacyMessages);
    expect(localStorage.getItem(STORAGE_KEYS.CONFIG)).toBe(legacyConfig);
  });

  test('注销后不读取或保存账号数据，再登录原账号仍可恢复', () => {
    const messages = [{ content: 'account-a-message' }];
    signIn(1);
    saveMessages(messages);
    localStorage.removeItem('user');
    expect(getPlaygroundUserId()).toBeNull();
    expect(loadMessages()).toBeNull();
    saveMessages([{ content: 'anonymous-message' }]);
    clearConfig();
    signIn(1);
    expect(loadMessages()).toEqual(messages);
  });

  test('旧账号的延迟保存和清除操作不能修改新账号的数据', () => {
    signIn(1);
    const ownerId = getPlaygroundUserId();
    saveMessages([{ content: 'account-a-message' }], ownerId);
    signIn(2);
    saveConfig({ ...DEFAULT_CONFIG, inputs: { model: 'account-b-model' } });
    saveMessages([{ content: 'account-b-message' }]);
    saveConfig({ inputs: { model: 'stale-model' } }, ownerId);
    saveMessages([{ content: 'stale-message' }], ownerId);
    clearConfig(ownerId);
    clearMessages(ownerId);
    expect(loadMessages(ownerId)).toBeNull();
    expect(loadConfig().inputs.model).toBe('account-b-model');
    expect(loadMessages()).toEqual([{ content: 'account-b-message' }]);
    signIn(1);
    expect(loadMessages()).toEqual([{ content: 'account-a-message' }]);
  });

  test('重置仅清除当前账号，不影响另一个账号', () => {
    signIn(1);
    saveMessages([{ content: 'account-a-message' }]);
    signIn(2);
    saveConfig(DEFAULT_CONFIG);
    saveMessages([{ content: 'account-b-message' }]);
    clearConfig();
    expect(hasStoredConfig()).toBe(false);
    expect(loadMessages()).toBeNull();
    signIn(1);
    expect(loadMessages()).toEqual([{ content: 'account-a-message' }]);
  });

  test('缺失、损坏和非法用户身份不创建共享存储', () => {
    for (const user of [
      '{broken',
      '{}',
      '{"id":0}',
      '{"id":-1}',
      '{"id":"1"}',
    ]) {
      localStorage.setItem('user', user);
      expect(getPlaygroundUserId()).toBeNull();
      saveConfig(DEFAULT_CONFIG);
      saveMessages([{ content: 'private-message' }]);
      expect(loadMessages()).toBeNull();
      expect(hasStoredConfig()).toBe(false);
    }
    expect(records.size).toBe(1);
  });

  test('文件读取期间切换账号不会将导入消息写进新账号', async () => {
    let reader;
    Object.defineProperty(globalThis, 'FileReader', {
      configurable: true,
      value: class {
        constructor() {
          reader = this;
        }
        readAsText() {}
      },
    });
    signIn(1);
    const pendingImport = importConfig({});
    signIn(2);
    saveMessages([{ content: 'account-b-message' }]);
    reader.onload({
      target: {
        result: JSON.stringify({
          inputs: {},
          parameterEnabled: {},
          messages: [{ content: 'account-a-import' }],
        }),
      },
    });
    await pendingImport;
    expect(loadMessages()).toEqual([{ content: 'account-b-message' }]);
  });
});
