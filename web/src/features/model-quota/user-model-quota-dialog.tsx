import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { formatQuota } from '@/lib/format'

import { getUserModelQuotaUsage, resetUserModelQuotaUsage } from './api'
import { formatTokensAsMillions } from './lib/token-units'
import type { UserModelQuotaUsage } from './types'

export function UserModelQuotaDialog({
  userId,
  open,
  onOpenChange,
}: {
  userId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['user-model-quota-usage', userId],
    queryFn: () => getUserModelQuotaUsage(userId as number),
    enabled: !!userId && open,
  })

  const resetMutation = useMutation({
    mutationFn: resetUserModelQuotaUsage,
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['user-model-quota-usage', userId],
      })
      toast.success(t('额度重置成功'))
    },
    onError: () => toast.error(t('额度重置失败')),
  })

  const usages: UserModelQuotaUsage[] = data?.data?.items ?? []

  function getProgressColor(percent: number) {
    if (percent >= 90) return 'bg-red-500'
    if (percent >= 70) return 'bg-yellow-500'
    return 'bg-green-500'
  }

  const ruleSourceText: Record<string, string> = {
    plan: t('订阅计划'),
    group: t('分组'),
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('用量限制使用情况')}</DialogTitle>
          <DialogDescription>
            {t('该用户当前周期的金额与 Token 使用情况。')}
          </DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <div className='flex justify-center py-8'>
            <Loader2 className='size-6 animate-spin' />
          </div>
        ) : usages.length === 0 ? (
          <div className='text-muted-foreground py-8 text-center'>
            {t('该用户暂无用量限制。')}
          </div>
        ) : (
          <div className='max-h-[60vh] space-y-3 overflow-y-auto'>
            {usages.map((usage) => {
              const quotaPercent =
                usage.quota_usage_percent ?? usage.usage_percent ?? 0
              const tokenPercent = usage.token_usage_percent ?? 0
              const remain =
                usage.quota_remain ?? usage.quota_limit - usage.quota_used
              const tokenRemain =
                usage.token_remain ?? usage.token_limit - usage.token_used
              return (
                <div key={usage.id} className='space-y-1 rounded-lg border p-3'>
                  <div className='flex items-center justify-between'>
                    <div className='flex items-center gap-2'>
                      <span className='font-mono font-medium'>
                        {usage.model_pattern || t('全部模型')}
                      </span>
                      <Badge variant='secondary'>
                        {ruleSourceText[usage.rule_source] ?? usage.rule_source}
                      </Badge>
                      {usage.status === 'expired' && (
                        <Badge variant='outline'>{t('已过期')}</Badge>
                      )}
                    </div>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => resetMutation.mutate(usage.id)}
                      disabled={resetMutation.isPending}
                    >
                      <RotateCcw className='mr-1 size-3' />
                      {t('重置')}
                    </Button>
                  </div>

                  {usage.quota_limit > 0 && (
                    <div className='space-y-1'>
                      <div className='text-muted-foreground flex justify-between text-xs'>
                        <span>{t('金额用量')}</span>
                        <span>{quotaPercent.toFixed(1)}%</span>
                      </div>
                      <div className='bg-muted relative h-2 w-full rounded-full'>
                        <div
                          className={`absolute top-0 left-0 h-2 rounded-full transition-all ${getProgressColor(quotaPercent)}`}
                          style={{ width: `${Math.min(quotaPercent, 100)}%` }}
                        />
                      </div>
                      <div className='text-muted-foreground flex justify-between text-sm'>
                        <span>
                          {t('已用')}: {formatQuota(usage.quota_used)} /{' '}
                          {formatQuota(usage.quota_limit)}
                        </span>
                        <span>
                          {t('剩余')}: {formatQuota(remain)}
                        </span>
                      </div>
                    </div>
                  )}
                  {usage.token_limit > 0 && (
                    <div className='space-y-1'>
                      <div className='text-muted-foreground flex justify-between text-xs'>
                        <span>{t('Token 用量')}</span>
                        <span>{tokenPercent.toFixed(1)}%</span>
                      </div>
                      <div className='bg-muted relative h-2 w-full rounded-full'>
                        <div
                          className={`absolute top-0 left-0 h-2 rounded-full transition-all ${getProgressColor(tokenPercent)}`}
                          style={{ width: `${Math.min(tokenPercent, 100)}%` }}
                        />
                      </div>
                      <div className='text-muted-foreground flex justify-between text-sm'>
                        <span>
                          {t('已用')}:{' '}
                          {formatTokensAsMillions(usage.token_used)} M /{' '}
                          {formatTokensAsMillions(usage.token_limit)} M
                        </span>
                        <span>
                          {t('剩余')}:{' '}
                          {formatTokensAsMillions(Math.max(0, tokenRemain))} M
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
