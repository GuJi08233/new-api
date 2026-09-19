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

/**
 * 计费表达式的档位时段解析。
 *
 * 表达式里的档位条件可以是 len/p/c 的用量比较，也可以是 hour()/weekday() 之类的
 * 时间函数。用量条件要等请求进来才知道结果，时间条件却是此刻就能算的——模型广场
 * 因此可以直接告诉用户"现在走哪一档、这一档对应哪些时间段"。
 *
 * 本模块只负责这件事：把表达式拆成档位分支、判定当前生效档位、把纯时间条件归纳成
 * 可读的时间段文案。含 param()/header() 等依赖请求内容的条件一律判为不可求值，
 * 由调用方降级展示。
 */

const TIME_FUNCS = ['hour', 'minute', 'weekday', 'month', 'day'];
const SCHEDULE_FUNCS = ['hour', 'weekday'];
const WEEKDAY_FROM_LABEL = {
  Sun: 0,
  Mon: 1,
  Tue: 2,
  Wed: 3,
  Thu: 4,
  Fri: 5,
  Sat: 6,
};
// 时段文案按周一开头排版，周六日才能连成一段
const WEEK_DISPLAY_ORDER = [1, 2, 3, 4, 5, 6, 0];
const WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const HOUR_MS = 3600000;
const WEEK_SAMPLE_HOURS = 168;

const TIME_COMPARISON_REGEX = new RegExp(
  `^(${TIME_FUNCS.join('|')})\\(\\s*"([^"]*)"\\s*\\)\\s*(==|!=|>=|<=|>|<)\\s*(-?\\d+(?:\\.\\d+)?)`,
);

const COVERAGE_CACHE_LIMIT = 200;

const conditionAstCache = new Map();
const zoneFormatterCache = new Map();
const coverageCache = new Map();

function skipStringLiteral(text, quoteIndex) {
  for (let i = quoteIndex + 1; i < text.length; i += 1) {
    if (text[i] === '\\') {
      i += 1;
      continue;
    }
    if (text[i] === '"') return i;
  }
  return text.length;
}

function findMatchingParen(text, openIndex) {
  let depth = 0;
  for (let i = openIndex; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === '"') {
      i = skipStringLiteral(text, i);
      continue;
    }
    if (ch === '(') depth += 1;
    else if (ch === ')') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

function unwrapOuterParens(expr) {
  let current = String(expr || '').trim();
  while (
    current.startsWith('(') &&
    findMatchingParen(current, 0) === current.length - 1
  ) {
    current = current.slice(1, -1).trim();
  }
  return current;
}

/**
 * 切开一层三元表达式：`cond ? then : else`。
 * 三元右结合，所以要跳过 then 分支里嵌套的 `?`，才能找到本层的 `:`。
 */
function splitTernary(expr) {
  const text = String(expr || '');
  let depth = 0;
  let questionIndex = -1;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === '"') {
      i = skipStringLiteral(text, i);
      continue;
    }
    if (ch === '(') depth += 1;
    else if (ch === ')') depth -= 1;
    else if (ch === '?' && depth === 0) {
      questionIndex = i;
      break;
    }
  }
  if (questionIndex < 0) return null;

  depth = 0;
  let nested = 0;
  for (let i = questionIndex + 1; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === '"') {
      i = skipStringLiteral(text, i);
      continue;
    }
    if (ch === '(') depth += 1;
    else if (ch === ')') depth -= 1;
    else if (depth === 0 && ch === '?') nested += 1;
    else if (depth === 0 && ch === ':') {
      if (nested === 0) {
        return {
          condition: text.slice(0, questionIndex).trim(),
          consequent: text.slice(questionIndex + 1, i).trim(),
          alternate: text.slice(i + 1).trim(),
        };
      }
      nested -= 1;
    }
  }
  return null;
}

function scanTierCalls(text) {
  const calls = [];
  const re = /\btier\(\s*"([^"]*)"\s*,/g;
  let m;
  while ((m = re.exec(text)) !== null) {
    const open = text.indexOf('(', m.index);
    const close = findMatchingParen(text, open);
    if (close < 0) break;
    calls.push({
      label: m[1],
      body: text.slice(m.index + m[0].length, close).trim(),
    });
    re.lastIndex = close + 1;
  }
  return calls;
}

