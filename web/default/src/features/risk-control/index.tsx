import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { RiskRankings } from './components/risk-rankings'
import { RiskSettings } from './components/risk-settings'

export function RiskControl() {
  const { t } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const isRoot = !!userRole && userRole >= ROLE.SUPER_ADMIN

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk Control')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Monitor abuse via IP / UA rankings and configure auto-ban rules')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <Tabs defaultValue="rankings">
          <TabsList>
            <TabsTrigger value="rankings">{t('Abuse Rankings')}</TabsTrigger>
            {isRoot && (
              <TabsTrigger value="settings">{t('Risk Settings')}</TabsTrigger>
            )}
          </TabsList>
          <TabsContent value="rankings" className="mt-4">
            <RiskRankings />
          </TabsContent>
          {isRoot && (
            <TabsContent value="settings" className="mt-4">
              <RiskSettings />
            </TabsContent>
          )}
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
