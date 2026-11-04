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

import React from 'react';
import { act, create } from 'react-test-renderer';
import i18next from 'i18next';
import { I18nextProvider } from 'react-i18next';
import { afterEach, beforeAll, beforeEach, expect, mock, test } from 'bun:test';

const i18n = i18next.createInstance();
const sent = [];
const reads = [];
let getReply;
let postReply;
let fightProps;
let confirmation;
const loggedOut = mock();
const patch = mock();
const info = { balance: 10, symbol: '币' };
const turnstile = { ready: true, take: () => '' };

mock.module('../../../helpers', () => ({
  showError: loggedOut,
  API: {
    get: (url, config) => {
      reads.push(url);
      return getReply(url, config);
    },
    post: (url, body) => {
      sent.push({ url, body });
      return postReply(url, body);
    },
  },
}));
mock.module('./useGameInfo', () => ({
  useGameInfo: () => ({ info, patch, loading: false, unavailable: '' }),
}));
mock.module('./usePlayTurnstile', () => ({
  usePlayTurnstile: () => turnstile,
  PlayTurnstile: () => null,
}));
mock.module('@douyinfe/semi-ui', () => ({
  Button: ({ children, onClick, disabled, ...props }) => (
    <button
      aria-label={props['aria-label']}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </button>
  ),
  Modal: { confirm: (options) => (confirmation = options) },
}));
mock.module('./GameLayout', () => ({
  default: ({ children }) => <div>{children}</div>,
}));
mock.module('./SideBox', () => ({
  RuleList: () => null,
  SideBox: () => null,
  StageMessage: () => null,
}));
mock.module('./skill', () => ({
  SkillSide: () => null,
  GameClock: () => null,
  playSkillFeedback: () => {},
  skillResultMessage: () => '',
}));
mock.module('./LocalBest', () => ({ localBestNote: () => '' }));
mock.module('./useLocalBest', () => ({
  useLocalBest: () => ({ bests: {}, record: () => null }),
}));
mock.module('./sound', () => ({ playSound: () => {} }));
mock.module('../sect/parts', () => ({
  useSectText: () => ({ resources: {} }),
  eventSentence: () => '',
}));
mock.module('../wendao/doufa/Fight', () => ({
  default: (props) => {
    fightProps = props;
    return <div />;
  },
}));

let useStepPuzzle;
let usePvpRoom;
let Sokoban;
let DuelRun;
beforeAll(async () => {
  await i18n.init({ lng: 'zh-CN', resources: {}, fallbackLng: false });
  ({ useStepPuzzle } = await import('./useStepPuzzle'));
  ({ usePvpRoom } = await import('./pvp/usePvpRoom'));
  ({ default: Sokoban } = await import('../sokoban'));
  ({ default: DuelRun } = await import('../wendao/duel/DuelRun'));
});

function deferred() {
  let resolve;
  const promise = new Promise((done) => (resolve = done));
  return { promise, resolve };
}

const ok = (data) => Promise.resolve({ data: { success: true, data } });
const oldRound = {
  session_id: 10,
  version: 0,
  status: 'playing',
  difficulty: 1,
  moves: 0,
};
const newRound = { ...oldRound, session_id: 11 };
let rendered;
let latest;
const nativeGlobals = {
  window: globalThis.window,
  document: globalThis.document,
  localStorage: globalThis.localStorage,
};

beforeEach(() => {
  sent.length = 0;
  reads.length = 0;
  patch.mockClear();
  loggedOut.mockClear();
  getReply = () => ok({ session: oldRound });
  postReply = (url) =>
    ok(
      url.endsWith('/start')
        ? newRound
        : { ...oldRound, version: 1, status: 'finished' },
    );
  const data = new Map();
  globalThis.localStorage = {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, value),
    removeItem: (key) => data.delete(key),
  };
  globalThis.window = { addEventListener() {}, removeEventListener() {} };
  globalThis.document = {};
});

afterEach(async () => {
  await act(async () => rendered?.unmount());
  rendered = null;
  for (const [key, value] of Object.entries(nativeGlobals)) {
    if (value === undefined) delete globalThis[key];
    else globalThis[key] = value;
  }
});

async function mount(element) {
  await act(async () => {
    rendered = create(<I18nextProvider i18n={i18n}>{element}</I18nextProvider>);
  });
}

function Puzzle() {
  latest = useStepPuzzle('lightsout');
  return null;
}

test('starting during the initial restore finishes loading and ignores the old read', async () => {
  const pending = deferred();
  getReply = () => pending.promise;
  await mount(<Puzzle />);
  expect(latest.loading).toBe(true);
  await act(async () => latest.send('start'));
  expect(latest.loading).toBe(false);
  expect(latest.round.session_id).toBe(11);
  await act(async () => pending.resolve(await ok({ session: null })));
  expect(latest.round.session_id).toBe(11);
});

test.each([oldRound, null])(
  'a delayed restore cannot overwrite a newly started puzzle: %j',
  async (stale) => {
    await mount(<Puzzle />);
    const pending = deferred();
    getReply = () => pending.promise;
    const restoring = latest.restore(10);
    await act(async () => {
      await latest.send('abandon');
      await latest.send('start');
    });
    expect(latest.round.session_id).toBe(11);
    await act(async () => {
      pending.resolve(await ok({ session: stale }));
      await restoring;
    });
    expect(latest.round.session_id).toBe(11);
    expect(latest.error).toBe('');
  },
);

