import { Skeleton } from '@/components/ui/skeleton'

export default function OnboardingLoading() {
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6 p-6">
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-8 w-48" />
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-2/3" />
      </div>
      <div className="flex justify-end gap-2">
        <Skeleton className="h-10 w-24" />
        <Skeleton className="h-10 w-24" />
      </div>
    </div>
  )
}