/**
 * 把表达式展开成按求值顺序排列的档位分支。
 *
 * 每个分支带上触发它的条件原文；`condition` 为空表示兜底分支（前面都没命中就走它）。
 * else 分支沿用外层条件而不取反，正是因为求值按顺序短路——能轮到它时，外层条件必然
 * 已经成立且前面的分支都没命中。
 */
export function splitTieredExprBranches(exprBody) {
  const branches = [];
  const collect = (expr, inheritedCondition) => {
    // 每层都要先剥括号：编辑器和文档里的嵌套档位写成 `a ? x : (b ? y : z)`，
    // 括号会把内层的 `?` 压到 depth 1，splitTernary 找不到它，两个内层档位就会一起
    // 退化成无条件的兜底分支，模型广场因此高亮错误的当前档位。
    const body = unwrapOuterParens(expr);
    const ternary = splitTernary(body);
    if (!ternary) {
      for (const call of scanTierCalls(body)) {
        branches.push({ ...call, condition: inheritedCondition });
      }
      return;
    }
    const nextCondition = inheritedCondition
      ? `(${inheritedCondition}) && (${ternary.condition})`
      : ternary.condition;
    collect(ternary.consequent, nextCondition);
    collect(ternary.alternate, inheritedCondition);
  };
  collect(exprBody, '');
  return branches;
}

// ---------------------------------------------------------------------------
// 条件表达式解析
// ---------------------------------------------------------------------------

function parseConditionExpr(source) {
  const text = String(source || '').trim();
  if (!text) return null;
  let pos = 0;

  const skipSpace = () => {
    while (pos < text.length && /\s/.test(text[pos])) pos += 1;
  };

  // 认不出来的片段整体吞掉，剩下的条件仍然照常解析
  const skipUnknown = () => {
    let depth = 0;
    while (pos < text.length) {
      const ch = text[pos];
      if (ch === '"') {
        pos = skipStringLiteral(text, pos) + 1;
        continue;
      }
      if (ch === '(') {
        depth += 1;
        pos += 1;
        continue;
      }
      if (ch === ')') {
        if (depth === 0) break;
        depth -= 1;
        pos += 1;
        continue;
      }
      if (
        depth === 0 &&
        (text.startsWith('&&', pos) || text.startsWith('||', pos))
      )
        break;
      pos += 1;
    }
    return { type: 'unknown' };
  };

  const parsePrimary = () => {
    skipSpace();
    if (text[pos] === '!' && text[pos + 1] !== '=') {
      pos += 1;
      return { type: 'not', operand: parsePrimary() };
    }
    if (text[pos] === '(') {
      const closing = findMatchingParen(text, pos);
      if (closing < 0) return skipUnknown();
      const inner = text.slice(pos + 1, closing);
      const resumeAt = closing + 1;
      // 括号后面跟着比较/算术运算符时，这不是一个独立的布尔子句
      const tail = text.slice(resumeAt).trimStart();
      if (
        tail &&
        !tail.startsWith('&&') &&
        !tail.startsWith('||') &&
        !tail.startsWith(')')
      ) {
        return skipUnknown();
      }
      pos = resumeAt;
      return parseConditionExpr(inner) || { type: 'unknown' };
    }
    const m = TIME_COMPARISON_REGEX.exec(text.slice(pos));
    if (!m) return skipUnknown();
    pos += m[0].length;
    return {
      type: 'comparison',
      func: m[1],
      timeZone: m[2],
      op: m[3],
      value: Number(m[4]),
    };
  };

  const parseAnd = () => {
    let node = parsePrimary();
    for (;;) {
      skipSpace();
      if (!text.startsWith('&&', pos)) return node;
      pos += 2;
      node = { type: 'and', left: node, right: parsePrimary() };
    }
  };

  let node = parseAnd();
  for (;;) {
    skipSpace();
    if (!text.startsWith('||', pos)) break;
    pos += 2;
    node = { type: 'or', left: node, right: parseAnd() };
  }

  skipSpace();
  // 有残留说明结构超出了这里能理解的范围，整体判为不可求值
  return pos < text.length ? { type: 'unknown' } : node;
}

function getConditionAst(condition) {
  const key = String(condition || '');
  if (!conditionAstCache.has(key)) {
    conditionAstCache.set(key, parseConditionExpr(key));
  }
  return conditionAstCache.get(key);
}

function walkConditionAst(node, visit) {
  if (!node) return;
  visit(node);
  if (node.left) walkConditionAst(node.left, visit);
  if (node.right) walkConditionAst(node.right, visit);
  if (node.operand) walkConditionAst(node.operand, visit);
}

