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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  CircleSlash2,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import {
  MultiSelect,
  type Option as MultiSelectOption,
} from '@/components/multi-select'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { formatTimestampToDate } from '@/lib/format'

import {
  getSensitiveDetectionChannels,
  getSensitiveDetectionStats,
  updateSensitiveDetectionChannels,
  updateSystemOption,
} from '../api'
import {
  SettingsForm,
  SettingsFormGrid,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type {
  SensitiveDetectionCounter,
  SensitiveDetectionRecentLog,
  SensitiveDetectionStats,
  SensitiveDetectionStatus,
  UpdateOptionRequest,
} from '../types'

const detectionSchema = z.object({
  SensitiveDetectionModel: z.string().optional(),
  SensitiveDetectionBaseURL: z.string().optional(),
  SensitiveDetectionAPIKey: z.string().optional(),
  SensitiveDetectionPrompt: z.string().optional(),
  SensitiveDetectionGroups: z.array(z.string()),
  SensitiveDetectionChannelIDs: z.array(z.string()),
  SensitiveDetectionTimeoutSeconds: z.number().min(0),
  SensitiveDetectionMaxIdleConns: z.number().min(0),
  SensitiveDetectionMaxIdleConnsPerHost: z.number().min(0),
  SensitiveDetectionRPM: z.number().min(0),
  SensitiveDetectionCacheEnabled: z.boolean(),
  SensitiveDetectionCacheTTLSeconds: z.number().min(0),
  SensitiveDetectionCacheMaxItems: z.number().min(0),
  SensitiveDetectionBreakerThreshold: z.number().min(0),
  SensitiveDetectionBreakerCooldownSeconds: z.number().min(0),
})

type DetectionFormValues = z.infer<typeof detectionSchema>

type SensitiveWordsSectionProps = {
  defaultValues: {
    SensitiveDetectionModel: string
    SensitiveDetectionBaseURL: string
    SensitiveDetectionAPIKey: string
    SensitiveDetectionPrompt: string
    SensitiveDetectionGroups: string
    SensitiveDetectionTimeoutSeconds: number
    SensitiveDetectionMaxIdleConns: number
    SensitiveDetectionMaxIdleConnsPerHost: number
    SensitiveDetectionRPM: number
    SensitiveDetectionCacheEnabled: boolean
    SensitiveDetectionCacheTTLSeconds: number
    SensitiveDetectionCacheMaxItems: number
    SensitiveDetectionBreakerThreshold: number
    SensitiveDetectionBreakerCooldownSeconds: number
    GroupRatio: string
  }
}

const EMPTY_STATS: SensitiveDetectionStats = {
  normal_count: 0,
  illegal_count: 0,
  allowed_count: 0,
  bypassed_count: 0,
  error_open_count: 0,
  top_objects: [],
  channel_stats: [],
  group_stats: [],
  recent_blocked: [],
}

function parseStringArray(value: string | undefined): string[] {
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (!Array.isArray(parsed)) return []
    return parsed
      .map((item) => String(item).trim())
      .filter((item) => item.length > 0)
  } catch {
    return []
  }
}

function parseGroupNames(groupRatio: string, selectedGroups: string[]) {
  const names = new Set(selectedGroups)
  try {
    const parsed = JSON.parse(groupRatio || '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      Object.keys(parsed).forEach((name) => names.add(name))
    }
  } catch {
    // Keep selected groups visible even if ratio JSON is temporarily invalid.
  }
  if (names.size === 0) names.add('default')
  return [...names].sort((a, b) => a.localeCompare(b))
}

function normalizeList(values: string[]) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].sort(
    (a, b) => a.localeCompare(b)
  )
}

function serializeList(values: string[]) {
  return JSON.stringify(normalizeList(values))
}

function formatCount(value: number | undefined) {
  return new Intl.NumberFormat().format(value ?? 0)
}

function truncateMiddle(value: string | undefined, maxLength = 80) {
  if (!value) return '-'
  if (value.length <= maxLength) return value
  return `${value.slice(0, maxLength - 1)}…`
}

