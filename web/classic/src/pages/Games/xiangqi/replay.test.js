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

import { describe, expect, test } from 'bun:test';
import {
  buildXiangqiReplay,
  resolveXiangqiReview,
  xiangqiReplayPosition,
} from './replay';

const initial = [
  'RNBAKABNR',
  '.........',
  '.C.....C.',
  'P.P.P.P.P',
  '.........',
  '.........',
  'p.p.p.p.p',
  '.c.....c.',
  '.........',
  'rnbakabnr',
];
const history = [
  { ply: 1, color: 'red', piece: 'C', from: [7, 2], to: [4, 2] },
  { ply: 2, color: 'black', piece: 'n', from: [7, 9], to: [6, 7] },
  { ply: 3, color: 'red', piece: 'C', captured: 'p', from: [4, 2], to: [4, 6] },
  { ply: 4, color: 'black', piece: 'r', from: [8, 9], to: [7, 9] },
];
const boards = [
  initial,
  initial.map((row, y) => (y === 2 ? '.C..C....' : row)),
  initial.map((row, y) => {
    if (y === 2) return '.C..C....';
    if (y === 7) return '.c....nc.';
    if (y === 9) return 'rnbakab.r';
    return row;
  }),
  initial.map((row, y) => {
    if (y === 2) return '.C.......';
    if (y === 6) return 'p.p.C.p.p';
    if (y === 7) return '.c....nc.';
    if (y === 9) return 'rnbakab.r';
    return row;
  }),
  initial.map((row, y) => {
    if (y === 2) return '.C.......';
    if (y === 6) return 'p.p.C.p.p';
    if (y === 7) return '.c....nc.';
    if (y === 9) return 'rnbakabr.';
    return row;
  }),
];

const live = {
  plies: 4,
  board: boards[4],
  history,
  turn: 'red',
  legal: [{ from: [4, 6], to: [4, 7] }],
  check: true,
  repeat: 2,
  quiet: 12,
  over: true,
  result: 'checkmate',
};

describe('象棋棋谱回看', () => {
  test('逐步逆推准确恢复每个盘面与被吃棋子，不修改服务端状态', () => {
    const before = JSON.stringify(live);
    const replay = buildXiangqiReplay(live);
    expect(replay.firstPly).toBe(0);
    expect(replay.lastPly).toBe(4);
    for (const [ply, board] of boards.entries()) {
      expect(replay.positions.get(ply)).toBe(board.join(''));
    }
    expect(JSON.stringify(live)).toBe(before);
  });

  test('旧局只回看到首条记录之前的局面，不伪造初始棋盘', () => {
    const replay = buildXiangqiReplay({ ...live, history: history.slice(2) });
    expect(replay.firstPly).toBe(2);
    expect(replay.positions.get(2)).toBe(boards[2].join(''));
    expect(replay.positions.has(1)).toBe(false);
    expect(replay.positions.has(0)).toBe(false);
    const unrecorded = buildXiangqiReplay({ ...live, history: [] });
    expect(unrecorded.firstPly).toBe(4);
    expect([...unrecorded.positions.keys()]).toEqual([4]);
  });

  test('记录断档、末步缺失或棋子不符时不跨越未知区间', () => {
    const gap = buildXiangqiReplay({
      ...live,
      history: [history[0], ...history.slice(2)],
    });
    expect(gap.firstPly).toBe(2);
    expect(gap.positions.has(1)).toBe(false);
    expect(
      xiangqiReplayPosition(
        { ...live, history: [history[0], ...history.slice(2)] },
        gap,
        2,
      ).last,
    ).toBeNull();
    expect(
      buildXiangqiReplay({ ...live, history: history.slice(0, 3) }).firstPly,
    ).toBe(4);
    expect(
      buildXiangqiReplay({
        ...live,
        history: [...history.slice(0, 3), { ...history[3], piece: 'c' }],
      }).firstPly,
    ).toBe(4);
  });

  test.each([
    { from: [-1, 9] },
    { to: [7, 10] },
    { to: [7.5, 9] },
    { captured: 'r' },
    { captured: 'unknown' },
    { color: 'red' },
  ])('异常记录 %j 不能用来推测盘面', (invalid) => {
    expect(
      buildXiangqiReplay({
        ...live,
        history: [...history.slice(0, 3), { ...history[3], ...invalid }],
      }).firstPly,
    ).toBe(4);
  });

  test('历史视图只包含当时的行棋与吃子，不带入实盘将军、终局或合法落点', () => {
    const replay = buildXiangqiReplay(live);
    const viewed = xiangqiReplayPosition(live, replay, 2);
    expect(viewed.board).toEqual(boards[2]);
    expect(viewed.turn).toBe('red');
    expect(viewed.history).toEqual(history.slice(0, 2));
    expect(viewed.last).toEqual(history[1]);
    expect(viewed.last_moves).toEqual({ red: history[0], black: history[1] });
    expect(viewed.legal).toEqual([]);
    expect(viewed.check).toBe(false);
    expect(viewed.over).toBe(false);
    expect(viewed.repeat).toBe(0);
    expect(viewed.result).toBe('');
    expect(xiangqiReplayPosition(live, replay, 1).turn).toBe('black');
    expect(xiangqiReplayPosition(live, replay, 4)).toBe(live);
    const old = { ...live, history: history.slice(2) };
    expect(
      xiangqiReplayPosition(old, buildXiangqiReplay(old), 2).last,
    ).toBeNull();
  });

  test('新增棋步保留回看，悔棋撤掉该局面或换局后返回实盘', () => {
    const gameId = 'room:12';
    const review = { gameId, ply: 2, board: boards[2].join('') };
    expect(resolveXiangqiReview(buildXiangqiReplay(live), review, gameId)).toBe(
      2,
    );
    const afterUndo = {
      ...live,
      plies: 3,
      board: boards[3],
      history: history.slice(0, 3),
    };
    expect(
      resolveXiangqiReview(buildXiangqiReplay(afterUndo), review, gameId),
    ).toBe(2);
    expect(
      resolveXiangqiReview(
        buildXiangqiReplay({
          ...live,
          plies: 1,
          board: boards[1],
          history: history.slice(0, 1),
        }),
        review,
        gameId,
      ),
    ).toBeNull();
    expect(
      resolveXiangqiReview(buildXiangqiReplay(live), review, 'room:13'),
    ).toBeNull();
    const differentBoard = { ...review, board: boards[1].join('') };
    expect(
      resolveXiangqiReview(buildXiangqiReplay(live), differentBoard, gameId),
    ).toBeNull();
    expect(
      resolveXiangqiReview(
        buildXiangqiReplay(live),
        { ...review, ply: 4 },
        gameId,
      ),
    ).toBeNull();
  });
});
