import type { SourceEnrichment } from '@/types'
import { flag } from '@/lib/utils'

const countryNames = new Intl.DisplayNames(['ru'], { type: 'region' })

export function SourceGeo({ enrichment }: { enrichment?: SourceEnrichment | null }) {
  const code = enrichment?.country_code
  const country = code && /^[A-Z]{2}$/.test(code) ? `${flag(code)} ${countryNames.of(code) ?? code}` : 'Страна не определена'
  const asn = enrichment?.asn ? `AS${enrichment.asn}` : 'ASN не определён'

  return (
    <span className="block mt-1 text-xs font-sans text-muted">
      <span>{country} · {asn}</span>
      {(code || enrichment?.asn) && <a href="https://db-ip.com" target="_blank" rel="noopener noreferrer" onClick={event => event.stopPropagation()} className="ml-1.5 underline hover:text-brand" title="IP Geolocation by DB-IP">DB-IP</a>}
    </span>
  )
}