function detectionStatusMeta(status?: SensitiveDetectionStatus): {
  label: string
  variant: StatusVariant
} {
  switch (status) {
    case 'allowed':
      return { label: 'Passed', variant: 'success' }
    case 'blocked':
      return { label: 'Blocked', variant: 'danger' }
    case 'bypassed':
      return { label: 'Bypassed', variant: 'neutral' }
    case 'error_open':
      return { label: 'Failed open', variant: 'warning' }
    default:
      return { label: 'Unmarked', variant: 'neutral' }
  }
}

function DetectionStatusBadge({
  status,
}: {
  status?: SensitiveDetectionStatus
}) {
  const { t } = useTranslation()
  const meta = detectionStatusMeta(status)
  return (
    <StatusBadge
      label={t(meta.label)}
      variant={meta.variant}
      size='sm'
      copyable={false}
    />
  )
}

function StatCard({
  title,
  value,
  icon,
  description,
}: {
  title: string
  value: number
  icon: ReactNode
  description: string
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='flex items-start justify-between gap-3 p-4'>
        <div className='min-w-0 space-y-1'>
          <p className='text-muted-foreground text-sm'>{t(title)}</p>
          <p className='text-2xl font-semibold tabular-nums'>
            {formatCount(value)}
          </p>
          <p className='text-muted-foreground text-xs'>{t(description)}</p>
        </div>
        <div className='bg-muted text-muted-foreground rounded-md p-2'>
          {icon}
        </div>
      </CardContent>
    </Card>
  )
}

function CounterTable({
  title,
  description,
  data,
  keyHeader,
  emptyText,
}: {
  title: string
  description: string
  data: SensitiveDetectionCounter[]
  keyHeader: string
  emptyText: string
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(title)}</CardTitle>
        <CardDescription>{t(description)}</CardDescription>
      </CardHeader>
      <CardContent>
        <StaticDataTable
          data={data}
          getRowKey={(row) => row.key}
          emptyContent={t(emptyText)}
          emptyClassName='text-muted-foreground h-20 text-sm'
          columns={[
            {
              id: 'key',
              header: t(keyHeader),
              className: 'min-w-32',
              cell: (row) => (
                <span className='block truncate' title={row.name || row.key}>
                  {truncateMiddle(row.name || row.key, 56)}
                </span>
              ),
            },
            {
              id: 'normal',
              header: t('Normal'),
              className: 'w-24 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (row) => formatCount(row.normal_count),
            },
            {
              id: 'illegal',
              header: t('Illegal'),
              className: 'w-24 text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (row) => formatCount(row.illegal_count),
            },
          ]}
        />
      </CardContent>
    </Card>
  )
}

function RecentViolationsTable({
  rows,
}: {
  rows: SensitiveDetectionRecentLog[]
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Recent violations')}</CardTitle>
        <CardDescription>
          {t('Latest requests blocked by violation detection.')}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <StaticDataTable
          data={rows}
          getRowKey={(row) => row.id}
          emptyContent={t('No violations recorded yet.')}
          emptyClassName='text-muted-foreground h-24 text-sm'
          columns={[
            {
              id: 'time',
              header: t('Time'),
              className: 'min-w-36',
              cell: (row) => formatTimestampToDate(row.created_at),
            },
            {
              id: 'status',
              header: t('Status'),
              className: 'w-28',
              cell: (row) => (
                <DetectionStatusBadge status={row.sensitive_detection_status} />
              ),
            },
            {
              id: 'target',
              header: t('Target'),
              className: 'min-w-44',
              cell: (row) => (
                <div className='min-w-0'>
                  <p className='truncate font-medium'>
                    {row.username || `#${row.id}`}
                  </p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {row.token_name || row.group || '-'}
                  </p>
                </div>
              ),
            },
            {
              id: 'route',
              header: t('Route'),
              className: 'min-w-44',
              cell: (row) => (
                <div className='min-w-0'>
                  <p className='truncate'>{row.model_name || '-'}</p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {row.channel_name || `#${row.channel || '-'}`}
                  </p>
                </div>
              ),
            },
            {
              id: 'trigger',
              header: t('Trigger'),
              className: 'w-32',
              cell: (row) => row.sensitive_detection_trigger || '-',
            },
            {
              id: 'reason',
              header: t('Reason'),
              className: 'min-w-56',
              cell: (row) => (
                <span
                  className='block truncate'
                  title={
                    row.sensitive_detection_reason ||
                    row.sensitive_detection_objects ||
                    ''
                  }
                >
                  {truncateMiddle(
                    row.sensitive_detection_reason ||
                      row.sensitive_detection_objects,
                    72
                  )}
                </span>
              ),
            },
          ]}
        />
      </CardContent>
    </Card>
  )
}

