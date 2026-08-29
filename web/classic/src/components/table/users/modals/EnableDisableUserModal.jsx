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

import React, { useEffect, useState } from 'react';
import { Modal, Space, TextArea, Typography } from '@douyinfe/semi-ui';

const { Text } = Typography;

const EnableDisableUserModal = ({
  visible,
  onCancel,
  onConfirm,
  user,
  action,
  t,
}) => {
  const isDisable = action === 'disable';
  const [reason, setReason] = useState('');

  useEffect(() => {
    if (visible) {
      setReason('');
    }
  }, [visible]);

  return (
    <Modal
      title={isDisable ? t('确定要禁用此用户吗？') : t('确定要启用此用户吗？')}
      visible={visible}
      onCancel={onCancel}
      onOk={() => onConfirm(reason.trim())}
      type='warning'
    >
      <Space vertical align='start' style={{ width: '100%' }}>
        <Text>
          {isDisable ? t('此操作将禁用用户账户') : t('此操作将启用用户账户')}
        </Text>
        <TextArea
          value={reason}
          onChange={setReason}
          placeholder={isDisable ? t('封禁原因(可选)') : t('解禁原因(可选)')}
          rows={3}
        />
        <Text type='tertiary' size='small'>
          {t('原因将写入风控事件记录,便于日后审计与解封时参考。')}
        </Text>
      </Space>
    </Modal>
  );
};

export default EnableDisableUserModal;
