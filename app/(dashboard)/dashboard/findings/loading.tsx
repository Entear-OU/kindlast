import { Skeleton } from '@/components/ui/skeleton'

export default function FindingsLoading() {
  return (
    <div className="flex flex-col gap-4 p-6">
      <Skeleton className="h-8 w-48" />
      <div className="flex gap-4">
        <Skeleton className="h-10 w-32" />
        <Skeleton className="h-10 w-32" />
      </div>
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-32 rounded-xl" />
      ))}
    </div>
  )
}
