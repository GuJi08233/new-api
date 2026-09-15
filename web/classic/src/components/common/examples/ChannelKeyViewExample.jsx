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

import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Modal } from '@douyinfe/semi-ui';
import { createApiCalls } from '../../../services/secureVerification';
import ChannelKeyDisplay from '../ui/ChannelKeyDisplay';

/**
 * 渠道密钥查看组件使用示例
 * 展示如何使用通用安全验证系统
 */
const ChannelKeyViewExample = ({ channelId }) => {
  const { t } = useTranslation();
  const [keyData, setKeyData] = useState('');
  const [showKeyModal, setShowKeyModal] = useState(false);

  const handleViewKey = async () => {
    const result = await createApiCalls.viewChannelKey(channelId)();
    if (result.success && result.data?.key) {
      setKeyData(result.data.key);
      setShowKeyModal(true);
    }
  };

  return (
    <>
      {/* 查看密钥按钮 */}
      <Button type='primary' theme='outline' onClick={handleViewKey}>
        {t('查看密钥')}
      </Button>

      {/* 密钥显示模态框 */}
      <Modal
        title={t('渠道密钥信息')}
        visible={showKeyModal}
        onCancel={() => setShowKeyModal(false)}
        footer={
          <Button type='primary' onClick={() => setShowKeyModal(false)}>
            {t('完成')}
          </Button>
        }
        width={700}
        style={{ maxWidth: '90vw' }}
      >
        <ChannelKeyDisplay
          keyData={keyData}
          showSuccessIcon={true}
          successText={t('密钥获取成功')}
          showWarning={true}
        />
      </Modal>
    </>
  );
};

export default ChannelKeyViewExample;
