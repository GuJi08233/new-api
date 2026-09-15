import { describe, expect, test } from 'bun:test';
import {
  buildRequestRuleExpr,
  tryParseRequestRuleExpr,
} from './requestRuleExpr';

function timeRange(start, end) {
  return {
    conditions: [
      {
        source: 'time',
        timeFunc: 'hour',
        timezone: 'Asia/Shanghai',
        mode: 'range',
        rangeStart: start,
        rangeEnd: end,
      },
    ],
    multiplier: '2',
  };
}

describe('时间倍率区间', () => {
  test.each([
    ['9', '17', [1, 2, 2, 1, 1]],
    ['21', '6', [2, 1, 1, 1, 2]],
    ['9', '9', [1, 1, 1, 1, 1]],
  ])('%s 到 %s 只在区间内应用倍率', (start, end, expected) => {
    const expr = buildRequestRuleExpr([timeRange(start, end)]);
    const evaluate = new Function('hour', `return ${expr}`);
    expect([0, 9, 12, 17, 23].map((hour) => evaluate(() => hour))).toEqual(
      expected,
    );
    expect(buildRequestRuleExpr(tryParseRequestRuleExpr(expr))).toBe(expr);
  });

  test('混合条件读回时保留同日区间', () => {
    const group = timeRange('9', '17');
    group.conditions.unshift({
      source: 'param',
      path: 'service_tier',
      mode: 'eq',
      value: 'fast',
    });
    const expr = buildRequestRuleExpr([group]);
    const parsed = tryParseRequestRuleExpr(expr);
    expect(parsed[0].conditions).toHaveLength(2);
    expect(parsed[0].conditions[1].mode).toBe('range');
    expect(buildRequestRuleExpr(parsed)).toBe(expr);
  });

  test.each([
    ['-1', '5'],
    ['9', '24'],
    ['9.5', '17'],
  ])('拒绝非法边界 %s-%s', (start, end) => {
    expect(buildRequestRuleExpr([timeRange(start, end)])).toBe('');
    expect(
      tryParseRequestRuleExpr(
        `(hour("Asia/Shanghai") >= ${start} && hour("Asia/Shanghai") < ${end} ? 2 : 1)`,
      ),
    ).toBeNull();
  });

  test('旧恒真表达式保持原始模式，避免读回后无提示改变计价', () => {
    expect(
      tryParseRequestRuleExpr(
        '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 17 ? 2 : 1)',
      ),
    ).toBeNull();
  });
});
