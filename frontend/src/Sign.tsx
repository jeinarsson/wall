import { Alert, Color } from './transit.ts'

import './transit.css';

// Shared components of the train signs.

export function Minutes({ minutes }: { minutes: number }) {
  return (
    <span className="transit-minutes">
      {minutes < 1 ? 'now' : minutes}
    </span>
  )
}

export function Heading({ title }: { title: string }) {
  return <div className="transit-heading">{title}</div>
}

export function RouteBullet({ route, color }: { route: string, color: Color }) {
  return <span className="transit-bullet" style={{ background: color.Color, color: color.TextColor }}>{route}</span>
}

// Alerts lists alert texts, drawing routes like "[E]" as bullets in colors.
export function Alerts({ alerts, colors = {} }: { alerts: Alert[], colors?: Record<string, Color> }) {
  if (alerts.length === 0) {
    return null
  }
  return (
    <div className="transit-alerts">
      {alerts.map(a => (
        <div key={a.Text} className={`transit-alert${a.Severe ? ' severe' : ''}`}>
          {a.Text.split(/(\[[^\]]+\])/).map((part, i) => {
            const route = part.match(/^\[(.+)\]$/)?.[1]
            return route && colors[route]
              ? <RouteBullet key={i} route={route} color={colors[route]} />
              : part
          })}
        </div>
      ))}
    </div>
  )
}
