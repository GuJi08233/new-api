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
import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { useSubscriptionArchive } from './useSubscriptionArchive';

function SubscriptionArchiveProbe({ subscriptions, onRender }) {
  onRender(useSubscriptionArchive(subscriptions));
  return null;
}

function setupSubscriptionClock() {
  let nowMilliseconds = 1000000;
  let nextTimerId = 0;
  const timers = new Map();
  spyOn(Date, 'now').mockImplementation(() => nowMilliseconds);
  spyOn(globalThis, 'setTimeout').mockImplementation((callback, delay) => {
    const id = ++nextTimerId;
    timers.set(id, { callback, delay });
    return id;
  });
  spyOn(globalThis, 'clearTimeout').mockImplementation((id) => {
    timers.delete(id);
  });
  return {
    timers,
    advanceTo(milliseconds) {
      nowMilliseconds = milliseconds;
    },
  };
}

afterEach(() => mock.restore());

test('无需刷新，到期时将订阅归档并更新生效数量，保留仍有效的未激活订阅', () => {
  const clock = setupSubscriptionClock();
  const active = { subscription: { id: 1, status: 'active', end_time: 1001 } };
  const inactive = {
    subscription: { id: 2, status: 'inactive', end_time: 1002 },
  };
  let snapshot;
  let renderer;
  act(() => {
    renderer = create(
      <SubscriptionArchiveProbe
        subscriptions={[active, inactive]}
        onRender={(value) => (snapshot = value)}
      />,
    );
  });
  expect(snapshot.activeCount).toBe(1);
  expect(snapshot.archivedSubscriptions).toEqual([]);
  const [timerId, timer] = [...clock.timers][0];
  expect(timer.delay).toBe(1000);

  clock.timers.delete(timerId);
  clock.advanceTo(1001000);
  act(() => timer.callback());

  expect(snapshot.activeCount).toBe(0);
  expect(snapshot.inactiveCount).toBe(1);
  expect(snapshot.currentSubscriptions).toEqual([inactive]);
  expect(snapshot.archivedSubscriptions).toEqual([active]);
  expect([...clock.timers.values()][0].delay).toBe(1000);
  act(() => renderer.unmount());
  expect(clock.timers.size).toBe(0);
});

test('较远的到期时间分段等待，避免浏览器定时器溢出并在卸载后取消等待', () => {
  const clock = setupSubscriptionClock();
  const durationMilliseconds = 40 * 86400 * 1000;
  let snapshot;
  let renderer;
  act(() => {
    renderer = create(
      <SubscriptionArchiveProbe
        subscriptions={[
          {
            subscription: {
              status: 'active',
              end_time: 1000 + durationMilliseconds / 1000,
            },
          },
        ]}
        onRender={(value) => (snapshot = value)}
      />,
    );
  });
  const [timerId, timer] = [...clock.timers][0];
  expect(timer.delay).toBe(2147483647);
  clock.timers.delete(timerId);
  clock.advanceTo(1000000 + timer.delay);
  act(() => timer.callback());

  expect(snapshot.activeCount).toBe(1);
  expect(snapshot.archivedSubscriptions).toEqual([]);
  expect([...clock.timers.values()][0].delay).toBe(
    durationMilliseconds - timer.delay,
  );
  act(() => renderer.unmount());
  expect(clock.timers.size).toBe(0);
});