export function SensitiveWordsSection({
  defaultValues,
}: SensitiveWordsSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const selectedGroups = useMemo(
    () => parseStringArray(defaultValues.SensitiveDetectionGroups),
    [defaultValues.SensitiveDetectionGroups]
  )

  const statsQuery = useQuery({
    queryKey: ['sensitive-detection-stats'],
    queryFn: () => getSensitiveDetectionStats(10),
  })
  const channelsQuery = useQuery({
    queryKey: ['sensitive-detection-channels'],
    queryFn: getSensitiveDetectionChannels,
  })

  const channels = useMemo(
    () => channelsQuery.data?.data ?? [],
    [channelsQuery.data]
  )
  const selectedChannelIds = useMemo(
    () =>
      channels
        .filter((channel) => channel.enabled)
        .map((channel) => String(channel.id)),
    [channels]
  )

  const formDefaults = useMemo<DetectionFormValues>(
    () => ({
      SensitiveDetectionModel: defaultValues.SensitiveDetectionModel || '',
      SensitiveDetectionBaseURL: defaultValues.SensitiveDetectionBaseURL || '',
      SensitiveDetectionAPIKey: '',
      SensitiveDetectionPrompt: defaultValues.SensitiveDetectionPrompt || '',
      SensitiveDetectionGroups: selectedGroups,
      SensitiveDetectionChannelIDs: selectedChannelIds,
      SensitiveDetectionTimeoutSeconds:
        defaultValues.SensitiveDetectionTimeoutSeconds,
      SensitiveDetectionMaxIdleConns: defaultValues.SensitiveDetectionMaxIdleConns,
      SensitiveDetectionMaxIdleConnsPerHost:
        defaultValues.SensitiveDetectionMaxIdleConnsPerHost,
      SensitiveDetectionRPM: defaultValues.SensitiveDetectionRPM,
      SensitiveDetectionCacheEnabled:
        defaultValues.SensitiveDetectionCacheEnabled,
      SensitiveDetectionCacheTTLSeconds:
        defaultValues.SensitiveDetectionCacheTTLSeconds,
      SensitiveDetectionCacheMaxItems:
        defaultValues.SensitiveDetectionCacheMaxItems,
      SensitiveDetectionBreakerThreshold:
        defaultValues.SensitiveDetectionBreakerThreshold,
      SensitiveDetectionBreakerCooldownSeconds:
        defaultValues.SensitiveDetectionBreakerCooldownSeconds,
    }),
    [defaultValues, selectedGroups, selectedChannelIds]
  )

  const form = useForm<DetectionFormValues>({
    resolver: zodResolver(detectionSchema),
    defaultValues: formDefaults,
  })

  useEffect(() => {
    form.reset(formDefaults)
  }, [form, formDefaults])

  const groupOptions = useMemo<MultiSelectOption[]>(() => {
    return parseGroupNames(defaultValues.GroupRatio, selectedGroups).map(
      (group) => ({
        label: group,
        value: group,
      })
    )
  }, [defaultValues.GroupRatio, selectedGroups])

  const channelOptions = useMemo<MultiSelectOption[]>(() => {
    return channels.map((channel) => ({
      label: `#${channel.id} ${channel.name}`,
      value: String(channel.id),
    }))
  }, [channels])

  const saveMutation = useMutation({
    mutationFn: async (values: DetectionFormValues) => {
      const updates: UpdateOptionRequest[] = []
      const groupValue = serializeList(values.SensitiveDetectionGroups)
      const currentGroupValue = serializeList(selectedGroups)

      const queueUpdate = (
        key: keyof SensitiveWordsSectionProps['defaultValues'],
        value: string,
        current: string
      ) => {
        if (value !== current) {
          updates.push({ key, value })
        }
      }

      const queueNumberUpdate = (
        key: keyof SensitiveWordsSectionProps['defaultValues'],
        value: number,
        current: number
      ) => {
        if (value !== current) {
          updates.push({ key, value: String(value) })
        }
      }

      const queueBooleanUpdate = (
        key: keyof SensitiveWordsSectionProps['defaultValues'],
        value: boolean,
        current: boolean
      ) => {
        if (value !== current) {
          updates.push({ key, value: String(value) })
        }
      }

      queueUpdate(
        'SensitiveDetectionModel',
        values.SensitiveDetectionModel || '',
        defaultValues.SensitiveDetectionModel || ''
      )
      queueUpdate(
        'SensitiveDetectionBaseURL',
        values.SensitiveDetectionBaseURL || '',
        defaultValues.SensitiveDetectionBaseURL || ''
      )
      queueUpdate(
        'SensitiveDetectionPrompt',
        values.SensitiveDetectionPrompt || '',
        defaultValues.SensitiveDetectionPrompt || ''
      )
      queueNumberUpdate(
        'SensitiveDetectionTimeoutSeconds',
        values.SensitiveDetectionTimeoutSeconds,
        defaultValues.SensitiveDetectionTimeoutSeconds
      )
      queueNumberUpdate(
        'SensitiveDetectionMaxIdleConns',
        values.SensitiveDetectionMaxIdleConns,
        defaultValues.SensitiveDetectionMaxIdleConns
      )
      queueNumberUpdate(
        'SensitiveDetectionMaxIdleConnsPerHost',
        values.SensitiveDetectionMaxIdleConnsPerHost,
        defaultValues.SensitiveDetectionMaxIdleConnsPerHost
      )
      queueNumberUpdate(
        'SensitiveDetectionRPM',
        values.SensitiveDetectionRPM,
        defaultValues.SensitiveDetectionRPM
      )
      queueBooleanUpdate(
        'SensitiveDetectionCacheEnabled',
        values.SensitiveDetectionCacheEnabled,
        defaultValues.SensitiveDetectionCacheEnabled
      )
      queueNumberUpdate(
        'SensitiveDetectionCacheTTLSeconds',
        values.SensitiveDetectionCacheTTLSeconds,
        defaultValues.SensitiveDetectionCacheTTLSeconds
      )
      queueNumberUpdate(
        'SensitiveDetectionCacheMaxItems',
        values.SensitiveDetectionCacheMaxItems,
        defaultValues.SensitiveDetectionCacheMaxItems
      )
      queueNumberUpdate(
        'SensitiveDetectionBreakerThreshold',
        values.SensitiveDetectionBreakerThreshold,
        defaultValues.SensitiveDetectionBreakerThreshold
      )
      queueNumberUpdate(
        'SensitiveDetectionBreakerCooldownSeconds',
        values.SensitiveDetectionBreakerCooldownSeconds,
        defaultValues.SensitiveDetectionBreakerCooldownSeconds
      )
      if (values.SensitiveDetectionAPIKey?.trim()) {
        updates.push({
          key: 'SensitiveDetectionAPIKey',
          value: values.SensitiveDetectionAPIKey.trim(),
        })
      }
      if (groupValue !== currentGroupValue) {
        updates.push({ key: 'SensitiveDetectionGroups', value: groupValue })
      }

      for (const update of updates) {
        const response = await updateSystemOption(update)
        if (!response.success) {
          throw new Error(response.message || t('Failed to update setting'))
        }
      }

      const nextChannelIds = normalizeList(values.SensitiveDetectionChannelIDs)
      if (serializeList(nextChannelIds) !== serializeList(selectedChannelIds)) {
        const response = await updateSensitiveDetectionChannels(
          nextChannelIds.map((id) => Number(id)).filter(Number.isFinite)
        )
        if (!response.success) {
          throw new Error(response.message || t('Failed to update channels'))
        }
      }
    },
    onSuccess: () => {
      toast.success(t('Violation detection settings saved'))
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      queryClient.invalidateQueries({
        queryKey: ['sensitive-detection-channels'],
      })
      queryClient.invalidateQueries({ queryKey: ['sensitive-detection-stats'] })
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save violation detection'))
    },
  })

  const stats = statsQuery.data?.data ?? EMPTY_STATS

  const [advancedOpen, setAdvancedOpen] = useState(false)

  const scrollToConfig = () => {
    document
      .getElementById('sensitive-detection-config')
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <SettingsSection title={t('Violation Detection')}>
      <div className='space-y-6'>
        <div className='flex justify-end'>
          <Button variant='outline' size='sm' onClick={scrollToConfig}>
            <SlidersHorizontal className='mr-2 h-4 w-4' />
            {t('Detection configuration')}
          </Button>
        </div>

        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
          <StatCard
            title='Normal requests'
            value={stats.normal_count}
            description='Requests forwarded upstream'
            icon={<ShieldCheck className='h-5 w-5' />}
          />
          <StatCard
            title='Illegal requests'
            value={stats.illegal_count}
            description='Requests blocked before upstream'
            icon={<ShieldAlert className='h-5 w-5' />}
          />
          <StatCard
            title='Detection passed'
            value={stats.allowed_count}
            description='Checked by the detector and passed'
            icon={<CheckCircle2 className='h-5 w-5' />}
          />
          <StatCard
            title='Failed open'
            value={stats.error_open_count}
            description='Detector failures that were forwarded'
            icon={<AlertTriangle className='h-5 w-5' />}
          />
        </div>

        <RecentViolationsTable rows={stats.recent_blocked ?? []} />

        <div className='grid gap-4 xl:grid-cols-3'>
          <CounterTable
            title='Top flagged objects'
            description='Most frequent detector labels among blocked requests.'
            data={stats.top_objects ?? []}
            keyHeader='Object'
            emptyText='No flagged objects yet.'
          />
          <CounterTable
            title='Channel detection stats'
            description='Forwarded and blocked requests grouped by channel.'
            data={stats.channel_stats ?? []}
            keyHeader='Channel'
            emptyText='No channel statistics yet.'
          />
          <CounterTable
            title='Group detection stats'
            description='Forwarded and blocked requests grouped by user group.'
            data={stats.group_stats ?? []}
            keyHeader='Group'
            emptyText='No group statistics yet.'
          />
        </div>

        <Card id='sensitive-detection-config' className='scroll-mt-6'>
          <CardHeader>
            <CardTitle>{t('Detection configuration')}</CardTitle>
            <CardDescription>
              {t(
                'Configure the OpenAI-compatible detector and enabled scopes.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <SettingsForm
                onSubmit={form.handleSubmit((values) =>
                  saveMutation.mutate(values)
                )}
              >
                <SettingsPageFormActions
                  onSave={form.handleSubmit((values) =>
                    saveMutation.mutate(values)
                  )}
                  isSaving={saveMutation.isPending}
                  saveLabel='Save violation detection'
                />

                <SettingsFormGrid>
                  <FormField
                    control={form.control}
                    name='SensitiveDetectionChannelIDs'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Enabled channels')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            options={channelOptions}
                            selected={field.value}
                            onChange={field.onChange}
                            placeholder={t('Select channels...')}
                            emptyText={t('No channels found.')}
                            maxVisibleChips={6}
                            disabled={channelsQuery.isLoading}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Requests using any selected channel will be checked.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionGroups'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Enabled groups')}</FormLabel>
                        <FormControl>
                          <MultiSelect
                            options={groupOptions}
                            selected={field.value}
                            onChange={field.onChange}
                            placeholder={t('Select groups...')}
                            emptyText={t('No groups found.')}
                            maxVisibleChips={6}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Requests from any selected group will be checked.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionModel'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector model ID')}</FormLabel>
                        <FormControl>
                          <Input placeholder='gpt-4.1-mini' {...field} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionBaseURL'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector base URL')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder='https://api.openai.com/v1'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionAPIKey'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Detector API key')}</FormLabel>
                        <FormControl>
                          <Input
                            type='password'
                            placeholder={t('Leave blank to keep current key')}
                            autoComplete='new-password'
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='SensitiveDetectionPrompt'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Violation detection prompt')}</FormLabel>
                        <FormControl>
                          <Textarea
                            rows={10}
                            placeholder={t(
                              'Return JSON only. Use status 200 for allowed requests.'
                            )}
                            {...field}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Only JSON content is accepted from the detector response.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <Collapsible
                    open={advancedOpen}
                    onOpenChange={setAdvancedOpen}
                    className='border-border bg-muted/20 rounded-lg border'
                  >
                    <CollapsibleTrigger className='hover:bg-muted flex w-full items-center justify-between rounded-lg px-4 py-3 text-sm font-medium'>
                      <span>{t('Advanced / Performance')}</span>
                      <ChevronDown
                        className={cn(
                          'h-4 w-4 transition-transform',
                          advancedOpen && 'rotate-180'
                        )}
                      />
                    </CollapsibleTrigger>
                    <CollapsibleContent className='space-y-4 p-4'>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Tune throughput and resilience. Enter 0 to disable a limit.'
                        )}
                      </p>
                      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionTimeoutSeconds'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Detector timeout (seconds)')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'Max wait for one detector call. 0 defaults to 5s.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionRPM'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Detector RPM limit')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'Max detector calls per minute. 0 = unlimited.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionBreakerThreshold'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Breaker failure threshold')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'Consecutive failures before tripping. 0 = disabled.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionBreakerCooldownSeconds'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Breaker cooldown (seconds)')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t(
                                  'How long the breaker stays open. Failures are forwarded.'
                                )}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionCacheEnabled'
                          render={({ field }) => (
                            <FormItem className='flex flex-row items-center justify-between rounded-lg border p-3'>
                              <div className='space-y-0.5'>
                                <FormLabel>{t('Cache results')}</FormLabel>
                                <FormDescription>
                                  {t(
                                    'Reuse verdicts for identical prompts. Blocked results are cached too.'
                                  )}
                                </FormDescription>
                              </div>
                              <FormControl>
                                <Switch
                                  checked={field.value === true}
                                  onCheckedChange={(checked) =>
                                    field.onChange(checked === true)
                                  }
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionCacheTTLSeconds'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Cache TTL (seconds)')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t('How long a cached verdict stays valid.')}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionCacheMaxItems'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Cache capacity (memory)')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t('In-memory LRU size when Redis is unavailable.')}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionMaxIdleConns'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Detector pool: max idle')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t('Global idle connections for the detector client.')}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name='SensitiveDetectionMaxIdleConnsPerHost'
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel>
                                {t('Detector pool: max per host')}
                              </FormLabel>
                              <FormControl>
                                <Input
                                  type='number'
                                  min={0}
                                  step={1}
                                  value={field.value ?? 0}
                                  onChange={(e) =>
                                    field.onChange(
                                      parseInt(e.target.value, 10) || 0
                                    )
                                  }
                                />
                              </FormControl>
                              <FormDescription>
                                {t('Idle connections per detector host.')}
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      </div>
                    </CollapsibleContent>
                  </Collapsible>
                </SettingsFormGrid>
              </SettingsForm>
            </Form>
          </CardContent>
        </Card>

        {stats.bypassed_count > 0 && (
          <div className='text-muted-foreground flex items-center gap-2 text-sm'>
            <CircleSlash2 className='h-4 w-4' />
            <span>
              {t('{{count}} requests bypassed detection by scope or format.', {
                count: formatCount(stats.bypassed_count),
              })}
            </span>
          </div>
        )}

        {statsQuery.isFetching && (
          <div className='text-muted-foreground flex items-center gap-2 text-sm'>
            <Activity className='h-4 w-4 animate-pulse' />
            <span>{t('Refreshing detection data...')}</span>
          </div>
        )}
      </div>
    </SettingsSection>
  )
}
