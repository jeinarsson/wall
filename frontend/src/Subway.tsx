import { Alerts, Heading, Minutes, RouteBullet } from './Sign.tsx'
import { Alert, Color, minutesUntil, usePoll, useTick } from './transit.ts'

type Board = {
  Routes: (Color & { Route: string, Times: string[] })[]
  Alerts: Alert[]
  Colors: Record<string, Color>
}

// Subway is a countdown sign like the ones at station entrances: the next
// few trains on each route, and any alerts. It shows whatever src (see
// subwaySign in the backend) returns, which lists trains until they leave.
function Subway({ title, src, count, maxAlerts }: { title: string, src: string, count: number, maxAlerts: number }) {
  const board = usePoll<Board>(src, 10_000)
  const now = useTick(10_000)
  if (!board) {
    return null
  }

  return (
    <div className="transit-sign">
      <Heading title={title} />
      <div className="subway-rows" style={{ gridTemplateColumns: `auto repeat(${count}, auto) auto` }}>
        {board.Routes.map(r => {
          const times = r.Times.slice(0, count).map(t => new Date(t))
          return (
            <div className="subway-row" key={r.Route}>
              <RouteBullet route={r.Route} color={r} />
              {times.length === 0
                ? <span className="subway-none">–</span>
                : times.map((t, i) => <Minutes key={i} minutes={minutesUntil(t, now)} />)}
              <span className="transit-unit">{times.length > 0 && 'min'}</span>
            </div>
          )
        })}
      </div>
      <Alerts alerts={board.Alerts.slice(0, maxAlerts)} colors={board.Colors} />
    </div>
  )
}

export default Subway
