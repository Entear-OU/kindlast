import { Card, CardContent } from '@/components/ui/card'

export function LegalDisclaimer() {
  return (
    <Card>
      <CardContent>
        <p className="text-xs text-muted-foreground italic">
          Kindlast provides AI-generated compliance guidance for educational and planning purposes. It is not a substitute for professional legal advice. For binding compliance determinations, consult a qualified data protection attorney or certified DPO.
        </p>
      </CardContent>
    </Card>
  )
}
