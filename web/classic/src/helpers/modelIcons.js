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

import { API } from './api';

// 与后端 model.NameRule* 常量保持一致
const NAME_RULE_EXACT = 0;
const NAME_RULE_PREFIX = 1;
const NAME_RULE_CONTAINS = 2;
const NAME_RULE_SUFFIX = 3;

// 非精确规则的匹配优先级，与后端 pricing 展开元数据的顺序一致，
// 保证日志与模型广场对同一模型解析出同一个图标
const PATTERN_RULE_PRIORITY = {
  [NAME_RULE_PREFIX]: 0,
  [NAME_RULE_SUFFIX]: 1,
  [NAME_RULE_CONTAINS]: 2,
};

const CACHE_TTL = 5 * 60 * 1000;

let exactIcons = new Map();
let patternRules = [];
let loadedAt = 0;
let loadPromise = null;
let version = 0;
const listeners = new Set();

/**
 * 拉取模型管理中配置的图标规则，结果在内存中缓存 5 分钟。
 * 图标属于非关键路径，失败时静默降级为“无图标”，不打断页面。
 */
export async function ensureModelIconsLoaded() {
  if (loadPromise) {
    return loadPromise;
  }
  if (loadedAt && Date.now() - loadedAt < CACHE_TTL) {
    return;
  }

  loadPromise = API.get('/api/model_icons', { skipErrorHandler: true })
    .then((res) => {
      const { success, data } = res.data;
      if (!success || !Array.isArray(data)) {
        return;
      }

      const nextExact = new Map();
      const nextPatterns = [];
      data.forEach((rule) => {
        if (!rule?.model_name || !rule?.icon) {
          return;
        }
        if (rule.name_rule === NAME_RULE_EXACT || rule.name_rule == null) {
          if (!nextExact.has(rule.model_name)) {
            nextExact.set(rule.model_name, rule.icon);
          }
          return;
        }
        nextPatterns.push(rule);
      });
      nextPatterns.sort(
        (a, b) =>
          PATTERN_RULE_PRIORITY[a.name_rule] -
          PATTERN_RULE_PRIORITY[b.name_rule],
      );

      exactIcons = nextExact;
      patternRules = nextPatterns;
      loadedAt = Date.now();
      version += 1;
      listeners.forEach((listener) => listener());
    })
    .catch(() => {})
    .finally(() => {
      loadPromise = null;
    });

  return loadPromise;
}

/**
 * 按模型名解析模型管理中配置的图标名称
 * @param {string} modelName - 模型名称
 * @returns {string} - @lobehub/icons 图标名，未配置时返回空字符串
 */
export function getConfiguredModelIcon(modelName) {
  if (!modelName) {
    return '';
  }

  const exact = exactIcons.get(modelName);
  if (exact) {
    return exact;
  }

  for (const rule of patternRules) {
    switch (rule.name_rule) {
      case NAME_RULE_PREFIX:
        if (modelName.startsWith(rule.model_name)) return rule.icon;
        break;
      case NAME_RULE_SUFFIX:
        if (modelName.endsWith(rule.model_name)) return rule.icon;
        break;
      case NAME_RULE_CONTAINS:
        if (modelName.includes(rule.model_name)) return rule.icon;
        break;
      default:
        break;
    }
  }

  return '';
}

export function subscribeModelIcons(listener) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getModelIconsVersion() {
  return version;
}
