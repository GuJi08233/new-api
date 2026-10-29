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
import { Button } from '@douyinfe/semi-ui';
import { Lightbulb, Undo2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { formatCoins } from '../api';

// 五子棋、四子棋、黑白棋棋盘下方的悔棋与提示。state.undo 是服务端给看的人的悔棋状态(还剩几次免费、下一次的费用、
// 能不能提、谁在等回应)；hintStatus 与 onHint 只在人机对战里有，押注房间里对手是真人，不提供提示。
// 人机对战里电脑总是同意悔棋；房间里要对方同意，对方的请求在这里同意或拒绝。
export const BoardAssist = ({
  state,
  seat,
  busy,
  over,
  onUndo,
  hintStatus,
  onHint,
  hintShown,
}) => {
  const { t } = useTranslation();
  const undo = state?.undo;
  if (over || !undo) return null;
  const hintAvailable = !!onHint && !!hintStatus?.available && !hintShown;
  return (
    <div className='board-assist'>
      {undo.pending_by === seat && (
        <span className='board-assist-note'>{t('已提出悔棋，等对方回应')}</span>
      )}
      {undo.pending_by >= 0 && undo.pending_by !== seat && (
        <>
          <span className='board-assist-note'>{t('对方想悔棋')}</span>
          <Button
            size='small'
            theme='solid'
            type='primary'
            disabled={busy}
            onClick={() => onUndo(true)}
          >
            {t('同意')}
          </Button>
          <Button size='small' disabled={busy} onClick={() => onUndo(false)}>
            {t('拒绝')}
          </Button>
        </>
      )}
      {undo.pending_by < 0 && !undo.declined && (
        <Button
          size='small'
          icon={<Undo2 size={14} />}
          disabled={busy || !undo.available}
          onClick={() => onUndo()}
        >
          {undo.free_left > 0
            ? t('悔棋（免费 {{left}} 次）', { left: undo.free_left })
            : t('悔棋（{{cost}} 游戏币）', { cost: formatCoins(undo.cost) })}
        </Button>
      )}
      {undo.pending_by < 0 && undo.declined && (
        <span className='board-assist-note'>{t('对方拒绝了悔棋')}</span>
      )}
      {onHint && hintStatus && (
        <Button
          size='small'
          icon={<Lightbulb size={14} />}
          disabled={busy || !hintAvailable}
          onClick={onHint}
        >
          {hintStatus.free_left > 0
            ? t('提示（免费 {{left}} 次）', { left: hintStatus.free_left })
            : t('提示（{{cost}} 游戏币）', {
                cost: formatCoins(hintStatus.cost),
              })}
        </Button>
      )}
    </div>
  );
};

// 当前局面的提示：只认分析时的落子数和现在一样的那一份，走了一步或悔棋之后旧提示作废。
export function currentHint(hint, state) {
  return hint && state && hint.moves === state.moves ? hint : null;
}