/** 三值逻辑：null 表示"这一部分算不出来"，不能当成 false 用。 */
function evaluateConditionAst(node, resolveFields) {
  if (!node) return null;
  switch (node.type) {
    case 'and': {
      const left = evaluateConditionAst(node.left, resolveFields);
      const right = evaluateConditionAst(node.right, resolveFields);
      if (left === false || right === false) return false;
      if (left === null || right === null) return null;
      return true;
    }
    case 'or': {
      const left = evaluateConditionAst(node.left, resolveFields);
      const right = evaluateConditionAst(node.right, resolveFields);
      if (left === true || right === true) return true;
      if (left === null || right === null) return null;
      return false;
    }
    case 'not': {
      const value = evaluateConditionAst(node.operand, resolveFields);
      return value === null ? null : !value;
    }
    case 'comparison': {
      const fields = resolveFields(node.timeZone);
      if (!fields) return null;
      const actual = fields[node.func];
      if (!Number.isFinite(actual)) return null;
      switch (node.op) {
        case '==':
          return actual === node.value;
        case '!=':
          return actual !== node.value;
        case '>=':
          return actual >= node.value;
        case '<=':
          return actual <= node.value;
        case '>':
          return actual > node.value;
        case '<':
          return actual < node.value;
        default:
          return null;
      }
    }
    default:
      return null;
  }
}

function timeFieldsInZone(date, timeZone) {
  if (!zoneFormatterCache.has(timeZone)) {
    let formatter = null;
    try {
      formatter = new Intl.DateTimeFormat('en-US', {
        timeZone,
        hour12: false,
        weekday: 'short',
        month: 'numeric',
        day: 'numeric',
        hour: 'numeric',
        minute: 'numeric',
      });
    } catch {
      formatter = null;
    }
    zoneFormatterCache.set(timeZone, formatter);
  }
  const formatter = zoneFormatterCache.get(timeZone);
  if (!formatter) return null;

  const parts = {};
  for (const part of formatter.formatToParts(date))
    parts[part.type] = part.value;
  const weekday = WEEKDAY_FROM_LABEL[parts.weekday];
  if (weekday === undefined) return null;
  return {
    // 部分运行时把午夜格式化成 24 点
    hour: Number(parts.hour) % 24,
    minute: Number(parts.minute),
    weekday,
    month: Number(parts.month),
    day: Number(parts.day),
  };
}

function createZoneFieldResolver(date) {
  const cache = new Map();
  return (timeZone) => {
    if (!cache.has(timeZone))
      cache.set(timeZone, timeFieldsInZone(date, timeZone));
    return cache.get(timeZone);
  };
}

function matchBranchAt(branchAsts, date) {
  const resolveFields = createZoneFieldResolver(date);
  for (let i = 0; i < branchAsts.length; i += 1) {
    const ast = branchAsts[i];
    if (ast === null) return i;
    const hit = evaluateConditionAst(ast, resolveFields);
    if (hit === null) return -1;
    if (hit) return i;
  }
  return -1;
}

const NUMBER_LITERAL_REGEX = /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?$/;
const UNRESOLVED_MULTIPLIER = Object.freeze({
  min: 1,
  max: 1,
  resolved: false,
});

/**
 * 按顶层 `*` 把表达式拆成乘数因子。
 *
 * 顶层同时出现 `*` 和别的中缀运算（含三元的 `?`/`:`）时结构超出这里能安全归因的范围，
 * 返回 null 让调用方降级——`a ? tier(x) * 2 : tier(y)` 这种写法里的 `* 2` 只作用于一个
 * 分支，按整体乘数处理会算错价。
 */
function splitTopLevelFactors(text) {
  const splits = [];
  let start = 0;
  let depth = 0;
  let hasOtherOperator = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === '"') {
      i = skipStringLiteral(text, i);
      continue;
    }
    if (ch === '(') depth += 1;
    else if (ch === ')') depth -= 1;
    else if (depth === 0) {
      if (ch === '*') {
        splits.push(text.slice(start, i));
        start = i + 1;
      } else if ('+-/%?:'.includes(ch)) {
        hasOtherOperator = true;
      }
    }
  }
  splits.push(text.slice(start));
  if (splits.length === 1) return splits;
  return hasOtherOperator ? null : splits;
}