test('only the latest restore updates the puzzle or its error', async () => {
  await mount(<Puzzle />);
  const pending = deferred();
  getReply = () => pending.promise;
  const first = latest.restore();
  getReply = () => ok({ session: newRound });
  await act(async () => latest.restore());
  await act(async () => {
    pending.resolve({ data: { success: false, message: 'stale failure' } });
    await first;
  });
  expect(latest.round.session_id).toBe(11);
  expect(latest.error).toBe('');
});

test('a settled puzzle cannot regress to playing at the same version', async () => {
  await mount(<Puzzle />);
  postReply = () => ok({ progress: {}, balance: 12, best_score: 1 });
  await act(async () => latest.send('abandon'));
  expect(latest.round.status).toBe('finished');
  await act(async () => latest.restore(10));
  expect(latest.round.status).toBe('finished');
  expect(patch).toHaveBeenCalledTimes(1);
});

test('Sokoban keeps moves after 401 and submits them after reloading', async () => {
  const session = {
    session_id: 7,
    elapsed: 30,
    puzzle: {
      width: 5,
      height: 3,
      walls: [0, 1, 2, 3, 4, 5, 9, 10, 11, 12, 13, 14],
      player: 6,
      boxes: [7],
      goals: [8],
    },
  };
  getReply = () => ok({ session });
  postReply = () => Promise.reject({ response: { status: 401 } });
  await mount(<Sokoban />);
  await act(async () => {
    rendered.root.findByProps({ 'aria-label': '右' }).props.onClick();
  });
  expect(loggedOut).toHaveBeenCalledTimes(1);
  expect(JSON.parse(localStorage.getItem('game-sokoban-v1'))).toEqual({
    session_id: 7,
    moves: 'R',
  });
  expect(sent[0].body).toEqual({ session_id: 7, moves: 'R', abandon: false });
  await act(async () => rendered.unmount());
  await mount(<Sokoban />);
  postReply = () => ok({ balance: 11, progress: {}, best_score: 1 });
  const submit = rendered.root
    .findAllByType('button')
    .find((node) => node.props.children === '提交成绩');
  expect(submit).toBeDefined();
  await act(async () => submit.props.onClick());
  expect(sent[1].body).toEqual(sent[0].body);
  expect(localStorage.getItem('game-sokoban-v1')).toBeNull();
});

function Room({ onUnavailable }) {
  latest = usePvpRoom(i18n.t, onUnavailable);
  return null;
}

test.each(['room_not_player', 'room_not_found'])(
  'room polling stops and refreshes the lobby for %s',
  async (code) => {
    const refresh = mock();
    getReply = () =>
      Promise.resolve({
        data: { success: false, message: 'unavailable', code },
      });
    await mount(<Room onUnavailable={refresh} />);
    await act(async () => {
      latest.setRoom({ id: 7, version: 0, status: 'waiting' });
    });
    expect(latest.room).toBeNull();
    expect(reads).toHaveLength(1);
    expect(refresh).toHaveBeenCalledTimes(1);
  },
);

test('an old poll cannot remove a newer room', async () => {
  const old = deferred();
  const next = deferred();
  const refresh = mock();
  getReply = (url) => (url.includes('/7?') ? old.promise : next.promise);
  await mount(<Room onUnavailable={refresh} />);
  await act(async () =>
    latest.setRoom({ id: 7, version: 0, status: 'waiting' }),
  );
  await act(async () =>
    latest.setRoom({ id: 8, version: 0, status: 'waiting' }),
  );
  await act(async () => {
    old.resolve({
      data: {
        success: false,
        message: 'not a member',
        code: 'room_not_player',
      },
    });
  });
  expect(latest.room.id).toBe(8);
  expect(refresh).not.toHaveBeenCalled();
});

test('manual duel settlement retries preserve the battle and request id', async () => {
  let attempts = 0;
  postReply = (url) => {
    if (url.endsWith('/start'))
      return ok({ duel: { seed: 1, token: 'fight-1' } });
    if (++attempts <= 2) return Promise.reject(new Error('lost answer'));
    return ok({ won: true, ticks: 2520, kills: 1, events: [] });
  };
  await mount(<DuelRun kind='trial' id='tianjian' onDone={() => {}} />);
  await act(async () => fightProps.onEnd({ inputs: [0, 1] }));
  const retry = rendered.root
    .findAllByType('button')
    .find((node) => node.props.children === '重新提交');
  expect(retry).toBeDefined();
  await act(async () => retry.props.onClick());
  const requests = sent.filter(({ url }) => url.endsWith('/clear'));
  expect(requests).toHaveLength(3);
  expect(requests[0].body.battle_token).toBe('fight-1');
  expect(requests[0].body.request_id).toBeTruthy();
  expect(requests[1].body).toEqual(requests[0].body);
  expect(requests[2].body).toEqual(requests[0].body);
  const html = JSON.stringify(rendered.toJSON());
  expect(html).toContain('凯旋');
  expect(html).toContain('0:42');
  expect(html).not.toContain('NaN');
});

test('duel give-up identifies the visible fight and reports a stale rejection', async () => {
  postReply = (url) =>
    url.endsWith('/start')
      ? ok({ duel: { seed: 1, token: 'fight-1' } })
      : Promise.resolve({ data: { success: false, message: 'battle moved' } });
  const onDone = mock();
  await mount(<DuelRun kind='trial' id='tianjian' onDone={onDone} />);
  await act(async () => fightProps.onExit());
  await act(async () => confirmation.onOk());
  expect(sent[1].body.battle_token).toBe('fight-1');
  expect(onDone).not.toHaveBeenCalled();
  expect(JSON.stringify(rendered.toJSON())).toContain('battle moved');
});
