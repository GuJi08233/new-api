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

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';

const DIRECTION_THRESHOLD = 6;

// 后一张牌盖住前一张，露出的左边缘才是每张牌的实际命中区域；鼠标一次越过多张牌时也能找到完整范围。
function cardIndexAtX(cards, clientX) {
  let index = 0;
  for (let next = 1; next < cards.length; next += 1) {
    if (clientX < cards[next].left) break;
    index = next;
  }
  return index;
}

// hand 按页面从左到右排列。resetKey 标识一局中的实际回合变化，不能用每次请求都会变化的对象或 busy 标志。
// handProps 放在单行手牌容器上，getCardProps(card) 放在牌的按钮上；容器需 touch-action: pan-y，保留纵向滚动。
export default function useCardSelection({ hand, enabled, resetKey }) {
  const [selected, setSelected] = useState([]);
  const handRef = useRef(hand);
  const enabledRef = useRef(enabled);
  const selectedRef = useRef([]);
  const containerRef = useRef(null);
  const gestureRef = useRef(null);
  const suppressClickRef = useRef(false);
  handRef.current = hand;
  enabledRef.current = enabled;
  const handKey = hand.join(',');

  const updateSelection = useCallback((cards) => {
    const wanted = new Set(cards);
    const next = handRef.current.filter((card) => wanted.has(card));
    selectedRef.current = next;
    setSelected(next);
  }, []);

  const finishGesture = useCallback(
    (restore = false) => {
      const gesture = gestureRef.current;
      if (!gesture) return;
      gestureRef.current = null;
      // 包含单击：pointerup 后浏览器还会发 click，不应再切换一次。下一次 pointerdown 会解除这个标记。
      suppressClickRef.current = true;
      if (gesture.element.hasPointerCapture?.(gesture.pointerId)) {
        gesture.element.releasePointerCapture(gesture.pointerId);
      }
      if (restore) updateSelection(gesture.before);
    },
    [updateSelection],
  );

  const selectCards = useCallback(
    (cards) => {
      finishGesture();
      updateSelection(cards);
    },
    [finishGesture, updateSelection],
  );

  const clearSelection = useCallback(() => selectCards([]), [selectCards]);

  const updateRange = useCallback(
    (gesture, index) => {
      gesture.index = index;
      const first = Math.min(gesture.anchor, index);
      const last = Math.max(gesture.anchor, index);
      const next = new Set(gesture.before);
      for (let i = first; i <= last; i += 1) {
        const card = gesture.cards[i].card;
        if (gesture.selecting) next.add(card);
        else next.delete(card);
      }
      // 始终从按下时的快照计算，拖回起点时退出范围的牌会恢复原来的状态。
      updateSelection(next);
    },
    [updateSelection],
  );

  const onPointerDown = useCallback(
    (event) => {
      if (
        !enabledRef.current ||
        event.button !== 0 ||
        event.isPrimary === false ||
        gestureRef.current
      ) {
        return;
      }
      const element = containerRef.current;
      const button = event.target.closest?.('[data-card-id]');
      if (!element || !button || !element.contains(button) || button.disabled) {
        return;
      }
      const buttons = new Map(
        [...element.querySelectorAll('[data-card-id]')].map((item) => [
          item.dataset.cardId,
          item,
        ]),
      );
      const cards = handRef.current.map((card) => {
        const node = buttons.get(String(card));
        return node ? { card, left: node.getBoundingClientRect().left } : null;
      });
      if (cards.some((card) => !card)) return;
      const anchor = cards.findIndex(
        (item) => String(item.card) === button.dataset.cardId,
      );
      if (anchor < 0) return;
      suppressClickRef.current = false;
      const gesture = {
        pointerId: event.pointerId,
        element,
        cards,
        anchor,
        index: anchor,
        before: [...selectedRef.current],
        selecting: !selectedRef.current.includes(cards[anchor].card),
        startX: event.clientX,
        startY: event.clientY,
        // 鼠标直接选牌；触屏和笔先判断方向，纵向手势交给浏览器滚动。
        pending: event.pointerType !== 'mouse',
      };
      gestureRef.current = gesture;
      if (!gesture.pending) {
        element.setPointerCapture?.(event.pointerId);
        updateRange(gesture, anchor);
      }
    },
    [updateRange],
  );

  useEffect(() => {
    const onPointerMove = (event) => {
      const gesture = gestureRef.current;
      if (!gesture || gesture.pointerId !== event.pointerId) return;
      if (gesture.pending) {
        const dx = Math.abs(event.clientX - gesture.startX);
        const dy = Math.abs(event.clientY - gesture.startY);
        if (Math.max(dx, dy) < DIRECTION_THRESHOLD) return;
        if (dy > dx) {
          finishGesture(true);
          return;
        }
        gesture.pending = false;
        gesture.element.setPointerCapture?.(gesture.pointerId);
        updateRange(gesture, cardIndexAtX(gesture.cards, event.clientX));
      } else {
        const index = cardIndexAtX(gesture.cards, event.clientX);
        if (index !== gesture.index) updateRange(gesture, index);
      }
      if (event.cancelable) event.preventDefault();
    };
    const onPointerUp = (event) => {
      const gesture = gestureRef.current;
      if (!gesture || gesture.pointerId !== event.pointerId) return;
      if (gesture.pending) {
        const dx = Math.abs(event.clientX - gesture.startX);
        const dy = Math.abs(event.clientY - gesture.startY);
        if (dy >= DIRECTION_THRESHOLD && dy > dx) {
          finishGesture(true);
          return;
        }
        if (dx >= DIRECTION_THRESHOLD) gesture.pending = false;
      }
      // 不依赖每张牌都收到 move：即使只在远处收到松手，也提交起点到松手位置的整段。
      updateRange(
        gesture,
        gesture.pending
          ? gesture.anchor
          : cardIndexAtX(gesture.cards, event.clientX),
      );
      finishGesture();
    };
    const onPointerCancel = (event) => {
      if (gestureRef.current?.pointerId === event.pointerId)
        finishGesture(true);
    };
    const onLostPointerCapture = (event) => {
      const gesture = gestureRef.current;
      // 触屏最初隐式捕获在牌按钮上；横滑后转交给容器时，旧按钮也会发此事件，不能据此取消。
      if (
        gesture?.pointerId === event.pointerId &&
        event.target === gesture.element &&
        !gesture.element.hasPointerCapture(event.pointerId)
      ) {
        finishGesture(true);
      }
    };
    const onBlur = () => finishGesture(true);
    window.addEventListener('pointermove', onPointerMove, { passive: false });
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerCancel);
    window.addEventListener('lostpointercapture', onLostPointerCapture);
    window.addEventListener('blur', onBlur);
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerCancel);
      window.removeEventListener('lostpointercapture', onLostPointerCapture);
      window.removeEventListener('blur', onBlur);
      const gesture = gestureRef.current;
      gestureRef.current = null;
      if (gesture?.element.hasPointerCapture?.(gesture.pointerId)) {
        gesture.element.releasePointerCapture(gesture.pointerId);
      }
    };
  }, [finishGesture, updateRange]);

  useLayoutEffect(() => {
    finishGesture();
    updateSelection([]);
  }, [handKey, resetKey, finishGesture, updateSelection]);

  useLayoutEffect(() => {
    if (!enabled) finishGesture(true);
  }, [enabled, finishGesture]);

  const getCardProps = (card) => ({
    'data-card-id': card,
    draggable: false,
    onClick: () => {
      if (!enabledRef.current) return;
      const previous = selectedRef.current;
      selectCards(
        previous.includes(card)
          ? previous.filter((value) => value !== card)
          : [...previous, card],
      );
    },
  });

  return {
    selected,
    selectCards,
    clearSelection,
    getCardProps,
    handProps: {
      ref: containerRef,
      onPointerDown,
      onClickCapture: (event) => {
        // 键盘/辅助技术发出的 click.detail 为 0，仍交给牌的 onClick 单选。
        if (event.detail !== 0 && suppressClickRef.current) {
          event.preventDefault();
          event.stopPropagation();
          suppressClickRef.current = false;
        }
      },
      onDragStart: (event) => event.preventDefault(),
    },
  };
}