function evaluateMultiplierFactor(factor, resolveFields) {
  const body = unwrapOuterParens(factor);
  if (NUMBER_LITERAL_REGEX.test(body)) {
    const value = Number(body);
    return value > 0 ? { min: value, max: value } : null;
  }
  const ternary = splitTernary(body);
  if (!ternary) return null;
  const whenTrue = Number(unwrapOuterParens(ternary.consequent));
  const whenFalse = Number(unwrapOuterParens(ternary.alternate));
  if (!(whenTrue > 0) || !(whenFalse > 0)) return null;

  const hit = evaluateConditionAst(
    getConditionAst(ternary.condition),
    resolveFields,
  );
  if (hit === true) return { min: whenTrue, max: whenTrue };
  if (hit === false) return { min: whenFalse, max: whenFalse };
  // 条件依赖请求内容，此刻只能给出取值区间
  return {
    min: Math.min(whenTrue, whenFalse),
    max: Math.max(whenTrue, whenFalse),
  };
}

/**
 * 算出乘在档位外层的条件乘数。
 *
 * 表达式的标准形态是 `(档位体) * (条件 ? 倍率 : 1) * ...`（见 combineBillingExpr）。只解析
 * tier() 里的系数，就会把 `tier("base", p * 5) * (param("service_tier") == "priority" ? 2 : 1)`
 * 显示成 $5，而 priority 请求实际按 $10 计费。时间条件此刻就能判定，依赖请求内容的条件
 * 给出取值区间，看不懂的结构报告无法归因，由调用方明确标注展示的是基准价。
 *
 * @param {string} exprBody - 去掉版本前缀的表达式正文
 * @param {Date} [now]
 * @returns {{min: number, max: number, resolved: boolean}}
 */
export function analyzeExprMultiplier(exprBody, now = new Date()) {
  const body = unwrapOuterParens(exprBody);
  if (!body) return { min: 1, max: 1, resolved: true };

  const factors = splitTopLevelFactors(body);
  if (!factors) return UNRESOLVED_MULTIPLIER;
  if (factors.length === 1) return { min: 1, max: 1, resolved: true };
  // 档位体必须落在唯一一个因子里，否则无法区分谁是价格、谁是乘数
  if (factors.filter((factor) => factor.includes('tier(')).length !== 1) {
    return UNRESOLVED_MULTIPLIER;
  }

  const resolveFields = createZoneFieldResolver(now);
  let min = 1;
  let max = 1;
  for (const factor of factors) {
    if (factor.includes('tier(')) continue;
    const range = evaluateMultiplierFactor(factor, resolveFields);
    if (!range) return UNRESOLVED_MULTIPLIER;
    min *= range.min;
    max *= range.max;
  }
  return { min, max, resolved: true };
}

// ---------------------------------------------------------------------------
// 时段文案
// ---------------------------------------------------------------------------

function formatWeekdays(weekdays, t) {
  if (weekdays.size === WEEK_DISPLAY_ORDER.length) return t('每天');
  const segments = [];
  let start = -1;
  for (let i = 0; i <= WEEK_DISPLAY_ORDER.length; i += 1) {
    const active =
      i < WEEK_DISPLAY_ORDER.length && weekdays.has(WEEK_DISPLAY_ORDER[i]);
    if (active && start < 0) start = i;
    if (!active && start >= 0) {
      segments.push([start, i - 1]);
      start = -1;
    }
  }
  return segments
    .map(([from, to]) => {
      const labels = [];
      for (let i = from; i <= to; i += 1) {
        labels.push(t(WEEKDAY_LABELS[WEEK_DISPLAY_ORDER[i]]));
      }
      if (labels.length >= 3) {
        return t('{{from}} 至 {{to}}', {
          from: labels[0],
          to: labels[labels.length - 1],
        });
      }
      return labels.join(t('、'));
    })
    .join(t('、'));
}

function formatHourRanges(hours) {
  const ranges = [];
  for (const hour of [...hours].sort((a, b) => a - b)) {
    const last = ranges[ranges.length - 1];
    if (last && last[1] === hour) last[1] = hour + 1;
    else ranges.push([hour, hour + 1]);
  }
  return ranges;
}

