import { format } from 'date-fns/format'

import { Alerts, Heading, Minutes } from './Sign.tsx'
import { Alert, minutesUntil, usePoll, useTick } from './transit.ts'

type Departure = {
  Train: string
  Destination: string
  Color: string
  Scheduled: string
  Expected: string | null // null without a live prediction
  Canceled: boolean
}

type Board = {
  Departures: Departure[]
  Alerts: Alert[]
}

// Delays from which the expected time is shown, and turns red.
const LATE = 60_000
const VERY_LATE = 5 * 60_000

function Row({ d, now }: { d: Departure, now: Date }) {
  const scheduled = new Date(d.Scheduled)
  const expected = d.Expected ? new Date(d.Expected) : null
  const delay = expected ? expected.getTime() - scheduled.getTime() : 0
  const lateness = delay >= VERY_LATE ? 'very-late' : delay >= LATE ? 'late' : ''

  return (
    <div className={`lirr-row${d.Canceled ? ' canceled' : ''}`}>
      <span className="lirr-dot" style={{ background: d.Color }} />
      <span className="lirr-time">
        {format(scheduled, 'HH:mm')}
        {expected && lateness && <span className={`lirr-expected ${lateness}`}>{format(expected, 'HH:mm')}</span>}
      </span>
      <span className="lirr-destination">{d.Destination}</span>
      {d.Canceled
        ? <span className="lirr-canceled">Canceled</span>
        : <>
            <Minutes minutes={minutesUntil(expected ?? scheduled, now)} />
            <span className="transit-unit">min</span>
          </>}
    </div>
  )
}

// Lirr is a timetable for one station: the next scheduled trains, with
// their expected times when late, cancellations, and any alerts. It shows
// whatever src (see lirrSign in the backend) returns.
function Lirr({ title, src, count, maxAlerts }: { title: string, src: string, count: number, maxAlerts: number }) {
  const board = usePoll<Board>(src, 30_000)
  const now = useTick(10_000)
  if (!board) {
    return null
  }

  // Show count trains that will run, and any canceled ones among them. The
  // backend only refreshes every minute; drop trains that have left since.
  const rows: Departure[] = []
  let running = 0
  for (const d of board.Departures) {
    if (running === count) {
      break
    }
    if (new Date(d.Expected ?? d.Scheduled) <= now) {
      continue
    }
    rows.push(d)
    if (!d.Canceled) {
      running++
    }
  }

  return (
    <div className="transit-sign">
      <Heading title={title} />
      <div className="lirr-rows">
        {rows.map(d => <Row key={d.Scheduled + d.Train} d={d} now={now} />)}
      </div>
      <Alerts alerts={board.Alerts.slice(0, maxAlerts)} />
    </div>
  )
}

export default Lirr
