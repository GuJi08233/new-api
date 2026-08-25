import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Form,
  Row,
  Col,
  Typography,
  Spin,
  Table,
  Modal,
  Input,
  Space,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';

const { Text } = Typography;

export default function SettingsPaymentGatewayEthereum({ options, refresh }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [signingKeyConfigured, setSigningKeyConfigured] = useState(false);
  const [inputs, setInputs] = useState({
    EthereumEnabled: false,
    EthereumChainId: 11155111,
    EthereumContractAddress: '',
    EthereumAlchemyWebhookSigningKey: '',
    EthereumMinTopUp: 1,
    EthereumWalletConnectProjectID: '',
    EthereumWalletConnectAppName: '',
    EthereumWalletConnectAppDescription: '',
    EthereumWalletConnectAppURL: '',
    EthereumWalletConnectAppIcon: '',
    EthereumWalletConnectRelayProxyEnabled: false,
    EthereumWalletConnectPrimaryRelayURL: '',
    EthereumWalletConnectBackupRelayURL: '',
  });
  const [tokens, setTokens] = useState([
    {
      symbol: 'ETH',
      address: '0x0000000000000000000000000000000000000000',
      decimals: 18,
      price: '0.001',
    },
  ]);
  const formApiRef = useRef(null);

  // Token editing modal state
  const [tokenModalVisible, setTokenModalVisible] = useState(false);
  const [editingTokenIndex, setEditingTokenIndex] = useState(-1);
  const [tokenForm, setTokenForm] = useState({
    symbol: '',
    address: '',
    decimals: '18',
    price: '1.0',
  });

  // Sync from parent options prop
  useEffect(() => {
    if (!options || !formApiRef.current) return;
    const currentInputs = {
      EthereumEnabled:
        options.EthereumEnabled === 'true' || options.EthereumEnabled === true,
      EthereumChainId: parseInt(options.EthereumChainId) || 11155111,
      EthereumContractAddress: options.EthereumContractAddress || '',
      EthereumAlchemyWebhookSigningKey:
        options.EthereumAlchemyWebhookSigningKey || '',
      EthereumMinTopUp: parseInt(options.EthereumMinTopUp) || 1,
      EthereumWalletConnectProjectID:
        options.EthereumWalletConnectProjectID || '',
      EthereumWalletConnectAppName: options.EthereumWalletConnectAppName || '',
      EthereumWalletConnectAppDescription:
        options.EthereumWalletConnectAppDescription || '',
      EthereumWalletConnectAppURL: options.EthereumWalletConnectAppURL || '',
      EthereumWalletConnectAppIcon: options.EthereumWalletConnectAppIcon || '',
      EthereumWalletConnectRelayProxyEnabled:
        options.EthereumWalletConnectRelayProxyEnabled === 'true' ||
        options.EthereumWalletConnectRelayProxyEnabled === true,
      EthereumWalletConnectPrimaryRelayURL:
        options.EthereumWalletConnectPrimaryRelayURL || '',
      EthereumWalletConnectBackupRelayURL:
        options.EthereumWalletConnectBackupRelayURL || '',
    };
    setSigningKeyConfigured(
      options.EthereumAlchemyWebhookSigningKeyConfigured === 'true' ||
        options.EthereumAlchemyWebhookSigningKeyConfigured === true,
    );
    setInputs(currentInputs);
    formApiRef.current.setValues(currentInputs);

    try {
      const parsed = JSON.parse(options.EthereumSupportedTokens || '[]');
      if (Array.isArray(parsed) && parsed.length > 0) {
        setTokens(parsed);
      }
    } catch {
      // keep defaults
    }
  }, [options]);

  const handleFormChange = (values) => setInputs(values);

  const submitSettings = async () => {
    setLoading(true);
    try {
      const formValues = formApiRef.current?.getValues?.() || inputs;
      const relayProxyEnabled = Boolean(
        formValues.EthereumWalletConnectRelayProxyEnabled,
      );
      const opts = [
        {
          key: 'EthereumEnabled',
          value: formValues.EthereumEnabled ? 'true' : 'false',
        },
        {
          key: 'EthereumChainId',
          value: String(formValues.EthereumChainId || 11155111),
        },
        {
          key: 'EthereumContractAddress',
          value: formValues.EthereumContractAddress || '',
        },
        {
          key: 'EthereumMinTopUp',
          value: String(formValues.EthereumMinTopUp || 1),
        },
        {
          key: 'EthereumWalletConnectProjectID',
          value: formValues.EthereumWalletConnectProjectID || '',
        },
        {
          key: 'EthereumWalletConnectAppName',
          value: formValues.EthereumWalletConnectAppName || '',
        },
        {
          key: 'EthereumWalletConnectAppDescription',
          value: formValues.EthereumWalletConnectAppDescription || '',
        },
        {
          key: 'EthereumWalletConnectAppURL',
          value: formValues.EthereumWalletConnectAppURL || '',
        },
        {
          key: 'EthereumWalletConnectAppIcon',
          value: formValues.EthereumWalletConnectAppIcon || '',
        },
        {
          key: 'EthereumWalletConnectRelayProxyEnabled',
          value: relayProxyEnabled ? 'true' : 'false',
        },
        {
          key: 'EthereumWalletConnectPrimaryRelayURL',
          value: relayProxyEnabled
            ? ''
            : formValues.EthereumWalletConnectPrimaryRelayURL || '',
        },
        {
          key: 'EthereumWalletConnectBackupRelayURL',
          value: relayProxyEnabled
            ? ''
            : formValues.EthereumWalletConnectBackupRelayURL || '',
        },
        { key: 'EthereumSupportedTokens', value: JSON.stringify(tokens) },
      ];
      if ((formValues.EthereumAlchemyWebhookSigningKey || '').trim()) {
        opts.push({
          key: 'EthereumAlchemyWebhookSigningKey',
          value: formValues.EthereumAlchemyWebhookSigningKey.trim(),
        });
      }

      const results = await Promise.all(
        opts.map((o) =>
          API.put('/api/option/', { key: o.key, value: o.value }),
        ),
      );

      const errors = results.filter((r) => !r.data.success);
      if (errors.length > 0) {
        errors.forEach((r) => showError(r.data.message));
      } else {
        showSuccess(t('更新成功'));
        refresh?.();
      }
    } catch {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  // Token table management
  const openAddToken = () => {
    setEditingTokenIndex(-1);
    setTokenForm({ symbol: '', address: '', decimals: '18', price: '1.0' });
    setTokenModalVisible(true);
  };
  const openEditToken = (record, index) => {
    setEditingTokenIndex(index);
    setTokenForm({
      symbol: record.symbol,
      address: record.address,
      decimals: String(record.decimals),
      price: record.price,
    });
    setTokenModalVisible(true);
  };
  const handleTokenModalOk = () => {
    if (!tokenForm.symbol.trim()) {
      showError(t('代币符号不能为空'));
      return;
    }
    const parsedDecimals = Number(tokenForm.decimals);
    const newToken = {
      symbol: tokenForm.symbol.trim(),
      address: tokenForm.address.trim(),
      decimals:
        Number.isInteger(parsedDecimals) && parsedDecimals >= 0
          ? parsedDecimals
          : 18,
      price: tokenForm.price.trim() || '1.0',
    };
    if (editingTokenIndex === -1) {
      setTokens([...tokens, newToken]);
    } else {
      const updated = [...tokens];
      updated[editingTokenIndex] = newToken;
      setTokens(updated);
    }
    setTokenModalVisible(false);
  };
  const deleteToken = (index) =>
    setTokens(tokens.filter((_, i) => i !== index));

  const relayProxyEnabled = Boolean(
    inputs.EthereumWalletConnectRelayProxyEnabled,
  );

  const tokenColumns = [
    { title: t('符号'), dataIndex: 'symbol', width: 100 },
    {
      title: t('合约地址'),
      dataIndex: 'address',
      render: (v) =>
        v === '0x0000000000000000000000000000000000000000' ? (
          <Text type='tertiary'>{t('ETH (原生)')}</Text>
        ) : (
          <Text style={{ fontFamily: 'monospace', fontSize: 12 }}>
            {v?.slice(0, 10)}...{v?.slice(-8)}
          </Text>
        ),
    },
    { title: t('精度'), dataIndex: 'decimals', width: 80 },
    {
      title: t('单价'),
      dataIndex: 'price',
      width: 120,
      render: (v, record) => (
        <Text>
          {v} {record.symbol}/unit
        </Text>
      ),
    },
    {
      title: t('操作'),
      key: 'action',
      width: 150,
      render: (_, record, index) => (
        <Space>
          <Button size='small' onClick={() => openEditToken(record, index)}>
            {t('编辑')}
          </Button>
          <Button size='small' type='danger' onClick={() => deleteToken(index)}>
            {t('删除')}
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={t('Ethereum 支付设置')}>
          <Banner
            type='info'
            description={
              <>
                {t(
                  '通过 MetaMask 钱包接受 ETH 及 ERC-20 代币支付。需要：1) 已部署智能合约 2) 配置 Alchemy Custom Webhook。',
                )}
                <br />
                {t(
                  '如需支持手机钱包二维码连接，请配置 WalletConnect Project ID。',
                )}
                <br />
                {t('Webhook 地址：')}
                <Text copyable code>
                  {window.location.origin}/api/ethereum/webhook
                </Text>
              </>
            }
          />

          <Row gutter={24} style={{ marginTop: 16 }}>
            <Col xs={24} md={8}>
              <Form.Switch
                field='EthereumEnabled'
                label={t('启用 Ethereum 支付')}
                checkedText='|'
                uncheckedText='O'
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={12}>
              <Form.InputNumber
                field='EthereumChainId'
                label={t('Chain ID')}
                placeholder='11155111'
                extraText={t('Sepolia=11155111, 主网=1, Polygon=137')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumContractAddress'
                label={t('合约地址')}
                placeholder='0x...'
                extraText={t('部署的 NewApiPayment 合约地址')}
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={16}>
              <Form.Input
                field='EthereumAlchemyWebhookSigningKey'
                label={t('Alchemy Webhook 签名密钥')}
                placeholder={t('从 Alchemy 控制台复制 Signing Key')}
                mode='password'
                extraText={t(
                  '用于验证 Alchemy 发来的 webhook 签名；留空表示保持现有密钥不变',
                )}
              />
              <div style={{ marginTop: 6 }}>
                <Text type={signingKeyConfigured ? 'success' : 'warning'}>
                  {t('Webhook Signing Key 状态：')}
                  {signingKeyConfigured ? t('已配置') : t('未配置')}
                </Text>
              </div>
            </Col>
            <Col xs={24} md={8}>
              <Form.InputNumber
                field='EthereumMinTopUp'
                label={t('最低充值数量')}
                min={1}
                step={1}
                placeholder='1'
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectProjectID'
                label={t('WalletConnect Project ID')}
                placeholder={t(
                  '从 cloud.walletconnect.com / Reown Dashboard 获取',
                )}
                extraText={t('配置后可在无浏览器扩展时拉起二维码连接')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectAppName'
                label={t('WalletConnect 应用名称')}
                placeholder={t('默认使用当前站点名称')}
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectAppDescription'
                label={t('WalletConnect 应用描述')}
                placeholder={t('可选')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectAppURL'
                label={t('WalletConnect 应用地址')}
                placeholder='https://your-domain.com'
                extraText={t('留空则默认使用当前站点地址')}
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectAppIcon'
                label={t('WalletConnect 图标地址')}
                placeholder='https://your-domain.com/icon.png'
                extraText={t('可选，建议使用公开可访问的 HTTPS 图片地址')}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.Switch
                field='EthereumWalletConnectRelayProxyEnabled'
                label={t('启用本站 WalletConnect Relay 代理')}
                checkedText='|'
                uncheckedText='O'
                extraText={t(
                  '开启后优先使用本站 /api/walletconnect/relay 代理官方 Relay，并忽略下方主/备用 Relay URL，避免二次代理',
                )}
              />
            </Col>
          </Row>

          <Row gutter={24}>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectPrimaryRelayURL'
                label={t('WalletConnect 主 Relay URL')}
                placeholder='wss://relay.walletconnect.com'
                disabled={relayProxyEnabled}
                extraText={t(
                  relayProxyEnabled
                    ? '已启用本站代理，此配置保存时会被清空'
                    : '用于二维码配对和钱包连接；留空则使用 WalletConnect 默认 Relay',
                )}
              />
            </Col>
            <Col xs={24} md={12}>
              <Form.Input
                field='EthereumWalletConnectBackupRelayURL'
                label={t('WalletConnect 备用 Relay URL')}
                placeholder='wss://your-backup-relay.example.com'
                disabled={relayProxyEnabled}
                extraText={t(
                  relayProxyEnabled
                    ? '已启用本站代理，此配置保存时会被清空'
                    : '主 Relay 初始化失败时自动尝试备用地址',
                )}
              />
            </Col>
          </Row>

          <Button
            theme='solid'
            onClick={submitSettings}
            style={{ marginTop: 16 }}
          >
            {t('保存 Ethereum 设置')}
          </Button>
        </Form.Section>
      </Form>

      {/* Token management */}
      <div style={{ marginTop: 24 }}>
        <Typography.Title heading={6} style={{ marginBottom: 8 }}>
          {t('接受的代币')}
        </Typography.Title>
        <Text type='secondary'>
          {t(
            '地址填 0x000...000 代表原生 ETH；ERC-20 填合约地址。价格 = 充值 1 单位需要多少代币。',
          )}
        </Text>
        <div style={{ margin: '12px 0' }}>
          <Button onClick={openAddToken}>{t('新增代币')}</Button>
        </div>
        <Table
          columns={tokenColumns}
          dataSource={tokens}
          rowKey={(_, i) => i}
          pagination={false}
          size='small'
        />
        <Button
          theme='solid'
          onClick={submitSettings}
          style={{ marginTop: 12 }}
        >
          {t('保存代币配置')}
        </Button>
      </div>

      {/* Token edit modal */}
      <Modal
        title={editingTokenIndex === -1 ? t('新增代币') : t('编辑代币')}
        visible={tokenModalVisible}
        onOk={handleTokenModalOk}
        onCancel={() => setTokenModalVisible(false)}
        okText={t('确定')}
        cancelText={t('取消')}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {[
            {
              label: t('符号'),
              key: 'symbol',
              placeholder: 'ETH / USDT / USDC',
            },
            {
              label: t('合约地址'),
              key: 'address',
              placeholder: '0x000...000 (ETH) 或 ERC-20 地址',
            },
            {
              label: t('精度'),
              key: 'decimals',
              placeholder: '18 (ETH) / 6 (USDT)',
            },
            {
              label: t('单价'),
              key: 'price',
              placeholder: '0.001 (每充值1单位扣多少代币)',
            },
          ].map(({ label, key, placeholder }) => (
            <div key={key}>
              <Text strong>{label}</Text>
              <Input
                value={String(tokenForm[key])}
                onChange={(v) => setTokenForm({ ...tokenForm, [key]: v })}
                placeholder={placeholder}
                style={{ marginTop: 4 }}
              />
            </div>
          ))}
        </div>
      </Modal>
    </Spin>
  );
}