function describeCoverage(points, t) {
  if (points.size === 0) return '';

  const hoursByWeekday = new Map();
  for (const point of points) {
    const weekday = Math.floor(point / 24);
    if (!hoursByWeekday.has(weekday)) hoursByWeekday.set(weekday, new Set());
    hoursByWeekday.get(weekday).add(point % 24);
  }

  // 时段相同的若干天并成一条，"周一至周五 01:00-04:00"才不会写成五行
  const byHourSignature = new Map();
  for (const [weekday, hours] of hoursByWeekday) {
    const ranges = formatHourRanges(hours);
    const signature = ranges.map(([from, to]) => `${from}-${to}`).join(',');
    if (!byHourSignature.has(signature)) {
      byHourSignature.set(signature, { ranges, weekdays: new Set() });
    }
    byHourSignature.get(signature).weekdays.add(weekday);
  }

  const pad = (value) => String(value).padStart(2, '0');
  return [...byHourSignature.values()]
    .map(({ ranges, weekdays }) => {
      const dayText = formatWeekdays(weekdays, t);
      if (ranges.length === 1 && ranges[0][0] === 0 && ranges[0][1] === 24)
        return dayText;
      const hourText = ranges
        .map(([from, to]) => `${pad(from)}:00-${pad(to)}:00`)
        .join(t('、'));
      return `${dayText} ${hourText}`;
    })
    .join(t('；'));
}

/**
 * 回放一周，算出每档覆盖哪些"星期×小时"格子。
 *
 * 结果只随小时边界变化，因此按表达式+小时缓存：模型广场一页就有几十张卡片，每次
 * 重渲染都重算 168 个采样点会拖慢筛选和翻页。缓存的是与语言无关的格子集合，文案仍
 * 由调用方按当前语言生成。
 */
function getTierCoverage(branchAsts, timeZone, now) {
  const cacheKey = `${Math.floor(now.getTime() / HOUR_MS)}|${timeZone}|${JSON.stringify(branchAsts)}`;
  if (coverageCache.has(cacheKey)) return coverageCache.get(cacheKey);

  let coverage = branchAsts.map(() => new Set());
  const base = Math.floor(now.getTime() / HOUR_MS) * HOUR_MS;
  for (let i = 0; i < WEEK_SAMPLE_HOURS; i += 1) {
    const sample = new Date(base + i * HOUR_MS);
    const fields = timeFieldsInZone(sample, timeZone);
    const matched = fields ? matchBranchAt(branchAsts, sample) : -1;
    if (matched < 0) {
      coverage = null;
      break;
    }
    coverage[matched].add(fields.weekday * 24 + fields.hour);
  }

  if (coverageCache.size >= COVERAGE_CACHE_LIMIT) coverageCache.clear();
  coverageCache.set(cacheKey, coverage);
  return coverage;
}

/**
 * 判定当前生效档位，并在条件只涉及星期/小时时给出每档的时间段文案。
 *
 * @param {Array} tiers - parseTiersFromExpr 的结果，需带 condExpr
 * @param {Function} t - i18next 翻译函数
 * @param {Date} [now]
 * @returns {{activeIndex: number, scheduleLabels: string[]|null, timeZone: string|null}}
 */
export function analyzeTierSchedule(tiers, t, now = new Date()) {
  const empty = { activeIndex: -1, scheduleLabels: null, timeZone: null };
  if (!Array.isArray(tiers) || tiers.length === 0) return empty;

  const branchAsts = tiers.map((tier) =>
    tier.condExpr ? getConditionAst(tier.condExpr) : null,
  );
  // 当前档位不走缓存：分钟级条件在小时内也会切换
  const activeIndex = matchBranchAt(branchAsts, now);

  const timeZones = new Set();
  const usedFuncs = new Set();
  let hasUnknown = false;
  for (const ast of branchAsts) {
    walkConditionAst(ast, (node) => {
      if (node.type === 'unknown') hasUnknown = true;
      if (node.type === 'comparison') {
        usedFuncs.add(node.func);
        timeZones.add(node.timeZone);
      }
    });
  }

  const schedulable =
    !hasUnknown &&
    usedFuncs.size > 0 &&
    [...usedFuncs].every((func) => SCHEDULE_FUNCS.includes(func));
  if (!schedulable) return { ...empty, activeIndex };

  const timeZone = [...timeZones][0];
  const coverage = getTierCoverage(branchAsts, timeZone, now);
  if (!coverage) return { ...empty, activeIndex };

  return {
    activeIndex,
    scheduleLabels: coverage.map((points) => describeCoverage(points, t)),
    timeZone,
  };
}
