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
import { useNavigate } from 'react-router-dom';
import { Button, Empty, Spin } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import { ArrowLeft, Coins } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import '../games.css';

const ILLUSTRATION_SIZE = { width: 150, height: 150 };

// 游戏页的外框：顶栏(返回游戏中心、标题、余额)，加载中与不可用时的占位，以及左边台面、右边信息栏的布局。
// side 为空时台面占满整行；className 加在容器上，供各游戏调整台面宽度。
const GameLayout = ({
  title,
  loading,
  unavailable,
  balance,
  symbol,
  side,
  className = '',
  children,
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();

  let body;
  if (loading) {
    body = (
      <div className='game-placeholder'>
        <Spin spinning size='large' />
      </div>
    );
  } else if (unavailable) {
    body = (
      <Empty
        image={<IllustrationConstruction style={ILLUSTRATION_SIZE} />}
        darkModeImage={
          <IllustrationConstructionDark style={ILLUSTRATION_SIZE} />
        }
        title={unavailable}
      >
        <Button onClick={() => navigate('/games')}>{t('返回游戏中心')}</Button>
      </Empty>
    );
  } else {
    body = (
      <div className={side ? 'game-layout' : 'game-layout is-single'}>
        <div className='game-main'>{children}</div>
        {side && <aside className='game-side'>{side}</aside>}
      </div>
    );
  }

  return (
    <div className='games-page'>
      <div className='games-glow' aria-hidden />
      <div className={`games-container game-page ${className}`}>
        <div className='game-topbar'>
          <Button
            theme='borderless'
            icon={<ArrowLeft size={16} />}
            onClick={() => navigate('/games')}
          >
            {t('游戏中心')}
          </Button>
          <h1 className='game-title'>{title}</h1>
          {!loading && !unavailable && balance !== undefined && (
            <div className='game-topbar-balance' title={t('游戏币余额')}>
              <Coins size={16} />
              <strong>{balance}</strong>
              <span>{symbol}</span>
            </div>
          )}
        </div>
        {body}
      </div>
    </div>
  );
};

export default GameLayout;
