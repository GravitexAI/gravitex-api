/*
Copyright (C) 2023-2026 QuantumNous

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
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { AuditLogViewer } from './components/audit-log-viewer'

// alpha：没有官方的 casbin audit.read 权限，管理员（role>=10）可看全部，普通用户只看自己的。
export function AuditLogs() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const [scope, setScope] = useState<'all' | 'self'>('all')
  const [accessRevoked, setAccessRevoked] = useState(false)
  const canReadAll = !!user && user.role >= ROLE.ADMIN
  const effectiveScope = canReadAll && !accessRevoked ? scope : 'self'
  const userId = user?.id
  const handleAccessDenied = useCallback(async () => {
    setAccessRevoked(true)
    setScope('self')
  }, [])
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Audit Logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {canReadAll && !accessRevoked && (
          <Tabs
            value={scope}
            onValueChange={(value) =>
              setScope(value === 'all' ? 'all' : 'self')
            }
          >
            <TabsList aria-label={t('View scope')}>
              <TabsTrigger value='all'>{t('All')}</TabsTrigger>
              <TabsTrigger value='self'>{t('Only Mine')}</TabsTrigger>
            </TabsList>
          </Tabs>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3'>
          {accessRevoked && (
            <p role='status' className='text-muted-foreground shrink-0 text-xs'>
              {t('Audit access changed. Showing only your records.')}
            </p>
          )}
          <div className='min-h-0 flex-1'>
            <AuditLogViewer
              key={`${userId}:${effectiveScope}`}
              scope={effectiveScope}
              onAccessDenied={handleAccessDenied}
            />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
